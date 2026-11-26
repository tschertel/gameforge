package main

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/flate"

	"gameforge/internal/sevenzip"
)

// romZip says whether the ROMs tab writes ZIP archives instead of 7z: some
// emulators list 7z among their formats but crash on it.
func romZip() bool { return sevenZip.ui != nil && sevenZip.ui.cfg.ROMArchive == "zip" }

// zipFile is a file being written into a ZIP archive: it keeps the CRC and
// size of what goes in, and stops when the run is cancelled.
type zipFile struct {
	ctx   context.Context
	w     io.Writer
	name  string
	crc   hash.Hash32
	n     int64
	count func(int)
}

func (z *zipFile) Write(p []byte) (int, error) {
	if err := z.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := z.w.Write(p)
	z.crc.Write(p[:n])
	z.n += int64(n)
	z.count(n)
	return n, err
}

// writeZip writes the ZIP archive for one file: the file itself, or the
// files inside a 7z, with their names and dates. It returns what it wrote,
// to check the archive against.
func writeZip(ctx context.Context, u *ui, path string, st os.FileInfo, out string,
	progress func(bool, int64, int64)) ([]sevenzip.Entry, error) {
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	bw := bufio.NewWriterSize(f, 4<<20)
	zw := zip.NewWriter(bw)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	var files []*zipFile
	var done, total int64
	add := func(name string, mod time.Time) (*zipFile, error) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mod})
		if err != nil {
			return nil, err
		}
		z := &zipFile{ctx: ctx, w: w, name: name, crc: crc32.NewIEEE(), count: func(n int) {
			done += int64(n)
			progress(false, done, total)
		}}
		files = append(files, z)
		return z, nil
	}

	if extOf(path) == ".7z" {
		list, err := sevenzip.List(path)
		if err != nil {
			return nil, err
		}
		var addErr error
		streamed := map[string]bool{}
		for _, e := range list {
			total += e.Size
		}
		mod := func(name string) time.Time {
			for _, e := range list {
				if e.Name == name && !e.Modified.IsZero() {
					return e.Modified
				}
			}
			return st.ModTime()
		}
		got, err := sevenzip.Extract(ctx, path, func(name string, size int64) io.Writer {
			z, err := add(name, mod(name))
			if err != nil {
				addErr = err
				return nil
			}
			streamed[name] = true
			return z
		}, nil)
		if err == nil {
			err = addErr
		}
		if err != nil {
			return nil, err
		}
		// Empty files have no data in a 7z, so Extract does not ask for them.
		for _, e := range got {
			if !e.Dir && e.Size == 0 && !streamed[e.Name] {
				if _, err := add(e.Name, mod(e.Name)); err != nil {
					return nil, err
				}
			}
		}
		// The 7z reader checks its own CRCs; this also covers the copy.
		crcs := map[string]uint32{}
		for _, e := range got {
			crcs[e.Name] = e.CRC
		}
		for _, z := range files {
			if z.n > 0 && z.crc.Sum32() != crcs[z.name] {
				return nil, fmt.Errorf("%s: CRC differs from the 7z's", z.name)
			}
		}
		u.logf("sz.zip_from_7z", len(files))
	} else {
		total = st.Size()
		z, err := add(filepath.Base(path), st.ModTime())
		if err != nil {
			return nil, err
		}
		in, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(z, in)
		in.Close()
		if err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := bw.Flush(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	entries := make([]sevenzip.Entry, len(files))
	for i, z := range files {
		entries[i] = sevenzip.Entry{Name: z.name, Size: z.n, CRC: z.crc.Sum32()}
	}
	return entries, nil
}

// checkZip reads the written ZIP back, to the end of each file (which
// checks its CRC), and compares its files with what was written.
func checkZip(ctx context.Context, path string, want []sevenzip.Entry, progress func(bool, int64, int64)) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) != len(want) {
		return fmt.Errorf("%d files instead of %d", len(zr.File), len(want))
	}
	var done, total int64
	for _, w := range want {
		total += w.Size
	}
	for i, zf := range zr.File {
		w := want[i]
		if zf.Name != w.Name || int64(zf.UncompressedSize64) != w.Size || zf.CRC32 != w.CRC {
			return fmt.Errorf("%s differs", w.Name)
		}
		r, err := zf.Open()
		if err != nil {
			return err
		}
		z := &zipFile{ctx: ctx, w: io.Discard, crc: crc32.NewIEEE(), count: func(n int) {
			done += int64(n)
			progress(true, done, total)
		}}
		_, err = io.Copy(z, r)
		r.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", w.Name, err)
		}
	}
	return nil
}
