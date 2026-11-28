package retroach

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
)

// Console numbers of RetroAchievements for disc consoles.
const (
	SegaCD    = 9
	PS1       = 12
	PS2       = 21
	Saturn    = 39
	Dreamcast = 40
	PSP       = 41
	GameCube  = 16
)

// DiscConsoles maps the platform names of disc images (as discinfo names
// them) to RetroAchievements console numbers.
var DiscConsoles = map[string]int{
	"PS1": PS1, "PS2": PS2, "PSP": PSP,
	"Dreamcast": Dreamcast, "Saturn": Saturn, "Sega CD": SegaCD,
	"GameCube": GameCube,
}

// Disc gives the disc hashes access to a disc's data.
type Disc struct {
	// Read returns the 2048 bytes of user data of a sector, by its
	// address on the disc (LBA), as the disc's file system counts them.
	Read func(lba int64) ([]byte, error)
	// Start is the first sector of the data track with the file system:
	// 0 on CDs and DVDs, 45000 on a GD-ROM.
	Start int64
}

// HashDisc computes the RetroAchievements hash of a disc, following
// rcheevos (rc_hash_disc.c): the boot executable with its name (PS1, PS2),
// PARAM.SFO and EBOOT.BIN (PSP), the start of IP.BIN and the boot file
// (Dreamcast), or the first 512 bytes of the disc (Saturn, Sega CD).
func HashDisc(console int, d Disc) (string, error) {
	h := md5.New()
	switch console {
	case PS1, PS2:
		key, prefix := "BOOT", "cdrom:"
		if console == PS2 {
			key, prefix = "BOOT2", "cdrom0:"
		}
		name, sector, size, err := playStationExe(d, key, prefix)
		if err != nil && console == PS1 {
			// Discs without SYSTEM.CNF boot PSX.EXE.
			if s, n, err2 := findFile(d, "PSX.EXE"); err2 == nil {
				name, sector, size, err = "PSX.EXE", s, n, nil
			}
		}
		if err != nil {
			return "", err
		}
		if console == PS1 {
			// A PS-X EXE header tells the size of the code after it.
			b, err := d.Read(sector)
			if err != nil {
				return "", err
			}
			if string(b[:7]) == "PS-X EX" {
				size = int64(binary.LittleEndian.Uint32(b[28:])) + 2048
			}
		}
		h.Write([]byte(name))
		if err := hashFile(h, d, sector, size); err != nil {
			return "", err
		}
	case PSP:
		for _, p := range []string{`PSP_GAME\PARAM.SFO`, `PSP_GAME\SYSDIR\EBOOT.BIN`} {
			sector, size, err := findFile(d, p)
			if err != nil {
				return "", err
			}
			if err := hashFile(h, d, sector, size); err != nil {
				return "", err
			}
		}
	case Dreamcast:
		ip, err := d.Read(d.Start)
		if err != nil {
			return "", err
		}
		if string(ip[:16]) != "SEGA SEGAKATANA " {
			return "", errors.New("not a Dreamcast disc")
		}
		h.Write(ip[:256])
		i := 0
		for i < 16 && !isSpace(ip[96+i]) {
			i++
		}
		if i == 0 {
			return "", errors.New("IP.BIN names no boot file")
		}
		sector, size, err := findFile(d, string(ip[96:96+i]))
		if err != nil {
			return "", err
		}
		if err := hashFile(h, d, sector, size); err != nil {
			return "", err
		}
	case Saturn, SegaCD:
		b, err := d.Read(0)
		if err != nil {
			return "", err
		}
		if m := string(b[:16]); m != "SEGADISCSYSTEM  " && m != "SEGA SEGASATURN " {
			return "", errors.New("not a Sega CD or Saturn disc")
		}
		h.Write(b[:512])
	default:
		return "", ErrNotSupported
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// playStationExe finds the boot executable that SYSTEM.CNF names after
// key (BOOT or BOOT2): its name as written, without the drive prefix,
// leading backslashes and version, and where it is.
func playStationExe(d Disc, key, prefix string) (string, int64, int64, error) {
	sector, _, err := findFile(d, "SYSTEM.CNF")
	if err != nil {
		return "", 0, 0, err
	}
	b, err := d.Read(sector)
	if err != nil {
		return "", 0, 0, err
	}
	text := string(b[:2047])
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		if !strings.HasPrefix(line, key) {
			continue
		}
		rest := strings.TrimLeft(line[len(key):], " \t\r\v\f")
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\v\f")
		rest = strings.TrimPrefix(rest, prefix)
		rest = strings.TrimLeft(rest, `\`)
		end := strings.IndexFunc(rest, func(r rune) bool { return r == ';' || r < 128 && isSpace(byte(r)) })
		if end >= 0 {
			rest = rest[:end]
		}
		if len(rest) > 63 {
			rest = rest[:63]
		}
		s, size, err := findFile(d, rest)
		return rest, s, size, err
	}
	return "", 0, 0, fmt.Errorf("SYSTEM.CNF names no %s file", key)
}

// findFile finds a file by its path (folders separated by backslashes,
// case ignored) in the ISO 9660 file system of the disc's data track.
func findFile(d Disc, path string) (int64, int64, error) {
	pvd, err := d.Read(d.Start + 16)
	if err != nil {
		return 0, 0, err
	}
	if !bytes.HasPrefix(pvd, []byte("\x01CD001")) {
		return 0, 0, errors.New("no ISO 9660 file system")
	}
	block := int64(binary.LittleEndian.Uint16(pvd[128:]))
	if block == 0 {
		block = 2048
	}
	dir := int64(pvd[158]) | int64(pvd[159])<<8 | int64(pvd[160])<<16
	dirSize := int64(binary.LittleEndian.Uint32(pvd[166:]))
	parts := strings.Split(strings.TrimPrefix(path, `\`), `\`)
	for i, part := range parts {
		last := i == len(parts)-1
		sector, size, found, err := findEntry(d, dir, dirSize, block, part, last)
		if err != nil {
			return 0, 0, err
		}
		if !found {
			return 0, 0, fmt.Errorf("%s not found", path)
		}
		if last {
			return sector, size, nil
		}
		dir = sector
	}
	return 0, 0, fmt.Errorf("%s not found", path)
}

// findEntry looks for a name in a directory, as rcheevos does: folders
// must match exactly, files may have a ";version" after the name. As in
// rcheevos, the directory's size is the root's for every folder.
func findEntry(d Disc, sector, size, block int64, name string, file bool) (int64, int64, bool, error) {
	for left := size; ; sector++ {
		b, err := d.Read(sector)
		if err != nil {
			return 0, 0, false, err
		}
		for p := 0; p < len(b)-34 && b[p] != 0; {
			n := int(b[p])
			nameLen := int(b[p+32])
			if p+33+nameLen <= len(b) {
				entry := b[p+33 : p+33+nameLen]
				match := len(entry) == len(name) ||
					file && len(entry) > len(name) && entry[len(name)] == ';'
				if match && strings.EqualFold(string(entry[:min(len(name), len(entry))]), name) {
					s := int64(b[p+2]) | int64(b[p+3])<<8 | int64(b[p+4])<<16
					return s, int64(binary.LittleEndian.Uint32(b[p+10:])), true, nil
				}
			}
			p += n
		}
		if left <= block {
			return 0, 0, false, nil
		}
		left -= block
	}
}

// hashFile adds size bytes from a sector on (at most 64 MiB).
func hashFile(h hash.Hash, d Disc, sector, size int64) error {
	size = min(size, maxBuffer)
	for size > 0 {
		b, err := d.Read(sector)
		if err != nil {
			return err
		}
		n := min(size, int64(len(b)))
		h.Write(b[:n])
		size -= n
		sector++
	}
	return nil
}

// HashGameCube computes the RetroAchievements hash of a GameCube disc
// image, following rcheevos (rc_hash_nintendo_disc_partition): the disc
// header with the apploader (at most 1 MiB), then the 18 segments of the
// boot DOL. As in rcheevos, the segments are read at their offsets in the
// DOL taken as offsets on the disc, so the hashes are the same.
func HashGameCube(r io.ReaderAt) (string, error) {
	const baseHeader, maxHeader, apploaderHeader = 0x2440, 1 << 20, 0x20
	var b [8]byte
	if _, err := r.ReadAt(b[:4], 0x1c); err != nil {
		return "", err
	}
	if binary.BigEndian.Uint32(b[:4]) != 0xc2339f3d {
		return "", errors.New("not a GameCube disc")
	}
	if _, err := r.ReadAt(b[:], baseHeader+0x14); err != nil {
		return "", err
	}
	size := uint64(baseHeader+apploaderHeader) + uint64(binary.BigEndian.Uint32(b[:4])) + uint64(binary.BigEndian.Uint32(b[4:]))
	head := make([]byte, min(size, maxHeader))
	if _, err := r.ReadAt(head, 0); err != nil {
		return "", err
	}
	h := md5.New()
	h.Write(head)
	dol := int64(binary.BigEndian.Uint32(head[0x420:]))
	addr := make([]byte, 0xd8)
	if _, err := r.ReadAt(addr, dol); err != nil {
		return "", err
	}
	for i := range 18 {
		off := int64(binary.BigEndian.Uint32(addr[4*i:]))
		n := int64(binary.BigEndian.Uint32(addr[0x90+4*i:]))
		if n == 0 {
			continue
		}
		if _, err := io.CopyN(h, io.NewSectionReader(r, off, n), n); err != nil {
			return "", fmt.Errorf("DOL segment %d: %w", i, err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
