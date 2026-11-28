package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gameforge/internal/chd"
	"gameforge/internal/ciso"
	"gameforge/internal/discinfo"
	"gameforge/internal/retroach"
	"gameforge/internal/rvz"
)

// gdHighDensity is where a GD-ROM's high-density area starts, and
// gdEnd where it ends.
const (
	gdHighDensity = 45000
	gdEnd         = 549150
)

// discReader opens a disc image of the Discs tab for the RetroAchievements
// hashes: sectors by their address on the disc. done closes it.
func discReader(path string) (retroach.Disc, func(), error) {
	switch {
	case isCHD(path):
		return chdReader(path)
	case isISO(path):
		f, err := os.Open(path)
		if err != nil {
			return retroach.Disc{}, nil, err
		}
		read := func(lba int64) ([]byte, error) {
			b := make([]byte, 2048)
			_, err := f.ReadAt(b, lba*2048)
			return b, err
		}
		return retroach.Disc{Read: read}, func() { f.Close() }, nil
	case isCSO(path):
		r, err := ciso.Open(path)
		if err != nil {
			return retroach.Disc{}, nil, err
		}
		return retroach.Disc{Read: r.Sector}, func() { r.Close() }, nil
	case strings.EqualFold(filepath.Ext(path), ".gdi"):
		return gdiReader(path)
	}
	return cueReader(path)
}

// span is a data track's place on the disc, for reading sectors by
// their address.
type span struct {
	start, frames int64
	read          func(rel int64) ([]byte, error)
}

func readSpans(spans []span, start int64) retroach.Disc {
	return retroach.Disc{Start: start, Read: func(lba int64) ([]byte, error) {
		for _, s := range spans {
			if lba >= s.start && lba < s.start+s.frames {
				return s.read(lba - s.start)
			}
		}
		return nil, fmt.Errorf("sector %d is not in a data track", lba)
	}}
}

func chdReader(path string) (retroach.Disc, func(), error) {
	d, err := chd.OpenDisc(path)
	if err != nil {
		return retroach.Disc{}, nil, err
	}
	done := func() { d.Close() }
	if d.DVD {
		return retroach.Disc{Read: func(lba int64) ([]byte, error) { return d.ReadSector(0, int(lba)) }}, done, nil
	}
	data := func(i int) bool { _, ok := chd.UserDataOffset(d.Tracks[i].Type); return ok }
	if !d.GDROM {
		// The first data track holds the file system from sector 0.
		for i := range d.Tracks {
			if data(i) {
				return fileSystemDisc(func(rel int64) ([]byte, error) { return d.ReadSector(i, int(rel)) }), done, nil
			}
		}
		done()
		return retroach.Disc{}, nil, errors.New("no data track")
	}
	// GD-ROM: the high-density area starts with track 3 at sector 45000
	// and the tracks follow each other; the last ends at 549150.
	if len(d.Tracks) < 3 {
		done()
		return retroach.Disc{}, nil, errors.New("GD-ROM without track 3")
	}
	var spans []span
	pos := int64(gdHighDensity)
	for i := 2; i < len(d.Tracks); i++ {
		t := d.Tracks[i]
		if i > 2 {
			pos += int64(t.Pregap)
		}
		if data(i) {
			spans = append(spans, span{pos, int64(t.Frames), func(rel int64) ([]byte, error) { return d.ReadSector(i, int(rel)) }})
		}
		pos += int64(t.Frames + t.Pad)
	}
	if last := len(d.Tracks) - 1; data(last) && pos != gdEnd && len(spans) > 1 {
		spans[len(spans)-1].start = gdEnd - int64(d.Tracks[last].Frames)
	}
	return readSpans(spans, gdHighDensity), done, nil
}

func gdiReader(path string) (retroach.Disc, func(), error) {
	tracks, err := chd.ParseGDI(path)
	if err != nil {
		return retroach.Disc{}, nil, err
	}
	var files []*chd.Image
	done := func() {
		for _, f := range files {
			f.Close()
		}
	}
	var spans []span
	for _, t := range tracks {
		skip, ok := chd.UserDataOffset(t.Type)
		if !ok || t.LBA < gdHighDensity {
			continue
		}
		f, err := chd.OpenImage(t.File)
		if err != nil {
			done()
			return retroach.Disc{}, nil, err
		}
		files = append(files, f)
		size := int64(t.SectorStride())
		spans = append(spans, span{int64(t.LBA), int64(t.Frames), func(rel int64) ([]byte, error) {
			b := make([]byte, 2048)
			_, err := f.ReadAt(b, rel*size+int64(skip))
			return b, err
		}})
	}
	return readSpans(spans, gdHighDensity), done, nil
}

func cueReader(path string) (retroach.Disc, func(), error) {
	tracks, err := chd.ParseImage(path)
	if err != nil {
		return retroach.Disc{}, nil, err
	}
	if chd.IsGDROM(tracks) {
		return retroach.Disc{}, nil, errors.New(T("ra.gdrom_cue"))
	}
	for _, t := range tracks {
		skip, ok := chd.UserDataOffset(t.Type)
		if !ok {
			continue
		}
		f, err := chd.OpenImage(t.File)
		if err != nil {
			return retroach.Disc{}, nil, err
		}
		start := t.DataStart()
		size := int64(t.SectorStride())
		read := func(lba int64) ([]byte, error) {
			b := make([]byte, 2048)
			_, err := f.ReadAt(b, start+lba*size+int64(skip))
			return b, err
		}
		return fileSystemDisc(read), func() { f.Close() }, nil
	}
	return retroach.Disc{}, nil, errors.New("no data track")
}

// dreamcastIcon reads the disc label of a Dreamcast CHD or GDI, or
// returns nil.
func dreamcastIcon(path string) []byte {
	d, done, err := discReader(path)
	if err != nil {
		return nil
	}
	defer done()
	icon, _ := discinfo.DreamcastLabel(d.Read, d.Start)
	return icon
}

// raDiscJobs are the checks of disc images, with their consoles.
func raDiscJobs(items []*convItem) []raJob {
	var jobs []raJob
	for _, it := range items {
		console := retroach.DiscConsoles[it.platform]
		jobs = append(jobs, raJob{item: it, console: console, system: it.platform,
			run: func(ctx context.Context, db *retroach.DB) raOutcome {
				return raDiscCheck(db, it, console)
			}})
	}
	return jobs
}

// raDiscCheck checks one disc image.
func raDiscCheck(db *retroach.DB, it *convItem, console int) raOutcome {
	base := filepath.Base(it.path)
	switch {
	case console == 0 && it.platform == "":
		return raSkip(msg("ra.st_console"), msg("ra.skip_disc", base))
	case console == 0:
		return raSkip(msg("ra.st_console"), msg("ra.skip_platform", base, it.platform))
	case !db.Has(console):
		return raSkip(msg("ra.st_nolist"), msg("ra.skip_nolist", base, it.platform))
	}
	var hash string
	var err error
	if console == retroach.GameCube {
		hash, err = gameCubeHash(it.path)
	} else {
		d, done, oerr := discReader(it.path)
		if oerr != nil {
			return raSkip(msg("ra.st_error"), msg("drename.skip_error", base, oerr))
		}
		hash, err = retroach.HashDisc(console, d)
		done()
	}
	if err != nil {
		return raSkip(msg("ra.st_error"), msg("drename.skip_error", base, err))
	}
	return raResult(db, console, it.platform, base, hash, strings.TrimSuffix(base, filepath.Ext(base)))
}

// raConsoleName names a RetroAchievements console with the app's name.
func raConsoleName(c int) string {
	var names []string
	for name, n := range retroach.Consoles {
		if n == c {
			names = append(names, name)
		}
	}
	for name, n := range retroach.DiscConsoles {
		if n == c {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int { return len(a) - len(b) })
	if len(names) == 0 {
		return fmt.Sprint(c)
	}
	return names[0]
}

// fileSystemDisc reads a CD by the addresses its file system uses, given
// the sectors of its first data track. That track usually starts the
// disc, but on a Dreamcast CD-R or MIL-CD it may follow audio tracks or a
// first session, and its ISO 9660 file system then counts from the start
// of the disc. The track's place is found from the root directory, whose
// first record (".") holds the root's own address.
func fileSystemDisc(read func(rel int64) ([]byte, error)) retroach.Disc {
	pvd, err := read(16)
	if err != nil || !bytes.HasPrefix(pvd, []byte("\x01CD001")) {
		return retroach.Disc{Read: read}
	}
	root := int64(binary.LittleEndian.Uint32(pvd[158:]))
	isRoot := func(rel int64) bool {
		b, err := read(rel)
		return err == nil && b[0] >= 34 && int64(binary.LittleEndian.Uint32(b[2:])) == root && b[32] == 1 && b[33] == 0
	}
	if root < 17 || isRoot(root) {
		return retroach.Disc{Read: read}
	}
	for rel := int64(17); rel < min(root, 2000); rel++ {
		if isRoot(rel) {
			start := root - rel
			return retroach.Disc{Start: start, Read: func(lba int64) ([]byte, error) { return read(lba - start) }}
		}
	}
	return retroach.Disc{Read: read}
}

// gameCubeHash computes the RetroAchievements hash of a GameCube disc in
// an ISO, GCM or RVZ file.
func gameCubeHash(path string) (string, error) {
	var r interface {
		io.ReaderAt
		io.Closer
	}
	var err error
	switch {
	case isRVZ(path):
		r, err = rvz.Open(path)
	case isISO(path) || hasExt(path, []string{".gcm"}):
		r, err = os.Open(path)
	default:
		return "", errors.New(T("ra.gc_format"))
	}
	if err != nil {
		return "", err
	}
	defer r.Close()
	return retroach.HashGameCube(r)
}
