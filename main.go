// Command gameforge is a GUI tool for backing up games you own: it merges a
// Nintendo Switch base game with its update and DLCs into one NSP/NSZ, and
// converts disc images (CD/DVD to CHD, Xbox discs to XISO, GameCube and Wii
// to RVZ, ISO to CSO/ZSO).
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	. "modernc.org/tk9.0"
	"modernc.org/tk9.0/extensions/eval"

	"gameforge/internal/chd"
	"gameforge/internal/config"
	"gameforge/internal/i18n"
	"gameforge/internal/icon"
	"gameforge/internal/merge"
	"gameforge/internal/nx"
	"gameforge/internal/titledb"
)

var T = i18n.T

// Tk must be used from the goroutine (and OS thread) that initialized it.
func init() { runtime.LockOSThread() }

// row is one line of the file list: a game header, a file of a game, or a
// file that could not be identified.
type row struct {
	game *merge.Game // set for headers and game files
	nsp  *merge.NSP  // nil for headers
}

type ui struct {
	cfg *config.Config

	iconMu      sync.Mutex
	iconJobs    []*convItem // icons waiting to be read in the background
	iconRunning bool

	keysEntry   *TEntryWidget
	keysStatus  *TLabelWidget
	langBox     *TComboboxWidget
	updateNote  *TLabelWidget // the notice of a new version, in the top row
	themeBox    *TComboboxWidget
	compress    *TCheckbuttonWidget
	compressVar *VariableOpt
	levelBox    *TComboboxWidget
	verify      *TCheckbuttonWidget
	applyUpdate *TCheckbuttonWidget
	applyRow    *TFrameWidget // applyUpdate with its info icon
	outEntry    *TEntryWidget
	outTab      string // the tab whose output folder outEntry shows
	tree        *TTreeviewWidget
	panes       *TPanedwindowWidget
	summary     *TLabelWidget
	progress    *TProgressbarWidget
	status      *TLabelWidget
	log         *TextWidget
	mergeBtn    *TButtonWidget
	cancelBtn   *TButtonWidget
	addButtons  []*TButtonWidget
	onlineBtn   *TMenubuttonWidget
	checkBtn    *TMenubuttonWidget
	dbStatus    *TLabelWidget // state of the game database
	dbErr       error

	db          *titledb.DB // nil until loaded or downloaded
	keys        *nx.Keys
	keysPath    string
	keysErr     error
	paths       []string
	nsps        []*merge.NSP
	games       []*merge.Game
	notebook    *TNotebookWidget
	tabIDs      map[string]string // tab name -> its frame
	tabs        []*convTab        // the conversion tabs, after the Switch tab
	rows        []row
	gameItems   map[string]uint64 // tree item → AppID of the game rows shown
	checked     map[string]bool   // by NSP path
	busy        bool
	cancelMerge context.CancelFunc
	analyzeMu   sync.Mutex

	// Updates from worker goroutines, applied on the GUI thread by flush.
	logLines     []fmt.Stringer // everything in the log, for language changes
	mu           sync.Mutex
	pendingLog   []fmt.Stringer
	logged       bool // something was logged since the log was last cleared
	logStart     int  // the first line of logLines from the current run
	pendingStat  *string
	pendingProg  float64
	progDirty    bool
	pendingItems map[*convItem]fmt.Stringer // row statuses of conversion tabs
}

func main() {
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		cfgErr = cfg.Save() // adds options introduced by newer versions
	}
	i18n.Set(cfg.Language)
	os.MkdirAll(keysDir(), 0o755)
	moveCaches(cfg.Path())

	InitializeExtension("eval")
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		ActivateTheme("clam") // the X11 default theme looks dated
	}
	setTkLanguage()
	patchDialogs()
	// Tk's own message boxes (Linux) are plain windows the window manager
	// lets resize; like the native ones on Windows, they keep their size.
	eval.Eval(`bind Dialog <Map> {+catch {wm resizable %W 0 0}}`)
	initThemes()
	initTooltips()

	u := &ui{cfg: cfg, checked: map[string]bool{}, tabs: newConvTabs()}
	applyTheme(u.darkMode())
	if cfgErr != nil {
		u.logLines = append(u.logLines, i18n.M("log.config_error", cfgErr))
	}
	u.build()
	// Title bar and taskbar icon; the larger image first, as Tk recommends.
	App.IconPhoto(DefaultIcon(), NewPhoto(Data(icon.Render(48))), NewPhoto(Data(icon.Render(32))),
		NewPhoto(Data(icon.Render(16))))
	u.restoreWindow()
	WmProtocol(App, WM_DELETE_WINDOW, Command(func() {
		u.saveLayout()
		u.cfg.Save() // a failure cannot be shown any more
		Destroy(App)
	}))

	// Dropping a folder behaves like "Open folder": it replaces the list.
	// Each dropped file goes to the tab of its format.
	enableDrop(u.dropPaths)
	setTitleBarDark(darkTheme) // the window frame exists only now
	NewTicker(100*time.Millisecond, u.flush)

	u.loadKeys(u.initialKeysPath())
	u.loadWiiKeys()
	u.loadDB()
	whenShown(u.startUpdateCheck) // a dialog opened earlier gets no input
	// Files given on the command line (dropped on the program, "Open
	// with") are added as if dropped on the window.
	if len(os.Args) > 1 {
		u.dropPaths(os.Args[1:])
	}
	App.Wait()
	if u.cancelMerge != nil {
		u.cancelMerge()
	}
}

// tcl evaluates a Tcl command built from words, quoted as needed.
func tcl(words ...string) string {
	r, _ := eval.Evalv(words...)
	return r
}

// setTkLanguage makes Tk's own texts (the file dialogs it draws itself and
// their buttons) follow the interface language.
func setTkLanguage() {
	tcl("::msgcat::mclocale", i18n.Current())
	// Tk loaded its catalog for the previous locale at startup.
	eval.Eval(`namespace eval ::tk {::msgcat::mcload [file join $::tk_library msgs]}`)
	// The "show hidden" box of the dialogs, which Tk translates only into
	// German among the app's languages.
	eval.Eval(`namespace eval ::tk {
	foreach {lang files dirs} {
		pt "Mostrar arquivos e pastas &ocultos" "Mostrar pastas &ocultas"
		fr "Afficher les fichiers et dossiers &cachés" "Afficher les dossiers &cachés"
		es "Mostrar archivos y carpetas &ocultos" "Mostrar carpetas &ocultas"
	} {
		::msgcat::mcset $lang "Show &Hidden Files and Directories" $files
		::msgcat::mcset $lang "Show &Hidden Directories" $dirs
	}
}`)
}

// patchDialogs improves Tk's own file and folder dialogs. Loading a dialog
// script again (auto_load) undoes it, so it is called after every auto_load
// of a dialog.
//
// In the folder dialog a folder could only be chosen by entering it: OK did
// return a selected folder, but clicking OK moves the focus away from the
// folder list, which clears the selection first. The selected folder is
// now shown in the path field and returned by OK.
//
// The folder dialog gets an "Include subfolders" box under "Show hidden
// folders", shown only when a folder is opened to add its files.
//
// On Windows the directory menu lists all drives: it only offered the
// current path and its parents, so another drive could not be reached.
func patchDialogs() {
	eval.Eval(`
foreach p {::tk::dialog::file::ListBrowse ::tk::dialog::file::chooseDir::OkCmd ::tk::dialog::file::Create} {
	if {[info procs $p] eq ""} {auto_load $p}
}
if {![string match *gameforge-folder* [info body ::tk::dialog::file::ListBrowse]]} {
	catch {rename ::tk::dialog::file::ListBrowseTk {}}
	rename ::tk::dialog::file::ListBrowse ::tk::dialog::file::ListBrowseTk
	proc ::tk::dialog::file::ListBrowse {w} {
		# gameforge-folder
		if {[winfo class $w] ne "TkChooseDir"} {
			return [::tk::dialog::file::ListBrowseTk $w]
		}
		upvar ::tk::dialog::file::[winfo name $w] data
		set sel [$data(icons) selection get]
		if {[llength $sel]} {
			::tk::dialog::file::chooseDir::ListBrowse $w [$data(icons) get [lindex $sel 0]]
			set data(gameforgePick) [$data(ent) get]
		}
	}
}
if {![string match *gameforge-folder* [info body ::tk::dialog::file::chooseDir::OkCmd]]} {
	catch {rename ::tk::dialog::file::chooseDir::OkCmdTk {}}
	rename ::tk::dialog::file::chooseDir::OkCmd ::tk::dialog::file::chooseDir::OkCmdTk
	proc ::tk::dialog::file::chooseDir::OkCmd {w} {
		# gameforge-folder: the path field still shows the selected folder.
		upvar ::tk::dialog::file::[winfo name $w] data
		if {[info exists data(gameforgePick)] && [$data(ent) get] eq $data(gameforgePick)
				&& [file isdirectory $data(gameforgePick)]} {
			::tk::dialog::file::chooseDir::Done $w $data(gameforgePick)
			return
		}
		::tk::dialog::file::chooseDir::OkCmdTk $w
	}
}
if {![string match *gameforge-subfolders* [info body ::tk::dialog::file::Create]]} {
	catch {rename ::tk::dialog::file::CreateTk {}}
	rename ::tk::dialog::file::Create ::tk::dialog::file::CreateTk
	proc ::tk::dialog::file::Create {w class} {
		# gameforge-subfolders
		::tk::dialog::file::CreateTk $w $class
		if {$class ne "TkChooseDir"} return
		foreach v {::gameforge_subfolders ::gameforge_subfolders_btn ::gameforge_subfolders_text} {
			if {![info exists $v]} {set $v 0}
		}
		set b [ttk::checkbutton $w.contents.f2.gfsub -text $::gameforge_subfolders_text -variable ::gameforge_subfolders]
		grid $b -row 2 -column 0 -columnspan 2 -padx 3p -sticky ew
		if {!$::gameforge_subfolders_btn} {grid remove $b}
	}
}`)
	if runtime.GOOS != "windows" {
		return
	}
	eval.Eval(`
if {[info procs ::tk::dialog::file::Update] eq ""} {auto_load ::tk::dialog::file::Update}
if {![string match *gameforge-drives* [info body ::tk::dialog::file::Update]]} {
	catch {rename ::tk::dialog::file::UpdatePath {}}
	rename ::tk::dialog::file::Update ::tk::dialog::file::UpdatePath
	proc ::tk::dialog::file::Update {w} {
		# gameforge-drives
		::tk::dialog::file::UpdatePath $w
		if {![winfo exists $w]} return
		upvar ::tk::dialog::file::[winfo name $w] data
		if {![info exists data(dirMenu)] || ![winfo exists $data(dirMenu)]} return
		set var ::tk::dialog::file::[winfo name $w](selectPath)
		$data(dirMenu) add separator
		foreach v [lsort [file volumes]] {
			# Drive letters only, not Tcl's internal //zipfs:/ volume.
			if {[regexp {^[A-Za-z]:/$} $v]} {
				$data(dirMenu) add command -label [file nativename $v] -command [list set $var $v]
			}
		}
	}
}`)
}

// recoverPanic, deferred in background jobs, turns a panic (a bug, or a
// file a parser does not expect) into a log line and frees the window,
// instead of closing the app with everything in it.
func (u *ui) recoverPanic() {
	p := recover()
	if p == nil {
		return
	}
	u.logf("log.panic", "GameForge", p)
	onGUI(func() {
		u.flush()
		u.cancelMerge = nil
		enable(u.cancelBtn, false)
		u.setBusy(false)
	})
}

// nativeDialogs reports whether the system file dialogs can be used: on
// Windows they always use the system language, so when the interface is in
// another language Tk's own (translated) dialogs are used instead.
func nativeDialogs() bool {
	return runtime.GOOS != "windows" || i18n.Current() == i18n.Detect()
}

// saveConfig writes the configuration, reporting failures in the log.
func (u *ui) saveConfig() {
	if err := u.cfg.Save(); err != nil {
		u.logf("log.config_error", err)
	}
}

var (
	pad   = Opts{Padx("1m"), Pady("1m")}
	ipad  = Opts{Ipadx("1m"), Ipady("0.5m")}
	edges = Sticky("we")
)

// build creates the window content in the current language, with the log
// shown again in that language.
func (u *ui) build() {
	for _, w := range WinfoChildren(App) {
		Destroy(w)
	}
	App.WmTitle(T("app.title"))
	App.Configure(Padx("2m"), Pady("2m"))

	// Language and theme, for all tabs.
	top := TFrame()
	Grid(top, Row(0), Column(0), edges)
	GridColumnConfigure(top.Window, 0, Weight(1))
	Grid(top.TLabel(Txt(T("lang.label"))), Row(0), Column(1), pad)
	u.langBox = u.buildLangBox(top)
	Grid(u.langBox, Row(0), Column(2), pad)
	Grid(top.TLabel(Txt(T("theme.label"))), Row(0), Column(3), pad)
	u.themeBox = u.buildThemeBox(top)
	Grid(u.themeBox, Row(0), Column(4), pad)
	Grid(top.TButton(Txt(T("btn.about")), Command(u.showAbout)), Row(0), Column(5), pad)
	u.updateNote = top.TLabel(Foreground(linkColor()), Font("GameForgeLink"))
	Bind(u.updateNote, "<Button-1>", Command(u.openUpdatePage))
	Grid(u.updateNote, Row(0), Column(0), Sticky("w"), pad)
	u.showUpdateNote()

	// One tab per platform above the log, with a movable divider.
	panes := TPanedwindow(Orient("vertical"))
	u.panes = panes
	Grid(panes, Row(1), Column(0), Sticky("news"), pad)
	u.notebook = panes.TNotebook()
	panes.Add(u.notebook.Window, Weight(3))
	logFrame := panes.TFrame()
	u.buildLog(logFrame)
	panes.Add(logFrame.Window, Weight(1))

	switchTab := u.notebook.TFrame(Padding("1m"))
	widthRows := []*TFrameWidget{u.buildSwitchTab(switchTab)}
	tcl(u.notebook.String(), "add", switchTab.String(), "-text", T("tab.switch"))
	u.tabIDs = map[string]string{"switch": switchTab.String()}
	for _, t := range u.tabs {
		f := u.notebook.TFrame(Padding("1m"))
		widthRows = append(widthRows, t.build(u, f))
		tcl(u.notebook.String(), "add", f.String(), "-text", T(t.conv.title))
		u.tabIDs[t.conv.id] = f.String()
	}
	if id, ok := u.tabIDs[u.cfg.Tab]; ok {
		u.notebook.Select(id)
	}
	Bind(u.notebook, "<<NotebookTabChanged>>", Command(func() {
		cur := u.notebook.Select(nil)
		for name, id := range u.tabIDs {
			if id == cur && name != u.cfg.Tab {
				u.cfg.Tab = name
				u.saveConfig()
				u.showOutputDir()
			}
		}
	}))

	// Output folder and verification, for all tabs.
	opts := TFrame()
	Grid(opts, Row(2), Column(0), edges)
	GridColumnConfigure(opts.Window, 1, Weight(1))
	Grid(opts.TLabel(Txt(T("form.output"))), Row(0), Column(0), pad)
	u.outEntry = opts.TEntry()
	u.showOutputDir()
	saveOut := func() {
		if s := strings.TrimSpace(u.outEntry.Textvariable()); s != u.outputDir(u.outTab) {
			u.setOutputDir(u.outTab, s, false)
		}
	}
	Bind(u.outEntry, "<KeyRelease>", Command(saveOut))
	Bind(u.outEntry, "<FocusOut>", Command(saveOut))
	Grid(u.outEntry, Row(0), Column(1), edges, pad)
	outBtn := opts.TButton(Txt(T("btn.browse")), Command(func() {
		if p := u.openFolder(false); p != "" {
			u.setOutputDir(u.outTab, p, true)
		}
	}))
	Grid(outBtn, Row(0), Column(2), pad)
	u.addButtons = append(u.addButtons, outBtn)
	verifyVar := Variable(tclBool(!u.cfg.SkipVerify))
	u.verify = opts.TCheckbutton(Txt(T("opt.verify")), verifyVar, Command(func() {
		u.cfg.SkipVerify = verifyVar.Get() != "1"
		u.saveConfig()
	}))
	Grid(u.verify, Row(0), Column(3), pad)

	// Progress and status.
	bottom := TFrame()
	Grid(bottom, Row(3), Column(0), edges)
	GridColumnConfigure(bottom.Window, 0, Weight(1))
	u.progress = bottom.TProgressbar(Maximum(1), Value(0))
	Grid(u.progress, Row(0), Column(0), edges, pad)
	u.cancelBtn = bottom.TButton(Txt(T("btn.cancel")), State("disabled"), Command(func() {
		if u.cancelMerge != nil {
			u.cancelMerge()
		}
	}))
	Grid(u.cancelBtn, Row(0), Column(1), pad, ipad)
	u.status = bottom.TLabel(Txt(T("status.initial")))
	Grid(u.status, Row(1), Column(0), Columnspan(2), Sticky("w"), pad)

	GridColumnConfigure(App, 0, Weight(1))
	GridRowConfigure(App, 1, Weight(1))
	// Wide enough for the button rows, whose width depends on the language.
	tcl("update", "idletasks")
	minWidth := 760
	for _, w := range widthRows {
		if n, err := strconv.Atoi(tcl("winfo", "reqwidth", w.String())); err == nil {
			minWidth = max(minWidth, n+60) // window, notebook and frame padding
		}
	}
	WmMinSize(App, minWidth, 600)

	u.updateKeysStatus()
	u.refreshList()
	for _, t := range u.tabs {
		t.refresh(u)
	}
	u.setBusy(u.busy)
	if u.cfg.Divider > 0 {
		// Once the window has its final size, or the position gets clamped.
		eval.Eval(fmt.Sprintf("after 200 {catch {%s sashpos 0 %d}}", panes, u.cfg.Divider))
	}
}

// selectTab shows the tab with the given name ("switch" or a converter id).
func (u *ui) selectTab(name string) {
	if id, ok := u.tabIDs[name]; ok {
		u.notebook.Select(id)
	}
}

// buildSwitchTab fills the Nintendo Switch tab: prod.keys, the file list,
// the merge options and the merge button. It returns the widest row.
func (u *ui) buildSwitchTab(tab *TFrameWidget) *TFrameWidget {
	GridColumnConfigure(tab.Window, 0, Weight(1))
	GridRowConfigure(tab.Window, 3, Weight(1))

	keys := tab.TFrame()
	Grid(keys, Row(0), Column(0), edges)
	GridColumnConfigure(keys.Window, 1, Weight(1))
	Grid(keys.TLabel(Txt("prod.keys")), Row(0), Column(0), pad)
	u.keysEntry = keys.TEntry(Textvariable(u.keysPath))
	Bind(u.keysEntry, "<Return>", Command(func() { u.loadKeys(u.keysEntry.Textvariable()) }))
	Grid(u.keysEntry, Row(0), Column(1), edges, pad)
	keysBtn := keys.TButton(Txt(T("btn.browse")), Command(func() {
		types := []FileType{{TypeName: "prod.keys", Extensions: []string{".keys", ".txt"}}}
		if p := u.openFile(types, keysDir(), false); p != "" {
			u.loadKeys(p)
		}
	}))
	Grid(keysBtn, Row(0), Column(2), pad)
	u.keysStatus = keys.TLabel(Wraplength("240m"), Justify("left"))
	Grid(u.keysStatus, Row(1), Column(0), Columnspan(3), Sticky("we"), pad)
	wrapToWidth(u.keysStatus)

	// List actions.
	actions := tab.TFrame()
	Grid(actions, Row(2), Column(0), Sticky("w"))
	openDir := actions.TButton(Txt(T("btn.open_folder")), Command(func() {
		if p := u.openFolder(true); p != "" {
			u.followInput("switch", []string{p})
			u.addPaths([]string{p}, true)
		}
	}))
	addFile := actions.TButton(Txt(T("btn.add_nsp")), Command(func() {
		types := []FileType{{TypeName: "Nintendo Switch NSP/NSZ/XCI", Extensions: gameExts}}
		if ps := u.openFiles(types); len(ps) > 0 {
			u.followInput("switch", ps)
			u.addPaths(ps, false)
		}
	}))
	clearBtn := actions.TButton(Txt(T("btn.clear")), Command(u.clearList))
	selDefault := actions.TButton(Txt(T("btn.select_default")), Command(func() { u.resetChecks(); u.refreshList() }))
	selNone := actions.TButton(Txt(T("btn.select_none")), Command(func() {
		// Explicitly false: files missing from u.checked get the default.
		for _, n := range u.nsps {
			u.checked[n.Path] = false
		}
		u.refreshList()
	}))
	// Check backups: files chosen in a dialog, or those checked in the list.
	u.checkBtn = actions.TMenubutton(Txt(T("btn.check")))
	checkBtn := u.checkBtn
	checkMenu := checkBtn.Menu(Tearoff(false))
	checkMenu.AddCommand(Lbl(T("check.files")), Command(func() {
		types := []FileType{{TypeName: "Nintendo Switch NSP/NSZ/XCI", Extensions: gameExts}}
		u.startCheck(u.openFiles(types))
	}))
	checkMenu.AddCommand(Lbl(T("check.listed")), Command(func() {
		if paths := u.checkedPaths(); len(paths) > 0 {
			u.startCheck(paths)
		} else {
			u.logf("list.none_checked")
			u.flush()
		}
	}))
	checkBtn.Configure(Mnu(checkMenu))
	setTooltip(checkBtn, T("backup.check.tip"))
	// Online check and database download share one menu button, to keep
	// the row narrow.
	u.onlineBtn = actions.TMenubutton(Txt(T("btn.online_menu")))
	onlineMenu := u.onlineBtn.Menu(Tearoff(false))
	onlineMenu.AddCommand(Lbl(T("btn.online")), Command(u.checkOnline))
	onlineMenu.AddCommand(Lbl(T("btn.online_export")), Command(u.exportMissing))
	onlineMenu.AddCommand(Lbl(T("btn.update_db")), Command(func() { u.startDownloadDB(nil) }))
	u.onlineBtn.Configure(Mnu(onlineMenu))
	u.dbStatus = actions.TLabel()
	setTooltip(u.onlineBtn, T("btn.online_menu.tip"))
	setTooltip(u.dbStatus, T("btn.online_menu.tip"))
	u.showDBStatus()
	renameBtn := actions.TButton(Txt(T("btn.rename")), Command(u.renameFiles))
	setTooltip(renameBtn, T("btn.rename.tip"))
	gridRow(openDir, addFile, clearBtn, actions.TSeparator(Orient("vertical")), selDefault, selNone,
		actions.TSeparator(Orient("vertical")), checkBtn, renameBtn, actions.TSeparator(Orient("vertical")), u.onlineBtn, u.dbStatus)
	u.addButtons = []*TButtonWidget{openDir, addFile, clearBtn, keysBtn, selDefault, selNone, renameBtn}

	listFrame := tab.TFrame()
	Grid(listFrame, Row(3), Column(0), Sticky("news"), pad)
	u.tree = u.buildTree(listFrame)

	optRow := tab.TFrame()
	Grid(optRow, Row(4), Column(0), Sticky("w"))
	u.buildCompressionOptions(optRow)
	Grid(u.compress, optRow.TLabel(Txt(T("opt.level"))), u.levelBox, optRow.TSeparator(Orient("vertical")), u.applyRow, Sticky("ns"), pad)

	run := tab.TFrame()
	Grid(run, Row(5), Column(0), edges)
	GridColumnConfigure(run.Window, 0, Weight(1))
	u.summary = run.TLabel()
	Grid(u.summary, Row(0), Column(0), Sticky("w"), pad)
	u.mergeBtn = run.TButton(Txt(T("btn.merge")), Default("active"), Command(u.startMerge))
	Grid(u.mergeBtn, Row(0), Column(1), pad, ipad)
	return actions
}

// gridRow places widgets side by side in their frame; separators without
// internal padding, which would widen them into bars.
func gridRow(widgets ...Widget) {
	for i, w := range widgets {
		if _, sep := w.(*TSeparatorWidget); sep {
			Grid(w, Row(0), Column(i), Sticky("ns"), pad)
		} else {
			// Not stretched: a label that wraps to several lines must
			// not make the buttons taller.
			Grid(w, Row(0), Column(i), pad, ipad)
		}
	}
}

// restoreWindow gives the window the size, position and state of the last
// run, or a default size centered on the screen.
func (u *ui) restoreWindow() {
	var w, h, x, y int
	if n, _ := fmt.Sscanf(u.cfg.Window, "%dx%d%d%d", &w, &h, &x, &y); n == 4 && w >= 400 && h >= 300 {
		if onScreen(x+w/2, y+10) { // the title bar must be reachable
			WmGeometry(App, u.cfg.Window)
		} else {
			WmGeometry(App, fmt.Sprintf("%dx%d", w, h))
			App.Center()
		}
	} else {
		WmGeometry(App, "1200x760")
		App.Center()
	}
	if u.cfg.Maximized {
		if runtime.GOOS == "windows" {
			tcl("wm", "state", ".", "zoomed")
		} else {
			tcl("wm", "attributes", ".", "-zoomed", "1")
		}
	}
}

// saveLayout stores the window geometry, column widths and divider position
// in the configuration (written by the caller).
func (u *ui) saveLayout() {
	switch tcl("wm", "state", ".") {
	case "zoomed":
		u.cfg.Maximized = true
	case "normal":
		u.cfg.Maximized = tcl("wm", "attributes", ".", "-zoomed") == "1" // X11
		if !u.cfg.Maximized {
			u.cfg.Window = tcl("wm", "geometry", ".")
		}
	}
	var widths []int
	for _, col := range []string{"#0", "detail"} {
		w, _ := strconv.Atoi(tcl(u.tree.String(), "column", col, "-width"))
		widths = append(widths, w)
	}
	u.cfg.Columns = widths
	u.cfg.Divider, _ = strconv.Atoi(tcl(u.panes.String(), "sashpos", "0"))
}

// buildLangBox creates the language selector: "Automatic" plus every
// supported language, each shown in its own language.
func (u *ui) buildLangBox(parent *TFrameWidget) *TComboboxWidget {
	codes := []string{i18n.Auto}
	options := []string{T("lang.auto", i18n.Name(i18n.Detect()))}
	for _, l := range i18n.Languages {
		codes = append(codes, l.Code)
		options = append(options, l.Name)
	}
	width := 0
	for _, o := range options {
		width = max(width, len([]rune(o)))
	}
	box := parent.TCombobox(State("readonly"), Values(tclList(options)), Width(width+1))
	sel := 0
	for i, c := range codes {
		if c == u.cfg.Language {
			sel = i
		}
	}
	box.Current(sel)
	Bind(box, "<<ComboboxSelected>>", Command(func() {
		i, _ := strconv.Atoi(box.Current(nil))
		code := codes[i]
		if code == u.cfg.Language {
			return
		}
		u.cfg.Language = code
		u.saveLayout() // the rebuilt window keeps the column widths
		u.saveConfig()
		i18n.Set(code)
		setTkLanguage()
		u.build()
	}))
	return box
}

// buildThemeBox creates the theme selector: automatic (following the
// system), light or dark.
func (u *ui) buildThemeBox(parent *TFrameWidget) *TComboboxWidget {
	system := T("theme.light")
	if systemDark() {
		system = T("theme.dark")
	}
	codes := []string{"auto", "light", "dark"}
	options := []string{T("theme.auto", strings.ToLower(system)), T("theme.light"), T("theme.dark")}
	width := 0
	for _, o := range options {
		width = max(width, len([]rune(o)))
	}
	box := parent.TCombobox(State("readonly"), Values(tclList(options)), Width(width+1))
	for i, c := range codes {
		if c == u.cfg.Theme {
			box.Current(i)
		}
	}
	Bind(box, "<<ComboboxSelected>>", Command(func() {
		i, _ := strconv.Atoi(box.Current(nil))
		if codes[i] == u.cfg.Theme {
			return
		}
		u.cfg.Theme = codes[i]
		u.saveLayout() // the rebuilt window keeps the column widths
		u.saveConfig()
		applyTheme(u.darkMode())
		u.build()
	}))
	return box
}

// buildTree creates the file list: game headers with their files below, each
// with a check mark that is toggled by clicking it or pressing space.
func (u *ui) buildTree(parent *TFrameWidget) *TTreeviewWidget {
	tv := scrolledTree(parent, Columns("detail"), Selectmode("browse"), Height(12))

	setRowHeight()
	tv.Heading("#0", Txt(T("list.file")), Anchor("w"))
	tv.Heading("detail", Txt(T("list.detail")), Anchor("w"))
	widths := []int{460, 620}
	if len(u.cfg.Columns) == 2 && u.cfg.Columns[0] > 20 && u.cfg.Columns[1] > 20 {
		widths = u.cfg.Columns // as left by the last run
	}
	tv.Column("#0", Width(widths[0]), Stretch(true))
	tv.Column("detail", Width(widths[1]), Stretch(true))
	tv.TagConfigure("game", Font("TkHeadingFont"))
	tv.TagConfigure("unknown", Foreground("gray50"))
	tv.TagConfigure("duplicate", Foreground("gray50"))

	Bind(tv, "<ButtonRelease-1>", Command(func(e *Event) {
		// Only a click on the check box toggles; clicks on the name or on a
		// game's icon (drawn right of the box in the same image) just select
		// the row. A point in the icon still has the image one box width to
		// its left.
		isImage := func(x int) bool { return strings.HasSuffix(tv.IdentifyElement(x, e.Y), "image") }
		if isImage(e.X) && !isImage(e.X-checkBoxImage(0).Bounds().Dx()) {
			u.toggle(tv.IdentifyItem(e.X, e.Y))
		}
	}))
	Bind(tv, "<space>", Command(func() { u.toggle(tv.Focus()) }))
	return tv
}

func (u *ui) buildLog(parent *TFrameWidget) {
	var sb *TScrollbarWidget
	u.log = parent.Text(Font("TkFixedFont"), Wrap("word"), Height(8), Undo(false),
		Yscrollcommand(func(e *Event) { e.ScrollSet(sb) }))
	sb = parent.TScrollbar(Command(func(e *Event) { e.Yview(u.log) }))
	Grid(u.log, Row(0), Column(0), Sticky("news"))
	Grid(sb, Row(0), Column(1), Sticky("ns"))
	GridColumnConfigure(parent.Window, 0, Weight(1))
	GridRowConfigure(parent.Window, 0, Weight(1))
	if len(u.logLines) > 0 {
		u.log.Insert("end", renderLog(u.logLines))
		u.log.See("end")
	}
}

// renderLog shows log lines in the current language.
func renderLog(lines []fmt.Stringer) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// toggle flips the check mark of a list row (an item ID "r<index>").
func (u *ui) toggle(item string) {
	i, err := strconv.Atoi(strings.TrimPrefix(item, "r"))
	if u.busy || err != nil || i < 0 || i >= len(u.rows) {
		return
	}
	r := u.rows[i]
	switch {
	case r.game == nil: // unidentified file: cannot be merged
		return
	case r.nsp == nil: // game header: toggles all files of the game
		v := !u.allChecked(r.game)
		for _, n := range r.game.Files() {
			u.checked[n.Path] = v
		}
	default:
		u.checked[r.nsp.Path] = !u.checked[r.nsp.Path]
	}
	u.refreshList()
	u.tree.Focus(item)
	u.tree.Selection("set", item)
}

func (u *ui) allChecked(g *merge.Game) bool {
	for _, n := range g.Files() {
		if !u.checked[n.Path] {
			return false
		}
	}
	return true
}

func checkMark(v bool) Opt {
	if v {
		return Image(checkImage(1))
	}
	return Image(checkImage(0))
}

func describeGame(g *merge.Game, db *titledb.DB) string {
	var parts []string
	switch b := g.Base(); {
	case b == nil:
		parts = append(parts, T("game.no_base"))
	case b.Patched():
		parts = append(parts, T("game.base_patched", shortVersion(b.Control(), b.Version)))
	default:
		parts = append(parts, T("game.base_ok"))
	}
	if v, t, ok := g.UpdateVersion(); ok {
		var c *nx.Control
		if t != nil {
			c = t.Control
		}
		parts = append(parts, T("game.latest_update", versionText(c, v)))
	} else {
		parts = append(parts, T("game.no_update"))
	}
	if fw := g.Firmware(); fw != "" {
		parts = append(parts, T("game.firmware", fw))
	}
	parts = append(parts, T("game.dlc_count", g.DLCCount()))
	if db != nil {
		parts = append(parts, onlineTag(g.Online(db)))
	}
	return strings.Join(parts, "  •  ")
}

// versionText shows an internal version with its display version, if
// known: "1.0.8 (v524288)".
func versionText(c *nx.Control, v uint32) string {
	if c != nil && c.Version != "" {
		return fmt.Sprintf("%s (v%d)", c.Version, v)
	}
	return fmt.Sprintf("v%d", v)
}

// shortVersion is the display version if known, else the internal one.
func shortVersion(c *nx.Control, v uint32) string {
	if c != nil && c.Version != "" {
		return c.Version
	}
	return fmt.Sprintf("v%d", v)
}

func describeFile(g *merge.Game, n *merge.NSP) string {
	parts := []string{n.Kind.String()}
	if n.Kind == merge.KindDLC {
		parts[0] += fmt.Sprintf(" %016X", n.TitleID)
	}
	parts = append(parts, versionText(n.Control(), n.Version))
	if n.Patched() {
		parts = append(parts, T("file.patched_base", shortVersion(n.Control(), n.Version)))
	}
	// What a merged NSP contains besides its own title.
	dlcs := 0
	var fw string
	if t := n.Own(); t != nil {
		fw = t.Firmware()
	}
	for _, t := range n.Bundled() {
		switch t.Kind() {
		case merge.KindUpdate:
			parts = append(parts, T("file.bundled_update", versionText(t.Control, t.Meta.Version)))
			fw = t.Firmware()
		case merge.KindDLC:
			dlcs++
		}
	}
	if dlcs > 0 {
		parts = append(parts, T("file.bundled_dlc", dlcs))
	}
	if fw != "" {
		parts = append(parts, T("file.firmware", fw))
	}
	parts = append(parts, humanSize(n.Size))
	if n.FromCNMT {
		parts = append(parts, T("file.via_cnmt"))
	} else {
		parts = append(parts, T("file.via_name"))
		if e := (*nx.MissingKeyError)(nil); errors.As(n.Note, &e) {
			parts = append(parts, "⚠ "+T("file.keys_too_old", nx.KeyFirmware(e.Rev)))
		}
	}
	if dup := g.DuplicateOf(n); dup != nil {
		parts = append(parts, T("file.duplicate", dup.Name()))
	} else if n.Kind == merge.KindUpdate && n != g.LatestUpdate() {
		parts = append(parts, T("file.old_update"))
	}
	return strings.Join(parts, "  •  ")
}

// tclList quotes strings as the elements of a Tcl list.
func tclList(items []string) string {
	var b strings.Builder
	for i, s := range items {
		if i > 0 {
			b.WriteByte(' ')
		}
		if s == "" {
			b.WriteString("{}")
			continue
		}
		for _, r := range s {
			if strings.ContainsRune("\\{}[]$\"; \t\n", r) {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// keysDir returns the "keys" folder inside the program folder.
func keysDir() string { return filepath.Join(config.AppDir(), "keys") }

func (u *ui) initialKeysPath() string {
	candidates := []string{filepath.Join(keysDir(), "prod.keys")}
	if u.cfg.KeysPath != "" {
		candidates = append(candidates, u.cfg.KeysPath)
	}
	candidates = append(candidates, filepath.Join(config.AppDir(), "prod.keys"))
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".switch", "prod.keys"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func (u *ui) loadKeys(path string) {
	path = strings.TrimSpace(path)
	u.keysPath = path
	u.keysEntry.Configure(Textvariable(path))
	u.keys, u.keysErr = nil, nil
	if path != "" {
		k, err := nx.LoadKeys(path)
		if err == nil {
			_, err = k.HeaderKey()
		}
		if err != nil {
			u.keysErr = err
		} else {
			u.keys = k
			if u.cfg.KeysPath != path {
				u.cfg.KeysPath = path
				u.saveConfig()
			}
		}
	}
	u.updateKeysStatus()
	if u.keys != nil && len(u.paths) > 0 {
		u.reanalyze()
	}
}

// compressionLevels maps the config value to the zstd level; the order is
// the order shown in the selector.
var compressionLevels = []struct {
	name, key string
	level     zstd.EncoderLevel
}{
	{"fast", "level.fast", zstd.SpeedDefault},
	{"normal", "level.normal", zstd.SpeedBetterCompression},
	{"max", "level.max", zstd.SpeedBestCompression},
}

func (u *ui) buildCompressionOptions(parent *TFrameWidget) {
	u.compressVar = Variable(tclBool(u.cfg.Format == "nsz"))
	u.compress = parent.TCheckbutton(u.compressVar, Command(func() {
		u.cfg.Format = map[bool]string{true: "nsz", false: "nsp"}[u.compressVar.Get() == "1"]
		u.saveConfig()
		u.updateCompressionOptions()
	}))

	var options []string
	width, sel := 0, 1
	for i, l := range compressionLevels {
		options = append(options, T(l.key))
		width = max(width, len([]rune(T(l.key))))
		if l.name == u.cfg.Level {
			sel = i
		}
	}
	u.levelBox = parent.TCombobox(State("readonly"), Values(tclList(options)), Width(width+1))
	u.levelBox.Current(sel)
	Bind(u.levelBox, "<<ComboboxSelected>>", Command(func() {
		i, _ := strconv.Atoi(u.levelBox.Current(nil))
		u.cfg.Level = compressionLevels[i].name
		u.saveConfig()
	}))

	applyVar := Variable(tclBool(u.cfg.ApplyUpdate))
	u.applyRow = parent.TFrame()
	u.applyUpdate = u.applyRow.TCheckbutton(Txt(T("opt.apply_update")), applyVar, Command(func() {
		u.cfg.ApplyUpdate = applyVar.Get() == "1"
		u.saveConfig()
	}))
	// An info icon that explains, on hover, how this differs from a merge.
	accent := "#0067c0"
	if darkTheme {
		accent = "#60b0ff"
	}
	info := u.applyRow.TLabel(Txt("ⓘ"), Foreground(accent), Cursor("question_arrow"))
	setTooltip(info, T("opt.apply_update.tip"))
	Grid(u.applyUpdate, Row(0), Column(0), Sticky("w"))
	Grid(info, Row(0), Column(1), Sticky("w"), Padx("1m"))
}

// tclBool is a check button value: "1" checked, "0" not.
func tclBool(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// compressing reports whether the next merge writes an NSZ; that needs the
// keys to decrypt the NCAs.
func (u *ui) compressing() bool { return u.cfg.Format == "nsz" && u.keys != nil }

func enable(w interface{ Configure(...Opt) *Window }, on bool) {
	w.Configure(State(map[bool]string{true: "normal", false: "disabled"}[on]))
}

func enableCombo(w *TComboboxWidget, on bool) {
	w.Configure(State(map[bool]string{true: "readonly", false: "disabled"}[on]))
}

func (u *ui) updateCompressionOptions() {
	if u.keys == nil {
		u.compress.Configure(Txt(T("opt.compress_no_keys")))
	} else {
		u.compress.Configure(Txt(T("opt.compress")))
	}
	enable(u.compress, u.keys != nil && !u.busy)
	enable(u.applyUpdate, u.keys != nil && !u.busy)
	enableCombo(u.levelBox, u.compressing() && !u.busy)
}

func (u *ui) updateKeysStatus() {
	u.updateCompressionOptions()
	switch {
	case u.keys != nil && u.keys.Newest() >= 0 && !nx.KnownKey(u.keys.Newest()):
		u.keysStatus.Configure(Txt(T("keys.loaded_newer", u.keys.Count(), fmt.Sprintf("%02x", u.keys.Newest()), nx.LastKnownFirmware())))
	case u.keys != nil:
		rev := u.keys.Newest()
		u.keysStatus.Configure(Txt(T("keys.loaded", u.keys.Count(), nx.KeyFirmware(rev), fmt.Sprintf("%02x", max(rev, 0)))))
	case u.keysErr != nil:
		u.keysStatus.Configure(Txt("⚠ " + u.keysErr.Error()))
	default:
		u.keysStatus.Configure(Txt(T("keys.not_found")))
	}
}

// openFile asks for one file. With remember the dialog starts in, and then
// remembers, the last folder used.
func (u *ui) openFile(types []FileType, startDir string, remember bool) string {
	if remember {
		startDir = u.cfg.LastDir
	}
	opts := []string{"-parent", ".", "-title", T("app.title"), "-filetypes", fileTypes(types)}
	if startDir != "" {
		opts = append(opts, "-initialdir", startDir)
	}
	cmd := "tk_getOpenFile"
	if !nativeDialogs() {
		cmd = "::tk::dialog::file::"
		opts = append([]string{"open"}, opts...)
		tcl("auto_load", cmd)
		patchDialogs()
	}
	u.prepareDialog()
	p := filepath.FromSlash(tcl(append([]string{cmd}, opts...)...))
	u.rememberHidden()
	if p != "" && remember {
		u.cfg.LastDir = filepath.Dir(p)
		u.saveConfig()
	}
	return p
}

// saveFile asks where to save a file, offering name in the last folder
// used; "" if cancelled.
func (u *ui) saveFile(name string, types []FileType) string {
	opts := []string{"-parent", ".", "-title", T("app.title"), "-filetypes", fileTypes(types),
		"-initialfile", name, "-defaultextension", filepath.Ext(name)}
	if u.cfg.LastDir != "" {
		opts = append(opts, "-initialdir", u.cfg.LastDir)
	}
	cmd := "tk_getSaveFile"
	if !nativeDialogs() {
		cmd = "::tk::dialog::file::"
		opts = append([]string{"save"}, opts...)
		tcl("auto_load", cmd)
		patchDialogs()
	}
	u.prepareDialog()
	p := filepath.FromSlash(tcl(append([]string{cmd}, opts...)...))
	u.rememberHidden()
	if p != "" {
		u.cfg.LastDir = filepath.Dir(p)
		u.saveConfig()
	}
	return p
}

// openFolder asks for a folder, starting in and remembering the last one.
// With input (a folder to add files from) the dialog has an "Include
// subfolders" box, so Tk's own dialog is used: the system dialogs take no
// extra controls.
func (u *ui) openFolder(input bool) string {
	opts := []string{"-parent", ".", "-title", T("app.title"), "-mustexist", "1"}
	if u.cfg.LastDir != "" {
		opts = append(opts, "-initialdir", u.cfg.LastDir)
	}
	cmd := "tk_chooseDirectory"
	if input || !nativeDialogs() {
		cmd = "::tk::dialog::file::chooseDir::"
		tcl("auto_load", cmd)
		patchDialogs()
	}
	u.prepareDialog()
	tcl("set", subfoldersBtnVar, tclBool(input))
	tcl("set", subfoldersVar, tclBool(u.cfg.Subfolders))
	tcl("set", subfoldersTextVar, T("opt.subfolders"))
	eval.Eval(showSubfoldersTcl)
	p := filepath.FromSlash(tcl(append([]string{cmd}, opts...)...))
	u.rememberHidden()
	if sub := tcl("set", subfoldersVar) == "1"; input && p != "" && sub != u.cfg.Subfolders {
		u.cfg.Subfolders = sub
		u.saveConfig()
	}
	if p != "" {
		u.cfg.LastDir = p
		u.saveConfig()
	}
	return p
}

// The "Include subfolders" box of the folder dialog: shown when
// subfoldersBtnVar is 1, checked when subfoldersVar is 1.
const (
	subfoldersBtnVar  = "::gameforge_subfolders_btn"
	subfoldersVar     = "::gameforge_subfolders"
	subfoldersTextVar = "::gameforge_subfolders_text"
)

// showSubfoldersTcl shows or hides the box in a folder dialog made before;
// a new one gets it from the Create wrapper in patchDialogs.
const showSubfoldersTcl = `
set b .__tk_choosedir.contents.f2.gfsub
if {[winfo exists $b]} {
	$b configure -text $::gameforge_subfolders_text
	if {$::gameforge_subfolders_btn} {grid $b} else {grid remove $b}
}`

// outputDir returns the output folder of a tab ("switch" or a converter
// id); "" writes each result next to its file. Tabs without one of their
// own use the folder of older versions, which had one for all tabs.
func (u *ui) outputDir(tab string) string {
	if d, ok := u.cfg.OutputDirs[tab]; ok {
		return d
	}
	return u.cfg.OutputDir
}

// setOutputDir sets the output folder of a tab and, with show, puts it in
// the field when that tab is shown.
func (u *ui) setOutputDir(tab, dir string, show bool) {
	if u.cfg.OutputDirs == nil {
		u.cfg.OutputDirs = map[string]string{}
	}
	u.cfg.OutputDirs[tab] = dir
	u.saveConfig()
	if show && tab == u.cfg.Tab {
		u.showOutputDir()
	}
}

// showOutputDir puts the output folder of the shown tab in the field.
func (u *ui) showOutputDir() {
	if u.outEntry == nil {
		return
	}
	u.outTab = u.cfg.Tab
	u.outEntry.Configure(Textvariable(u.outputDir(u.outTab)))
}

// followInput makes the output folder of a tab the folder just opened in
// it, or the folder of the first file added or dropped.
func (u *ui) followInput(tab string, paths []string) {
	if len(paths) == 0 {
		return
	}
	dir := paths[0]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	u.setOutputDir(tab, dir, true)
}

// prepareDialog makes Tk's own file and folder dialogs (used on Linux,
// and on Windows when the app's language is not the system's) hide hidden
// files and folders, whose names start with a dot, unless the user showed
// them last time; the dialogs get a check box to show them. The Windows
// dialogs follow Explorer's setting instead.
func (u *ui) prepareDialog() {
	// The dialog's code sets both variables when it is loaded. auto_load
	// sources the file again even when it is loaded, which would undo
	// patchDialogs (the drives in the folder menu on Windows), so only the
	// first time.
	eval.Eval(`if {[info procs ::tk::dialog::file::Update] eq ""} {auto_load ::tk::dialog::file::}`)
	// Tk leaves out names starting with a dot only on Unix, where glob
	// skips them; on Windows it lists them, and files with the hidden
	// attribute too. Filter its listing the same way everywhere.
	eval.Eval(hiddenFilterTcl)
	tcl("set", "::tk::dialog::file::showHiddenBtn", "1")
	tcl("set", "::tk::dialog::file::showHiddenVar", tclBool(u.cfg.ShowHidden))
}

// hiddenFilterTcl wraps the listing of Tk's file dialog: unless hidden
// files are shown, it drops names starting with a dot and, on Windows,
// files and folders with the hidden attribute.
const hiddenFilterTcl = `
if {[info procs ::tk::dialog::file::GlobFiltered] eq ""} {auto_load ::tk::dialog::file::GlobFiltered}
if {![string match *gameforge-hidden* [info body ::tk::dialog::file::GlobFiltered]]} {
	catch {rename ::tk::dialog::file::GlobFilteredTk {}}
	rename ::tk::dialog::file::GlobFiltered ::tk::dialog::file::GlobFilteredTk
	proc ::tk::dialog::file::GlobFiltered {dir type {overrideFilter 0}} {
		# gameforge-hidden
		variable showHiddenVar
		upvar 1 data data ;# Tk's version reads its caller's data(filter)
		set result [GlobFilteredTk $dir $type $overrideFilter]
		if {$showHiddenVar} {
			return $result
		}
		set windows [expr {$::tcl_platform(platform) eq "windows"}]
		set out {}
		foreach f $result {
			if {[string index $f 0] eq "."} continue
			if {$windows && ![catch {file attributes [file join $dir $f] -hidden} hidden] && $hidden} continue
			lappend out $f
		}
		return $out
	}
}
`

// rememberHidden keeps the state of the dialog's "show hidden" box.
func (u *ui) rememberHidden() {
	if v := tcl("set", "::tk::dialog::file::showHiddenVar") == "1"; v != u.cfg.ShowHidden {
		u.cfg.ShowHidden = v
		u.saveConfig()
	}
}

// fileTypes formats a -filetypes list, with "all files" at the end.
func fileTypes(types []FileType) string {
	var items []string
	for _, t := range append(types, FileType{TypeName: "*", Extensions: []string{"*"}}) {
		items = append(items, tclList([]string{t.TypeName, tclList(t.Extensions)}))
	}
	return tclList(items)
}

func (u *ui) clearList() {
	u.paths, u.nsps, u.games = nil, nil, nil
	u.checked = map[string]bool{}
	u.refreshList()
}

// addPaths adds NSP files, scanning folders (and, with the option, their
// subfolders). With replace
// the current list and the log are cleared first, so logs of different
// folders do not mix.
func (u *ui) addPaths(paths []string, replace bool) {
	if u.busy {
		return
	}
	if replace {
		u.clearList()
		u.mu.Lock()
		u.pendingLog, u.logged = nil, false
		u.mu.Unlock()
		u.logLines = nil
		u.log.Clear()
	}
	known := map[string]bool{}
	for _, p := range u.paths {
		known[p] = true
	}
	var added []string
	for _, p := range paths {
		walkFiles(p, u.cfg.Subfolders, func(path string) {
			if !isGameFile(path) {
				return
			}
			if abs, err := filepath.Abs(path); err == nil && !known[abs] {
				known[abs] = true
				added = append(added, abs)
			}
		})
	}
	if len(added) == 0 {
		u.logf("log.no_new")
		return
	}
	u.paths = append(u.paths, added...)
	u.analyze(added)
}

// walkFiles calls fn for root if it is a file, or for the files in the
// folder root; with sub also for those in its subfolders.
func walkFiles(root string, sub bool, fn func(path string)) {
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if !sub && path != root {
				return fs.SkipDir
			}
			return nil
		}
		fn(path)
		return nil
	})
}

// gameExts are the file types the app reads: NSP, NSZ (compressed NSP) and
// XCI (game card image).
var gameExts = []string{".nsp", ".nsz", ".xci"}

func isGameFile(path string) bool {
	for _, ext := range gameExts {
		if strings.EqualFold(filepath.Ext(path), ext) {
			return true
		}
	}
	return false
}

func (u *ui) reanalyze() {
	u.nsps = nil
	u.checked = map[string]bool{}
	u.analyze(u.paths)
}

func (u *ui) analyze(paths []string) {
	u.setBusy(true)
	keys := u.keys
	go func() {
		defer u.recoverPanic()
		u.analyzeMu.Lock()
		defer u.analyzeMu.Unlock()
		var results []*merge.NSP
		for i, p := range paths {
			u.setStatus(T("status.analyzing", i+1, len(paths), filepath.Base(p)))
			u.setProgress(float64(i) / float64(len(paths)))
			n, err := merge.Analyze(p, keys)
			if err != nil {
				u.println(lazyf("✗ %v", err))
				continue
			}
			if n.Kind == merge.KindUnknown {
				u.logf("log.unknown", n.Name(), noteText(n))
			} else {
				u.println(lazyLine(func() string {
					src := T("file.via_cnmt")
					if !n.FromCNMT {
						src = T("file.via_name")
						if n.Note != nil {
							src += " — " + n.Note.Error()
						}
					}
					return fmt.Sprintf("• %s → %s %016X v%d [%s]", n.Name(), n.Kind, n.TitleID, n.Version, src)
				}))
			}
			results = append(results, n)
		}
		u.setProgress(0)
		u.setStatus(T("status.analyzed", len(results)))
		onGUI(func() {
			u.flush()
			u.nsps = append(u.nsps, results...)
			u.setBusy(false)
			u.refreshList()
		})
	}()
}

// resetChecks marks the default selection of every game: base, newest
// update and newest version of each DLC.
func (u *ui) resetChecks() {
	u.checked = map[string]bool{}
	for _, n := range u.nsps {
		u.checked[n.Path] = false
	}
	games, _ := merge.Group(u.nsps)
	for _, g := range games {
		for _, n := range g.DefaultSelection() {
			u.checked[n.Path] = true
		}
	}
}

// refreshList regroups the files, rebuilds the rows and pre-checks the
// default selection for files seen for the first time.
func (u *ui) refreshList() {
	games, unknown := merge.Group(u.nsps)
	u.games = games
	u.rows = u.rows[:0]
	for _, g := range games {
		defaults := map[*merge.NSP]bool{}
		for _, n := range g.DefaultSelection() {
			defaults[n] = true
		}
		u.rows = append(u.rows, row{game: g})
		for _, n := range g.Files() {
			if _, seen := u.checked[n.Path]; !seen {
				u.checked[n.Path] = defaults[n]
			}
			u.rows = append(u.rows, row{game: g, nsp: n})
		}
	}
	for _, n := range unknown {
		u.rows = append(u.rows, row{nsp: n})
	}
	u.fillTree()
	u.updateSummary()
}

// fillTree shows the rows, keeping the games the user collapsed collapsed
// and the scroll position.
func (u *ui) fillTree() {
	path := u.tree.String()
	yview := strings.Fields(tcl(path, "yview"))
	closed := map[uint64]bool{}
	for id, app := range u.gameItems {
		if tcl(path, "exists", id) == "1" && !tclTrue(tcl(path, "item", id, "-open")) {
			closed[app] = true
		}
	}
	u.gameItems = map[string]uint64{}
	u.tree.Delete(u.tree.Children(""))
	parent := ""
	for i, r := range u.rows {
		id := fmt.Sprintf("r%d", i)
		switch {
		case r.nsp == nil:
			title := fmt.Sprintf("%s   [%016X]", r.game.Title(), r.game.AppID)
			u.tree.Insert("", "end", Id(id), gameMark(r.game, u.allChecked(r.game)), Txt(title),
				Values(tclList([]string{describeGame(r.game, u.db)})), Open(!closed[r.game.AppID]), Tags("game"))
			u.gameItems[id] = r.game.AppID
			parent = id
		case r.game == nil:
			u.tree.Insert("", "end", Id(id), Image(checkImage(-1)), Txt(r.nsp.Name()),
				Values(tclList([]string{T("row.unknown")})), Tags("unknown"))
		default:
			var tags []string
			if r.game.DuplicateOf(r.nsp) != nil {
				tags = append(tags, "duplicate") // shown grayed: nothing to add
			}
			u.tree.Insert(parent, "end", Id(id), checkMark(u.checked[r.nsp.Path]), Txt(r.nsp.Name()),
				Values(tclList([]string{describeFile(r.game, r.nsp)})), Tags(tclList(tags)))
		}
	}
	fitColumns(u.tree)
	if len(yview) > 0 {
		tcl("update", "idletasks")
		tcl(path, "yview", "moveto", yview[0])
	}
}

// tclTrue reports whether a Tcl boolean is true.
func tclTrue(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// selectedFiles returns, per game, the checked files in merge order.
func (u *ui) selectedFiles() map[*merge.Game][]*merge.NSP {
	out := map[*merge.Game][]*merge.NSP{}
	for _, g := range u.games {
		for _, n := range g.Files() {
			if u.checked[n.Path] {
				out[g] = append(out[g], n)
			}
		}
	}
	return out
}

func (u *ui) updateSummary() {
	sel := u.selectedFiles()
	var total int64
	files := 0
	for _, l := range sel {
		files += len(l)
		total += merge.TotalSize(merge.Plan(l))
	}
	u.summary.Configure(Txt(T("summary", len(u.nsps), len(u.games), files, len(sel), humanSize(total))))
	enable(u.mergeBtn, len(sel) > 0 && !u.busy)
}

func (u *ui) setBusy(b bool) {
	if b != u.busy && u.log != nil {
		// A run starts or ends: what it logged goes to a file, if wanted.
		u.flush()
		if b {
			u.logStart = len(u.logLines)
		} else if u.cfg.SaveLog && u.logStart <= len(u.logLines) {
			if err := saveRunLog(u.cfg.Path(), u.logLines[u.logStart:], time.Now()); err != nil {
				u.logf("log.save_error", err)
				u.flush()
			}
		}
	}
	u.busy = b
	for _, btn := range u.addButtons {
		enable(btn, !b)
	}
	// Changing the language rebuilds the window, so it waits for idle.
	enableCombo(u.langBox, !b)
	enableCombo(u.themeBox, !b)
	enable(u.verify, !b)
	enable(u.onlineBtn, !b)
	enable(u.checkBtn, !b)
	enable(u.keysEntry, !b)
	for _, t := range u.tabs {
		t.setBusy(u, b)
	}
	u.updateCompressionOptions()
	u.updateSummary()
}

func (u *ui) startMerge() {
	selected := u.selectedFiles()
	var games []*merge.Game
	for _, g := range u.games {
		if len(selected[g]) > 0 {
			games = append(games, g)
		}
	}
	if u.busy || len(games) == 0 {
		return
	}
	outDir := strings.TrimSpace(u.outEntry.Textvariable())
	if outDir != "" {
		if st, err := os.Stat(outDir); err != nil || !st.IsDir() {
			MessageBox(Parent(App), Icon("error"), Title(T("app.title")), Msg(T("err.out_dir", outDir)))
			return
		}
	}

	opt := merge.Options{Keys: u.keys, Compress: u.compressing(), Verify: !u.cfg.SkipVerify}
	for _, l := range compressionLevels {
		if l.name == u.cfg.Level {
			opt.Level = l.level
		}
	}
	patch := u.cfg.ApplyUpdate && u.keys != nil

	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)

	go func() {
		defer u.recoverPanic()
		defer cancel()
		ok := 0
		for i, g := range games {
			sel := selected[g]
			dir := outDir
			if dir == "" {
				dir = filepath.Dir(sel[0].Path)
			}
			name := g.OutputName(sel, opt.Ext())
			if patch {
				name = strings.TrimSuffix(name, opt.Ext()) + "[patched]" + opt.Ext()
			}
			out := filepath.Join(dir, name)
			u.separate()
			if _, err := os.Stat(out); err == nil {
				u.logf("log.exists", filepath.Base(out))
				continue
			}
			u.println(lazyf("▶ [%d/%d] %s", i+1, len(games), filepath.Base(out)))
			hasBase := false
			for _, n := range sel {
				hasBase = hasBase || n.Kind == merge.KindBase
				u.println(lazyf("    + %s (%s v%d)", n.Name(), n.Kind, n.Version))
			}
			if !hasBase {
				u.logf("log.no_base")
			}

			start := time.Now()
			phaseStart := start
			lastPhase := merge.PhaseWrite
			write := merge.Write
			if patch {
				write = merge.WritePatched
			}
			res, err := write(ctx, sel, out, opt, func(phase merge.Phase, done, total int64) {
				if phase != lastPhase {
					lastPhase, phaseStart = phase, time.Now()
				}
				speed := int64(float64(done) / max(time.Since(phaseStart).Seconds(), 0.001))
				text := fmt.Sprintf("%s — %s / %s (%s/s)", g.Title(), humanSize(done), humanSize(total), humanSize(speed))
				if phase == merge.PhaseVerify {
					text = T("status.verifying", g.Title(), humanSize(done), humanSize(total))
				}
				text += eta(phaseStart, done, total)
				u.setProgress(float64(done) / float64(max(total, 1)))
				u.setStatus(text)
			}, u.println)
			if err != nil {
				if ctx.Err() != nil {
					u.logf("log.cancelled")
					break
				}
				u.logf("log.merge_error", err)
				continue
			}
			ok++
			saved := 0
			if res.InputSize > 0 {
				saved = int(100 - res.OutputSize*100/res.InputSize)
			}
			key := "log.done"
			if saved < 0 { // NCZ restored to NCA
				key, saved = "log.done_larger", -saved
			}
			u.logf(key, time.Since(start).Round(time.Second), filepath.Base(out),
				humanSize(res.InputSize), humanSize(res.OutputSize), saved)
		}
		u.setProgress(0)
		u.setStatus(T("status.done", ok, len(games)))
		onGUI(func() {
			u.flush()
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
		})
	}()
}

// startCheck verifies an existing NSP or NSZ in the background.
// checkedPaths are the files checked in the Switch list, in list order.
func (u *ui) checkedPaths() []string {
	var paths []string
	for _, n := range u.nsps {
		if u.checked[n.Path] {
			paths = append(paths, n.Path)
		}
	}
	return paths
}

// startCheck checks backups one after the other; with more than one, a
// summary at the end tells how many are intact and names the others.
func (u *ui) startCheck(paths []string) {
	if u.busy || len(paths) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)
	keys := u.keys

	go func() {
		defer u.recoverPanic()
		defer cancel()
		var ok int
		var bad []string
		var result i18n.Message
		for i, path := range paths {
			if ctx.Err() != nil {
				break
			}
			name := filepath.Base(path)
			if len(paths) > 1 {
				u.separate()
				u.logf("check.start_n", name, i+1, len(paths))
			} else {
				u.logf("check.start", name)
			}
			start := time.Now()
			progress := func(done, total int64) {
				u.setProgress(float64(done) / float64(max(total, 1)))
				u.setStatus(T("status.checking", name, humanSize(done), humanSize(total)) + eta(start, done, total))
			}
			var res merge.CheckResult
			var err error
			hunks := -1
			if strings.EqualFold(filepath.Ext(path), ".chd") {
				hunks, err = chd.Verify(ctx, path, progress)
			} else {
				res, err = merge.Check(ctx, path, keys, progress, u.println)
			}
			var shaErr *chd.HeaderSHA1Error
			switch {
			case ctx.Err() != nil:
				result = i18n.M("log.cancelled")
			case errors.As(err, &shaErr):
				result = i18n.M("check.chd_header_sha1", name, shaErr.Hunks,
					hex.EncodeToString(shaErr.Stored[:]), hex.EncodeToString(shaErr.Actual[:]))
			case err != nil:
				result = i18n.M("log.merge_error", err)
			case hunks >= 0:
				result = i18n.M("check.chd_ok", name, hunks)
			case res.Problems > 0:
				result = i18n.M("check.failed", name, res.Problems)
			default:
				result = i18n.M("check.ok", name, res.NCAs)
			}
			u.println(result)
			switch {
			case ctx.Err() != nil:
			case err != nil || res.Problems > 0:
				bad = append(bad, name)
			default:
				ok++
			}
		}
		if len(paths) > 1 {
			u.separate()
			result = i18n.M("check.summary", ok+len(bad), len(paths), ok, len(bad))
			u.println(result)
			for _, name := range bad {
				u.logf("check.summary_bad", name)
			}
		}
		u.setProgress(0)
		u.setStatus(result.String())
		onGUI(func() {
			u.flush()
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
		})
	}()
}

// println appends a line to the log; safe to call from any goroutine. The
// line is kept as a fmt.Stringer, so that a language change can show the
// whole log again in the new language.
func (u *ui) println(line fmt.Stringer) {
	u.mu.Lock()
	u.pendingLog = append(u.pendingLog, line)
	u.logged = true
	u.mu.Unlock()
}

// separate starts a new block in the log: a blank line, unless the log is
// still empty. It goes before the first line about each file, so that the
// files of a batch are easy to tell apart.
func (u *ui) separate() {
	u.mu.Lock()
	if u.logged {
		u.pendingLog = append(u.pendingLog, lazyf(""))
	}
	u.mu.Unlock()
}

// logf appends the message for key to the log, translated when shown.
func (u *ui) logf(key string, args ...any) { u.println(i18n.M(key, args...)) }

// lazyLine is a log line computed each time it is shown.
type lazyLine func() string

func (f lazyLine) String() string { return f() }

// lazyf is a log line formatted when shown, so that arguments such as
// content kinds follow the current language.
func lazyf(format string, args ...any) lazyLine {
	return func() string { return fmt.Sprintf(format, args...) }
}

// setStatus and setProgress are safe to call from any goroutine; frequent
// updates are coalesced until the next flush.
func (u *ui) setStatus(s string) {
	u.mu.Lock()
	u.pendingStat = &s
	u.mu.Unlock()
}

func (u *ui) setProgress(v float64) {
	u.mu.Lock()
	u.pendingProg, u.progDirty = v, true
	u.mu.Unlock()
}

// onGUI runs f on the GUI thread; safe to call from any goroutine. f runs
// from a Tcl timer and not straight from the event PostEvent queues: Tk
// services queued events also inside its own short waits, which hold back
// all other window events (scrolling a text, waiting for a window to be
// mapped). A dialog opened there, as f often does, got no input at all and
// could not be closed; on Linux the GUI thread even spun at full CPU. Timers
// run only from the normal event loop.
func onGUI(f func()) {
	PostEvent(func() { TclAfter(0, f) }, false)
}

// wrapToWidth makes a label wrap its text at its own width, which follows
// the window: a wide window shows a description in fewer lines, a narrow one
// in more. The label must stretch across its column (sticky "we").
func wrapToWidth(label interface{ String() string }) {
	// Let the grid determine the width; wrapping must not change its request.
	tcl(label.String(), "configure", "-width", "1")
	tcl("bind", label.String(), "<Configure>",
		"if {%w > 1 && [%W cget -wraplength] != %w} {%W configure -wraplength %w}")
}

// flush shows the pending updates; it runs on the GUI thread.
func (u *ui) flush() {
	u.mu.Lock()
	lines, stat, prog, progDirty := u.pendingLog, u.pendingStat, u.pendingProg, u.progDirty
	items := u.pendingItems
	u.pendingLog, u.pendingStat, u.progDirty, u.pendingItems = nil, nil, false, nil
	u.mu.Unlock()
	u.flushItems(items)
	if len(lines) > 0 {
		u.logLines = append(u.logLines, lines...)
		u.log.Insert("end", renderLog(lines))
		u.log.See("end")
	}
	if stat != nil {
		u.status.Configure(Txt(*stat))
	}
	if progDirty {
		u.progress.Configure(Value(prog))
	}
}

var humanSize = merge.HumanSize

// eta returns "  •  about 2 min 10 s left" for work started at start, of
// which done of total bytes are finished; "" during the first seconds, when
// the speed is not known well enough yet.
func eta(start time.Time, done, total int64) string {
	elapsed := time.Since(start)
	if elapsed < 2*time.Second || done <= 0 || done >= total {
		return ""
	}
	left := time.Duration(float64(elapsed) * float64(total-done) / float64(done))
	return "  •  " + T("status.eta", formatDuration(left))
}

// formatDuration shows a duration in its two largest units: "45 s",
// "2 min 10 s", "1 h 05 min".
func formatDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%d s", max(s, 1))
	case s < 3600:
		return fmt.Sprintf("%d min %02d s", s/60, s%60)
	}
	return fmt.Sprintf("%d h %02d min", s/3600, s%3600/60)
}

// noteText is why a file's metadata could not be read, or "".
func noteText(n *merge.NSP) fmt.Stringer {
	return lazyLine(func() string {
		if n.Note == nil {
			return ""
		}
		return n.Note.Error()
	})
}
