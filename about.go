package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	. "modernc.org/tk9.0"

	"gameforge/internal/update"
)

// appVersion is the version of the app: 1.x was nspmerge (Switch only),
// 2.0.0 the first GameForge, with the disc image tabs, 2.1.0 added LPC to
// the FLAC of CD audio in CHD, 2.2.0 the highest Zstandard level and a
// block size option for RVZ, 2.2.1 moved the game database state from the
// log to the Switch tab, 2.3.0 added renaming Switch files, 2.3.1 made the
// log easier to read (file names only, a blank line between files) and
// added the conversion time to the file status, 2.4.0 added the 7z tab,
// 2.5.0 shows what disc images and CHDs say about their games, 2.6.0
// renames disc images with their Redump names, 2.7.0 shows the icons of
// PSP games, has a menu to update the Redump database, calls the CHD tab
// "Discs", hides hidden files in Tk's file dialogs, evens out the tabs of
// the clam theme and keeps one output folder per tab, 2.7.1 renames the
// RetroArch saves, states and thumbnails of renamed disc images, keeps
// Tk's message boxes from being resized and has an About window with a
// link to the project's page, 2.8.0 renames ROMs with their No-Intro names,
// 2.9.0 checks ROMs against RetroAchievements and keeps the downloaded
// databases in a cache folder, 2.10.0 shows the icons of DS games, the
// labels of Dreamcast discs and the title images of Xbox games, 2.11.0
// renames Neo Geo games in the .neo format and compresses several files at
// a time in the 7z tab, and checks once a day (if the user agrees) whether
// a newer version is out, 2.11.1 renames and checks only the checked
// files, gives CHDs, RVZs and 7z files in the conversion tabs a check box
// and lets the file lists scroll sideways, 2.12.0 finds the duplicates in
// the ROMs tab (the same game in several files, such as a .neo, a .7z and
// MAME's .zip set), checks the extra copies and deletes the checked files,
// 2.13.0 checks for new versions daily, weekly or monthly, chosen in the
// About window, which can also check at once, 2.13.1 waits for the window
// to be shown before asking about the check, since on KDE Wayland the
// question got no input and could be neither answered nor closed, 2.14.0
// checks arcade sets against the lists of sets of FBNeo and of RetroArch's
// MAME cores (2000, 2003, 2003-Plus, 2010, 2015, 2016) and of Flycast
// (Naomi, Atomiswave), with their CHDs, 2.15.0 checks Neo Geo .neo files
// (Geolith) and tells their AES and MVS versions, and tells .neo files
// apart from arcade sets in the arcade check, 2.16.0 extracts CHDs back to
// CUE/BIN (named as Redump's), GDI or ISO, and makes the ISO → CSO/ZSO tab
// part of the Discs tab, which also lists and extracts CSO and ZSO files,
// as well as the Xbox → XISO and GameCube/Wii → RVZ tabs, and renames Xbox
// discs with Redump names, 2.17.0 trims Nintendo DS ROMs and restores their
// full size, recognizes trimmed ROMs when renaming, shows the game code and
// title of DS and Nintendo 64 ROMs, converts .v64 and .n64 ROMs to .z64,
// and keeps one version of each game by region (1G1R), 2.17.1 opens the
// dialogs at the end of a task (Neo Geo marking, renaming, …) only from the
// normal event loop, since one opened while Tk scrolled the log got no input
// on Linux and kept the program busy, 2.18.0 opens the page of the hashes
// RetroAchievements accepts for a game with a click on the status of a
// version it does not support, saves an HTML report of each
// RetroAchievements check for each console, and wraps the description of
// each tab at the window's width, 2.19.0 makes that report optional, off
// by default, with an entry in the names database menu, and asks at
// startup, if the user agreed and as often as it checks for new versions,
// whether to update the downloaded lists that have grown old, and converts
// .bin.ecm, .nrg and .cdi images and disc images in ZIP or 7z files, and
// explains a CHD whose hunks are intact but whose header holds the SHA-1 of
// other data (chdman verify fails such files too), checks the header
// checksums of N64, SNES, Mega Drive and DS ROMs, converts .smd to .md and
// removes SNES and PC Engine copier headers, 2.20.0 converts Wii WBFS
// images to RVZ, 2.20.1 reads subfolders of opened folders only with the
// new "Include subfolders" option, 2.20.2 keeps collapsed Switch games
// collapsed when the list is redrawn, 2.20.3 fixes what an audit found:
// truncated NCZs, config saves, same-name outputs, panics, case-only
// renames and permanent deletes on Windows, 2.21.0 writes a report of the
// games in a folder of console folders, 2.21.1 shows arcade sets in the
// ROMs tab by their titles, 2.22.0 compresses ROMs to ZIP as well as 7z
// and stops the descriptions from resizing the window on macOS; it also
// checks many Switch backups at once, exports the missing updates and DLCs
// to CSV and can save the log of each run, 2.23.0 asks the project's server
// for new versions, which counts the checks, 2.23.1 fixes what an audit
// found: checks lost on a format change, slow .bin folders, arcade titles
// of the collection report and the CSV separator, 2.23.2 writes the
// details of ZIP files in the ROMs tab like those of 7z files, 2.23.3 shows
// the console of the ROMs inside 7z and ZIP files in the Platform column,
// the system of arcade sets, filters the RetroAchievements report and moves
// its choice to the Check menu of the ROMs tab.
const appVersion = "2.23.3"

// repoURL is the project's page, with the source code and the releases.
const repoURL = "https://github.com/tschertel/gameforge"

// aboutWin is the open About window, if any.
var aboutWin *ToplevelWidget

// showAbout shows the version, what the app does, what it builds on and a
// link to the project's page, in a window of fixed size over the main one
// (a message box cannot hold a link).
func (u *ui) showAbout() {
	if aboutWin != nil && tcl("winfo", "exists", aboutWin.String()) == "1" {
		tcl("raise", aboutWin.String())
		return
	}
	top := Toplevel()
	aboutWin = top
	path := top.String()
	tcl("wm", "withdraw", path)
	top.WmTitle(T("about.title"))
	tcl("wm", "transient", path, ".")
	tcl("wm", "resizable", path, "0", "0")
	tcl(path, "configure", "-background", tcl(".", "cget", "-background"))

	f := top.TFrame(Padding("5m 4m"))
	Grid(f, Row(0), Column(0), Sticky("news"))
	icon := f.TLabel()
	tcl("catch", icon.String()+" configure -image ::tk::icons::information")
	Grid(icon, Row(0), Column(0), Rowspan(3), Sticky("n"), Padx("0 4m"))
	tcl("catch", "font create GameForgeTitle {*}[font configure TkDefaultFont] -weight bold")
	Grid(f.TLabel(Txt(T("app.title")+" "+appVersion), Font("GameForgeTitle")), Row(0), Column(1), Sticky("w"))
	text := T("about.text", runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)
	Grid(f.TLabel(Txt(text), Wraplength("110m"), Justify("left")), Row(1), Column(1), Sticky("w"), Pady("3m 0"))

	link := f.TLabel(Txt(repoURL), Foreground(linkColor()), Font("GameForgeLink"), Cursor("hand2"))
	Bind(link, "<Button-1>", Command(func() { openURL(repoURL) }))
	Grid(link, Row(2), Column(1), Sticky("w"), Pady("3m 0"))

	// How often to check for a new version, a button to check now, and
	// what the last check found.
	box := f.TLabelframe(Txt(T("upd.settings")), Padding("3m 2m"))
	Grid(box, Row(3), Column(0), Columnspan(2), Sticky("we"), Pady("4m 0"))
	label := box.TLabel(Txt(T("upd.option")))
	var choices []string
	width := 0
	for i := 0; i <= len(updateIntervals); i++ {
		c := T("upd.off")
		if i > 0 {
			c = T(updateIntervals[i-1].key)
		}
		choices = append(choices, c)
		width = max(width, len([]rune(c)))
	}
	every := box.TCombobox(State("readonly"), Values(tclList(choices)), Width(width+1))
	every.Current(u.updateChoice())
	Bind(every, "<<ComboboxSelected>>", Command(func() {
		i, _ := strconv.Atoi(every.Current(nil))
		u.setUpdateChoice(i)
	}))
	setTooltip(label, updateDetail("upd.option.tip"))
	setTooltip(every, updateDetail("upd.option.tip"))
	result := box.TLabel(Wraplength("110m"), Justify("left"))
	showResult := func() {
		if update.Newer(u.cfg.UpdateLatest, appVersion) {
			result.Configure(Txt(T("upd.note", u.cfg.UpdateLatest)), Foreground(linkColor()), Font("GameForgeLink"), Cursor("hand2"))
			return
		}
		text := ""
		if !u.cfg.UpdateChecked.IsZero() {
			text = T("upd.last", u.cfg.UpdateChecked.Format("2006-01-02 15:04"))
		}
		result.Configure(Txt(text), Foreground(""), Font("TkDefaultFont"), Cursor(""))
	}
	showResult()
	Bind(result, "<Button-1>", Command(u.openUpdatePage))
	var now *TButtonWidget
	now = box.TButton(Txt(T("upd.now")), Command(func() {
		now.Configure(State("disabled"))
		result.Configure(Txt(T("upd.checking")), Foreground(""), Font("TkDefaultFont"), Cursor(""))
		u.fetchUpdate(func(r update.Release, err error) {
			if aboutWin == nil || tcl("winfo", "exists", path) != "1" {
				return
			}
			now.Configure(State("normal"))
			switch {
			case err != nil:
				result.Configure(Txt(T("upd.failed", err)))
			case update.Newer(r.Version, appVersion):
				showResult()
			default:
				result.Configure(Txt(T("upd.current", appVersion)))
			}
		})
	}))
	setTooltip(now, T("upd.now.tip"))
	Grid(label, Row(0), Column(0), Sticky("w"))
	Grid(every, Row(0), Column(1), Sticky("w"), Padx("2m"))
	Grid(now, Row(0), Column(2), Sticky("w"))
	listsVar := Variable(tclBool(u.listsOn()))
	lists := box.TCheckbutton(Txt(T("upd.lists")), listsVar, Command(func() {
		on := listsVar.Get() == "1"
		u.cfg.ListsCheck = &on
		u.saveConfig()
	}))
	setTooltip(lists, T("upd.lists.tip"))
	Grid(lists, Row(1), Column(0), Columnspan(3), Sticky("w"), Pady("2m 0"))
	Grid(result, Row(2), Column(0), Columnspan(3), Sticky("w"), Pady("2m 0"))

	// Saving the log of each run, for later review.
	logVar := Variable(tclBool(u.cfg.SaveLog))
	saveLog := f.TCheckbutton(Txt(T("opt.save_log")), logVar, Command(func() {
		u.cfg.SaveLog = logVar.Get() == "1"
		u.saveConfig()
	}))
	setTooltip(saveLog, T("opt.save_log.tip", filepath.Join(filepath.Dir(u.cfg.Path()), logsDirName)))
	Grid(saveLog, Row(4), Column(0), Columnspan(2), Sticky("w"), Pady("3m 0"))

	closeWin := func() {
		tcl("grab", "release", path)
		Destroy(top)
		aboutWin = nil
	}
	ok := f.TButton(Txt("OK"), Default("active"), Command(closeWin))
	Grid(ok, Row(5), Column(0), Columnspan(2), Pady("5m 0"))
	Bind(top, "<Escape>", Command(closeWin))
	Bind(top, "<Return>", Command(closeWin))
	WmProtocol(top.Window, WM_DELETE_WINDOW, Command(closeWin))

	// Centered over the main window, then shown and kept in front of it.
	tcl("update", "idletasks")
	num := func(words ...string) int { n, _ := strconv.Atoi(tcl(words...)); return n }
	x := num("winfo", "rootx", ".") + (num("winfo", "width", ".")-num("winfo", "reqwidth", path))/2
	y := num("winfo", "rooty", ".") + (num("winfo", "height", ".")-num("winfo", "reqheight", path))/3
	tcl("wm", "geometry", path, fmt.Sprintf("+%d+%d", max(x, 0), max(y, 0)))
	tcl("wm", "deiconify", path)
	tcl("focus", ok.String())
	tcl("catch", "grab set "+path)
}

// linkColor is the color of links; it also creates their font.
func linkColor() string {
	tcl("catch", "font create GameForgeLink {*}[font configure TkDefaultFont] -underline 1")
	if darkTheme {
		return "#6cb6ff"
	}
	return "#0b5cad"
}

// openURL opens a web page in the user's browser.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
