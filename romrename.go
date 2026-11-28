package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	. "modernc.org/tk9.0"

	"gameforge/internal/datfile"
	"gameforge/internal/i18n"
	"gameforge/internal/nointro"
	"gameforge/internal/sevenzip"
)

// romDB is the local copy of the No-Intro databases, loaded on first use,
// and the label of the ROMs tab that shows its state.
var romDB struct {
	db     *nointro.DB
	err    error
	loaded bool
	status *TLabelWidget
}

func (u *ui) noIntroPath() string {
	return cachePath(u.cfg.Path(), nointro.FileName)
}

func (u *ui) noIntroDB() *nointro.DB {
	if !romDB.loaded {
		romDB.loaded = true
		db, err := nointro.Load(u.noIntroPath())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			u.logf("nointro.load_error", err)
			romDB.err = err
		}
		romDB.db = db
	}
	return romDB.db
}

func allSystems() []string {
	var out []string
	for _, s := range nointro.Systems {
		out = append(out, s.Name)
	}
	return out
}

// showNoIntroStatus shows which consoles the No-Intro database has and
// how current it is, next to its menu in the ROMs tab.
func (u *ui) showNoIntroStatus() {
	if romDB.status == nil {
		return
	}
	db := u.noIntroDB()
	var have []string
	newest := ""
	if db != nil {
		for _, s := range nointro.Systems {
			if l := db.Lists[s.Name]; l != nil {
				have = append(have, s.Name)
				newest = max(newest, strings.ReplaceAll(l.Version, ".", "-"))
			}
		}
	}
	consoles := strings.Join(have, ", ")
	switch {
	case len(have) == len(nointro.Systems):
		consoles = T("redump.all")
	case len(have) > 4:
		consoles = T("nointro.consoles", len(have))
	}
	var text string
	switch {
	case len(have) > 0 && time.Since(db.Oldest()) > redumpMaxAge:
		text = T("redump.status_old", consoles, newest, int(time.Since(db.Oldest()).Hours()/24))
	case len(have) == len(nointro.Systems):
		text = T("redump.status_all", newest) // short: the row is wide
	case len(have) > 0:
		text = T("redump.status", consoles, newest)
	case romDB.err != nil:
		text = T("redump.status_error")
	default:
		text = T("redump.status_none")
	}
	romDB.status.Configure(Txt(text))
}

// updateNoIntro downloads (or checks for changes) the No-Intro databases
// of all consoles.
func (u *ui) updateNoIntro() {
	if u.busy {
		return
	}
	u.separate()
	u.downloadNoIntro(allSystems(), func() {})
}

// downloadNoIntro downloads the No-Intro databases of the consoles in the
// background, then runs then on the GUI thread.
func (u *ui) downloadNoIntro(systems []string, then func()) {
	u.logf("nointro.download_start", strings.Join(systems, ", "))
	old := u.noIntroDB()
	u.runDownload(func(ctx context.Context, progress datfile.Progress) func() {
		db, err := nointro.Download(ctx, old, systems, progress)
		if err == nil {
			if serr := db.Save(u.noIntroPath()); serr != nil {
				u.logf("nointro.save_error", serr)
			}
			for _, s := range systems {
				if l := db.Lists[s]; l != nil {
					u.logf("nointro.loaded", s, len(l.Games), l.Version)
				}
			}
		} else if ctx.Err() == nil {
			u.logf("nointro.download_error", err)
		}
		return func() {
			if err == nil {
				romDB.db, romDB.err = db, nil
			}
			u.showNoIntroStatus()
		}
	}, then)
}

// romFile is a file of the ROMs tab as far as renaming goes: a ROM, or a
// ZIP or 7z archive with the ROM inside.
type romFile struct {
	item    *convItem
	inner   string // name of the ROM inside the archive; "" for a loose ROM
	files   int    // files inside the archive (1 for a loose ROM)
	size    int64
	crc     uint32
	haveCRC bool
	err     error
}

// ext is the extension that tells the console: the ROM's inside an archive.
func (r romFile) ext() string {
	if r.inner != "" {
		return strings.ToLower(filepath.Ext(r.inner))
	}
	return strings.ToLower(filepath.Ext(r.item.path))
}

// open reads the ROM from the start. A ROM in a 7z archive is decompressed
// as it is read (usually it is identified by the CRC its header records,
// without this); closing the reader stops the decompression.
func (r romFile) open() func() (io.ReadCloser, error) {
	path := r.item.path
	switch strings.ToLower(filepath.Ext(path)) {
	case ".7z":
		if r.inner == "" {
			return nil
		}
		return func() (io.ReadCloser, error) {
			pr, pw := io.Pipe()
			go func() {
				defer func() {
					if p := recover(); p != nil {
						pw.CloseWithError(fmt.Errorf("internal error: %v", p))
					}
				}()
				_, err := sevenzip.Extract(context.Background(), path, func(name string, size int64) io.Writer {
					if name == r.inner {
						return pw
					}
					return nil
				}, nil)
				pw.CloseWithError(err)
			}()
			return pr, nil
		}
	case ".zip":
		return func() (io.ReadCloser, error) {
			zr, files, err := openZip(path)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(files, func(f *zip.File) bool { return f.Name == r.inner })
			if i < 0 {
				zr.Close()
				return nil, fmt.Errorf("%s: not found", r.inner)
			}
			rc, err := files[i].Open()
			if err != nil {
				zr.Close()
				return nil, err
			}
			return closers{rc, rc, zr}, nil
		}
	}
	return func() (io.ReadCloser, error) { return os.Open(path) }
}

type closers struct {
	io.Reader
	a, b io.Closer
}

func (c closers) Close() error {
	err := c.a.Close()
	if err2 := c.b.Close(); err == nil {
		err = err2
	}
	return err
}

// scanRom reads what a file holds; for archives, only their headers.
// Inside an archive, notes, images and other side files (such as a
// download site's .txt) do not count: one ROM with them is one ROM.
func scanRom(it *convItem) romFile {
	r := romFile{item: it, files: 1}
	switch strings.ToLower(filepath.Ext(it.path)) {
	case ".zip":
		zr, files, err := openZip(it.path)
		if err != nil {
			r.err = err
			return r
		}
		zr.Close()
		files = slices.DeleteFunc(files, func(f *zip.File) bool { return sideExts[extOf(f.Name)] })
		r.files = len(files)
		if len(files) == 1 {
			f := files[0]
			r.inner, r.size, r.crc, r.haveCRC = f.Name, int64(f.UncompressedSize64), f.CRC32, true
		}
	case ".7z":
		entries, err := sevenzip.List(it.path)
		if err != nil {
			r.err = err
			return r
		}
		var files []sevenzip.Entry
		for _, e := range entries {
			if !e.Dir && !sideExts[extOf(e.Name)] {
				files = append(files, e)
			}
		}
		r.files = len(files)
		if len(files) == 1 {
			e := files[0]
			r.inner, r.size, r.crc, r.haveCRC = e.Name, e.Size, e.CRC, true
		}
	default:
		st, err := os.Stat(it.path)
		if err != nil {
			r.err = err
			return r
		}
		r.size = st.Size()
	}
	return r
}

// romItems returns the checked ROMs, ZIPs and 7z files of the ROMs tab
// (not saves and other side files), or false after telling the user that
// there are none.
func (t *convTab) romItems() ([]*convItem, bool) {
	var items []*convItem
	for _, it := range t.items {
		if markable(it) && !sideExts[extOf(it.path)] {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("rrename.empty")))
		return nil, false
	}
	return t.keepChecked(items)
}

// renameRoms gives the checked ROMs, ZIPs and 7z files of the ROMs tab their
// No-Intro names, downloading the databases of their consoles first if
// the user agrees, and showing the changes before renaming.
func (t *convTab) renameRoms(u *ui) {
	if u.busy {
		return
	}
	items, ok := t.romItems()
	if !ok {
		return
	}
	var roms []romFile
	for _, it := range items {
		roms = append(roms, scanRom(it))
	}
	missing := missingLists(u.noIntroDB(), roms)
	if len(missing) > 0 {
		answer := MessageBox(Parent(App), Icon("question"), Type("yesno"), Title(T("app.title")),
			Msg(T("rrename.ask_db", strings.Join(missing, ", "))), Detail(T("rrename.ask_db.detail", nointro.Source)))
		if answer == "yes" {
			u.separate()
			u.downloadNoIntro(missing, func() { t.planRomRenames(u, roms) })
			return
		}
	}
	t.planRomRenames(u, roms)
}

// missingLists returns the consoles of the ROMs whose database is not
// there yet.
func missingLists(db *nointro.DB, roms []romFile) []string {
	var missing []string
	for _, r := range roms {
		if s := nointro.SystemOf(r.ext()); s != "" && r.files == 1 && !db.Has(s) && !slices.Contains(missing, s) {
			missing = append(missing, s)
		}
	}
	return missing
}

// planRomRenames finds the new names in the background (loose ROMs are
// read whole for their CRC), then shows them and renames.
func (t *convTab) planRomRenames(u *ui, roms []romFile) {
	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)
	db := u.noIntroDB()
	u.separate()
	u.logf("drename.start", len(roms))
	go func() {
		defer u.recoverPanic()
		defer cancel()
		var plan []discRename
		for _, r := range roms {
			if ctx.Err() != nil {
				break
			}
			name := filepath.Base(r.item.path)
			u.setStatus(T("drename.status", name))
			newName, skip := romName(ctx, u, db, r)
			switch {
			case skip != nil:
				u.println(skip)
			case newName == name:
				u.println(i18n.M("drename.same", name))
			case newName != "":
				plan = append(plan, discRename{r.item, filepath.Join(filepath.Dir(r.item.path), newName)})
			}
		}
		u.setProgress(0)
		u.setStatus("")
		cancelled := ctx.Err() != nil
		onGUI(func() {
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
			u.flush()
			if cancelled {
				u.logf("log.cancelled")
				u.flush()
				return
			}
			t.applyRenames(u, plan)
		})
	}()
}

// romName returns the new file name of a ROM or archive, or why it keeps
// its name.
func romName(ctx context.Context, u *ui, db *nointro.DB, r romFile) (string, fmt.Stringer) {
	m, skip := romGame(ctx, u, db, &r)
	if m.Name == "" {
		return "", skip
	}
	return fileSafe(m.Name) + filepath.Ext(r.item.path), nil
}

// romGame identifies a ROM or archive in the databases, reading a loose ROM
// whole for its CRC (which it then keeps in r), or tells why it cannot.
// Both are empty if ctx was cancelled.
func romGame(ctx context.Context, u *ui, db *nointro.DB, r *romFile) (nointro.Match, fmt.Stringer) {
	base := filepath.Base(r.item.path)
	switch {
	case r.err != nil:
		return nointro.Match{}, i18n.M("drename.skip_error", base, r.err)
	case r.files == 0:
		return nointro.Match{}, i18n.M("rrename.skip_empty", base)
	case r.files > 1:
		return nointro.Match{}, i18n.M("rrename.skip_set", base, r.files)
	}
	progress := func(done int64) {
		u.setProgress(float64(done) / float64(max(r.size, 1)))
		u.setStatus(T("rrename.crc", base, int(done*100/max(r.size, 1))))
	}
	open := r.open()
	if !r.haveCRC {
		rc, err := open()
		if err != nil {
			return nointro.Match{}, i18n.M("drename.skip_error", base, err)
		}
		r.crc, r.size, err = nointro.CRC(ctx, rc, nointro.Fix{}, progress)
		rc.Close()
		if err != nil {
			if ctx.Err() != nil {
				return nointro.Match{}, nil
			}
			return nointro.Match{}, i18n.M("drename.skip_error", base, err)
		}
		r.haveCRC = true
	}
	matches, _, err := db.Identify(ctx, r.crc, r.size, r.ext(), open, progress)
	u.setProgress(0)
	if err != nil {
		if ctx.Err() != nil {
			return nointro.Match{}, nil
		}
		return nointro.Match{}, i18n.M("drename.skip_error", base, err)
	}
	system := nointro.SystemOf(r.ext())
	switch {
	case len(matches) == 0 && system != "" && !db.Has(system):
		return nointro.Match{}, i18n.M("rrename.skip_nodb", base, system)
	case len(matches) == 0:
		return nointro.Match{}, i18n.M("rrename.skip_unknown", base)
	case len(matches) > 1 && !(system != "" && matches[0].System == system && matches[1].System != system):
		var names []string
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return nointro.Match{}, i18n.M("rrename.skip_ambiguous", base, strings.Join(names, " / "))
	}
	return matches[0], nil
}
