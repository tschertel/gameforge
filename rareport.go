package main

import (
	"bytes"
	"errors"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gameforge/internal/i18n"
	"gameforge/internal/retroach"
)

// raRow is a checked file, for the report.
type raRow struct {
	job raJob
	out raOutcome
}

// writeRAReports saves an HTML report of a RetroAchievements check for each
// console, next to the checked files (in the folder of the first one), named
// after the console and the day: "RetroAchievements - SNES - 2026-11-22.html".
// A report of the same day is not written over: the new one gets " (2)".
// Files whose console is not known are left out.
func (u *ui) writeRAReports(rows []raRow, now time.Time) {
	var order []string
	groups := map[string][]raRow{}
	for _, r := range rows {
		if r.job.console == 0 || r.out.status == nil {
			continue
		}
		name := raConsoleName(r.job.console)
		if groups[name] == nil {
			order = append(order, name)
		}
		groups[name] = append(groups[name], r)
	}
	for _, console := range order {
		group := groups[console]
		dir := filepath.Dir(group[0].job.item.path)
		path, err := raReportPath(dir, console, now)
		if err == nil {
			err = os.WriteFile(path, raReportHTML(console, group, now), 0o644)
		}
		if err != nil {
			u.logf("ra.report_error", console, err)
			continue
		}
		u.logf("ra.report_saved", path)
	}
}

// raReportPath is a free name for the report of console in dir.
func raReportPath(dir, console string, now time.Time) (string, error) {
	return reportPath(dir, "RetroAchievements - "+safeFileName(console)+" - "+now.Format("2006-01-02"))
}

// reportPath is a free name in dir for a report named base: base.html,
// else "base (2).html" and so on, so a report is never written over.
func reportPath(dir, base string) (string, error) { return freePath(dir, base, ".html") }

// freePath is a free name in dir for a file named base with extension ext:
// base+ext, else "base (2)"+ext and so on.
func freePath(dir, base, ext string) (string, error) {
	for n := 1; n < 1000; n++ {
		name := base + ext
		if n > 1 {
			name = base + " (" + strconv.Itoa(n) + ")" + ext
		}
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fs.ErrExist
}

// safeFileName replaces the characters a file name cannot hold on Windows.
func safeFileName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
}

// raReportRow is a row of the report as the template shows it.
type raReportRow struct {
	File, Result, Class    string
	Game, GameURL          string
	Versions, VersionsText string
	Hash                   string
}

var raReportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; --bg: #fff; --fg: #1d1d1f; --muted: #6e6e73; --line: #d2d2d7;
  --ok: #1a7f37; --other: #9a6700; --bad: #cf222e; --link: #0b5cad; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #1c1c1e; --fg: #f2f2f7; --muted: #a1a1a6; --line: #3a3a3c;
    --ok: #4ac26b; --other: #d4a72c; --bad: #ff7b72; --link: #6cb6ff; }
}
body { background: var(--bg); color: var(--fg); font: 15px/1.45 system-ui, sans-serif; margin: 2em auto; max-width: 75em; padding: 0 1em; }
h1 { font-size: 1.5em; margin: 0 0 .2em; }
p { margin: .2em 0; color: var(--muted); }
.summary { color: var(--fg); margin: .8em 0 1.2em; }
table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; vertical-align: top; padding: .45em .6em; border-bottom: 1px solid var(--line); }
th { font-weight: 600; }
td.hash { font-family: ui-monospace, monospace; font-size: .85em; color: var(--muted); }
a { color: var(--link); }
.ok td.result { color: var(--ok); }
.other td.result { color: var(--other); }
.bad td.result { color: var(--bad); }
.skip td.result { color: var(--muted); }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Checked}}</p>
<p class="summary">{{.Summary}}</p>
<table>
<thead><tr><th>{{.FileHead}}</th><th>{{.ResultHead}}</th><th>{{.GameHead}}</th><th>Hash</th></tr></thead>
<tbody>
{{range .Rows}}<tr class="{{.Class}}"><td>{{.File}}</td><td class="result">{{.Result}}</td><td>{{if .GameURL}}<a href="{{.GameURL}}">{{.Game}}</a>{{else}}{{.Game}}{{end}}{{if .Versions}}<br><a href="{{.Versions}}">{{.VersionsText}}</a>{{end}}</td><td class="hash">{{.Hash}}</td></tr>
{{end}}</tbody>
</table>
</body>
</html>
`))

// raReportHTML is the report of the files of one console.
func raReportHTML(console string, rows []raRow, now time.Time) []byte {
	found := 0
	var out []raReportRow
	for _, r := range rows {
		o := r.out
		row := raReportRow{File: filepath.Base(r.job.item.path), Result: statusText(o.status), Hash: o.hash}
		switch {
		case o.ok:
			found++
			row.Class = "ok"
		case o.other:
			row.Class = "other"
		case o.hash != "":
			row.Class = "bad"
		default:
			row.Class = "skip"
		}
		if g := o.game; g != nil {
			row.Game, row.GameURL = g.Title, retroach.GameURL(g.ID)
			if o.other { // what matters is which files it accepts
				row.GameURL = ""
				row.Versions, row.VersionsText = retroach.HashesURL(g.ID), T("ra.report_versions")
			}
		}
		out = append(out, row)
	}
	var b bytes.Buffer
	err := raReportTemplate.Execute(&b, map[string]any{
		"Lang":       i18n.Current(),
		"Title":      T("ra.report_title", console),
		"Checked":    T("ra.report_checked", now.Format("2006-01-02 15:04"), appVersion),
		"Summary":    strings.TrimPrefix(T("ra.summary", found, len(rows)), "■ "),
		"FileHead":   T("list.file"),
		"ResultHead": T("list.status"),
		"GameHead":   T("ra.report_game"),
		"Rows":       out,
	})
	if err != nil {
		panic(err) // the template is fixed
	}
	return b.Bytes()
}
