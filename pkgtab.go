package main

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	. "modernc.org/tk9.0"

	"gameforge/internal/pkg"
)

// pkgState holds the PKG tab's keys and what each listed package holds.
var pkgState struct {
	mu       sync.Mutex
	pkgs     map[string]*pkg.Package
	keys     *pkg.Keys
	keysErr  error
	loaded   bool
	status   *TLabelWidget
	keysPath string
}

// pkgKeysPath is keys/pkg.keys next to the program.
func pkgKeysPath() string { return filepath.Join(keysDir(), "pkg.keys") }

// pkgKeys loads keys/pkg.keys on first use.
func pkgKeys() *pkg.Keys {
	if !pkgState.loaded {
		pkgState.loaded = true
		pkgState.keysPath = pkgKeysPath()
		pkgState.keys, pkgState.keysErr = pkg.LoadKeys(pkgState.keysPath)
		if errors.Is(pkgState.keysErr, fs.ErrNotExist) {
			pkgState.keysErr = nil
		}
	}
	return pkgState.keys
}

func showPkgKeys() {
	if pkgState.status == nil {
		return
	}
	pkgKeys()
	switch {
	case pkgState.keysErr != nil:
		pkgState.status.Configure(Txt("⚠ " + pkgState.keysErr.Error()))
	case len(pkgState.keys.Names()) > 0:
		pkgState.status.Configure(Txt(T("pkg.keys_loaded", strings.Join(pkgState.keys.Names(), ", "))))
	default:
		pkgState.status.Configure(Txt(T("pkg.keys_missing")))
	}
}

func pkgOf(path string) *pkg.Package {
	pkgState.mu.Lock()
	defer pkgState.mu.Unlock()
	return pkgState.pkgs[path]
}

// pkgKindKey is the message key of a kind of content.
var pkgKindKey = map[string]string{
	pkg.KindGame: "pkg.kind.game", pkg.KindUpdate: "pkg.kind.update",
	pkg.KindDLC: "pkg.kind.dlc", pkg.KindTheme: "pkg.kind.theme",
}

// pkgDetail describes a package for the list.
type pkgDetail struct{ p *pkg.Package }

func (d pkgDetail) String() string {
	p := d.p
	var parts []string
	if t := p.Title(); t != "" {
		parts = append(parts, t)
	}
	if k, ok := pkgKindKey[p.Kind]; ok {
		parts = append(parts, T(k))
	}
	if v := p.Version(); v != "" {
		parts = append(parts, "v"+v)
	}
	parts = append(parts, p.ContentID, humanSize(p.Size))
	switch {
	case p.NeedKey != "":
		parts = append(parts, T("pkg.need_key", p.NeedKey))
	case p.Platform == "PS4":
		parts = append(parts, T("pkg.ps4_sys", p.Files))
	case p.Debug:
		parts = append(parts, T("pkg.debug"))
	}
	return strings.Join(parts, " · ")
}

// pkgConverter lists PlayStation packages (PS4, PS3, PSP, PS Vita),
// renames them and extracts them to folders.
var pkgConverter = &converter{
	id:     "pkg",
	title:  "tab.pkg",
	info:   "tab.pkg.info",
	button: "btn.pkg",
	tip:    "btn.pkg.tip",
	exts:   []string{".pkg"},
	types:  []FileType{{TypeName: "PKG", Extensions: []string{".pkg"}}},
	describe: func(it *convItem) {
		it.ok, it.infoOnly = false, false
		p, err := pkg.Open(it.path, pkgKeys())
		if err != nil {
			it.detail = lazyf("⚠ %v", err)
			return
		}
		pkgState.mu.Lock()
		if pkgState.pkgs == nil {
			pkgState.pkgs = map[string]*pkg.Package{}
		}
		pkgState.pkgs[it.path] = p
		pkgState.mu.Unlock()
		it.platform = p.Platform
		it.icon = p.Icon
		it.size = p.Size
		it.ok = p.CanExtract()
		it.infoOnly = !it.ok // still renamed
		it.detail = pkgDetail{p}
	},
	outName: func(it *convItem) string { return pkgOf(it.path).FolderName() },
	run: func(ctx context.Context, u *ui, it *convItem, out string, verify bool,
		progress func(verifying bool, done, total int64)) (int64, int64, error) {
		p := pkgOf(it.path)
		n, err := p.Extract(ctx, out, func(done, total int64) { progress(false, done, total) })
		if err != nil {
			return 0, 0, err
		}
		u.logf("pkg.extracted", p.Files, filepath.Base(out))
		return n, n, nil
	},
	extra: func(u *ui, parent *TFrameWidget) []Widget {
		var t *convTab
		for _, tab := range u.tabs {
			if tab.conv.id == "pkg" {
				t = tab
			}
		}
		rename := parent.TButton(Txt(T("btn.rename")), Command(func() { t.renamePkgs(u) }))
		setTooltip(rename, T("btn.pkg_rename.tip"))
		return []Widget{rename}
	},
	options: func(u *ui, parent *TFrameWidget) []Widget {
		label := parent.TLabel(Txt("pkg.keys:"))
		pkgState.status = parent.TLabel()
		reload := parent.TButton(Txt(T("btn.pkg_reload")), Command(func() {
			pkgState.loaded = false
			showPkgKeys()
			for _, tab := range u.tabs {
				if tab.conv.id != "pkg" {
					continue
				}
				for _, it := range tab.items {
					was := it.ok
					tab.conv.describe(it)
					if it.ok && !was {
						tab.checked[it.path] = true
					}
				}
				tab.refresh(u)
			}
		}))
		setTooltip(label, T("pkg.keys.tip"))
		setTooltip(pkgState.status, T("pkg.keys.tip"))
		setTooltip(reload, T("pkg.keys.tip"))
		showPkgKeys()
		return []Widget{label, pkgState.status, reload}
	},
}

// renamePkgs gives the checked packages their standard names, after
// showing what changes.
func (t *convTab) renamePkgs(u *ui) {
	if u.busy {
		return
	}
	items, ok := t.keepChecked(t.items)
	if !ok {
		return
	}
	type change struct {
		it *convItem
		to string
	}
	var plan []change
	for _, it := range items {
		p := pkgOf(it.path)
		if p == nil {
			continue
		}
		name := p.StandardName()
		if name == "" || name == filepath.Base(it.path) {
			continue
		}
		plan = append(plan, change{it, filepath.Join(filepath.Dir(it.path), name)})
	}
	if len(plan) == 0 {
		MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("pkg.rename_none")))
		return
	}
	var lines []string
	for i, c := range plan {
		if i == 15 {
			lines = append(lines, T("rename.more", len(plan)-i))
			break
		}
		lines = append(lines, filepath.Base(c.it.path)+"\n   → "+filepath.Base(c.to))
	}
	answer := MessageBox(Parent(App), Icon("question"), Type("yesno"), Title(T("app.title")),
		Msg(T("rename.confirm", len(plan))), Detail(strings.Join(lines, "\n")))
	if answer != "yes" {
		return
	}
	done := 0
	for _, c := range plan {
		from := c.it.path
		if err := renameFile(from, c.to); err != nil {
			u.logf("log.rename_error", filepath.Base(from), err)
			continue
		}
		u.logf("log.renamed", filepath.Base(from), filepath.Base(c.to))
		pkgState.mu.Lock()
		if p := pkgState.pkgs[from]; p != nil {
			p.Path = c.to
			pkgState.pkgs[c.to] = p
			delete(pkgState.pkgs, from)
		}
		pkgState.mu.Unlock()
		t.checked[c.to] = t.checked[from]
		delete(t.checked, from)
		c.it.path = c.to
		done++
	}
	u.logf("log.rename_done", done, len(plan))
	t.refresh(u)
}
