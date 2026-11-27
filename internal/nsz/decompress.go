package nsz

import (
	"context"
	"encoding/binary"
	"io"

	"github.com/klauspost/compress/zstd"

	"gameforge/internal/i18n"
)

// Decompress writes the original NCA of the NCZ stored at [off, off+size)
// in r to w. Both block and solid NCZ files are supported.
func Decompress(ctx context.Context, r io.ReaderAt, off, size int64, w io.Writer) error {
	head := make([]byte, HeaderSize)
	if _, err := r.ReadAt(head, off); err != nil {
		return err
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	sections, p, err := decodeSections(r, off+HeaderSize)
	if err != nil {
		return err
	}
	end := off + size

	var magic [8]byte
	if _, err := r.ReadAt(magic[:], p); err != nil {
		return err
	}
	if string(magic[:]) == "NCZBLOCK" {
		return decompressBlocks(ctx, r, p, end, sections, w)
	}
	return decompressSolid(ctx, io.NewSectionReader(r, p, end-p), sections, w)
}

// NCASize returns the size of the NCA that the NCZ at off restores, read
// from its section table.
func NCASize(r io.ReaderAt, off int64) (int64, error) {
	sections, _, err := decodeSections(r, off+HeaderSize)
	if err != nil {
		return 0, err
	}
	return ncaSize(sections), nil
}

// ncaSize is the size of the NCA the sections describe.
func ncaSize(sections []Section) int64 {
	size := int64(HeaderSize)
	for _, s := range sections {
		size = max(size, s.Offset+s.Size)
	}
	return size
}

func decompressBlocks(ctx context.Context, r io.ReaderAt, p, end int64, sections []Section, w io.Writer) error {
	var hdr [24]byte
	if _, err := r.ReadAt(hdr[:], p); err != nil {
		return err
	}
	exp := hdr[11]
	n := int64(binary.LittleEndian.Uint32(hdr[12:]))
	total := int64(binary.LittleEndian.Uint64(hdr[16:]))
	if exp < 14 || exp > 32 || n == 0 || (n-1)<<exp >= total || total > n<<exp || p+24+4*n > end {
		return i18n.Errorf("err.ncz.format")
	}
	list := make([]byte, 4*n)
	if _, err := r.ReadAt(list, p+24); err != nil {
		return err
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer dec.Close()

	blockSize := int64(1) << exp
	pos := p + 24 + 4*n
	abs := int64(HeaderSize)
	var buf []byte
	for i := range n {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := min(blockSize, total-i*blockSize)
		csize := int64(binary.LittleEndian.Uint32(list[4*i:]))
		if pos+csize > end {
			return i18n.Errorf("err.ncz.format")
		}
		buf = buf[:0]
		if csize < want {
			c := make([]byte, csize)
			if _, err := r.ReadAt(c, pos); err != nil {
				return err
			}
			if buf, err = dec.DecodeAll(c, buf); err != nil {
				return err
			}
		} else {
			buf = append(buf, make([]byte, want)...)
			if _, err := r.ReadAt(buf, pos); err != nil {
				return err
			}
		}
		if int64(len(buf)) != want {
			return i18n.Errorf("err.ncz.format")
		}
		applyCTR(sections, abs, buf)
		if _, err := w.Write(buf); err != nil {
			return err
		}
		pos += csize
		abs += want
	}
	return nil
}

// decompressSolid decompresses a single zstd stream. It must restore the
// whole NCA: zstd reports a cut-off stream as io.ErrUnexpectedEOF, which
// io.ReadFull also returns for the short last chunk of a complete one, so
// only the size tells a truncated NCZ.
func decompressSolid(ctx context.Context, r io.Reader, sections []Section, w io.Writer) error {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return err
	}
	defer dec.Close()
	buf := make([]byte, 4<<20)
	abs := int64(HeaderSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(dec, buf)
		if n > 0 {
			applyCTR(sections, abs, buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			abs += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if abs != ncaSize(sections) {
				return i18n.Errorf("err.ncz.format")
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}
