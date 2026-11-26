package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gameforge/internal/i18n"
	"gameforge/internal/merge"
	"gameforge/internal/titledb"
)

// bom is the byte order mark the CSV starts with.
var bom = string(rune(0xFEFF))

// TestMissingCSV: one row per game with a checked file that lacks
// something; up-to-date, unknown and unchecked games are left out.
func TestMissingCSV(t *testing.T) {
	const app, dlc1, dlc2 = 0x0100EBA004726000, 0x0100EBA004727001, 0x0100EBA004727002
	const current, other = 0x0100AAA000000000, 0x0100BBB000000000
	db := titledb.New(map[uint64][]titledb.Release{
		app:     {{Version: 65536, Date: "2020-01-01"}, {Version: 131072, Date: "2020-06-01"}},
		dlc1:    {{Version: 65536, Date: "2020-03-01"}},
		current: {{Version: 65536, Date: "2021-01-01"}},
	}, map[uint64]string{app: "Game", dlc1: "One", dlc2: "Two", current: "Current", other: "Other"})
	nsp := func(kind merge.Kind, appID, id uint64, v uint32) *merge.NSP {
		return &merge.NSP{Path: fmt.Sprintf("%X", id), Kind: kind, TitleID: id, AppID: appID, Version: v}
	}
	lacking := &merge.Game{AppID: app,
		Bases:   []*merge.NSP{nsp(merge.KindBase, app, app, 0)},
		Updates: []*merge.NSP{nsp(merge.KindUpdate, app, app|0x800, 65536)},
		DLCs:    []*merge.NSP{nsp(merge.KindDLC, app, dlc1, 0)},
	}
	upToDate := &merge.Game{AppID: current, Bases: []*merge.NSP{nsp(merge.KindBase, current, current, 65536)}}
	unchecked := &merge.Game{AppID: other, Bases: []*merge.NSP{nsp(merge.KindBase, other, other, 0)}}
	games := []*merge.Game{lacking, upToDate, unchecked}
	selected := map[*merge.Game][]*merge.NSP{lacking: lacking.Bases, upToDate: upToDate.Bases}

	data, n := missingCSV(games, selected, db)
	if n != 1 {
		t.Fatalf("%d games listed, want 1", n)
	}
	if !strings.HasPrefix(string(data), bom) {
		t.Error("no byte order mark")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), bom))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{lacking.Title(), "0100EBA004726000", "v65536", "v131072", "2020-06-01",
		"1", "Two [0100EBA004727002]", "1", "One [0100EBA004727001] v0 → v65536"}
	if len(rows) != 2 || strings.Join(rows[1], "|") != strings.Join(want, "|") {
		t.Errorf("rows %q\nwant %q", rows, want)
	}

	// Outside English, the separator is a semicolon, as Excel expects there.
	i18n.Set("pt")
	defer i18n.Set("en")
	data, _ = missingCSV(games, selected, db)
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), bom)))
	r.Comma = ';'
	if rows, err := r.ReadAll(); err != nil || len(rows) != 2 || rows[1][1] != "0100EBA004726000" {
		t.Errorf("with semicolons: %q, %v", rows, err)
	}
}

// TestSaveRunLog: the lines of a run go to a dated text file in the logs
// folder; a run that logged nothing writes none, and two runs in the same
// second get two files.
func TestSaveRunLog(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "gameforge.json")
	now := time.Date(2026, 11, 26, 9, 30, 5, 0, time.Local)
	if err := saveRunLog(config, []fmt.Stringer{lazyf(""), lazyf("  ")}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, logsDirName)); err == nil {
		t.Error("an empty run made the logs folder")
	}
	lines := []fmt.Stringer{lazyf("▶ Checking a.nsz"), lazyf("✓ a.nsz: %d NCAs checked", 3)}
	for range 2 {
		if err := saveRunLog(config, lines, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"GameForge - 2026-11-26 09-30-05.txt", "GameForge - 2026-11-26 09-30-05 (2).txt"} {
		b, err := os.ReadFile(filepath.Join(dir, logsDirName, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "▶ Checking a.nsz\n✓ a.nsz: 3 NCAs checked\n" {
			t.Errorf("%s: %q", name, b)
		}
	}
}
