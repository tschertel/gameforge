package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gameforge/internal/retroach"
)

func TestRAReport(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 22, 10, 30, 0, 0, time.Local)
	game := retroach.Game{ID: 123, Title: "Tom & Jerry <Deluxe>", Achievements: 20}
	job := func(name string) raJob {
		return raJob{item: &convItem{path: filepath.Join(dir, name)}, console: 3, system: "SNES"}
	}
	rows := []raRow{
		{job("a.sfc"), raOutcome{status: msg("ra.st_ok", game.Title, 20), ok: true, hash: "aa", game: &game}},
		{job("b <x>.sfc"), raOutcome{status: linkStatus{msg("ra.st_other"), retroach.HashesURL(123)}, other: true, hash: "bb", game: &game}},
		{job("c.sfc"), raOutcome{status: msg("ra.st_unknown"), hash: "cc"}},
		{job("d.zip"), raSkip(msg("ra.st_set"), nil)},
	}
	html := string(raReportHTML("SNES", rows, now))
	for _, want := range []string{
		"<title>", "SNES", "2026-11-22 10:30", appVersion,
		"Tom &amp; Jerry &lt;Deluxe&gt;", "b &lt;x&gt;.sfc", // escaped
		`<a href="https://retroachievements.org/game/123">Tom &amp; Jerry`, // recognized: the game
		`<br><a href="https://retroachievements.org/game/123/hashes">`,     // another version: the hashes
		`class="ok"`, `class="other"`, `class="bad"`, `class="skip"`,
		" 1 ", " 4 ", // 1 of 4 recognized
		`<option value="other bad skip">`, `data-t="b &lt;x&gt;.sfc tom &amp; jerry &lt;deluxe&gt;"`, // filter and search
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if strings.Contains(html, "↗") {
		t.Error("the report shows the status with its address")
	}

	p1, err := raReportPath(dir, "Game Boy/Color", now)
	if err != nil || filepath.Base(p1) != "RetroAchievements - Game Boy_Color - 2026-11-22.html" {
		t.Fatal(p1, err)
	}
	os.WriteFile(p1, nil, 0o644)
	if p2, _ := raReportPath(dir, "Game Boy/Color", now); filepath.Base(p2) != "RetroAchievements - Game Boy_Color - 2026-11-22 (2).html" {
		t.Error(p2)
	}

	u := &ui{}
	u.writeRAReports(rows, now)
	if _, err := os.Stat(filepath.Join(dir, "RetroAchievements - "+raConsoleName(3)+" - 2026-11-22.html")); err != nil {
		t.Error(err)
	}
}
