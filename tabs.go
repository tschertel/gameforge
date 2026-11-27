package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	. "modernc.org/tk9.0"

	"gameforge/internal/arcade"
	"gameforge/internal/i18n"
)

// converter is a file conversion with a tab of its own. Adding a format to
// the app is adding a converter to newConvTabs.
type converter struct {
	id     string // config name of the tab
	title  string // message keys: tab title, description, button and its tip
	info   string
	button string
	tip    string
	exts   []string          // input extensions
	accept func(string) bool // optional check beyond the extension
	// anyExt converters take files of any extension: the tab keeps every
	// file it is given in a pool and lists those accept takes (exts is
	// unused). refilter lists them again after the choice changes.
	anyExt bool
	// drop, if set, limits the files dropped on the window that go to this
	// tab when it is not shown (e.g. .iso goes to the Discs tab by default).
	drop  func(string) bool
	types []FileType // for the file dialog
	// describe fills in what a file is (ok, platform, detail), or why it
	// cannot be converted. It is called again when the item's mode changes.
	describe func(it *convItem)
	// outName is the output file name for an item.
	outName func(it *convItem) string
	// run converts the item to out, logging what it found. progress reports
	// the conversion, then the verification (verifying true).
	run func(ctx context.Context, u *ui, it *convItem, out string, verify bool,
		progress func(verifying bool, done, total int64)) (inSize, outSize int64, err error)
	// Optional: buttons for the action row, options below the list, entries
	// of the right-click menu of an item, and work after a batch.
	extra   func(u *ui, parent *TFrameWidget) []Widget
	options func(u *ui, parent *TFrameWidget) []Widget
	// options2, if set, is a second row of options below the first.
	options2 func(u *ui, parent *TFrameWidget) []Widget
	menu     func(u *ui, t *convTab, it *convItem) []menuEntry
	after    func(u *ui, done []converted)
	// jobs, if set, is how many files are converted at a time (else 1).
	jobs func(u *ui) int
	// back, if set, is a conversion the other way (e.g. CHD to CUE/BIN)
	// of the checked files listed for what they are, with a button of its
	// own next to the convert button.
	back *convAction
}

// convAction is a conversion of the checked files of a tab: the tab's own
// (from the converter's outName, run and after) or its back conversion.
type convAction struct {
	button, tip string                  // message keys
	accept      func(it *convItem) bool // the items it converts (back only)
	outName     func(it *convItem) string
	run         func(ctx context.Context, u *ui, it *convItem, out string, verify bool,
		progress func(verifying bool, done, total int64)) (inSize, outSize int64, err error)
	after func(u *ui, done []converted)
}

// menuEntry is an entry of an item's right-click menu.
type menuEntry struct {
	label    string
	selected bool
	action   func()
}

// converted is a finished conversion, for converter.after.
type converted struct {
	item *convItem
	out  string
}

// convItem is a file in a conversion tab.
type convItem struct {
	path     string
	mode     string // converter-specific choice, e.g. "cd" or "dvd"; "" = automatic
	kind     string // what describe resolved, e.g. "cd" or "dvd"
	size     int64  // input size including the files a sheet refers to; 0 = the file's size
	ok       bool
	infoOnly bool   // listed for what it is, not to convert (e.g. a CHD)
	icon     []byte // PNG shown next to the check box (PSP games)
	// iconLater, if set, reads an icon that takes long to read (a DS ROM
	// in a 7z archive); it runs in the background after the file is listed.
	iconLater func() []byte
	platform  string
	detail    fmt.Stringer
	status    fmt.Stringer
	tab       *convTab
	id        string // row in the list
}

// convTab holds a converter's file list; the widgets are rebuilt with the
// window.
type convTab struct {
	conv    *converter
	items   []*convItem
	checked map[string]bool
	pool    []string    // anyExt: every file given to the tab, listed or not
	dupes   []dupeGroup // the ROMs tab: what the last duplicate check found

	tree    *TTreeviewWidget
	summary *TLabelWidget
	runBtn  *TButtonWidget
	backBtn *TButtonWidget
	buttons []interface{ Configure(...Opt) *Window } // disabled while busy
	widgets []Widget                                 // options, disabled while busy
}

func newConvTabs() []*convTab {
	var tabs []*convTab
	for _, c := range []*converter{chdConverter, sevenZipConverter} {
		tabs = append(tabs, &convTab{conv: c, checked: map[string]bool{}})
	}
	return tabs
}

func hasExt(path string, exts []string) bool {
	for _, ext := range exts {
		if strings.EqualFold(filepath.Ext(path), ext) {
			return true
		}
	}
	return false
}

// accepts reports whether a file belongs to this tab.
func (t *convTab) accepts(path string) bool {
	if t.conv.anyExt {
		return t.conv.accept(path)
	}
	return hasExt(path, t.conv.exts) && (t.conv.accept == nil || t.conv.accept(path))
}

// add lists the accepted files among paths, scanning folders (and, with
// the option, their subfolders), and returns
// how many were new.
func (t *convTab) add(u *ui, paths []string) int {
	known := map[string]bool{}
	for _, it := range t.items {
		known[it.path] = true
	}
	pooled := map[string]bool{}
	for _, p := range t.pool {
		pooled[p] = true
	}
	n := 0
	for _, p := range paths {
		walkFiles(p, u.cfg.Subfolders, func(path string) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return
			}
			if t.conv.anyExt && !pooled[abs] {
				pooled[abs] = true
				t.pool = append(t.pool, abs)
			}
			if known[abs] || !t.accepts(abs) {
				return
			}
			known[abs] = true
			t.list(u, abs)
			n++
		})
	}
	return n
}

// list adds a file to the list, checked when it can be converted.
func (t *convTab) list(u *ui, path string) {
	it := &convItem{path: path, tab: t}
	t.conv.describe(it)
	if it.iconLater != nil {
		u.loadIconLater(it)
	}
	t.items = append(t.items, it)
	t.checked[path] = it.ok || it.infoOnly
	if !it.ok && !it.infoOnly {
		u.println(lazyf("✗ %s: %s", filepath.Base(path), it.detail))
	}
}

// refilter lists again the pooled files that the converter now accepts,
// in pool order, keeping the items and choices already there.
func (t *convTab) refilter(u *ui) {
	old := map[string]*convItem{}
	for _, it := range t.items {
		old[it.path] = it
	}
	t.items = nil
	for _, p := range t.pool {
		if !t.accepts(p) {
			delete(t.checked, p)
			continue
		}
		if it, ok := old[p]; ok {
			t.items = append(t.items, it)
		} else {
			t.list(u, p)
		}
	}
	t.refresh(u)
}

// markable reports whether an item has a check box: a file to convert, or
// one listed for what it is (a CHD, an RVZ, a 7z), which the tab's other
// actions (renaming, the RetroAchievements check) still work on.
func markable(it *convItem) bool { return it.ok || it.infoOnly }

// marked reports whether an item is checked.
func (t *convTab) marked(it *convItem) bool { return markable(it) && t.checked[it.path] }

// keepChecked keeps the checked items among those an action works on. If
// there are some but none is checked, it says so and returns false.
func (t *convTab) keepChecked(items []*convItem) ([]*convItem, bool) {
	var out []*convItem
	for _, it := range items {
		if t.marked(it) {
			out = append(out, it)
		}
	}
	if len(out) == 0 && len(items) > 0 {
		MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("list.none_checked")))
		return nil, false
	}
	return out, true
}

func (t *convTab) checkedItems() []*convItem {
	var out []*convItem
	for _, it := range t.items {
		if it.ok && t.checked[it.path] {
			out = append(out, it)
		}
	}
	return out
}

// build fills the tab: actions, a description, the list, options and the
// convert button. It returns the action row, to size the window.
func (t *convTab) build(u *ui, tab *TFrameWidget) *TFrameWidget {
	GridColumnConfigure(tab.Window, 0, Weight(1))
	GridRowConfigure(tab.Window, 2, Weight(1))

	actions := tab.TFrame()
	Grid(actions, Row(0), Column(0), Sticky("w"))
	openDir := actions.TButton(Txt(T("btn.open_folder")), Command(func() {
		if p := u.openFolder(true); p != "" {
			u.followInput(t.conv.id, []string{p})
			t.addAndShow(u, []string{p})
		}
	}))
	addFile := actions.TButton(Txt(T("btn.add_nsp")), Command(func() {
		if ps := u.openFiles(t.conv.types); len(ps) > 0 {
			u.followInput(t.conv.id, ps)
			t.addAndShow(u, ps)
		}
	}))
	clearBtn := actions.TButton(Txt(T("btn.clear")), Command(func() {
		t.items, t.checked, t.pool = nil, map[string]bool{}, nil
		t.refresh(u)
	}))
	all := actions.TButton(Txt(T("btn.select_all")), Command(func() { t.checkAll(u, true) }))
	none := actions.TButton(Txt(T("btn.select_none")), Command(func() { t.checkAll(u, false) }))
	t.buttons = []interface{ Configure(...Opt) *Window }{openDir, addFile, clearBtn, all, none}
	row := []Widget{openDir, addFile, clearBtn, actions.TSeparator(Orient("vertical")), all, none}
	if t.conv.extra != nil {
		row = append(row, actions.TSeparator(Orient("vertical")))
		for _, w := range t.conv.extra(u, actions) {
			row = append(row, w)
			switch b := w.(type) {
			case *TButtonWidget:
				t.buttons = append(t.buttons, b)
			case *TMenubuttonWidget:
				t.buttons = append(t.buttons, b)
			}
		}
	}
	gridRow(row...)

	info := tab.TLabel(Txt(T(t.conv.info)), Wraplength("240m"), Justify("left"))
	Grid(info, Row(1), Column(0), Sticky("we"), pad)
	wrapToWidth(info)

	listFrame := tab.TFrame()
	Grid(listFrame, Row(2), Column(0), Sticky("news"), pad)
	t.tree = t.buildTree(u, listFrame)

	t.widgets = nil
	for row, options := range []func(*ui, *TFrameWidget) []Widget{t.conv.options, t.conv.options2} {
		if options == nil {
			continue
		}
		opts := tab.TFrame()
		Grid(opts, Row(3+row), Column(0), Sticky("w"))
		widgets := options(u, opts)
		for i, w := range widgets {
			Grid(w, Row(0), Column(i), Sticky("w"), pad)
		}
		t.widgets = append(t.widgets, widgets...)
	}

	run := tab.TFrame()
	Grid(run, Row(5), Column(0), edges)
	GridColumnConfigure(run.Window, 0, Weight(1))
	t.summary = run.TLabel()
	Grid(t.summary, Row(0), Column(0), Sticky("w"), pad)
	t.backBtn = nil
	if b := t.conv.back; b != nil {
		t.backBtn = run.TButton(Txt(T(b.button)), Command(func() { t.startWith(u, t.backItems(), *b) }))
		setTooltip(t.backBtn, T(b.tip))
		Grid(t.backBtn, Row(0), Column(1), pad, ipad)
	}
	t.runBtn = run.TButton(Txt(T(t.conv.button)), Default("active"), Command(func() { t.start(u) }))
	setTooltip(t.runBtn, T(t.conv.tip))
	Grid(t.runBtn, Row(0), Column(2), pad, ipad)
	return actions
}

func (t *convTab) buildTree(u *ui, parent *TFrameWidget) *TTreeviewWidget {
	tv := scrolledTree(parent, Columns("platform detail status"), Selectmode("browse"), Height(12))
	tv.Heading("#0", Txt(T("list.file")), Anchor("w"))
	tv.Heading("platform", Txt(T("list.platform")), Anchor("w"))
	tv.Heading("detail", Txt(T("list.detail")), Anchor("w"))
	tv.Heading("status", Txt(T("list.status")), Anchor("w"))
	tv.Column("#0", Width(380), Stretch(true))
	tv.Column("platform", Width(110), Stretch(false))
	tv.Column("detail", Width(380), Stretch(true))
	tv.Column("status", Width(200), Stretch(true))
	tv.TagConfigure("unknown", Foreground("gray50"))
	Bind(tv, "<ButtonRelease-1>", Command(func(e *Event) {
		// Only a click on the check box toggles, not one on an icon drawn
		// right of it in the same image (as in the Switch list).
		isImage := func(x int) bool { return strings.HasSuffix(tv.IdentifyElement(x, e.Y), "image") }
		if isImage(e.X) && !isImage(e.X-checkBoxImage(0).Bounds().Dx()) {
			t.toggle(u, tv.IdentifyItem(e.X, e.Y))
		}
		if url := t.linkAt(tv, e.X, e.Y); url != "" {
			openURL(url)
		}
	}))
	cursor := ""
	Bind(tv, "<Motion>", Command(func(e *Event) {
		c := ""
		if t.linkAt(tv, e.X, e.Y) != "" {
			c = "hand2"
		}
		if c != cursor {
			cursor = c
			tcl(tv.String(), "configure", "-cursor", c)
		}
	}))
	Bind(tv, "<space>", Command(func() { t.toggle(u, tv.Focus()) }))
	if t.conv.menu != nil {
		Bind(tv, "<Button-3>", Command(func(e *Event) { t.popup(u, tv.IdentifyItem(e.X, e.Y), e.XRoot, e.YRoot) }))
	}
	return tv
}

// linkAt is the web page of the status under x, y in the list, or "": a
// status such as "only another version is supported" opens the page of the
// game on RetroAchievements when clicked.
func (t *convTab) linkAt(tv *TTreeviewWidget, x, y int) string {
	X, Y := strconv.Itoa(x), strconv.Itoa(y)
	if tcl(tv.String(), "identify", "region", X, Y) != "cell" ||
		tcl(tv.String(), "identify", "column", X, Y) != "#3" { // status
		return ""
	}
	it := t.item(tv.IdentifyItem(x, y))
	if it == nil {
		return ""
	}
	return statusLink(it.status)
}

func (t *convTab) item(id string) *convItem {
	i, err := strconv.Atoi(strings.TrimPrefix(id, "c"))
	if err != nil || i < 0 || i >= len(t.items) {
		return nil
	}
	return t.items[i]
}

func (t *convTab) toggle(u *ui, id string) {
	it := t.item(id)
	if u.busy || it == nil || !markable(it) {
		return
	}
	t.checked[it.path] = !t.checked[it.path]
	t.refresh(u)
	t.tree.Focus(id)
	t.tree.Selection("set", id)
}

// popup shows the right-click menu of an item.
func (t *convTab) popup(u *ui, id string, x, y int) {
	it := t.item(id)
	if u.busy || it == nil {
		return
	}
	entries := t.conv.menu(u, t, it)
	if len(entries) == 0 {
		return
	}
	t.tree.Focus(id)
	t.tree.Selection("set", id)
	m := Menu(Tearoff(false))
	for _, e := range entries {
		label := "     " + e.label
		if e.selected {
			label = "✓  " + e.label
		}
		m.AddCommand(Lbl(label), Command(e.action))
	}
	Popup(m.Window, x, y, nil)
}

func (t *convTab) checkAll(u *ui, v bool) {
	for _, it := range t.items {
		t.checked[it.path] = v && markable(it)
	}
	t.refresh(u)
}

// addAndShow adds files and shows the result.
func (t *convTab) addAndShow(u *ui, paths []string) {
	if u.busy {
		return
	}
	if t.add(u, paths) == 0 {
		u.logf("log.no_new_files")
	}
	t.refresh(u)
}

func stringOf(s fmt.Stringer) string {
	if s == nil {
		return ""
	}
	return s.String()
}

// arcadeSet is the arcade set a ZIP or 7z of the ROMs tab is named after,
// from the arcade lists, or nil.
func (t *convTab) arcadeSet(u *ui, it *convItem) *arcade.Set {
	name := filepath.Base(it.path)
	if ext := extOf(name); t.conv.id != "7z" || (ext != ".zip" && ext != ".7z") {
		return nil
	}
	return u.arcadeLists().Set(strings.TrimSuffix(name, filepath.Ext(name)))
}

// rowName is how the list shows a file: in the ROMs tab, an arcade set by
// its title from the arcade lists, with the file name in brackets.
func (t *convTab) rowName(u *ui, it *convItem) string {
	name := filepath.Base(it.path)
	if s := t.arcadeSet(u, it); s != nil && s.Title != "" {
		return s.Title + " [" + name + "]"
	}
	return name
}

// rowPlatform is the Platform column: for an arcade set whose files tell
// no console, its system from the arcade lists ("Arcade", "Naomi"…).
func (t *convTab) rowPlatform(u *ui, it *convItem) string {
	if it.platform == "ZIP" || it.platform == "7z" {
		if s := t.arcadeSet(u, it); s != nil {
			return s.System()
		}
	}
	return it.platform
}

// refresh shows the list, keeping the scroll position, and updates the
// summary and the button.
func (t *convTab) refresh(u *ui) {
	if t.tree == nil {
		return
	}
	path := t.tree.String()
	yview := strings.Fields(tcl(path, "yview"))
	t.tree.Delete(t.tree.Children(""))
	var size int64
	// Icons are drawn as wide as the widest one, so that the names line up.
	width := 0
	for _, it := range t.items {
		if len(it.icon) > 0 && rowHeight > 0 {
			width = max(width, iconWidth(it.icon, rowHeight-4))
		}
	}
	for i, it := range t.items {
		it.id = fmt.Sprintf("c%d", i)
		values := Values(tclList([]string{t.rowPlatform(u, it), stringOf(it.detail), stringOf(it.status)}))
		state := map[bool]int{true: 1, false: 0}[t.checked[it.path]]
		if !markable(it) {
			state = -1
		}
		mark, hasIcon := discMark(it.icon, state, it.path, width)
		if !hasIcon {
			mark = Image(checkImage(state))
		}
		if !markable(it) {
			t.tree.Insert("", "end", Id(it.id), mark, Txt(t.rowName(u, it)), values, Tags("unknown"))
			continue
		}
		t.tree.Insert("", "end", Id(it.id), mark, Txt(t.rowName(u, it)), values)
		if t.checked[it.path] {
			if it.size > 0 {
				size += it.size
			} else if st, err := os.Stat(it.path); err == nil {
				size += st.Size()
			}
		}
	}
	fitColumns(t.tree)
	if len(yview) > 0 {
		tcl(path, "yview", "moveto", yview[0])
	}
	marked := 0
	for _, it := range t.items {
		if t.marked(it) {
			marked++
		}
	}
	t.summary.Configure(Txt(T("summary.conv", len(t.items), marked, humanSize(size))))
	t.setBusy(u, u.busy)
}

func (t *convTab) setBusy(u *ui, busy bool) {
	if t.runBtn == nil {
		return
	}
	for _, b := range t.buttons {
		enable(b, !busy)
	}
	for _, w := range t.widgets {
		if f, ok := w.(*TFrameWidget); ok {
			enableTree(f.Window, !busy)
		} else if c, ok := w.(interface{ Configure(...Opt) *Window }); ok {
			enable(c, !busy)
		}
	}
	enable(t.runBtn, !busy && len(t.checkedItems()) > 0)
	if t.backBtn != nil {
		enable(t.backBtn, !busy && len(t.backItems()) > 0)
	}
}

// backItems are the checked items the back conversion takes.
func (t *convTab) backItems() []*convItem {
	var out []*convItem
	if t.conv.back == nil {
		return nil
	}
	for _, it := range t.items {
		if t.marked(it) && t.conv.back.accept(it) {
			out = append(out, it)
		}
	}
	return out
}

// setItemStatus shows a status in an item's row; safe to call from any
// goroutine (applied by flush).
func (u *ui) setItemStatus(it *convItem, s fmt.Stringer) {
	u.mu.Lock()
	if u.pendingItems == nil {
		u.pendingItems = map[*convItem]fmt.Stringer{}
	}
	u.pendingItems[it] = s
	u.mu.Unlock()
}

// flushItems applies the pending item statuses; it runs on the GUI thread.
func (u *ui) flushItems(pending map[*convItem]fmt.Stringer) {
	for it, s := range pending {
		it.status = s
		if it.tab.tree != nil && it.id != "" && tcl(it.tab.tree.String(), "exists", it.id) == "1" {
			tcl(it.tab.tree.String(), "set", it.id, "status", stringOf(s))
			growColumn(it.tab.tree, "status", stringOf(s))
		}
	}
}

// start converts the checked files in the background, one after the
// other (or several at a time, see converter.jobs), into the output folder
// or next to each file.
func (t *convTab) start(u *ui) {
	t.startWith(u, t.checkedItems(), t.mainAction())
}

// mainAction is the tab's own conversion.
func (t *convTab) mainAction() convAction {
	return convAction{outName: t.conv.outName, run: t.conv.run, after: t.conv.after}
}

// startWith converts items with act in the background.
func (t *convTab) startWith(u *ui, items []*convItem, act convAction) {
	if u.busy || len(items) == 0 {
		return
	}
	outDir := strings.TrimSpace(u.outEntry.Textvariable())
	if outDir != "" {
		if st, err := os.Stat(outDir); err != nil || !st.IsDir() {
			MessageBox(Parent(App), Icon("error"), Title(T("app.title")), Msg(T("err.out_dir", outDir)))
			return
		}
	}
	verify := !u.cfg.SkipVerify
	ctx, cancel := context.WithCancel(context.Background())
	u.cancelMerge = cancel
	u.setBusy(true)
	enable(u.cancelBtn, true)
	for _, it := range items {
		u.setItemStatus(it, i18n.M("st.queued"))
	}

	jobs := 1
	if t.conv.jobs != nil {
		jobs = max(1, min(t.conv.jobs(u), len(items)))
	}
	go func() {
		defer u.recoverPanic()
		defer cancel()
		done := t.convertAll(ctx, u, items, outDir, verify, jobs, act)
		if act.after != nil && len(done) > 0 {
			act.after(u, done)
		}
		u.setProgress(0)
		u.setStatus(T("status.conv_done", len(done), len(items)))
		onGUI(func() {
			u.flush()
			t.replaceRemoved(u, done)
			u.cancelMerge = nil
			enable(u.cancelBtn, false)
			u.setBusy(false)
		})
	}()
}

// replaceRemoved lists, in place of the converted files whose original was
// removed ("delete the originals"), the files written from them, so that
// the next action does not work on files that are gone.
func (t *convTab) replaceRemoved(u *ui, done []converted) {
	outOf := map[string]string{}
	for _, c := range done {
		if _, err := os.Stat(c.item.path); errors.Is(err, fs.ErrNotExist) {
			outOf[c.item.path] = c.out
		}
	}
	if len(outOf) == 0 {
		return
	}
	for i, p := range t.pool {
		if out, ok := outOf[p]; ok {
			t.pool[i] = out
		}
	}
	var items []*convItem
	for _, it := range t.items {
		if _, ok := outOf[it.path]; ok {
			delete(t.checked, it.path)
		} else {
			items = append(items, it)
		}
	}
	t.items = items
	for _, c := range done {
		if out, ok := outOf[c.item.path]; ok && t.accepts(out) {
			t.list(u, out)
		}
	}
	t.refresh(u)
}

// convertAll converts the items, jobs at a time, and returns those done,
// in order. It runs in the background.
func (t *convTab) convertAll(ctx context.Context, u *ui, items []*convItem, outDir string, verify bool, jobs int, act convAction) []converted {
	results := make([]*converted, len(items))
	// With several files at a time, the progress bar shows the share of the
	// whole batch done: a verified file is half conversion, half
	// verification.
	var share sync.Mutex
	// Outputs taken by an item of this run: two items with the same output
	// name (game.gb and game.zip both become game.7z) must not write the
	// same file at the same time, which could leave one original deleted
	// with the other's data in its archive.
	claimed := map[string]bool{}
	shares := make([]float64, len(items))
	finished, running := 0, 0
	showShare := func(i int, verifying bool, done, total int64) {
		f := float64(done) / float64(max(total, 1))
		switch {
		case verify:
			f /= 2
			if verifying {
				f += 0.5
			}
		case verifying: // checked before removing the original: done
			f = 1
		}
		share.Lock()
		shares[i] = f
		sum := 0.0
		for _, v := range shares {
			sum += v
		}
		u.setProgress(sum / float64(len(items)))
		u.setStatus(T("status.parallel", finished, len(items), running))
		share.Unlock()
	}
	convert := func(i int, it *convItem) {
		defer func() {
			if p := recover(); p != nil {
				u.setItemStatus(it, lazyf("✗ %v", p))
				u.logf("log.panic", filepath.Base(it.path), p)
			}
		}()
		if jobs > 1 {
			share.Lock()
			running++
			share.Unlock()
			defer func() {
				share.Lock()
				running--
				finished++
				share.Unlock()
				showShare(i, true, 1, 1)
			}()
		}
		if ctx.Err() != nil {
			u.setItemStatus(it, i18n.M("st.cancelled"))
			return
		}
		in := it.path
		dir := outDir
		if dir == "" {
			dir = filepath.Dir(in)
		}
		out := filepath.Join(dir, act.outName(it))
		name := filepath.Base(out)
		u.separate()
		if strings.EqualFold(filepath.Clean(out), filepath.Clean(in)) {
			u.logf("log.same_file", filepath.Base(in))
			u.setItemStatus(it, i18n.M("st.same"))
			return
		}
		claim := strings.ToLower(filepath.Clean(out))
		share.Lock()
		taken := claimed[claim]
		claimed[claim] = true
		share.Unlock()
		if _, err := os.Stat(out); taken || err == nil {
			u.logf("log.exists", name)
			u.setItemStatus(it, i18n.M("st.exists"))
			return
		}
		u.println(lazyf("▶ [%d/%d] %s → %s", i+1, len(items), filepath.Base(in), name))
		u.setItemStatus(it, i18n.M("st.converting", 0))

		start := time.Now()
		phase, phaseStart, lastPct := false, start, -1
		inSize, outSize, err := act.run(ctx, u, it, out, verify, func(verifying bool, done, total int64) {
			if verifying != phase {
				phase, phaseStart, lastPct = verifying, time.Now(), -1
			}
			pct := int(done * 100 / max(total, 1))
			key := "st.converting"
			if verifying {
				key = "st.verifying"
			}
			if pct != lastPct {
				lastPct = pct
				u.setItemStatus(it, i18n.M(key, pct))
			}
			if jobs > 1 {
				showShare(i, verifying, done, total)
				return
			}
			u.setProgress(float64(done) / float64(max(total, 1)))
			if verifying {
				u.setStatus(T("status.verifying", name, humanSize(done), humanSize(total)) + eta(phaseStart, done, total))
				return
			}
			speed := int64(float64(done) / max(time.Since(phaseStart).Seconds(), 0.001))
			u.setStatus(name + " — " + humanSize(done) + " / " + humanSize(total) +
				" (" + humanSize(speed) + "/s)" + eta(phaseStart, done, total))
		})
		if err != nil {
			os.Remove(out) // incomplete or not verified
			if ctx.Err() != nil {
				u.logf("log.cancelled")
				u.setItemStatus(it, i18n.M("st.cancelled"))
				return
			}
			u.setItemStatus(it, lazyf("✗ %v", err))
			if jobs > 1 { // the log lines of other files come in between
				err = fmt.Errorf("%s: %w", filepath.Base(in), err)
			}
			u.logf("log.merge_error", err)
			return
		}
		took := time.Since(start).Round(time.Second)
		saved := 0
		if inSize > 0 {
			saved = int(100 - outSize*100/inSize)
		}
		if saved >= 0 {
			u.setItemStatus(it, i18n.M("st.done", humanSize(outSize), saved, took))
		} else {
			u.setItemStatus(it, i18n.M("st.done_larger", humanSize(outSize), -saved, took))
		}
		key := "log.done"
		if saved < 0 { // decompressed
			key, saved = "log.done_larger", -saved
		}
		u.logf(key, took, name, humanSize(inSize), humanSize(outSize), saved)
		results[i] = &converted{it, out}
	}
	if jobs <= 1 {
		for i, it := range items {
			convert(i, it)
		}
	} else {
		var wg sync.WaitGroup
		slots := make(chan struct{}, jobs)
		for i, it := range items {
			slots <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() {
					<-slots
					wg.Done()
				}()
				convert(i, it)
			}()
		}
		wg.Wait()
	}
	var done []converted
	for _, c := range results {
		if c != nil {
			done = append(done, *c)
		}
	}
	return done
}

// dropPaths sends dropped files to the tabs of their formats: to the tab
// shown if it takes the file, else to the first tab that takes it by
// default. It then shows a tab that received files. Dropped folders
// replace the Switch list, as "Open folder" does.
func (u *ui) dropPaths(paths []string) {
	if u.busy {
		return
	}
	var cur *convTab
	for _, t := range u.tabs {
		if t.conv.id == u.cfg.Tab {
			cur = t
		}
	}
	hasDir, hasGame := false, false
	perTab := map[*convTab][]string{}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			hasDir = true
		}
		walkFiles(p, u.cfg.Subfolders, func(path string) {
			if cur != nil && (cur.accepts(path) || cur.conv.anyExt) {
				perTab[cur] = append(perTab[cur], path)
				return
			}
			if isGameFile(path) {
				hasGame = true
				return
			}
			for _, t := range u.tabs {
				if t.accepts(path) && (t.conv.drop == nil || t.conv.drop(path)) {
					perTab[t] = append(perTab[t], path)
					break
				}
			}
		})
	}
	var got []string
	if hasGame {
		u.followInput("switch", paths)
		u.addPaths(paths, hasDir)
		got = append(got, "switch")
	}
	for _, t := range u.tabs {
		if len(perTab[t]) > 0 && t.add(u, perTab[t]) > 0 {
			u.followInput(t.conv.id, paths)
			t.refresh(u)
			got = append(got, t.conv.id)
		}
	}
	if len(got) == 0 {
		u.logf("log.no_new_files")
		return
	}
	for _, g := range got {
		if g == u.cfg.Tab {
			return // the current tab received files: stay there
		}
	}
	u.selectTab(got[0])
}

// enableTree enables or disables the widgets inside a frame, at any depth
// (frames and labels without a state are skipped).
func enableTree(w *Window, on bool) {
	for _, c := range WinfoChildren(w) {
		state := map[bool]string{true: "normal", false: "disabled"}[on]
		if on && tcl("winfo", "class", c.String()) == "TCombobox" {
			state = "readonly" // "normal" would let one type into the box
		}
		tcl("catch", c.String()+" configure -state "+state)
		enableTree(c, on)
	}
}

// msg is a translated message for a converter's description.
func msg(key string, args ...any) fmt.Stringer { return i18n.M(key, args...) }
