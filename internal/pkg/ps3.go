package pkg

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A PS3, PSP or PS Vita PKG (big-endian) has a plain header and metadata,
// then a data area encrypted with AES-128-CTR. The data area starts with a
// table of items (files and folders) whose names and contents follow.
// Retail packages use a platform key (PS3, PSP) or a key derived from a
// Vita key and the package's IV; debug packages use a SHA-1 key stream that
// needs no key.

const ps3HeaderSize = 0x100

// Item types (the low byte of the flags) that are folders.
func ps3IsDir(flags uint32) bool { t := flags & 0xFF; return t == 4 || t == 0x12 }

type ps3Item struct {
	name         string
	offset, size uint64 // in the data area
	flags        uint32
	psp          bool // a PSP item of a Vita package: decrypted with the PSP key
}

type ps3Pkg struct {
	dataOffset, dataSize uint64
	main, psp            *stream // psp is nil when not needed or not loaded
	items                []ps3Item
	total                int64
}

// stream decrypts the data area at any offset.
type stream struct {
	block cipher.Block // retail: AES-CTR with iv
	iv    []byte
	debug []byte // debug: the digest the SHA-1 key stream is made of
}

// xorAt decrypts b, which is at offset off of the data area, in place.
func (s *stream) xorAt(b []byte, off uint64) {
	for len(b) > 0 {
		blk, skip := off/16, int(off%16)
		var ks [16]byte
		n := min(16-skip, len(b))
		if s.debug != nil {
			ks = s.debugBlock(blk)
		} else if skip != 0 || len(b) < 16 {
			s.block.Encrypt(ks[:], counter(s.iv, blk))
		} else {
			// Whole blocks: the standard CTR mode is much faster.
			whole := len(b) &^ 15
			cipher.NewCTR(s.block, counter(s.iv, blk)).XORKeyStream(b[:whole], b[:whole])
			b, off = b[whole:], off+uint64(whole)
			continue
		}
		for i := range n {
			b[i] ^= ks[skip+i]
		}
		b, off = b[n:], off+uint64(n)
	}
}

// counter is iv + blk, as a 128-bit big-endian number.
func counter(iv []byte, blk uint64) []byte {
	n := new(big.Int).SetBytes(iv)
	n.Add(n, new(big.Int).SetUint64(blk))
	out := make([]byte, 16)
	b := n.Bytes()
	if len(b) > 16 {
		b = b[len(b)-16:]
	}
	copy(out[16-len(b):], b)
	return out
}

func (s *stream) debugBlock(blk uint64) [16]byte {
	var buf [64]byte
	d := s.debug
	copy(buf[0:], d[0:8])
	copy(buf[8:], d[0:8])
	copy(buf[16:], d[8:16])
	copy(buf[24:], d[8:16])
	binary.BigEndian.PutUint64(buf[56:], blk)
	sum := sha1.Sum(buf[:])
	var ks [16]byte
	copy(ks[:], sum[:16])
	return ks
}

func aesStream(key, iv []byte) *stream {
	b, _ := aes.NewCipher(key)
	return &stream{block: b, iv: iv}
}

func (p *Package) openPS3(f io.ReaderAt, keys *Keys) error {
	var h [ps3HeaderSize]byte
	if _, err := f.ReadAt(h[:], 0); err != nil {
		return errors.New("PKG: header too short")
	}
	be := binary.BigEndian
	revision := be.Uint16(h[4:])
	pkgType := be.Uint16(h[6:])
	metaOff, metaCount := be.Uint32(h[8:]), be.Uint32(h[12:])
	itemCount := be.Uint32(h[20:])
	pk := &ps3Pkg{dataOffset: be.Uint64(h[0x20:]), dataSize: be.Uint64(h[0x28:])}
	if pk.dataOffset+pk.dataSize > uint64(p.Size) || itemCount > 1<<20 ||
		uint64(itemCount)*32 > pk.dataSize {
		return errors.New("PKG: bad header")
	}
	p.ContentID = strings.TrimRight(string(h[0x30:0x54]), "\x00")
	p.TitleID = titleIDOf(p.ContentID)
	digest, iv := h[0x60:0x70], h[0x70:0x80]

	contentType := uint32(0)
	off := int64(metaOff)
	for range min(metaCount, 64) {
		var e [8]byte
		if _, err := f.ReadAt(e[:], off); err != nil {
			break
		}
		id, size := be.Uint32(e[0:]), be.Uint32(e[4:])
		if id == 2 && size >= 4 {
			var v [4]byte
			f.ReadAt(v[:], off+8)
			contentType = be.Uint32(v[:])
		}
		off += 8 + int64(size)
	}

	keyID := uint32(0)
	if pkgType == 2 && bytes.Equal(h[0xC0:0xC4], []byte("\x7fext")) {
		keyID = be.Uint32(h[0xE4:]) & 7
	}
	p.Platform = ps3Platform(pkgType, keyID, contentType)
	p.Kind = ps3KindOf(contentType)

	// The key.
	switch {
	case revision&0x8000 == 0:
		p.Debug = true
		pk.main = &stream{debug: bytes.Clone(digest)}
	case pkgType == 1:
		if k := keys.Get("ps3"); k != nil {
			pk.main = aesStream(k, iv)
		} else {
			p.NeedKey = "ps3"
		}
	case keyID <= 1:
		if k := keys.Get("psp"); k != nil {
			pk.main = aesStream(k, iv)
		} else {
			p.NeedKey = "psp"
		}
	default:
		name := fmt.Sprintf("vita%d", keyID)
		if k := keys.Get(name); k != nil {
			// The package key is the IV encrypted with the Vita key.
			b, _ := aes.NewCipher(k)
			pkgKey := make([]byte, 16)
			b.Encrypt(pkgKey, iv)
			pk.main = aesStream(pkgKey, iv)
		} else {
			p.NeedKey = name
		}
		if k := keys.Get("psp"); k != nil {
			pk.psp = aesStream(k, iv)
		}
	}
	p.ps3 = pk
	if pk.main == nil {
		return nil // only the header can be read
	}

	read := func(s *stream, at, n uint64) ([]byte, error) {
		if at+n > pk.dataSize || n > 1<<30 {
			return nil, errors.New("PKG: item past the end of the data")
		}
		b := make([]byte, n)
		if _, err := f.ReadAt(b, int64(pk.dataOffset+at)); err != nil {
			return nil, err
		}
		s.xorAt(b, at)
		return b, nil
	}
	table, err := read(pk.main, 0, uint64(itemCount)*32)
	if err != nil {
		return err
	}
	for i := range itemCount {
		e := table[i*32:]
		it := ps3Item{
			offset: be.Uint64(e[8:]), size: be.Uint64(e[16:]),
			flags: be.Uint32(e[24:]),
		}
		nameOff, nameSize := uint64(be.Uint32(e[0:])), uint64(be.Uint32(e[4:]))
		s := pk.main
		if keyID >= 2 && it.flags>>24 == 0x90 {
			it.psp = true
			if pk.psp == nil {
				p.NeedKey = "psp"
				return nil
			}
			s = pk.psp
		}
		if nameSize > 1024 {
			return errors.New("PKG: bad item name")
		}
		name, err := read(s, nameOff, nameSize)
		if err != nil {
			return err
		}
		it.name = strings.TrimRight(string(name), "\x00")
		if !ps3IsDir(it.flags) && it.offset+it.size > pk.dataSize {
			return fmt.Errorf("PKG: %s past the end of the data", it.name)
		}
		if !utf8Clean(it.name) {
			return errors.New("PKG: wrong key (the item names do not decrypt)")
		}
		pk.items = append(pk.items, it)
		if !ps3IsDir(it.flags) {
			pk.total += int64(it.size)
			p.Files++
		}
	}
	p.readSFO(pk, read)
	if p.SFO != nil {
		if k := ps3KindOfSFO(p.Platform, p.SFO["CATEGORY"]); k != "" {
			p.Kind = k
		}
	}
	return nil
}

// utf8Clean reports whether a decrypted name looks like a file name: a
// wrong key gives random bytes.
func utf8Clean(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0xFFFD {
			return false
		}
	}
	return true
}

// readSFO finds the PARAM.SFO and the icon among the items: in the root
// (PS3), in sce_sys (Vita) or inside EBOOT.PBP (PSP).
func (p *Package) readSFO(pk *ps3Pkg, read func(*stream, uint64, uint64) ([]byte, error)) {
	var sfo, icon, pbp *ps3Item
	depth := func(it *ps3Item) int { return strings.Count(it.name, "/") }
	for i := range pk.items {
		it := &pk.items[i]
		if ps3IsDir(it.flags) {
			continue
		}
		base := strings.ToLower(path.Base(it.name))
		switch {
		case base == "param.sfo" && (sfo == nil || depth(it) < depth(sfo)):
			sfo = it
		case base == "icon0.png" && (icon == nil || depth(it) < depth(icon)):
			icon = it
		case base == "eboot.pbp" && (pbp == nil || depth(it) < depth(pbp)):
			pbp = it
		}
	}
	get := func(it *ps3Item, at, n uint64) []byte {
		s := pk.main
		if it.psp {
			s = pk.psp
		}
		if at+n > it.size || n > 16<<20 {
			return nil
		}
		b, _ := read(s, it.offset+at, n)
		return b
	}
	if sfo != nil {
		p.SFO, _ = ParseSFO(get(sfo, 0, sfo.size))
	}
	if icon != nil {
		p.Icon = get(icon, 0, icon.size)
	}
	// EBOOT.PBP: "\0PBP", version, then the offsets of PARAM.SFO, ICON0.PNG,
	// ICON1, PIC0, PIC1, SND0, DATA.PSP and DATA.PSAR.
	if pbp != nil && (p.SFO == nil || p.Icon == nil) {
		if h := get(pbp, 0, 40); h != nil && bytes.Equal(h[:4], []byte("\x00PBP")) {
			le := binary.LittleEndian
			o := func(i int) uint64 { return uint64(le.Uint32(h[8+4*i:])) }
			if p.SFO == nil && o(1) > o(0) {
				p.SFO, _ = ParseSFO(get(pbp, o(0), o(1)-o(0)))
			}
			if p.Icon == nil && o(2) > o(1) {
				p.Icon = get(pbp, o(1), o(2)-o(1))
			}
		}
	}
	if !bytes.HasPrefix(p.Icon, []byte("\x89PNG")) {
		p.Icon = nil
	}
}

// ps3Platform tells the console a package is for.
func ps3Platform(pkgType uint16, keyID, contentType uint32) string {
	switch {
	case pkgType == 2 && keyID >= 2, contentType >= 0x15 && contentType <= 0x1F:
		return "PS Vita"
	case pkgType == 2, contentType == 0x7 || contentType == 0xE || contentType == 0xF:
		return "PSP"
	}
	return "PS3"
}

// ps3KindOf tells what a package holds from the content type of its
// metadata.
func ps3KindOf(contentType uint32) string {
	switch contentType {
	case 0x4, 0x5, 0x6, 0x7, 0xE, 0xF, 0x10, 0x12, 0x14, 0x15:
		return KindGame
	case 0x16:
		return KindDLC
	case 0x9, 0x1F:
		return KindTheme
	}
	return KindOther
}

// ps3KindOfSFO tells what a package holds from its CATEGORY, or "".
func ps3KindOfSFO(platform, category string) string {
	switch platform {
	case "PS Vita":
		switch {
		case category == "gp":
			return KindUpdate
		case category == "ac":
			return KindDLC
		case strings.HasPrefix(category, "gd"):
			return KindGame
		}
	case "PS3":
		switch category {
		case "HG", "DG", "1P", "2P", "MN", "PE", "PP", "HM", "CB", "AM":
			return KindGame
		case "GD":
			return KindUpdate
		}
	case "PSP":
		if category == "EG" || category == "ME" || category == "MG" {
			return KindGame
		}
	}
	return ""
}

// extract writes the items into dir, keeping their folders.
func (pk *ps3Pkg) extract(ctx context.Context, f *os.File, dir string, progress func(done, total int64)) (int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	var done int64
	buf := make([]byte, 1<<20)
	for _, it := range pk.items {
		out, err := safePath(dir, it.name)
		if err != nil {
			return done, err
		}
		if ps3IsDir(it.flags) {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return done, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return done, err
		}
		s := pk.main
		if it.psp {
			s = pk.psp
		}
		w, err := os.Create(out)
		if err != nil {
			return done, err
		}
		for at := uint64(0); at < it.size && err == nil; {
			if err = ctx.Err(); err != nil {
				break
			}
			n := min(uint64(len(buf)), it.size-at)
			b := buf[:n]
			if _, err = f.ReadAt(b, int64(pk.dataOffset+it.offset+at)); err != nil {
				break
			}
			s.xorAt(b, it.offset+at)
			if _, err = w.Write(b); err != nil {
				break
			}
			at += n
			done += int64(n)
			progress(done, pk.total)
		}
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return done, err
		}
	}
	return done, nil
}
