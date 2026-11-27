package discinfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gameforge/internal/chd"
	"gameforge/internal/ciso"
	"gameforge/internal/rvz"
	"gameforge/internal/wbfs"
)

// Info is what a game disc says about itself.
type Info struct {
	Platform string
	Serial   string   // product code, e.g. SLUS-00594, ULUS-10041, T-1234N
	Title    string   // as written on the disc; PS1 and PS2 discs have none
	Version  string   // e.g. 1.01
	Regions  []string // e.g. USA, Europe, Japan
	Date     string   // release date on the disc, YYYY-MM-DD
	Disc     int      // disc number of a set, 0 when unknown
	Discs    int      // discs in the set, 0 when unknown (Disc may be known)
	Firmware string   // PSP: minimum system software version
	Icon     []byte   // PSP: ICON0.PNG
}

// InfoFromISO reads the game information of a 2048-byte sector image.
func InfoFromISO(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	platform, _, err := FromISO(path)
	if err != nil {
		return Info{}, err
	}
	if platform == GameCube || platform == Wii {
		info := Info{Platform: platform}
		head := make([]byte, 0x100)
		if _, err := f.ReadAt(head, 0); err == nil {
			nintendoInfo(&info, head)
		}
		return info, nil
	}
	return infoFrom(func(lba int) ([]byte, error) {
		b := make([]byte, 2048)
		_, err := f.ReadAt(b, int64(lba)*2048)
		return b, err
	}), nil
}

// InfoFromCSO reads the game information of a CSO or ZSO image.
func InfoFromCSO(path string) (Info, error) {
	r, err := ciso.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer r.Close()
	return infoFrom(func(lba int) ([]byte, error) { return r.Sector(int64(lba)) }), nil
}

// InfoFromRVZ reads the game information of an RVZ (GameCube or Wii), from
// the start of the disc that its header keeps uncompressed.
func InfoFromRVZ(path string) (Info, error) {
	h, err := rvz.ReadHeader(path)
	if err != nil {
		return Info{}, err
	}
	info := Info{Platform: GameCube}
	if h.Wii {
		info.Platform = Wii
	}
	head := make([]byte, 0x100) // the title runs past what the header keeps
	copy(head, h.Disc[:])
	nintendoInfo(&info, head)
	return info, nil
}

// InfoFromWBFS reads the game information of a Wii disc in a WBFS image.
func InfoFromWBFS(path string) (Info, error) {
	r, err := wbfs.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer r.Close()
	info := Info{Platform: Wii}
	nintendoInfo(&info, r.Header())
	return info, nil
}

// InfoFromCue reads the game information of the disc of a CUE sheet or a
// GDI file.
func InfoFromCue(path string) (Info, error) {
	tracks, err := chd.ParseImage(path)
	if err != nil {
		return Info{}, err
	}
	var files []*chd.Image
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	reader := func(t *chd.Track) sectorReader {
		skip, ok := chd.UserDataOffset(t.Type)
		if !ok {
			return nil
		}
		f, err := chd.OpenImage(t.File)
		if err != nil {
			return nil
		}
		files = append(files, f)
		start := t.DataStart()
		return func(lba int) ([]byte, error) {
			b := make([]byte, 2048)
			_, err := f.ReadAt(b, start+int64(lba)*int64(t.SectorStride())+int64(skip))
			return b, err
		}
	}
	if chd.IsGDROM(tracks) {
		if len(tracks) < 3 {
			return Info{Platform: Dreamcast}, nil
		}
		return dreamcastInfo(reader(tracks[2])), nil
	}
	readers := make([]sectorReader, len(tracks))
	for i, t := range tracks {
		readers[i] = reader(t)
	}
	return infoFromTracks(readers), nil
}

// InfoFromCHD reads the game information of a CHD made from a disc image.
func InfoFromCHD(path string) (Info, error) {
	d, err := chd.OpenDisc(path)
	if err != nil {
		return Info{}, err
	}
	defer d.Close()
	if d.DVD {
		return infoFrom(func(lba int) ([]byte, error) { return d.ReadSector(0, lba) }), nil
	}
	track := func(i int) sectorReader {
		if i >= len(d.Tracks) {
			return nil
		}
		if _, ok := chd.UserDataOffset(d.Tracks[i].Type); !ok {
			return nil
		}
		return func(lba int) ([]byte, error) { return d.ReadSector(i, lba) }
	}
	if d.GDROM {
		return dreamcastInfo(track(2)), nil
	}
	readers := make([]sectorReader, len(d.Tracks))
	for i := range d.Tracks {
		readers[i] = track(i)
	}
	return infoFromTracks(readers), nil
}

// infoFromTracks reads the game information of a CD from its data tracks
// (nil for audio tracks). The first track that tells the platform wins:
// a self-booting Dreamcast CD-R often has a data track with no game in its
// first session, and IP.BIN at the start of the second session's track,
// maybe after a stored pregap.
func infoFromTracks(readers []sectorReader) Info {
	var first *Info
	for _, read := range readers {
		if read == nil {
			continue
		}
		info := infoFrom(read)
		if info.Platform == "" {
			if b, err := read(1); err == nil && bytes.Contains(b[:256], []byte("PC Engine CD-ROM SYSTEM")) {
				info.Platform = PCEngine
			} else if dc, ok := findIPBin(read); ok {
				info = dc
			}
		}
		if info.Platform != "" {
			return info
		}
		if first == nil {
			first = &info
		}
	}
	if first != nil {
		return *first
	}
	return Info{Platform: AudioCD}
}

// infoFrom detects the platform of a data track and reads what the disc
// says about the game.
func infoFrom(read sectorReader) Info {
	info := Info{Platform: detect(read)}
	s0, _ := read(0)
	switch info.Platform {
	case Saturn:
		saturnInfo(&info, s0)
	case SegaCD:
		segaCDInfo(&info, s0)
	case Dreamcast: // a Dreamcast game on a CD (MIL-CD)
		ipBinInfo(&info, s0)
	case PS1, PS2:
		playStationInfo(&info, read)
	case PSP:
		pspInfo(&info, read)
	}
	return info
}

// dreamcastInfo reads IP.BIN at the start of the high-density area of a
// GD-ROM. Where the track's data starts depends on how its pregap was
// stored, so the first sectors are searched.
func dreamcastInfo(read sectorReader) Info {
	if read == nil {
		return Info{Platform: Dreamcast}
	}
	info, _ := findIPBin(read)
	info.Platform = Dreamcast
	return info
}

// findIPBin looks for IP.BIN in the first sectors of a track.
func findIPBin(read sectorReader) (Info, bool) {
	for lba := 0; lba < 300; lba++ {
		b, err := read(lba)
		if err != nil {
			break
		}
		if bytes.HasPrefix(b, []byte("SEGA SEGAKATANA")) {
			info := Info{Platform: Dreamcast}
			ipBinInfo(&info, b)
			return info, true
		}
	}
	return Info{}, false
}

// text returns a fixed-size text field without padding, with runs of
// spaces collapsed.
func text(b []byte) string {
	b = bytes.TrimRight(b, "\x00 ")
	return strings.Join(strings.Fields(strings.ToValidUTF8(string(b), "")), " ")
}

func date(s string) string {
	if len(s) >= 8 {
		if _, err := strconv.Atoi(s[:8]); err == nil {
			return s[:4] + "-" + s[4:6] + "-" + s[6:8]
		}
	}
	return ""
}

var discOf = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)

func setDisc(info *Info, s string) {
	if m := discOf.FindStringSubmatch(s); m != nil {
		info.Disc, _ = strconv.Atoi(m[1])
		info.Discs, _ = strconv.Atoi(m[2])
		if info.Discs < 2 || info.Disc < 1 || info.Disc > info.Discs {
			info.Disc, info.Discs = 0, 0
		}
	}
}

// segaRegions reads Sega's area symbols.
func segaRegions(s string) []string {
	names := map[rune]string{'J': "Japan", 'T': "Asia", 'U': "USA", 'B': "Brazil", 'K': "Korea", 'A': "Asia", 'E': "Europe", 'L': "Latin America"}
	var out []string
	seen := map[string]bool{}
	for _, c := range s {
		if n, ok := names[c]; ok && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func version(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(strings.TrimPrefix(s, "V"), "v")
}

// saturnInfo reads the Saturn system ID in sector 0.
func saturnInfo(info *Info, s []byte) {
	if len(s) < 0xD0 {
		return
	}
	info.Serial = text(s[0x20:0x2A])
	info.Version = version(text(s[0x2A:0x30]))
	info.Date = date(text(s[0x30:0x38]))
	setDisc(info, text(s[0x38:0x40]))
	info.Regions = segaRegions(text(s[0x40:0x50]))
	info.Title = text(s[0x60:0xD0])
}

// ipBinInfo reads a Dreamcast IP.BIN.
func ipBinInfo(info *Info, s []byte) {
	if len(s) < 0x100 {
		return
	}
	setDisc(info, text(s[0x20:0x30]))
	info.Regions = segaRegions(text(s[0x30:0x38]))
	info.Serial = text(s[0x40:0x4A])
	info.Version = version(text(s[0x4A:0x50]))
	info.Date = date(text(s[0x50:0x60]))
	info.Title = text(s[0x80:0x100])
}

// segaCDInfo reads the Mega Drive style header of a Sega CD disc.
func segaCDInfo(info *Info, s []byte) {
	if len(s) < 0x200 {
		return
	}
	info.Title = text(s[0x150:0x180]) // overseas title
	if info.Title == "" {
		info.Title = text(s[0x120:0x150])
	}
	serial := text(s[0x180:0x18E])
	serial = strings.TrimSpace(strings.TrimPrefix(serial, "GM"))
	if i := strings.LastIndex(serial, "-"); i > 0 && i >= len(serial)-3 {
		info.Version = strings.TrimSpace(serial[i+1:])
		serial = strings.TrimSpace(serial[:i])
	}
	info.Serial = serial
	info.Regions = segaRegions(text(s[0x1F0:0x1F3]))
}

// nintendoInfo reads the header of a GameCube or Wii disc: the game ID
// (whose fourth letter is the region), disc number, version and title.
func nintendoInfo(info *Info, h []byte) {
	id := text(h[:6])
	if len(id) != 6 {
		return
	}
	info.Serial = id
	switch id[3] {
	case 'E':
		info.Regions = []string{"USA"}
	case 'J':
		info.Regions = []string{"Japan"}
	case 'K':
		info.Regions = []string{"Korea"}
	case 'W':
		info.Regions = []string{"Taiwan"}
	case 'U':
		info.Regions = []string{"Australia"}
	case 'P', 'D', 'F', 'S', 'I', 'H', 'X', 'Y', 'Z':
		info.Regions = []string{"Europe"}
	}
	if n := int(h[6]); n > 0 && n < 9 {
		info.Disc = n + 1
	}
	info.Version = fmt.Sprintf("1.%02d", h[7])
	// The title field is 64 bytes; after it, backup tools may keep an MD5
	// of the image ("MD5#…").
	title := h[0x20:min(len(h), 0x60)]
	if i := bytes.IndexByte(title, 0); i >= 0 {
		title = title[:i]
	}
	info.Title = text(title)
}

var psSerial = regexp.MustCompile(`^([A-Z]{4})[_-](\d{3})\.?(\d{2})`)

// playStationInfo reads the boot file name (the product code) and the
// version from SYSTEM.CNF.
func playStationInfo(info *Info, read sectorReader) {
	root, err := rootDir(read)
	if err != nil {
		return
	}
	cnf, err := readFile(read, root["SYSTEM.CNF"])
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(cnf), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.ToUpper(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "BOOT", "BOOT2":
			name := strings.ToUpper(v)
			if i := strings.LastIndexAny(name, `\:/`); i >= 0 {
				name = name[i+1:]
			}
			if m := psSerial.FindStringSubmatch(name); m != nil {
				info.Serial = m[1] + "-" + m[2] + m[3]
				info.Regions = serialRegion(m[1])
			}
		case "VER":
			info.Version = v
		}
	}
}

// serialRegion tells the region of a Sony product code prefix (SLUS,
// SCES, ULJM…) from its third letter.
func serialRegion(prefix string) []string {
	if len(prefix) < 3 {
		return nil
	}
	switch prefix[2] {
	case 'U':
		return []string{"USA"}
	case 'E':
		return []string{"Europe"}
	case 'P', 'J':
		return []string{"Japan"}
	case 'K':
		return []string{"Korea"}
	case 'A', 'H':
		return []string{"Asia"}
	}
	return nil
}

// pspInfo reads PSP_GAME/PARAM.SFO and the icon.
func pspInfo(info *Info, read sectorReader) {
	root, err := rootDir(read)
	if err != nil {
		return
	}
	game, ok := root["PSP_GAME"]
	if !ok {
		return
	}
	dir, err := listDir(read, game)
	if err != nil {
		return
	}
	if sfo, err := readFileMax(read, dir["PARAM.SFO"], 64<<10); err == nil {
		vals := parseSFO(sfo)
		info.Title = strings.Join(strings.Fields(vals["TITLE"]), " ")
		if id := vals["DISC_ID"]; len(id) == 9 {
			info.Serial = id[:4] + "-" + id[4:]
			info.Regions = serialRegion(id[:4])
		}
		info.Version = vals["DISC_VERSION"]
		info.Firmware = strings.TrimLeft(vals["PSP_SYSTEM_VER"], "0")
		if strings.HasPrefix(info.Firmware, ".") {
			info.Firmware = "0" + info.Firmware
		}
		n, _ := strconv.Atoi(vals["DISC_NUMBER"])
		total, _ := strconv.Atoi(vals["DISC_TOTAL"])
		if total > 1 && n >= 1 && n <= total {
			info.Disc, info.Discs = n, total
		}
	}
	if e, ok := dir["ICON0.PNG"]; ok && e.size > 0 && e.size <= 1<<20 {
		if icon, err := readFileMax(read, e, 1<<20); err == nil && bytes.HasPrefix(icon, []byte("\x89PNG")) {
			info.Icon = icon
		}
	}
}

// parseSFO reads the keys of a PARAM.SFO; numbers become decimal text.
func parseSFO(b []byte) map[string]string {
	out := map[string]string{}
	if len(b) < 20 || string(b[:4]) != "\x00PSF" {
		return out
	}
	keys := int(binary.LittleEndian.Uint32(b[8:]))
	data := int(binary.LittleEndian.Uint32(b[12:]))
	n := int(binary.LittleEndian.Uint32(b[16:]))
	for i := 0; i < n && 20+16*i+16 <= len(b); i++ {
		e := b[20+16*i:]
		ko := keys + int(binary.LittleEndian.Uint16(e[0:]))
		format := binary.LittleEndian.Uint16(e[2:])
		size := int(binary.LittleEndian.Uint32(e[4:]))
		do := data + int(binary.LittleEndian.Uint32(e[12:]))
		if ko >= len(b) || do < 0 || do+size > len(b) || size < 0 {
			continue
		}
		key := string(b[ko:])
		if j := strings.IndexByte(key, 0); j >= 0 {
			key = key[:j]
		}
		v := b[do : do+size]
		switch format {
		case 0x0404:
			if size >= 4 {
				out[key] = strconv.Itoa(int(binary.LittleEndian.Uint32(v)))
			}
		default:
			out[key] = string(bytes.TrimRight(v, "\x00"))
		}
	}
	return out
}

// listDir lists an ISO 9660 directory by upper-case name without version.
func listDir(read sectorReader, dir extent) (map[string]extent, error) {
	if dir.size <= 0 || dir.size > 1<<20 {
		return nil, errors.New("invalid directory")
	}
	out := map[string]extent{}
	for s := 0; s*2048 < dir.size; s++ {
		b, err := read(dir.lba + s)
		if err != nil {
			return out, nil
		}
		for p := 0; p < len(b); {
			n := int(b[p])
			if n == 0 || p+n > len(b) || n < 34 {
				break
			}
			nameLen := int(b[p+32])
			if 33+nameLen <= n {
				name := strings.ToUpper(string(b[p+33 : p+33+nameLen]))
				if i := strings.IndexByte(name, ';'); i >= 0 {
					name = name[:i]
				}
				out[name] = extent{int(binary.LittleEndian.Uint32(b[p+2:])), int(binary.LittleEndian.Uint32(b[p+10:]))}
			}
			p += n
		}
	}
	return out, nil
}

// readFileMax reads a file of up to limit bytes.
func readFileMax(read sectorReader, e extent, limit int) ([]byte, error) {
	if e.size <= 0 || e.size > limit {
		return nil, errors.New("file missing or too large")
	}
	var out []byte
	for s := 0; len(out) < e.size; s++ {
		b, err := read(e.lba + s)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out[:e.size], nil
}
