package main

import (
	"bytes"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	. "modernc.org/tk9.0"

	"gameforge/internal/arcade"
	"gameforge/internal/i18n"
	"gameforge/internal/nointro"
)

// collectionRow is a game in one console folder of the collection.
type collectionRow struct {
	Name, Console string
	Others        string // the other console folders with the same title
	Search        string // what the search field matches, in lower case
	Consoles      int    // how many console folders have the title
	key           string
}

// isCollectionGame tells the files of a console folder that are games: the
// formats the app reads (cartridge ROMs, disc images, Switch files, ZIP
// and 7z), not the saves, caches and settings that emulators keep there.
// A .bin is also an emulator's file name (fst.bin, banner.bin), so only one
// named with No-Intro or Redump flags counts.
func isCollectionGame(name string) bool {
	ext := extOf(name)
	if ext == ".bin" {
		return strings.Contains(name, "(")
	}
	return nointro.SystemOf(ext) != "" || slices.Contains(discImageExts, ext) || slices.Contains(gameExts, ext) ||
		slices.Contains([]string{".chd", ".rvz", ".wbfs", ".gcm", ".cso", ".zso", ".pbp", ".zip", ".7z"}, ext)
}

// titleKey is what tells two releases of a game apart from other games:
// the title before the No-Intro or Redump flags ("(USA)", "(Disc 1)",
// "[b]"), in lower case, with letters and digits only, so "Game: Part II"
// and "Game - Part II" are the same. "The" is left out, as it is first in
// MAME's titles and last in No-Intro's and Redump's ("Game, The").
func titleKey(name string) (title, key string) {
	// Inner extensions of some tools: game.xiso.iso, game.nkit.iso.
	for _, inner := range []string{".xiso", ".nkit"} {
		if strings.HasSuffix(strings.ToLower(name), inner) {
			name = name[:len(name)-len(inner)]
		}
	}
	title = name
	if i := strings.IndexAny(name, "(["); i > 0 {
		title = strings.TrimSpace(name[:i])
	}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	words = slices.DeleteFunc(words, func(w string) bool { return w == "the" })
	return title, strings.Join(words, " ")
}

// scanCollection lists the games in the console folders of root (each
// folder of root is a console, named as the folder): one row per title and
// console, however many files it has (tracks, discs, regions). arcade
// finds arcade sets by their short names ("sfa3"), so that they count by
// their titles and match the same game on consoles; BIOS sets are left out.
func scanCollection(root string, arcade *arcade.DB) ([]collectionRow, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var rows []collectionRow
	consoles := map[string]map[string]bool{} // key → console folders
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		console := e.Name()
		seen := map[string]bool{}
		filepath.WalkDir(filepath.Join(root, console), func(path string, d fs.DirEntry, err error) error {
			if err != nil || strings.HasPrefix(d.Name(), ".") {
				if d != nil && d.IsDir() && err == nil {
					return fs.SkipDir
				}
				return nil
			}
			stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			if d.IsDir() || !isCollectionGame(d.Name()) {
				return nil
			}
			if set := arcade.Set(stem); set != nil {
				if set.Bios {
					return nil
				}
				stem = set.Title
			}
			title, key := titleKey(stem)
			if key == "" || seen[key] {
				return nil
			}
			seen[key] = true
			if consoles[key] == nil {
				consoles[key] = map[string]bool{}
			}
			consoles[key][console] = true
			rows = append(rows, collectionRow{Name: title, Console: console, key: key})
			return nil
		})
	}
	for i := range rows {
		r := &rows[i]
		var others []string
		for c := range consoles[r.key] {
			if c != r.Console {
				others = append(others, c)
			}
		}
		slices.SortFunc(others, compareFold)
		r.Others = strings.Join(others, ", ")
		r.Consoles = len(consoles[r.key])
		r.Search = strings.ToLower(r.Name + " " + r.Console + " " + r.Others)
	}
	slices.SortFunc(rows, func(a, b collectionRow) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		return compareFold(a.Console, b.Console)
	})
	return rows, nil
}

// compareFold orders console folders as a file manager does: "gba" before "PS2".
func compareFold(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

var collectionTemplate = template.Must(template.New("collection").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; --bg: #fff; --fg: #1d1d1f; --muted: #6e6e73; --line: #d2d2d7; --mark: #9a6700; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #1c1c1e; --fg: #f2f2f7; --muted: #a1a1a6; --line: #3a3a3c; --mark: #d4a72c; }
}
body { background: var(--bg); color: var(--fg); font: 15px/1.45 system-ui, sans-serif; margin: 2em auto; max-width: 75em; padding: 0 1em; }
h1 { font-size: 1.5em; margin: 0 0 .2em; }
p { margin: .2em 0; color: var(--muted); }
.filters { display: flex; flex-wrap: wrap; gap: .6em 1.2em; align-items: center; margin: 1em 0; }
input[type=search], select { font: inherit; padding: .3em .5em; }
input[type=search] { flex: 1 1 18em; }
table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; vertical-align: top; padding: .45em .6em; border-bottom: 1px solid var(--line); }
th { font-weight: 600; }
td.others { color: var(--mark); }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Made}}</p>
<div class="filters">
<input type="search" id="q" placeholder="{{.SearchText}}" autofocus>
<select id="c"><option value="">{{.AllText}}</option>{{range .Consoles}}<option value="{{.}}">{{.}}</option>{{end}}</select>
<label><input type="checkbox" id="m"> {{.MultiText}}</label>
<span>{{.ShownText}} <b id="n"></b></span>
</div>
<table>
<thead><tr><th>{{.GameHead}}</th><th>{{.ConsoleHead}}</th><th>{{.OthersHead}}</th></tr></thead>
<tbody>
{{range .Rows}}<tr data-t="{{.Search}}" data-c="{{.Console}}" data-n="{{.Consoles}}"><td>{{.Name}}</td><td>{{.Console}}</td><td class="others">{{.Others}}</td></tr>
{{end}}</tbody>
</table>
<script>
const q = document.getElementById("q"), c = document.getElementById("c"), m = document.getElementById("m");
const rows = [...document.querySelectorAll("tbody tr")];
function filter() {
  const s = q.value.trim().toLowerCase();
  let n = 0;
  for (const r of rows) {
    const show = (!s || r.dataset.t.includes(s)) && (!c.value || r.dataset.c === c.value) && (!m.checked || r.dataset.n > 1);
    r.hidden = !show;
    if (show) n++;
  }
  document.getElementById("n").textContent = n;
}
for (const e of [q, c, m]) e.addEventListener("input", filter);
filter();
</script>
</body>
</html>
`))

// collectionHTML is the report of the rows of the collection in root.
func collectionHTML(root string, rows []collectionRow, now time.Time) []byte {
	var consoles []string
	for _, r := range rows {
		if !slices.Contains(consoles, r.Console) {
			consoles = append(consoles, r.Console)
		}
	}
	slices.SortFunc(consoles, compareFold)
	var b bytes.Buffer
	err := collectionTemplate.Execute(&b, map[string]any{
		"Lang":        i18n.Current(),
		"Title":       T("coll.title", filepath.Base(root)),
		"Made":        T("coll.made", now.Format("2006-01-02 15:04"), appVersion),
		"SearchText":  T("coll.search"),
		"AllText":     T("coll.all"),
		"MultiText":   T("coll.multi"),
		"ShownText":   T("coll.shown"),
		"GameHead":    T("coll.game"),
		"ConsoleHead": T("coll.console"),
		"OthersHead":  T("coll.others"),
		"Consoles":    consoles,
		"Rows":        rows,
	})
	if err != nil {
		panic(err) // the template is fixed
	}
	return b.Bytes()
}

// collectionReport asks for the folder of the console folders, writes the
// report of the games in it there and says where.
func (u *ui) collectionReport() {
	if u.busy {
		return
	}
	root := u.openFolder(false)
	if root == "" {
		return
	}
	// The titles of the arcade sets, when the lists were downloaded.
	arcade := u.arcadeLists()
	u.setBusy(true)
	u.separate()
	go func() {
		defer u.recoverPanic()
		u.setStatus(T("drename.status", filepath.Base(root)))
		rows, err := scanCollection(root, arcade)
		path := ""
		if err == nil && len(rows) == 0 {
			u.logf("coll.none", root)
		} else if err == nil {
			path, err = reportPath(root, T("coll.file")+" - "+time.Now().Format("2006-01-02"))
			if err == nil {
				err = os.WriteFile(path, collectionHTML(root, rows, time.Now()), 0o644)
			}
		}
		if err != nil {
			u.logf("coll.error", err)
		} else if path != "" {
			consoles, multi := map[string]bool{}, map[string]bool{}
			for _, r := range rows {
				consoles[r.Console] = true
				if r.Consoles > 1 {
					multi[r.key] = true
				}
			}
			u.logf("coll.saved", len(rows), len(consoles), len(multi), path)
		}
		u.setStatus("")
		onGUI(func() {
			u.flush()
			u.setBusy(false)
			if path != "" && err == nil {
				MessageBox(Parent(App), Icon("info"), Title(T("app.title")), Msg(T("coll.saved_box")), Detail(path))
			}
		})
	}()
}
