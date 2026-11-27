// Package chd writes and reads MAME CHD (Compressed Hunks of Data) version 5
// files, the compressed disc image format of MAME and of emulators such as
// DuckStation, PCSX2, Flycast and the libretro cores.
//
// A CHD stores the disc as fixed-size hunks, each compressed with the best of
// up to four codecs listed in the header; hunks equal to an earlier one are
// stored as a reference to it. A map, itself compressed, gives the codec,
// size, position and CRC-16 of every hunk. Metadata entries describe the
// tracks of a CD.
package chd

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

const (
	headerSize     = 124
	metaHeaderSize = 16
	mapHeaderSize  = 16
	mapEntrySize   = 12

	// CD frames: a 2352-byte raw sector followed by 96 bytes of subcode.
	SectorSize    = 2352
	SubcodeSize   = 96
	FrameSize     = SectorSize + SubcodeSize
	FramesPerHunk = 8
	trackPadding  = 4 // tracks are padded to a multiple of 4 frames

	// DVDSectorSize is the sector size of DVD images (ISO).
	DVDSectorSize = 2048
	dvdHunkBytes  = 2 * DVDSectorSize

	metaChecksum = 0x01 // metadata flag: part of the overall SHA-1
)

func fourCC(s string) uint32 { return binary.BigEndian.Uint32([]byte(s)) }

var (
	codecZlib   = fourCC("zlib")
	codecLZMA   = fourCC("lzma")
	codecCDZlib = fourCC("cdzl")
	codecCDLZMA = fourCC("cdlz")
	codecCDFLAC = fourCC("cdfl")
	codecFLAC   = fourCC("flac")
	codecZstd   = fourCC("zstd")
	codecCDZstd = fourCC("cdzs")
	codecHuff   = fourCC("huff")

	tagCDTrack = fourCC("CHT2")
	tagGDTrack = fourCC("CHGD")
	tagDVD     = fourCC("DVD ")
)

func codecName(c uint32) string {
	if c == 0 {
		return "none"
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], c)
	return string(b[:])
}

// Hunk compression types in the map. Types 0-3 select a codec of the header.
const (
	compType0 = iota
	compType1
	compType2
	compType3
	compNone
	compSelf
	compParent
	compRLESmall
	compRLELarge
	compSelf0
	compSelf1
	compParentSelf
	compParent0
	compParent1
)

// crc16 is the CRC-16/CCITT used by CHD for hunks and the map.
func crc16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc = crc<<8 ^ crc16Table[byte(crc>>8)^b]
	}
	return crc
}

var crc16Table = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return
}()

// bitWriter packs values most significant bit first.
type bitWriter struct {
	buf []byte
	acc uint64
	n   uint
}

func (w *bitWriter) write(v uint64, nbits uint) {
	for nbits > 0 {
		k := min(nbits, 32)
		nbits -= k
		w.acc = w.acc<<k | (v>>nbits)&(1<<k-1)
		w.n += k
		for w.n >= 8 {
			w.n -= 8
			w.buf = append(w.buf, byte(w.acc>>w.n))
		}
	}
}

func (w *bitWriter) bytes() []byte {
	if w.n > 0 {
		w.buf = append(w.buf, byte(w.acc<<(8-w.n)))
		w.n = 0
	}
	return w.buf
}

// The map's compression types are Huffman coded with 16 symbols of at most
// 8 bits.
const (
	huffCodes   = 16
	huffMaxBits = 8
)

// huffLengths returns code lengths of at most maxBits for the symbol
// counts: a Huffman code, with the counts flattened until it fits.
func huffLengths(counts []uint32, maxBits int) []uint8 {
	c := append([]uint32(nil), counts...)
	for {
		lengths := huffman(c)
		longest := uint8(0)
		for _, l := range lengths {
			longest = max(longest, l)
		}
		if int(longest) <= maxBits {
			return lengths
		}
		for i := range c {
			if c[i] > 0 {
				c[i] = (c[i] + 1) / 2
			}
		}
	}
}

func huffman(counts []uint32) []uint8 {
	type node struct {
		weight uint64
		syms   []int
	}
	var nodes []node
	for s, c := range counts {
		if c > 0 {
			nodes = append(nodes, node{uint64(c), []int{s}})
		}
	}
	lengths := make([]uint8, len(counts))
	if len(nodes) == 1 {
		lengths[nodes[0].syms[0]] = 1
		return lengths
	}
	for len(nodes) > 1 {
		// Merge the two lightest nodes; every symbol below gets one bit longer.
		a, b := 0, 1
		if nodes[b].weight < nodes[a].weight {
			a, b = b, a
		}
		for i := 2; i < len(nodes); i++ {
			switch {
			case nodes[i].weight < nodes[a].weight:
				a, b = i, a
			case nodes[i].weight < nodes[b].weight:
				b = i
			}
		}
		merged := node{nodes[a].weight + nodes[b].weight, append(append([]int(nil), nodes[a].syms...), nodes[b].syms...)}
		for _, s := range merged.syms {
			lengths[s]++
		}
		lo, hi := min(a, b), max(a, b)
		nodes = append(nodes[:hi], nodes[hi+1:]...)
		nodes[lo] = merged
	}
	return lengths
}

// canonicalCodes assigns codes to lengths as MAME's huffman.cpp does: the
// longest codes get the lowest values.
func canonicalCodes(lengths []uint8) ([]uint32, error) {
	var histo [33]uint32
	for _, l := range lengths {
		histo[l]++
	}
	start := uint32(0)
	for l := 32; l > 0; l-- {
		next := (start + histo[l]) >> 1
		if l != 1 && next*2 != start+histo[l] {
			return nil, fmt.Errorf("chd: invalid Huffman code lengths")
		}
		histo[l] = start
		start = next
	}
	codes := make([]uint32, len(lengths))
	for i, l := range lengths {
		if l > 0 {
			codes[i] = histo[l]
			histo[l]++
		}
	}
	return codes, nil
}

// writeHuffTree stores the code lengths the way huffman_import_tree_rle
// reads them: 4 bits each for a maximum of 8 bits, with the value 1
// escaped as 1,1.
func writeHuffTree(w *bitWriter, lengths []uint8) {
	for _, l := range lengths {
		w.write(uint64(l), 4)
		if l == 1 {
			w.write(1, 4)
		}
	}
}

// mapEntry is a hunk's entry in the decoded map.
type mapEntry struct {
	typ    uint8  // compType0..3, compNone or compSelf
	length uint32 // compressed length (hunk size for compNone)
	offset uint64 // file offset, or the referenced hunk for compSelf
	crc    uint16 // CRC-16 of the uncompressed hunk
}

func rawMap(entries []mapEntry) []byte {
	raw := make([]byte, len(entries)*mapEntrySize)
	for i, e := range entries {
		b := raw[i*mapEntrySize:]
		b[0] = e.typ
		b[1], b[2], b[3] = byte(e.length>>16), byte(e.length>>8), byte(e.length)
		for k := range 6 {
			b[4+k] = byte(e.offset >> (40 - 8*k))
		}
		binary.BigEndian.PutUint16(b[10:], e.crc)
	}
	return raw
}

func bitsFor(v uint64) uint { return uint(bits.Len64(v)) }

// encodeMap compresses the map as chd_file::compress_v5_map: Huffman-coded
// types with run-length codes, then the lengths, CRCs and references.
// firstOffs is the file offset of the first stored hunk.
func encodeMap(entries []mapEntry, firstOffs uint64) []byte {
	mapCRC := crc16(rawMap(entries))

	// Pseudo types for references to the previous referenced hunk or the
	// one after it.
	types := make([]uint8, len(entries))
	var lastSelf, maxSelf uint64
	var maxLen uint32
	for i, e := range entries {
		t := e.typ
		switch t {
		case compSelf:
			switch e.offset {
			case lastSelf:
				t = compSelf0
			case lastSelf + 1:
				t = compSelf1
			default:
				maxSelf = max(maxSelf, e.offset)
			}
			lastSelf = e.offset
		case compType0, compType1, compType2, compType3:
			maxLen = max(maxLen, e.length)
		}
		types[i] = t
	}

	// Symbols: each run of a type is the type once, then run-length codes
	// that repeat it (RLE_SMALL n: 3+n more, RLE_LARGE h l: 19+16h+l more).
	var syms []uint8
	for i := 0; i < len(types); {
		t := types[i]
		j := i + 1
		for j < len(types) && types[j] == t {
			j++
		}
		syms = append(syms, t)
		for rest := j - i - 1; rest > 0; {
			switch {
			case rest >= 19:
				n := min(rest, 19+255)
				syms = append(syms, compRLELarge, uint8((n-19)>>4), uint8((n-19)&15))
				rest -= n
			case rest >= 3:
				syms = append(syms, compRLESmall, uint8(rest-3))
				rest = 0
			default:
				syms = append(syms, t)
				rest--
			}
		}
		i = j
	}
	counts := make([]uint32, huffCodes)
	for _, s := range syms {
		counts[s]++
	}
	lengths := huffLengths(counts, huffMaxBits)
	codes, err := canonicalCodes(lengths)
	if err != nil {
		panic(err) // huffman always produces a complete code
	}

	lengthBits, selfBits := bitsFor(uint64(maxLen)), bitsFor(maxSelf)
	var w bitWriter
	writeHuffTree(&w, lengths)
	for _, s := range syms {
		w.write(uint64(codes[s]), uint(lengths[s]))
	}
	for i, e := range entries {
		switch types[i] {
		case compType0, compType1, compType2, compType3:
			w.write(uint64(e.length), lengthBits)
			w.write(uint64(e.crc), 16)
		case compNone:
			w.write(uint64(e.crc), 16)
		case compSelf:
			w.write(e.offset, selfBits)
		}
	}
	data := w.bytes()

	out := make([]byte, mapHeaderSize, mapHeaderSize+len(data))
	binary.BigEndian.PutUint32(out[0:], uint32(len(data)))
	for k := range 6 {
		out[4+k] = byte(firstOffs >> (40 - 8*k))
	}
	binary.BigEndian.PutUint16(out[10:], mapCRC)
	out[12], out[13], out[14] = byte(lengthBits), byte(selfBits), 0
	return append(out, data...)
}

// decodeMap reverses encodeMap, as libchdr's decompress_v5_map does.
func decodeMap(hdr, data []byte, hunks int, hunkBytes uint32) ([]mapEntry, error) {
	firstOffs := uint64(0)
	for k := range 6 {
		firstOffs = firstOffs<<8 | uint64(hdr[4+k])
	}
	mapCRC := binary.BigEndian.Uint16(hdr[10:])
	lengthBits, selfBits, parentBits := uint(hdr[12]), uint(hdr[13]), uint(hdr[14])

	r := &bitReader{data: data}
	dec, err := readHuffTree(r)
	if err != nil {
		return nil, err
	}
	types := make([]uint8, hunks)
	var last uint8
	rep := 0
	for i := range types {
		if rep > 0 {
			types[i] = last
			rep--
			continue
		}
		switch v := uint8(dec.decode(r)); v {
		case compRLESmall:
			types[i], rep = last, 2+int(dec.decode(r))
		case compRLELarge:
			types[i], rep = last, 2+16+int(dec.decode(r))<<4
			rep += int(dec.decode(r))
		default:
			types[i], last = v, v
		}
	}

	entries := make([]mapEntry, hunks)
	cur := firstOffs
	var lastSelf uint64
	for i, t := range types {
		e := &entries[i]
		switch t {
		case compType0, compType1, compType2, compType3:
			e.typ, e.offset = t, cur
			e.length = uint32(r.read(lengthBits))
			e.crc = uint16(r.read(16))
			cur += uint64(e.length)
		case compNone:
			e.typ, e.offset, e.length = t, cur, hunkBytes
			e.crc = uint16(r.read(16))
			cur += uint64(hunkBytes)
		case compSelf:
			lastSelf = r.read(selfBits)
			e.typ, e.offset = compSelf, lastSelf
		case compSelf1:
			lastSelf++
			e.typ, e.offset = compSelf, lastSelf
		case compSelf0:
			e.typ, e.offset = compSelf, lastSelf
		case compParent:
			r.read(parentBits)
			return nil, fmt.Errorf("chd: CHDs with a parent are not supported")
		default:
			return nil, fmt.Errorf("chd: unsupported hunk type %d", t)
		}
	}
	if r.overflow {
		return nil, fmt.Errorf("chd: truncated map")
	}
	if crc16(rawMap(entries)) != mapCRC {
		return nil, fmt.Errorf("chd: map CRC mismatch")
	}
	return entries, nil
}
