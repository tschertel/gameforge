package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gameforge/internal/config"
	"gameforge/internal/sevenzip"
)

func TestSevenZipConverter(t *testing.T) {
	dir := t.TempDir()
	rom := bytes.Repeat([]byte("GBC ROM DATA "), 20_000)
	os.WriteFile(filepath.Join(dir, "Game (USA).gbc"), rom, 0o644)
	os.WriteFile(filepath.Join(dir, "Game (USA).state.auto"), []byte("save"), 0o644)
	os.WriteFile(filepath.Join(dir, "Game (USA).png"), []byte("png"), 0o644)

	// An arcade-style ZIP: several ROM files, deflated and stored.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	when := time.Date(1991, 3, 1, 10, 0, 0, 0, time.UTC)
	files := map[string][]byte{"sf2.01": bytes.Repeat([]byte{1, 2, 3, 4}, 30_000), "sf2.02": []byte("short"), "empty.bin": nil}
	for _, name := range []string{"sf2.01", "sf2.02", "empty.bin"} {
		method := zip.Deflate
		if name == "sf2.02" {
			method = zip.Store
		}
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: when})
		w.Write(files[name])
	}
	zw.Close()
	os.WriteFile(filepath.Join(dir, "sf2.zip"), zbuf.Bytes(), 0o644)

	u := &ui{cfg: &config.Config{SevenZipDelete: true}}
	sevenZip.ui = u
	removed := map[string]bool{}
	removeFile = func(p string) error { removed[filepath.Base(p)] = true; return nil }
	defer func() { removeFile, sevenZip.ui = removeOriginal, nil }()

	tab := &convTab{conv: sevenZipConverter, checked: map[string]bool{}}
	tab.add(u, []string{dir})
	var listed []string
	for _, it := range tab.items {
		listed = append(listed, filepath.Base(it.path))
		if !it.ok {
			t.Errorf("%s not convertible: %v", it.path, it.detail)
		}
	}
	if strings.Join(listed, ",") != "Game (USA).gbc,sf2.zip" {
		t.Fatalf("listed %v", listed)
	}
	if len(tab.pool) != 4 {
		t.Errorf("pool has %d files", len(tab.pool))
	}

	for _, it := range tab.items {
		out := filepath.Join(dir, sevenZipConverter.outName(it))
		in, size, err := sevenZipConverter.run(context.Background(), u, it, out, false, func(bool, int64, int64) {})
		if err != nil {
			t.Fatalf("%s: %v", it.path, err)
		}
		st, _ := os.Stat(out)
		if size != st.Size() || in == 0 {
			t.Errorf("%s: sizes %d -> %d", out, in, size)
		}
		entries, err := sevenzip.Check(context.Background(), out, nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(out, "sf2.7z") {
			if len(entries) != 3 || entries[0].Name != "sf2.01" || !entries[0].Modified.Equal(when) || entries[2].Dir {
				t.Errorf("sf2.7z entries %+v", entries)
			}
		}
		if exe := os.Getenv("SEVENZIP"); exe != "" {
			if b, err := exec.Command(exe, "t", out).CombinedOutput(); err != nil {
				t.Errorf("7z t %s: %v\n%s", out, err, b)
			}
		}
	}
	if !removed["Game (USA).gbc"] || !removed["sf2.zip"] || len(removed) != 2 {
		t.Errorf("removed %v", removed)
	}
}

// TestConvertAllJobs compresses several files at a time: every archive is
// written and verified, and the results keep the order of the list.
func TestConvertAllJobs(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := range 7 {
		p := filepath.Join(dir, fmt.Sprintf("game %d.gba", i))
		os.WriteFile(p, bytes.Repeat([]byte(fmt.Sprintf("ROM %d ", i)), 50_000+i*1000), 0o644)
		paths = append(paths, p)
	}
	u := &ui{cfg: &config.Config{SevenZipJobs: 3}}
	sevenZip.ui = u
	defer func() { sevenZip.ui = nil }()
	tab := &convTab{conv: sevenZipConverter, checked: map[string]bool{}}
	tab.add(u, paths)
	if len(tab.items) != 7 {
		t.Fatalf("%d items", len(tab.items))
	}
	done := tab.convertAll(context.Background(), u, tab.items, "", true, sevenZipJobs(u), tab.mainAction())
	if len(done) != 7 {
		t.Fatalf("%d of 7 done", len(done))
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(left) > 0 {
		t.Fatalf(".part files left: %v", left)
	}
	for i, c := range done {
		if c.item != tab.items[i] {
			t.Errorf("result %d is %s", i, c.item.path)
		}
		if _, err := sevenzip.Check(context.Background(), c.out, func(int64, int64) {}); err != nil {
			t.Errorf("%s: %v", c.out, err)
		}
	}
}

// TestInfoOnlyChecked checks that files listed only for what they are (a
// 7z in the ROMs tab) get a check box, checked at first, and that only
// checked files count as marked.
func TestInfoOnlyChecked(t *testing.T) {
	tab := &convTab{checked: map[string]bool{}}
	a := &convItem{path: "a.7z", infoOnly: true}
	b := &convItem{path: "b.gbc", ok: true}
	c := &convItem{path: "c.bin"} // could not be read: no check box
	tab.items = []*convItem{a, b, c}
	for _, it := range tab.items {
		tab.checked[it.path] = markable(it)
	}
	if !tab.marked(a) || !tab.marked(b) || tab.marked(c) {
		t.Fatalf("marked: %v %v %v", tab.marked(a), tab.marked(b), tab.marked(c))
	}
	tab.checked["a.7z"] = false
	if tab.marked(a) || len(tab.checkedItems()) != 1 {
		t.Errorf("after unchecking: %v %d", tab.marked(a), len(tab.checkedItems()))
	}
}

// TestIsROMFile: ROMs and archives are listed; saves, images and disc
// tracks (a .bin next to a CUE sheet) are not.
func TestIsROMFile(t *testing.T) {
	dir := t.TempDir()
	disc, cart := filepath.Join(dir, "disc"), filepath.Join(dir, "cart")
	os.MkdirAll(disc, 0o755)
	os.MkdirAll(cart, 0o755)
	os.WriteFile(filepath.Join(disc, "Game (USA).cue"), nil, 0o644)
	for path, want := range map[string]bool{
		filepath.Join(cart, "Sonic (World).bin"):        true,
		filepath.Join(disc, "Game (USA) (Track 1).bin"): false,
		filepath.Join(cart, "Game (USA).gba"):           true,
		filepath.Join(cart, "Game (USA).A52"):           true, // a No-Intro extension, any case
		filepath.Join(cart, "sf2.zip"):                  true,
		filepath.Join(cart, "Game (USA).7z"):            true,
		filepath.Join(cart, "Game (USA).srm"):           false,
		filepath.Join(cart, "Game (USA).state.auto"):    false,
		filepath.Join(cart, "Game (USA).png"):           false,
	} {
		if got := isROMFile(path); got != want {
			t.Errorf("%s: %v, want %v", filepath.Base(path), got, want)
		}
	}
	// A sheet added to the folder later is seen: the folder is read again.
	os.WriteFile(filepath.Join(cart, "Disc (USA).cue"), nil, 0o644)
	// On Windows the folder's time can stay the same within a clock tick.
	later := time.Now().Add(time.Minute)
	os.Chtimes(cart, later, later)
	if isROMFile(filepath.Join(cart, "Sonic (World).bin")) {
		t.Error("a .bin next to a new CUE sheet is still listed")
	}
}

func TestPlatformOf(t *testing.T) {
	for _, c := range []struct {
		names []string
		want  string
	}{
		{[]string{"Sonic (USA).md", "readme.txt"}, "Mega Drive"},
		{[]string{"a/Game (USA).GBA"}, "Game Boy Advance"},
		{[]string{"x.gb", "y.gbc", "z.gb"}, "Game Boy, Game Boy Color"},
		{[]string{"epr-1.ic1", "mpr-2.bin"}, ""},
		{nil, ""},
	} {
		if got := platformOf(c.names); got != c.want {
			t.Errorf("platformOf(%q) = %q, want %q", c.names, got, c.want)
		}
	}
}
