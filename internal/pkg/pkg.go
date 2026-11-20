package pkg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Kinds of content.
const (
	KindGame   = "game"
	KindUpdate = "update"
	KindDLC    = "dlc"
	KindTheme  = "theme"
	KindOther  = "other"
)

// Package is an opened PKG file.
type Package struct {
	Path      string
	Size      int64
	Platform  string // "PS4", "PS3", "PSP" or "PS Vita"
	ContentID string // e.g. "EP9000-CUSA00001_00-0000000000000000"
	TitleID   string // e.g. "CUSA00001", from the content ID
	Kind      string
	SFO       SFO    // nil when the PARAM.SFO is encrypted and no key is loaded
	Icon      []byte // icon0.png, if readable
	// Debug is a PS3/PSP/Vita package made with a debug (not retail) key,
	// which needs no key file.
	Debug bool
	// Fake is a PS4 fake package (fPKG): its game files can be extracted.
	Fake bool
	// NeedKey names the key in pkg.keys that the package needs but that is
	// not loaded; then only the header is read.
	NeedKey string
	// Files is how many files extraction writes.
	Files int

	ps4 *ps4Pkg
	ps3 *ps3Pkg
}

// Open reads what a PKG holds. keys may be nil; then PS3, PSP and Vita
// retail packages show only what their header says.
func Open(path string, keys *Keys) (*Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var magic [4]byte
	if _, err := f.ReadAt(magic[:], 0); err != nil {
		return nil, errors.New("not a PKG file")
	}
	p := &Package{Path: path, Size: st.Size()}
	switch string(magic[:]) {
	case "\x7fCNT":
		err = p.openPS4(f)
	case "\x7fPKG":
		err = p.openPS3(f, keys)
	default:
		return nil, errors.New("not a PKG file")
	}
	if err != nil {
		return nil, err
	}
	if p.SFO != nil {
		if id := p.SFO["TITLE_ID"]; p.TitleID == "" && id != "" {
			p.TitleID = id
		}
	}
	return p, nil
}

// titleIDOf takes the title ID out of a content ID
// ("UP0001-NPUB30001_00-…" → "NPUB30001").
func titleIDOf(contentID string) string {
	if len(contentID) >= 16 && contentID[6] == '-' {
		return contentID[7:16]
	}
	return ""
}

// Title is the name in PARAM.SFO, or "".
func (p *Package) Title() string { return p.SFO.Title() }

// Version is the version in PARAM.SFO, or "".
func (p *Package) Version() string { return p.SFO.Version() }

// CanExtract reports whether Extract can write anything.
func (p *Package) CanExtract() bool { return p.NeedKey == "" && p.Files > 0 }

// Extract writes the package's files into dir, which must not exist yet.
// progress reports the bytes written. It returns how many were written.
func (p *Package) Extract(ctx context.Context, dir string, progress func(done, total int64)) (int64, error) {
	if !p.CanExtract() {
		if p.NeedKey != "" {
			return 0, fmt.Errorf("key %q missing from pkg.keys", p.NeedKey)
		}
		return 0, errors.New("nothing to extract")
	}
	if _, err := os.Stat(dir); err == nil {
		return 0, fmt.Errorf("%s: already exists", filepath.Base(dir))
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var n int64
	switch {
	case p.ps4 != nil:
		n, err = p.ps4.extract(ctx, f, dir, progress)
	case p.ps3 != nil:
		n, err = p.ps3.extract(ctx, f, dir, progress)
	}
	if err != nil {
		os.RemoveAll(dir)
	}
	return n, err
}

// StandardName is the file name the package gets when renamed, with its
// extension, or "" when the title is not known:
//
//	Title [CUSA00001][v1.00].pkg
//	Title [CUSA00001][Update][v1.05].pkg
//	DLC name [CUSA00001][DLC][LABEL].pkg
func (p *Package) StandardName() string {
	title := cleanTitle(p.Title())
	if title == "" || p.TitleID == "" {
		return ""
	}
	name := title + " [" + p.TitleID + "]"
	switch p.Kind {
	case KindUpdate:
		name += "[Update]"
	case KindDLC:
		name += "[DLC]"
		if l := p.label(); l != "" {
			name += "[" + l + "]"
		}
	case KindTheme:
		name += "[Theme]"
	}
	if v := p.Version(); v != "" && p.Kind != KindDLC {
		name += "[v" + v + "]"
	}
	return sanitize(name) + ".pkg"
}

// FolderName is the name of the folder a package is extracted into.
func (p *Package) FolderName() string {
	if n := p.StandardName(); n != "" {
		return strings.TrimSuffix(n, ".pkg")
	}
	if p.ContentID != "" {
		return sanitize(p.ContentID)
	}
	return strings.TrimSuffix(filepath.Base(p.Path), filepath.Ext(p.Path))
}

// label is the last part of the content ID, which tells DLCs of a game apart.
func (p *Package) label() string {
	if len(p.ContentID) == 36 && p.ContentID[19] == '-' {
		return strings.TrimRight(p.ContentID[20:], "\x00 ")
	}
	return ""
}

// cleanTitle makes a title fit for a file name: "A: B" becomes "A - B",
// trademark signs go, and runs of spaces become one.
func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ": ", " - ")
	s = strings.NewReplacer("™", "", "®", "", "©", "", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, ". ")
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
}

// safePath joins a file name from a package to dir, refusing names that
// would leave it.
func safePath(dir, name string) (string, error) {
	name = strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", fmt.Errorf("bad file name in package: %q", name)
	}
	return filepath.Join(dir, clean), nil
}

// copyN copies n bytes, checking ctx and reporting progress.
func copyN(ctx context.Context, w io.Writer, r io.Reader, n int64, report func(int64)) error {
	buf := make([]byte, 1<<20)
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		k := int64(len(buf))
		if k > n {
			k = n
		}
		if _, err := io.ReadFull(r, buf[:k]); err != nil {
			return err
		}
		if _, err := w.Write(buf[:k]); err != nil {
			return err
		}
		n -= k
		report(k)
	}
	return nil
}
