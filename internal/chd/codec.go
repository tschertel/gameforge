package chd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz/lzma"
)

// lzmaDictSize is the dictionary size MAME's LZMA codec derives for a
// block of n bytes (LzmaEncProps_Normalize with reduceSize = n). The
// decoder uses it too, as it is not stored in the stream.
func lzmaDictSize(n int) int {
	for i := 11; i <= 30; i++ {
		if n <= 2<<i {
			return 2 << i
		}
		if n <= 3<<i {
			return 3 << i
		}
	}
	return 1 << 26
}

// lzmaProps are the literal context/position bits MAME uses: lc=3 lp=0 pb=2.
var lzmaProps = lzma.Properties{LC: 3, LP: 0, PB: 2}

const lzmaHeaderLen = 13 // properties, dictionary size, uncompressed size

// lzmaCompress returns a raw LZMA stream (no header, no end marker), the
// form of CHD's "lzma" codec.
func lzmaCompress(src []byte, dictSize int) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(len(src)/2 + lzmaHeaderLen)
	w, err := lzma.WriterConfig{
		Properties:   &lzmaProps,
		DictCap:      max(dictSize, lzma.MinDictCap),
		Matcher:      lzma.HashTable4, // BinaryTree is slower and compresses worse here
		SizeInHeader: true,
		Size:         int64(len(src)),
	}.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(src); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes()[lzmaHeaderLen:], nil
}

func lzmaDecompress(src []byte, dst []byte, dictSize int) error {
	hdr := make([]byte, lzmaHeaderLen)
	hdr[0] = byte((lzmaProps.PB*5+lzmaProps.LP)*9 + lzmaProps.LC)
	binary.LittleEndian.PutUint32(hdr[1:], uint32(max(dictSize, lzma.MinDictCap)))
	binary.LittleEndian.PutUint64(hdr[5:], uint64(len(dst)))
	r, err := lzma.NewReader(io.MultiReader(bytes.NewReader(hdr), bytes.NewReader(src)))
	if err != nil {
		return err
	}
	if _, err := io.ReadFull(r, dst); err != nil {
		return fmt.Errorf("chd: lzma: %w", err)
	}
	return nil
}

// deflater produces raw deflate streams (CHD's "zlib" codec has no zlib
// header), reusing one writer.
type deflater struct {
	buf bytes.Buffer
	w   *flate.Writer
}

func (d *deflater) compress(src []byte) []byte {
	d.buf.Reset()
	if d.w == nil {
		d.w, _ = flate.NewWriter(&d.buf, flate.BestCompression)
	} else {
		d.w.Reset(&d.buf)
	}
	d.w.Write(src)
	d.w.Close()
	return bytes.Clone(d.buf.Bytes())
}

func inflate(src, dst []byte) error {
	r := flate.NewReader(bytes.NewReader(src))
	defer r.Close()
	if _, err := io.ReadFull(r, dst); err != nil {
		return fmt.Errorf("chd: deflate: %w", err)
	}
	return nil
}

// compressor compresses hunks with the codecs of one CHD; each worker has
// its own.
type compressor struct {
	codecs      []uint32 // the header's codecs, in order
	hunkBytes   int
	def         deflater
	cdBuf       []byte
	flac        flacEncoder
	left, right []int32
}

// compressCDFLAC implements the cdfl codec: the sectors as 16-bit
// big-endian stereo samples in FLAC frames of 2352 samples, followed by the
// subcode compressed with deflate.
func (c *compressor) compressCDFLAC(hunk []byte) []byte {
	frames := len(hunk) / FrameSize
	n := frames * SectorSize / 4
	if cap(c.left) < n {
		c.left, c.right = make([]int32, n), make([]int32, n)
	}
	left, right := c.left[:n], c.right[:n]
	subcode := make([]byte, 0, frames*SubcodeSize)
	for f := range frames {
		frame := hunk[f*FrameSize:]
		for i := range SectorSize / 4 {
			s := frame[4*i:]
			left[f*SectorSize/4+i] = int32(int16(uint16(s[0])<<8 | uint16(s[1])))
			right[f*SectorSize/4+i] = int32(int16(uint16(s[2])<<8 | uint16(s[3])))
		}
		subcode = append(subcode, frame[SectorSize:FrameSize]...)
	}
	out := c.flac.encode(left, right, flacBlockSize(frames*SectorSize, SectorSize))
	return append(out, c.def.compress(subcode)...)
}

func decompressCDFLAC(src, dst []byte) error {
	frames := len(dst) / FrameSize
	n := frames * SectorSize / 4
	left, right := make([]int32, n), make([]int32, n)
	used, err := flacDecode(src, left, right)
	if err != nil {
		return err
	}
	subcode := make([]byte, frames*SubcodeSize)
	if err := inflate(src[used:], subcode); err != nil {
		return err
	}
	for f := range frames {
		frame := dst[f*FrameSize : (f+1)*FrameSize]
		for i := range SectorSize / 4 {
			l, r := left[f*SectorSize/4+i], right[f*SectorSize/4+i]
			frame[4*i], frame[4*i+1], frame[4*i+2], frame[4*i+3] = byte(l>>8), byte(l), byte(r>>8), byte(r)
		}
		copy(frame[SectorSize:], subcode[f*SubcodeSize:(f+1)*SubcodeSize])
	}
	return nil
}

// decompressFLAC implements the flac codec: 'L' or 'B' for little or big
// endian samples, then FLAC frames.
func decompressFLAC(src, dst []byte) error {
	if len(src) < 1 || (src[0] != 'L' && src[0] != 'B') {
		return errFLAC
	}
	n := len(dst) / 4
	left, right := make([]int32, n), make([]int32, n)
	if _, err := flacDecode(src[1:], left, right); err != nil {
		return err
	}
	for i := range n {
		l, r := uint16(left[i]), uint16(right[i])
		s := dst[4*i:]
		if src[0] == 'L' {
			s[0], s[1], s[2], s[3] = byte(l), byte(l>>8), byte(r), byte(r>>8)
		} else {
			s[0], s[1], s[2], s[3] = byte(l>>8), byte(l), byte(r>>8), byte(r)
		}
	}
	return nil
}

// compress returns the compressed hunk and its compression type, or
// compNone when no codec makes it smaller.
func (c *compressor) compress(hunk []byte) ([]byte, uint8, error) {
	var best []byte
	bestType := uint8(compNone)
	for i, codec := range c.codecs {
		var out []byte
		var err error
		switch codec {
		case codecLZMA:
			out, err = lzmaCompress(hunk, lzmaDictSize(c.hunkBytes))
		case codecZlib:
			out = c.def.compress(hunk)
		case codecCDLZMA, codecCDZlib:
			out, err = c.compressCD(hunk, codec)
		case codecCDFLAC:
			out = c.compressCDFLAC(hunk)
		default:
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		if out != nil && len(out) < len(hunk) && (best == nil || len(out) < len(best)) {
			best, bestType = out, uint8(compType0+i)
		}
	}
	if best == nil {
		return hunk, compNone, nil
	}
	return best, bestType, nil
}

// cdHeader returns the sizes of the CD codec header: a bit per frame
// telling whether its sync header and parity were removed, then the
// length of the compressed sector data.
func cdHeader(hunkBytes int) (frames, eccBytes, lenBytes int) {
	frames = hunkBytes / FrameSize
	eccBytes = (frames + 7) / 8
	lenBytes = 2
	if hunkBytes >= 65536 {
		lenBytes = 3
	}
	return
}

// compressCD implements the cdlz and cdzl codecs: the sectors (with sync
// header and parity removed where they can be regenerated) compressed with
// LZMA or deflate, followed by the subcode compressed with deflate. It
// returns nil when the sector data does not fit the length field.
func (c *compressor) compressCD(hunk []byte, codec uint32) ([]byte, error) {
	frames, eccBytes, lenBytes := cdHeader(len(hunk))
	if cap(c.cdBuf) < len(hunk) {
		c.cdBuf = make([]byte, len(hunk))
	}
	buf := c.cdBuf[:len(hunk)]
	out := make([]byte, eccBytes+lenBytes, len(hunk))
	sectors, subcode := buf[:frames*SectorSize], buf[frames*SectorSize:]
	for f := range frames {
		frame := hunk[f*FrameSize:]
		sector := sectors[f*SectorSize : (f+1)*SectorSize]
		copy(sector, frame[:SectorSize])
		copy(subcode[f*SubcodeSize:(f+1)*SubcodeSize], frame[SectorSize:FrameSize])
		if bytes.Equal(sector[:12], syncHeader[:]) && eccVerify(sector) {
			out[f/8] |= 1 << (f % 8)
			clear(sector[:12])
			eccClear(sector)
		}
	}
	var base []byte
	var err error
	if codec == codecCDLZMA {
		base, err = lzmaCompress(sectors, lzmaDictSize(len(sectors)))
		if err != nil {
			return nil, err
		}
	} else {
		base = c.def.compress(sectors)
	}
	if len(base) >= 1<<(8*lenBytes) {
		return nil, nil
	}
	for k := range lenBytes {
		out[eccBytes+k] = byte(len(base) >> (8 * (lenBytes - 1 - k)))
	}
	out = append(out, base...)
	return append(out, c.def.compress(subcode)...), nil
}

// decompress decodes a hunk stored with the given codec into dst.
func decompress(codec uint32, src, dst []byte) error {
	switch codec {
	case codecLZMA:
		return lzmaDecompress(src, dst, lzmaDictSize(len(dst)))
	case codecZlib:
		return inflate(src, dst)
	case codecZstd:
		return zstdDecompress(src, dst)
	case codecCDLZMA, codecCDZlib, codecCDZstd:
		return decompressCD(codec, src, dst)
	case codecCDFLAC:
		return decompressCDFLAC(src, dst)
	case codecFLAC:
		return decompressFLAC(src, dst)
	case codecHuff:
		return huffDecompress(src, dst)
	}
	return fmt.Errorf("chd: unsupported codec %q", codecName(codec))
}

func decompressCD(codec uint32, src, dst []byte) error {
	frames, eccBytes, lenBytes := cdHeader(len(dst))
	if len(src) < eccBytes+lenBytes {
		return fmt.Errorf("chd: truncated CD hunk")
	}
	baseLen := 0
	for k := range lenBytes {
		baseLen = baseLen<<8 | int(src[eccBytes+k])
	}
	start := eccBytes + lenBytes
	if start+baseLen > len(src) {
		return fmt.Errorf("chd: truncated CD hunk")
	}
	buf := make([]byte, frames*FrameSize)
	sectors, subcode := buf[:frames*SectorSize], buf[frames*SectorSize:]
	// cdzs keeps the subcode with zstd too; cdlz and cdzl with deflate.
	sub := inflate
	var err error
	switch codec {
	case codecCDLZMA:
		err = lzmaDecompress(src[start:start+baseLen], sectors, lzmaDictSize(len(sectors)))
	case codecCDZstd:
		err = zstdDecompress(src[start:start+baseLen], sectors)
		sub = zstdDecompress
	default:
		err = inflate(src[start:start+baseLen], sectors)
	}
	if err != nil {
		return err
	}
	if err := sub(src[start+baseLen:], subcode); err != nil {
		return err
	}
	for f := range frames {
		frame := dst[f*FrameSize : (f+1)*FrameSize]
		copy(frame, sectors[f*SectorSize:(f+1)*SectorSize])
		copy(frame[SectorSize:], subcode[f*SubcodeSize:(f+1)*SubcodeSize])
		if src[f/8]&(1<<(f%8)) != 0 {
			copy(frame, syncHeader[:])
			eccGenerate(frame[:SectorSize])
		}
	}
	return nil
}

// zstdDec decodes the zstd and cdzs codecs (MAME 0.249 and later); its
// DecodeAll may be used by several goroutines at once.
var zstdDec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecoderLowmem(true))

// zstdDecompress decodes a zstd frame that must fill dst exactly.
func zstdDecompress(src, dst []byte) error {
	out, err := zstdDec.DecodeAll(src, dst[:0:len(dst)])
	if err != nil {
		return err
	}
	if len(out) != len(dst) || (len(out) > 0 && &out[0] != &dst[0]) {
		return fmt.Errorf("chd: zstd hunk has %d bytes, want %d", len(out), len(dst))
	}
	return nil
}
