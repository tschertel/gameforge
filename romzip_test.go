package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gameforge/internal/config"
	"gameforge/internal/sevenzip"
)

// TestZipConverter: with ZIP chosen, a ROM and a 7z become ZIPs with the
// same files and dates, and a ZIP is listed but not converted.
func TestZipConverter(t *testing.T) {
	dir := t.TempDir()
	rom := bytes.Repeat([]byte("GBA ROM DATA "), 20_000)
	os.WriteFile(filepath.Join(dir, "Game (USA).gba"), rom, 0o644)
	os.WriteFile(filepath.Join(dir, "Old (USA).zip"), zipOf(t, map[string][]byte{"a.bin": []byte("x")}), 0o644)

	when := time.Date(1998, 9, 4, 12, 30, 0, 0, time.UTC)
	files := map[string][]byte{"sfa3.01": bytes.Repeat([]byte{1, 2, 3, 4}, 30_000), "sfa3.02": []byte("short"), "empty.bin": nil}
	var srcs []sevenzip.Source
	for _, name := range []string{"sfa3.01", "sfa3.02", "empty.bin"} {
		data := files[name]
		srcs = append(srcs, sevenzip.Source{Name: name, Size: int64(len(data)), Modified: when,
			Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }})
	}
	if _, err := sevenzip.Write(context.Background(), filepath.Join(dir, "sfa3.7z"), srcs, nil); err != nil {
		t.Fatal(err)
	}

	u := &ui{cfg: &config.Config{ROMArchive: "zip", SevenZipDelete: true}}
	sevenZip.ui = u
	removed := map[string]bool{}
	removeFile = func(p string) error { removed[filepath.Base(p)] = true; return nil }
	defer func() { removeFile, sevenZip.ui = removeOriginal, nil }()

	tab := &convTab{conv: sevenZipConverter, checked: map[string]bool{}}
	tab.add(u, []string{dir})
	converted := 0
	for _, it := range tab.items {
		name := filepath.Base(it.path)
		if name == "Old (USA).zip" {
			if it.ok || !it.infoOnly {
				t.Errorf("%s would be converted again", name)
			}
			continue
		}
		if !it.ok {
			t.Fatalf("%s not convertible: %v", name, it.detail)
		}
		out := filepath.Join(dir, sevenZipConverter.outName(it))
		if _, _, err := sevenZipConverter.run(context.Background(), u, it, out, false, func(bool, int64, int64) {}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		converted++
		want := map[string][]byte{"Game (USA).gba": rom}
		if name == "sfa3.7z" {
			want = files
		}
		zr, err := zip.OpenReader(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, zf := range zr.File {
			r, _ := zf.Open()
			got, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(got, want[zf.Name]) {
				t.Errorf("%s: %s differs (%v)", out, zf.Name, err)
			}
			if name == "sfa3.7z" && !zf.Modified.Equal(when) {
				t.Errorf("%s: %s dated %v", out, zf.Name, zf.Modified)
			}
			delete(want, zf.Name)
		}
		zr.Close()
		if len(want) > 0 {
			t.Errorf("%s: missing %v", out, want)
		}
		if exe := os.Getenv("SEVENZIP"); exe != "" {
			if b, err := exec.Command(exe, "t", out).CombinedOutput(); err != nil {
				t.Errorf("7z t %s: %v\n%s", out, err, b)
			}
		}
	}
	if converted != 2 || !removed["Game (USA).gba"] || !removed["sfa3.7z"] || len(removed) != 2 {
		t.Errorf("converted %d, removed %v", converted, removed)
	}
}

// zipOf is a ZIP archive of files.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	zw.Close()
	return b.Bytes()
}
