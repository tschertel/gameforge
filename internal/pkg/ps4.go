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
// is a PFS image after them (see pfs.go): fake packages are extracted in
// full; of retail ones, whose PFS key only the console has, only the
// unencrypted entries are.

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

	// The game's PFS image, when its key is known (a fake package).
	pfsOffset, pfsSize int64
	ek                 []byte
	pfsFiles           int
	pfsTotal           int64
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

	// The PFS image: offset and size at 0x410 of the extended header.
	var x [16]byte
	if _, err := f.ReadAt(x[:], 0x410); err == nil {
		pk.pfsOffset, pk.pfsSize = int64(be.Uint64(x[0:])), int64(be.Uint64(x[8:]))
	}
	if pk.pfsSize > 0 && pk.pfsOffset+pk.pfsSize <= p.Size {
		ek := ekpfs(p.ContentID, DefaultPasscode)
		if inner, err := pk.inner(f, ek); err == nil {
			err = inner.walk(inner.uroot, "", func(_ string, ino int, size int64) {
				if size >= 0 {
					pk.pfsFiles++
					pk.pfsTotal += size
				}
			})
			if err == nil {
				pk.ek = ek
				p.Fake = true
				p.Files = pk.pfsFiles
			} else {
				pk.pfsFiles, pk.pfsTotal = 0, 0
			}
		}
	}
	return nil
}

// innerImage is the inner PFS image of a package and its uroot folder.
type innerImage struct {
	*pfsImage
	uroot int
}

// inner opens the outer PFS image with the EKPFS ek, then the inner one.
func (pk *ps4Pkg) inner(f io.ReaderAt, ek []byte) (*innerImage, error) {
	outer, err := openPFS(io.NewSectionReader(f, pk.pfsOffset, pk.pfsSize), pk.pfsSize, ek)
	if err != nil {
		return nil, err
	}
	root, err := outer.lookup(0, "uroot")
	if err != nil {
		return nil, err
	}
	dat, err := outer.lookup(root.ino, "pfs_image.dat")
	if err != nil {
		return nil, err
	}
	r, size, err := outer.open(dat.ino)
	if err != nil {
		return nil, err
	}
	img, err := openPFS(r, size, nil)
	if err != nil {
		return nil, fmt.Errorf("inner %w", err)
	}
	u, err := img.lookup(0, "uroot")
	if err != nil {
		return nil, err
	}
	return &innerImage{img, u.ino}, nil
}

// walk calls fn for each file under a folder, with its path, inode and
// size, and for each folder with size -1.
func (img *innerImage) walk(dir int, prefix string, fn func(path string, ino int, size int64)) error {
	return img.walkDepth(dir, prefix, fn, 0)
}

func (img *innerImage) walkDepth(dir int, prefix string, fn func(string, int, int64), depth int) error {
	if depth > 64 {
		return errors.New("PFS: folders too deep")
	}
	ents, err := img.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		path := prefix + e.name
		if e.typ == 3 {
			fn(path, e.ino, -1)
			if err := img.walkDepth(e.ino, path+"/", fn, depth+1); err != nil {
				return err
			}
			continue
		}
		if e.ino < 0 || e.ino >= len(img.inodes) {
			return fmt.Errorf("PFS: %s: bad inode", path)
		}
		_, size, err := img.open(e.ino)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fn(path, e.ino, size)
	}
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

// extract writes the game's files (fake packages) and the named,
// unencrypted entries into dir/sce_sys, without replacing the game's own.
func (pk *ps4Pkg) extract(ctx context.Context, f *os.File, dir string, progress func(done, total int64)) (int64, error) {
	var done int64
	total := pk.total + pk.pfsTotal
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	if pk.ek != nil {
		img, err := pk.inner(f, pk.ek)
		if err != nil {
			return 0, err
		}
		var files []struct {
			name string
			ino  int
			dir  bool
		}
		err = img.walk(img.uroot, "", func(name string, ino int, size int64) {
			files = append(files, struct {
				name string
				ino  int
				dir  bool
			}{name, ino, size < 0})
		})
		if err != nil {
			return 0, err
		}
		for _, fl := range files {
			out, err := safePath(dir, fl.name)
			if err != nil {
				return done, err
			}
			if fl.dir {
				if err := os.MkdirAll(out, 0o755); err != nil {
					return done, err
				}
				continue
			}
			r, size, err := img.open(fl.ino)
			if err != nil {
				return done, fmt.Errorf("%s: %w", fl.name, err)
			}
			if err := writeFile(ctx, out, io.NewSectionReader(r, 0, size), size, func(k int64) {
				done += k
				progress(done, total)
			}); err != nil {
				return done, fmt.Errorf("%s: %w", fl.name, err)
			}
		}
	}
	sys := filepath.Join(dir, "sce_sys")
	for _, e := range pk.entries {
		out, err := safePath(sys, e.name)
		if err != nil {
			return done, err
		}
		if _, err := os.Stat(out); err == nil {
			done += int64(e.size) // the game's own copy
			progress(done, total)
			continue
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
			progress(done, total)
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

// writeFile copies size bytes of r to a new file.
func writeFile(ctx context.Context, path string, r io.Reader, size int64, report func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	w, err := os.Create(path)
	if err != nil {
		return err
	}
	err = copyN(ctx, w, r, size, report)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	return err
}
