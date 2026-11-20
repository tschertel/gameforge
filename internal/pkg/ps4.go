package pkg

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A PS4 PKG (big-endian) starts with a header that holds the content ID
// and points to a table of entries: the package's own files (PARAM.SFO,
// icons, PlayGo data, licences…), some of them encrypted. The game itself
// is a PFS image after them, encrypted with keys that only the console
// has, so only the unencrypted entries are extracted.

const (
	ps4EntryNames = 0x0200
	ps4ParamSFO   = 0x1000
	ps4Icon0      = 0x1200
	ps4Encrypted  = 0x80000000 // in flags1
)

type ps4Entry struct {
	id, nameOff, flags1, flags2 uint32
	offset, size                uint32
	name                        string // under sce_sys, "" if unknown
}

type ps4Pkg struct {
	entries []ps4Entry // the unencrypted entries with a name
	total   int64
}

// ps4Names names the entries by ID, as the console installs them in
// sce_sys. Entries without a name here (or in the package's own name
// table) are the package's internal data and are not extracted.
func ps4Name(id uint32) string {
	fixed := map[uint32]string{
		0x0400: "license.dat", 0x0401: "license.info", 0x0402: "nptitle.dat",
		0x0403: "npbind.dat", 0x0404: "selfinfo.dat", 0x0406: "imageinfo.dat",
		0x0407: "target-deltainfo.dat", 0x0408: "origin-deltainfo.dat",
		0x0409: "psreserved.dat",
		0x1000: "param.sfo", 0x1001: "playgo-chunk.dat", 0x1002: "playgo-chunk.sha",
		0x1003: "playgo-manifest.xml", 0x1004: "pronunciation.xml",
		0x1005: "pronunciation.sig", 0x1006: "pic1.png", 0x1007: "pubtoolinfo.dat",
		0x1008: "app/playgo-chunk.dat", 0x1009: "app/playgo-chunk.sha",
		0x100A: "app/playgo-manifest.xml", 0x100B: "shareparam.json",
		0x100C: "shareoverlayimage.png", 0x100D: "save_data.png",
		0x100E: "shareprivacyguardimage.png",
		0x1200: "icon0.png", 0x1220: "pic0.png", 0x1240: "snd0.at9",
		0x1260: "changeinfo/changeinfo.xml", 0x1280: "icon0.dds",
		0x12A0: "pic0.dds", 0x12C0: "pic1.dds",
	}
	if n, ok := fixed[id]; ok {
		return n
	}
	// Localized variants: _00 to _30.
	switch {
	case id > 0x1200 && id <= 0x121F:
		return fmt.Sprintf("icon0_%02d.png", id-0x1201)
	case id > 0x1240 && id <= 0x125F:
		return fmt.Sprintf("pic1_%02d.png", id-0x1241)
	case id > 0x1260 && id <= 0x127F:
		return fmt.Sprintf("changeinfo/changeinfo_%02d.xml", id-0x1261)
	case id > 0x1280 && id <= 0x129F:
		return fmt.Sprintf("icon0_%02d.dds", id-0x1281)
	case id > 0x12C0 && id <= 0x12DF:
		return fmt.Sprintf("pic1_%02d.dds", id-0x12C1)
	case id >= 0x1400 && id <= 0x147F:
		return fmt.Sprintf("trophy/trophy%02d.trp", id-0x1400)
	case id >= 0x1600 && id <= 0x1609:
		return fmt.Sprintf("keymap_rp/%03d.png", id-0x1600+1)
	case id >= 0x1610 && id <= 0x17F9:
		n := id - 0x1610
		return fmt.Sprintf("keymap_rp/%02d/%03d.png", n/10, n%10+1)
	}
	return ""
}

func (p *Package) openPS4(f io.ReaderAt) error {
	var h [0x80]byte
	if _, err := f.ReadAt(h[:], 0); err != nil {
		return errors.New("PS4 PKG: header too short")
	}
	be := binary.BigEndian
	count := be.Uint32(h[0x10:])
	table := be.Uint32(h[0x18:])
	if count == 0 || count > 1<<16 || int64(table)+int64(count)*32 > p.Size {
		return errors.New("PS4 PKG: bad entry table")
	}
	p.Platform = "PS4"
	p.ContentID = strings.TrimRight(string(h[0x40:0x64]), "\x00")
	p.TitleID = titleIDOf(p.ContentID)

	raw := make([]byte, count*32)
	if _, err := f.ReadAt(raw, int64(table)); err != nil {
		return fmt.Errorf("PS4 PKG: %w", err)
	}
	all := make([]ps4Entry, count)
	var names []byte
	for i := range all {
		e := raw[i*32:]
		all[i] = ps4Entry{
			id: be.Uint32(e[0:]), nameOff: be.Uint32(e[4:]),
			flags1: be.Uint32(e[8:]), flags2: be.Uint32(e[12:]),
			offset: be.Uint32(e[16:]), size: be.Uint32(e[20:]),
		}
		if int64(all[i].offset)+int64(all[i].size) > p.Size {
			return fmt.Errorf("PS4 PKG: entry %#x past the end of the file", all[i].id)
		}
	}
	read := func(e ps4Entry) ([]byte, error) {
		if e.size > 64<<20 {
			return nil, fmt.Errorf("entry %#x too large", e.id)
		}
		b := make([]byte, e.size)
		_, err := f.ReadAt(b, int64(e.offset))
		return b, err
	}
	for _, e := range all {
		if e.id == ps4EntryNames && e.flags1&ps4Encrypted == 0 {
			names, _ = read(e)
		}
	}
	pk := &ps4Pkg{}
	for _, e := range all {
		if e.flags1&ps4Encrypted != 0 || e.id < 0x0400 {
			continue
		}
		e.name = ps4Name(e.id)
		if e.name == "" && e.nameOff > 0 && int(e.nameOff) < len(names) {
			n := names[e.nameOff:]
			if j := bytes.IndexByte(n, 0); j >= 0 {
				n = n[:j]
			}
			e.name = string(n)
		}
		if e.name == "" {
			continue
		}
		switch e.id {
		case ps4ParamSFO:
			if b, err := read(e); err == nil {
				p.SFO, _ = ParseSFO(b)
			}
		case ps4Icon0:
			if b, err := read(e); err == nil && bytes.HasPrefix(b, []byte("\x89PNG")) {
				p.Icon = b
			}
		}
		pk.entries = append(pk.entries, e)
		pk.total += int64(e.size)
	}
	p.ps4 = pk
	p.Files = len(pk.entries)
	p.Kind = ps4Kind(p.SFO["CATEGORY"], be.Uint32(h[0x74:]))
	return nil
}

// ps4Kind tells what a package holds from the CATEGORY of its PARAM.SFO
// or, without one, the content type of its header.
func ps4Kind(category string, contentType uint32) string {
	switch {
	case category == "gp":
		return KindUpdate
	case category == "ac":
		return KindDLC
	case strings.HasPrefix(category, "gd"):
		return KindGame
	}
	switch contentType {
	case 0x1A:
		return KindGame
	case 0x1B, 0x1C:
		return KindDLC
	case 0x1E:
		return KindUpdate
	}
	return KindOther
}

// extract writes the named, unencrypted entries into dir/sce_sys.
func (pk *ps4Pkg) extract(ctx context.Context, f *os.File, dir string, progress func(done, total int64)) (int64, error) {
	sys := filepath.Join(dir, "sce_sys")
	var done int64
	for _, e := range pk.entries {
		out, err := safePath(sys, e.name)
		if err != nil {
			return done, err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return done, err
		}
		w, err := os.Create(out)
		if err != nil {
			return done, err
		}
		r := io.NewSectionReader(f, int64(e.offset), int64(e.size))
		err = copyN(ctx, w, r, int64(e.size), func(k int64) {
			done += k
			progress(done, pk.total)
		})
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return done, err
		}
	}
	return done, nil
}
