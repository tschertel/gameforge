package pkg

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// The game in a PS4 PKG is a PFS image (a read-only file system, little-
// endian) encrypted with AES-128-XTS. Its keys come from the EKPFS, which
// for fake packages (fPKG, as homebrew and the PKG tools of the PS4 scene
// make them) is derived from the content ID and the 32-character passcode
// the package was built with: no console key is needed. Retail packages
// from PSN have an EKPFS only the console can unwrap.
//
// The outer image holds one file, uroot/pfs_image.dat: the inner image,
// compressed with PFSC (zlib blocks), which holds the game's files.

// DefaultPasscode is the passcode of nearly every fake package.
const DefaultPasscode = "00000000000000000000000000000000"

const (
	pfsSector     = 0x1000 // XTS sector
	pfsMagic      = 20130315
	pfsSigned     = 0x1
	pfs64         = 0x2
	pfsEncrypted  = 0x4
	inoDir        = 0x4000
	inoCompressed = 0x1 // in the flags
)

// ekpfs derives the EKPFS of a fake package from its passcode.
func ekpfs(contentID, passcode string) []byte {
	sum := func(b ...[]byte) []byte {
		h := sha256.New()
		for _, x := range b {
			h.Write(x)
		}
		return h.Sum(nil)
	}
	cid := make([]byte, 48)
	copy(cid, contentID)
	return sum(sum([]byte{0, 0, 0, 1}), sum(cid), []byte(passcode))
}

// pfsKeys derives the XTS keys (tweak, data) of an image from the EKPFS and
// the seed in its header.
func pfsKeys(ekpfs, seed []byte) (tweak, data []byte) {
	m := hmac.New(sha256.New, ekpfs)
	m.Write([]byte{1, 0, 0, 0})
	m.Write(seed)
	k := m.Sum(nil)
	return k[:16], k[16:]
}

// xtsReader decrypts an encrypted PFS image at any offset. The first
// block (the header) is not encrypted.
type xtsReader struct {
	r          io.ReaderAt
	size       int64
	plain      int64 // bytes at the start that are not encrypted
	tweak, dat cipher.Block
}

func (x *xtsReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= x.size {
		return 0, io.EOF
	}
	// Read the whole sectors the range covers at once, then decrypt them.
	first := off / pfsSector * pfsSector
	last := min(x.size, (off+int64(len(p))+pfsSector-1)/pfsSector*pfsSector)
	buf := make([]byte, last-first)
	if _, err := x.r.ReadAt(buf, first); err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	for s := int64(0); s < int64(len(buf)); s += pfsSector {
		if first+s >= x.plain {
			e := min(s+pfsSector, int64(len(buf)))
			x.decrypt(buf[s:s+(e-s)&^15], uint64((first+s)/pfsSector))
		}
	}
	n := copy(p, buf[off-first:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (x *xtsReader) decrypt(b []byte, sector uint64) {
	var t [16]byte
	binary.LittleEndian.PutUint64(t[:], sector)
	x.tweak.Encrypt(t[:], t[:])
	for i := 0; i+16 <= len(b); i += 16 {
		blk := b[i : i+16]
		subtle16(blk, t[:])
		x.dat.Decrypt(blk, blk)
		subtle16(blk, t[:])
		// The next tweak: multiply by x in GF(2^128).
		carry := t[15] >> 7
		for j := 15; j > 0; j-- {
			t[j] = t[j]<<1 | t[j-1]>>7
		}
		t[0] <<= 1
		if carry != 0 {
			t[0] ^= 0x87
		}
	}
}

func subtle16(dst, src []byte) {
	for i := range 16 {
		dst[i] ^= src[i]
	}
}

// pfsInode is the part of an inode the reader needs.
type pfsInode struct {
	mode  uint16
	flags uint32
	// For a compressed file, one of the two sizes is that of its PFSC
	// stream in the image (the larger, as PFSC blocks are whole) and the
	// PFSC header gives the size of the data.
	size, size2 int64
	firstBlock  int64
}

func (i pfsInode) isDir() bool { return i.mode&inoDir != 0 }

// pfsImage is an opened PFS image.
type pfsImage struct {
	r      io.ReaderAt
	block  int64
	inodes []pfsInode
}

// openPFS reads the header and inodes of a PFS image. If the image is
// encrypted, ek is its EKPFS.
func openPFS(r io.ReaderAt, size int64, ek []byte) (*pfsImage, error) {
	h := make([]byte, 0x400)
	if _, err := r.ReadAt(h, 0); err != nil {
		return nil, fmt.Errorf("PFS: %w", err)
	}
	le := binary.LittleEndian
	if le.Uint64(h[8:]) != pfsMagic {
		return nil, errors.New("PFS: bad magic")
	}
	mode := le.Uint16(h[0x1C:])
	block := int64(int32(le.Uint32(h[0x20:])))
	count := int64(le.Uint64(h[0x30:]))
	inodeBlocks := int64(le.Uint64(h[0x40:]))
	if block < pfsSector || block > 1<<20 || count <= 0 || count > 1<<24 || inodeBlocks <= 0 || (inodeBlocks+1)*block > size {
		return nil, errors.New("PFS: bad header")
	}
	if mode&pfsEncrypted != 0 {
		if ek == nil {
			return nil, errors.New("PFS: encrypted")
		}
		tk, dk := pfsKeys(ek, h[0x370:0x380])
		tc, _ := aes.NewCipher(tk)
		dc, _ := aes.NewCipher(dk)
		r = &xtsReader{r: r, size: size, plain: block, tweak: tc, dat: dc}
	}
	// Inode sizes: the block numbers are 32 or 64 bits, and signed images
	// keep a 32-byte signature before each.
	ptr := int64(4)
	if mode&pfs64 != 0 {
		ptr = 8
	}
	if mode&pfsSigned != 0 {
		ptr += 32
	}
	head := int64(0x64)
	if mode&pfs64 != 0 {
		head = 0x68 // the block count is 64 bits too
	}
	inoSize := head + 17*ptr
	perBlock := block / inoSize
	img := &pfsImage{r: r, block: block}
	buf := make([]byte, block)
	for b := int64(0); b < inodeBlocks && int64(len(img.inodes)) < count; b++ {
		if _, err := r.ReadAt(buf, (1+b)*block); err != nil {
			return nil, fmt.Errorf("PFS inodes: %w", err)
		}
		for i := int64(0); i < perBlock && int64(len(img.inodes)) < count; i++ {
			e := buf[i*inoSize:]
			db := e[head:]
			if mode&pfsSigned != 0 {
				db = db[32:]
			}
			first := int64(le.Uint32(db))
			if mode&pfs64 != 0 {
				first = int64(le.Uint64(db))
			}
			img.inodes = append(img.inodes, pfsInode{
				mode: le.Uint16(e[0:]), flags: le.Uint32(e[4:]),
				size: int64(le.Uint64(e[8:])), size2: int64(le.Uint64(e[16:])),
				firstBlock: first,
			})
		}
	}
	if len(img.inodes) == 0 || !img.inodes[0].isDir() {
		return nil, errors.New("PFS: wrong key (the inodes do not decrypt)")
	}
	return img, nil
}

// raw is the stored data of an inode. Files in a PKG are contiguous.
func (img *pfsImage) raw(ino int) (*io.SectionReader, error) {
	if ino < 0 || ino >= len(img.inodes) {
		return nil, fmt.Errorf("PFS: no inode %d", ino)
	}
	i := img.inodes[ino]
	n := i.size
	if i.flags&inoCompressed != 0 {
		n = max(i.size, i.size2)
	}
	return io.NewSectionReader(img.r, i.firstBlock*img.block, n), nil
}

// open is the content of an inode, decompressed if needed.
func (img *pfsImage) open(ino int) (io.ReaderAt, int64, error) {
	r, err := img.raw(ino)
	if err != nil {
		return nil, 0, err
	}
	i := img.inodes[ino]
	if i.flags&inoCompressed != 0 {
		c, err := openPFSC(r)
		if err != nil {
			return nil, 0, err
		}
		return c, c.length, nil
	}
	return r, i.size, nil
}

// pfsEntry is an entry of a directory.
type pfsEntry struct {
	ino  int
	typ  uint32 // 2 = file, 3 = directory
	name string
}

// readDir lists a directory, without "." and "..".
func (img *pfsImage) readDir(ino int) ([]pfsEntry, error) {
	r, size, err := img.open(ino)
	if err != nil {
		return nil, err
	}
	if size > 64<<20 {
		return nil, errors.New("PFS: directory too large")
	}
	b := make([]byte, size)
	if _, err := r.ReadAt(b, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	le := binary.LittleEndian
	var out []pfsEntry
	for off := 0; off+16 <= len(b); {
		e := b[off:]
		num, typ := int(le.Uint32(e[0:])), le.Uint32(e[4:])
		nameLen, entSize := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
		if entSize == 0 {
			// The rest of the block is empty; entries go on in the next.
			next := (off/int(img.block) + 1) * int(img.block)
			if next <= off || next >= len(b) {
				break
			}
			off = next
			continue
		}
		if entSize < 16 || off+entSize > len(b) || 16+nameLen > entSize {
			return nil, errors.New("PFS: bad directory entry")
		}
		if typ == 2 || typ == 3 {
			out = append(out, pfsEntry{num, typ, string(e[16 : 16+nameLen])})
		}
		off += entSize
	}
	return out, nil
}

// lookup finds a name in a directory.
func (img *pfsImage) lookup(dir int, name string) (pfsEntry, error) {
	ents, err := img.readDir(dir)
	if err != nil {
		return pfsEntry{}, err
	}
	for _, e := range ents {
		if e.name == name {
			return e, nil
		}
	}
	return pfsEntry{}, fmt.Errorf("PFS: %s not found", name)
}

// pfscReader reads a PFSC stream: an image split into blocks, each
// compressed with zlib or stored as is.
type pfscReader struct {
	r       io.ReaderAt
	block   int64
	length  int64
	offsets []uint64

	mu     sync.Mutex
	cached int64
	data   []byte
}

func openPFSC(r io.ReaderAt) (*pfscReader, error) {
	h := make([]byte, 0x30)
	if _, err := r.ReadAt(h, 0); err != nil {
		return nil, fmt.Errorf("PFSC: %w", err)
	}
	if !bytes.Equal(h[:4], []byte("PFSC")) {
		return nil, errors.New("PFSC: bad magic")
	}
	le := binary.LittleEndian
	block := int64(le.Uint32(h[0x0C:]))
	table := int64(le.Uint64(h[0x18:]))
	length := int64(le.Uint64(h[0x28:]))
	if block <= 0 || block > 1<<24 || length < 0 {
		return nil, errors.New("PFSC: bad header")
	}
	n := (length + block - 1) / block
	if n > 1<<26 {
		return nil, errors.New("PFSC: too many blocks")
	}
	raw := make([]byte, (n+1)*8)
	if _, err := r.ReadAt(raw, table); err != nil {
		return nil, fmt.Errorf("PFSC: %w", err)
	}
	c := &pfscReader{r: r, block: block, length: length, offsets: make([]uint64, n+1), cached: -1}
	for i := range c.offsets {
		c.offsets[i] = le.Uint64(raw[i*8:])
	}
	return c, nil
}

// load decompresses block i into c.data.
func (c *pfscReader) load(i int64) error {
	if c.cached == i {
		return nil
	}
	start, end := c.offsets[i], c.offsets[i+1]
	if end < start || end-start > uint64(c.block) {
		return errors.New("PFSC: bad block")
	}
	stored := make([]byte, end-start)
	if _, err := c.r.ReadAt(stored, int64(start)); err != nil {
		return fmt.Errorf("PFSC: %w", err)
	}
	if c.data == nil {
		c.data = make([]byte, c.block)
	}
	switch {
	case int64(len(stored)) == c.block:
		copy(c.data, stored)
	case len(stored) == 0:
		clear(c.data)
	default:
		z, err := zlib.NewReader(bytes.NewReader(stored))
		if err != nil {
			return fmt.Errorf("PFSC block %d: %w", i, err)
		}
		n, err := io.ReadFull(z, c.data)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("PFSC block %d: %w", i, err)
		}
		clear(c.data[n:])
	}
	c.cached = i
	return nil
}

func (c *pfscReader) ReadAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for n < len(p) && off < c.length {
		i := off / c.block
		if err := c.load(i); err != nil {
			return n, err
		}
		in := off - i*c.block
		k := copy(p[n:], c.data[in:min(c.block, c.length-i*c.block)])
		n += k
		off += int64(k)
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
