package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	. "modernc.org/tk9.0"

	"gameforge/internal/nointro"
	"gameforge/internal/retroach"
	"gameforge/internal/sevenzip"
)

// raState is the local copy of the RetroAchievements game lists, loaded
// on first use.
var raState struct {
	db     *retroach.DB
	loaded bool
}

func (u *ui) raPath() string { return cachePath(u.cfg.Path(), retroach.FileName) }

func (u *ui) raDB() *retroach.DB {
	if !raState.loaded {
		raState.loaded = true
		db, err := retroach.Load(u.raPath())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			u.logf("ra.load_error", err)
		}
		raState.db = db
	}
	return raState.db
}

// raSettingsURL is where users find their web API key.
const raSettingsURL = "https://retroachievements.org/settings"

// askRAKey asks for the user's RetroAchievements web API key and saves it;
// it reports whether there is a key afterwards.
func (u *ui) askRAKey() bool {
	top := Toplevel()
	path := top.String()
	tcl("wm", "withdraw", path)
	top.WmTitle("RetroAchievements")
	tcl("wm", "transient", path, ".")
	tcl("wm", "resizable", path, "0", "0")
	tcl(path, "configure", "-background", tcl(".", "cget", "-background"))

	f := top.TFrame(Padding("5m 4m"))
	Grid(f, Row(0), Column(0), Sticky("news"))
	Grid(f.TLabel(Txt(T("ra.key_text")), Wraplength("110m"), Justify("left")), Row(0), Column(0), Columnspan(3), Sticky("w"))
	link := f.TLabel(Txt(raSettingsURL), Foreground(linkColor()), Font("GameForgeLink"), Cursor("hand2"))
	Bind(link, "<Button-1>", Command(func() { openURL(raSettingsURL) }))
	Grid(link, Row(1), Column(0), Columnspan(3), Sticky("w"), Pady("2m"))
	entry := f.TEntry(Textvariable(u.cfg.RAKey), Show("•"), Width(44))
	Grid(entry, Row(2), Column(0), Columnspan(3), Sticky("we"), Pady("1m 4m"))

	saved := false
	closeWin := func() {
		tcl("grab", "release", path)
		Destroy(top)
	}
	save := func() {
		u.cfg.RAKey = strings.TrimSpace(entry.Textvariable())
		u.saveConfig()
		saved = true
		closeWin()
	}
	ok := f.TButton(Txt("OK"), Default("active"), Command(save))
	cancel := f.TButton(Txt(T("btn.cancel")), Command(closeWin))
	Grid(ok, Row(3), Column(1), Sticky("e"), Padx("0 2m"))
	Grid(cancel, Row(3), Column(2), Sticky("e"))
	tcl("grid", "columnconfigure", f.String(), "0", "-weight", "1")
	Bind(top, "<Return>", Command(save))
	Bind(top, "<Escape>", Command(closeWin))
	WmProtocol(top.Window, WM_DELETE_WINDOW, Command(closeWin))

	centerOver(path)
	tcl("focus", entry.String())
	tcl("catch", "grab set "+path)
	tcl("tkwait", "window", path)
	if saved {
		u.logf(map[bool]string{true: "ra.key_saved", false: "ra.key_removed"}[u.cfg.RAKey != ""])
		u.flush()
	}
	return u.cfg.RAKey != ""
}

// centerOver shows a window centered over the main one.
func centerOver(path string) {
	tcl("update", "idletasks")
	num := func(words ...string) int {
		var v int
		fmt.Sscan(tcl(words...), &v)
		return v
	}
	x := num("winfo", "rootx", ".") + (num("winfo", "width", ".")-num("winfo", "reqwidth", path))/2
	y := num("winfo", "rooty", ".") + (num("winfo", "height", ".")-num("winfo", "reqheight", path))/3
	tcl("wm", "geometry", path, fmt.Sprintf("+%d+%d", max(x, 0), max(y, 0)))
	tcl("wm", "deiconify", path)
}

// raConsole is the RetroAchievements console of a ROM, from its extension
// (the ROM's inside an archive), with the app's name for it.
func raConsole(r romFile) (int, string) {
	system := nointro.SystemOf(r.ext())
	return retroach.Consoles[system], system
}

// updateRA downloads again the RetroAchievements lists already there, or
// asks for the key first.
func (u *ui) updateRA() {
	if u.busy {
		return
	}
	if u.cfg.RAKey == "" && !u.askRAKey() {
		return
	}
	var consoles []int
	if db := u.raDB(); db != nil {
		for c := range db.Lists {
			consoles = append(consoles, c)
		}
	}
	if len(consoles) == 0 {
		MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("ra.update_none")))
		return
	}
	slices.Sort(consoles)
	u.separate()
	u.downloadRA(consoles, func() {})
}

// downloadRA downloads the game lists of the consoles in the background,
// then runs then on the GUI thread (also after a failure).
func (u *ui) downloadRA(consoles []int, then func()) {
	names := map[int]string{}
	for _, c := range consoles {
		names[c] = raConsoleName(c)
	}
	var list []string
	for _, c := range consoles {
		list = append(list, names[c])
	}
	u.logf("ra.download_start", strings.Join(list, ", "))
	old := u.raDB()
	key := u.cfg.RAKey
	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)
	go func() {
		defer u.recoverPanic()
		defer cancel()
		i := 0
		db, err := retroach.Download(ctx, old, key, consoles, func(c int) {
			u.setProgress(float64(i) / float64(len(consoles)))
			u.setStatus(T("ra.downloading", names[c]))
			i++
		})
		switch {
		case err == nil:
			if serr := db.Save(u.raPath()); serr != nil {
				u.logf("ra.save_error", serr)
			}
			for _, c := range consoles {
				if l := db.Lists[c]; l != nil {
					u.logf("ra.loaded", names[c], len(l.Games))
				}
			}
		case errors.Is(err, retroach.ErrKey):
			u.logf("ra.key_refused")
		case ctx.Err() == nil:
			u.logf("ra.download_error", err)
		}
		u.setProgress(0)
		u.setStatus("")
		cancelled := ctx.Err() != nil
		onGUI(func() {
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
			if err == nil {
				raState.db = db
			}
			if cancelled {
				u.logf("log.cancelled")
				u.flush()
				return
			}
			u.flush()
			then()
		})
	}()
}

// raJob is a file to check against RetroAchievements: its console
// (0 when unknown) and how to check it.
type raJob struct {
	item    *convItem
	console int
	system  string
	run     func(ctx context.Context, db *retroach.DB) raOutcome
}

// raOutcome is what the check of one file found: the status for its row,
// the line for the log (nil for none) and, for the report, the game found.
type raOutcome struct {
	status, line fmt.Stringer
	ok           bool   // RetroAchievements recognizes this version
	other        bool   // RetroAchievements has the game, in other versions only
	hash         string // "" when not computed
	game         *retroach.Game
}

// raSkip is the outcome of a file that was not looked up.
func raSkip(status, line fmt.Stringer) raOutcome { return raOutcome{status: status, line: line} }

// linkStatus is a status with a web page, opened by a click on the status
// column; the address is shown after the text.
type linkStatus struct {
	fmt.Stringer
	url string
}

func (l linkStatus) String() string {
	return l.Stringer.String() + "  ↗ " + strings.TrimPrefix(l.url, "https://")
}

// statusLink is the web page of a status, or "".
func statusLink(s fmt.Stringer) string {
	if l, ok := s.(linkStatus); ok {
		return l.url
	}
	return ""
}

// statusText is a status without its web address.
func statusText(s fmt.Stringer) string {
	if l, ok := s.(linkStatus); ok {
		return l.Stringer.String()
	}
	return stringOf(s)
}

// checkRA tells, in the status column and the log, whether each checked ROM of
// the ROMs tab is a version RetroAchievements recognizes, downloading the
// game lists of their consoles first.
func (t *convTab) checkRA(u *ui) {
	if u.busy {
		return
	}
	items, ok := t.romItems()
	if !ok {
		return
	}
	var jobs []raJob
	for _, it := range items {
		r := scanRom(it)
		console, system := raConsole(r)
		if r.files != 1 {
			console = 0 // not looked up: no list needed
		}
		jobs = append(jobs, raJob{item: it, console: console, system: system,
			run: func(ctx context.Context, db *retroach.DB) raOutcome {
				return raCheck(ctx, db, r)
			}})
	}
	u.startRA(jobs)
}

// checkRADiscs does the same for the checked disc images of the Discs tab.
func (t *convTab) checkRADiscs(u *ui) {
	if u.busy {
		return
	}
	var items []*convItem
	for _, it := range t.items {
		// Wii discs are listed too, to say they are not checked yet.
		if (isCHD(it.path) || isISO(it.path) || isCSO(it.path) || isCDImage(it.path) || isRVZ(it.path) ||
			isWBFS(it.path) || hasExt(it.path, []string{".gcm"})) && !isXboxPlatform(it.platform) {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("ra.empty_discs")))
		return
	}
	items, ok := t.keepChecked(items)
	if !ok {
		return
	}
	u.startRA(raDiscJobs(items))
}

// startRA asks for the key if needed, downloads the missing game lists and
// runs the checks.
func (u *ui) startRA(jobs []raJob) {
	if u.cfg.RAKey == "" && !u.askRAKey() {
		return
	}
	db := u.raDB()
	var missing []int
	for _, j := range jobs {
		if j.console != 0 && !db.Has(j.console) && !slices.Contains(missing, j.console) {
			missing = append(missing, j.console)
		}
	}
	u.separate()
	if len(missing) > 0 {
		slices.Sort(missing)
		u.downloadRA(missing, func() { u.runRACheck(jobs) })
		return
	}
	u.runRACheck(jobs)
}

// runRACheck checks the files in the background and shows the results.
func (u *ui) runRACheck(jobs []raJob) {
	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)
	db := u.raDB()
	u.logf("ra.start", len(jobs))
	go func() {
		defer u.recoverPanic()
		defer cancel()
		found := 0
		var rows []raRow
		for i, j := range jobs {
			if ctx.Err() != nil {
				break
			}
			name := filepath.Base(j.item.path)
			u.setProgress(float64(i) / float64(len(jobs)))
			u.setStatus(T("ra.hashing", name))
			o := j.run(ctx, db)
			if ctx.Err() != nil {
				break
			}
			if o.ok {
				found++
			}
			u.setItemStatus(j.item, o.status)
			if o.line != nil {
				u.println(o.line)
			}
			rows = append(rows, raRow{job: j, out: o})
		}
		u.setProgress(0)
		u.setStatus("")
		cancelled := ctx.Err() != nil
		if !cancelled {
			u.logf("ra.summary", found, len(jobs))
			if u.cfg.RAReport {
				u.writeRAReports(rows, time.Now())
			}
		}
		onGUI(func() {
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
			if cancelled {
				u.logf("log.cancelled")
			}
			u.flush()
		})
	}()
}

// raResult looks a hash up. When the version is not recognized, names (the
// file's, the ROM's in an archive) tell whether RetroAchievements has the
// game in another version; the status then links to the page listing the
// hashes RetroAchievements accepts for it.
func raResult(db *retroach.DB, console int, system, base, hash string, names ...string) raOutcome {
	g, ok := db.Find(console, hash)
	switch {
	case !ok:
		if other, found := db.FindTitle(console, names...); found && other.Achievements > 0 {
			url := retroach.HashesURL(other.ID)
			return raOutcome{status: linkStatus{msg("ra.st_other"), url},
				line: msg("ra.other", base, other.Title, system, hash, url), other: true, hash: hash, game: &other}
		}
		return raOutcome{status: msg("ra.st_unknown"), line: msg("ra.unknown", base, system, hash), hash: hash}
	case g.Achievements == 0:
		return raOutcome{status: msg("ra.st_none", g.Title),
			line: msg("ra.none", base, g.Title, retroach.GameURL(g.ID)), ok: true, hash: hash, game: &g}
	}
	return raOutcome{status: msg("ra.st_ok", g.Title, g.Achievements),
		line: msg("ra.ok", base, g.Title, g.Achievements), ok: true, hash: hash, game: &g}
}

// raCheck checks one ROM.
func raCheck(ctx context.Context, db *retroach.DB, r romFile) raOutcome {
	base := filepath.Base(r.item.path)
	console, system := raConsole(r)
	switch {
	case r.err != nil:
		return raSkip(msg("ra.st_error"), msg("drename.skip_error", base, r.err))
	case r.files == 0:
		return raSkip(msg("ra.st_error"), msg("rrename.skip_empty", base))
	case r.files > 1:
		return raSkip(msg("ra.st_set"), msg("ra.skip_set", base, r.files))
	case console == 0:
		return raSkip(msg("ra.st_console"), msg("ra.skip_console", base))
	case !db.Has(console):
		return raSkip(msg("ra.st_nolist"), msg("ra.skip_nolist", base, system))
	}
	data, size, done, err := romReader(ctx, r)
	if err != nil {
		if ctx.Err() != nil {
			return raOutcome{}
		}
		return raSkip(msg("ra.st_error"), msg("drename.skip_error", base, err))
	}
	hash, err := retroach.Hash(console, data, size)
	done()
	if err != nil {
		return raSkip(msg("ra.st_error"), msg("drename.skip_error", base, err))
	}
	names := []string{strings.TrimSuffix(base, filepath.Ext(base))}
	if r.inner != "" {
		names = append(names, strings.TrimSuffix(path.Base(r.inner), path.Ext(r.inner)))
	}
	return raResult(db, console, system, base, hash, names...)
}

// romReader gives random access to a ROM: the file itself, or the ROM
// inside a ZIP or 7z, unpacked to memory or, when large (DS), to a
// temporary file. done releases it.
func romReader(ctx context.Context, r romFile) (io.ReaderAt, int64, func(), error) {
	path := r.item.path
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".zip" && ext != ".7z" {
		f, err := os.Open(path)
		if err != nil {
			return nil, 0, nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, nil, err
		}
		return f, st.Size(), func() { f.Close() }, nil
	}
	var mem bytes.Buffer
	var tmp *os.File
	sink := func(size int64) (io.Writer, error) {
		if size <= 128<<20 {
			mem.Grow(int(size))
			return &mem, nil
		}
		var err error
		tmp, err = os.CreateTemp("", "gameforge-*.rom")
		return tmp, err
	}
	cleanup := func() {
		if tmp != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}
	var size int64
	switch ext {
	case ".zip":
		rc, err := r.open()()
		if err != nil {
			return nil, 0, nil, err
		}
		w, err := sink(r.size)
		if err == nil {
			size, err = io.Copy(w, readerCtx{ctx, rc})
		}
		rc.Close()
		if err != nil {
			cleanup()
			return nil, 0, nil, err
		}
	case ".7z":
		var werr error
		_, err := sevenzip.Extract(ctx, path, func(name string, n int64) io.Writer {
			if name != r.inner {
				return nil
			}
			w, err := sink(n)
			werr = err
			if err != nil {
				return nil
			}
			size = n
			return w
		}, nil)
		if err == nil {
			err = werr
		}
		if err != nil {
			cleanup()
			return nil, 0, nil, err
		}
	}
	if tmp != nil {
		return tmp, size, cleanup, nil
	}
	return bytes.NewReader(mem.Bytes()), size, func() {}, nil
}

// readerCtx stops reading when the context is cancelled.
type readerCtx struct {
	ctx context.Context
	r   io.Reader
}

func (r readerCtx) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// raReportVar is the Tcl variable of the "Save an HTML report" entries,
// shared by the menus of the Discs and ROMs tabs so that both show the
// same. (The tk9.0 package cannot link a variable to a menu entry, so it is
// set with Tcl.)
const raReportVar = "::gameforge_ra_report"

// addRAReportItem adds to a menu the choice of saving an HTML report of
// each RetroAchievements check.
func (u *ui) addRAReportItem(menu *MenuWidget) {
	tcl("set", raReportVar, tclBool(u.cfg.RAReport))
	menu.AddCheckbutton(Lbl(T("ra.report_menu")), Command(func() {
		u.cfg.RAReport = tcl("set", raReportVar) == "1"
		u.saveConfig()
	}))
	tcl(menu.String(), "entryconfigure", "last", "-variable", raReportVar, "-onvalue", "1", "-offvalue", "0")
}
