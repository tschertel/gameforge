package pkg

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFakePKG builds fake PS4 packages with LibOrbisPkg's PkgTool (set
// PKGTOOL to PkgTool.exe or PkgTool.Core.exe) and extracts them.
func TestFakePKG(t *testing.T) {
	tool := os.Getenv("PKGTOOL")
	if tool == "" {
		t.Skip("PKGTOOL not set")
	}
	rng := rand.New(rand.NewSource(1))
	random := make([]byte, 300_000)
	rng.Read(random)
	files := map[string][]byte{
		"eboot.bin":            bytes.Repeat([]byte("EBOOT"), 100),
		"data/big.txt":         bytes.Repeat([]byte("compressible line of text\n"), 400_000),
		"data/sub/random.bin":  random,
		"data/sub/empty.dat":   {},
		"data/sub/deep/x.json": []byte(`{"a":1}`),
	}
	for i := range 40 { // more entries than fit in one directory block
		files[fmt.Sprintf("many/file_with_a_rather_long_name_%03d.txt", i)] = []byte(strings.Repeat("x", i))
	}

	for _, tc := range []struct {
		passcode string
		fake     bool
	}{{DefaultPasscode, true}, {"0123456789ABCDEFGHIJKLMNOPQRSTUV", false}} {
		dir := t.TempDir()
		proj := filepath.Join(dir, "proj")
		run := func(args ...string) {
			t.Helper()
			cmd := exec.Command(tool, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), "Error") {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
		os.MkdirAll(filepath.Join(proj, "sce_sys"), 0o755)
		sfo := filepath.Join(proj, "sce_sys", "param.sfo")
		run("sfo_new", sfo)
		for _, kv := range [][2]string{{"TITLE", "Test Game"}, {"APP_VER", "01.02"}, {"CATEGORY", "gd"}, {"TITLE_ID", "TEST00000"}, {"CONTENT_ID", "UP0000-TEST00000_00-0000000000000000"}} {
			run("sfo_setentry", "--type", "utf8", "--maxsize", "128", "--value", kv[1], sfo, kv[0])
		}
		var list strings.Builder
		dirs := map[string]bool{}
		for name, data := range files {
			p := filepath.Join(proj, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, data, 0o644)
			comp := ""
			if name == "data/big.txt" {
				comp = ` pfs_compression="enable"`
			}
			fmt.Fprintf(&list, "<file targ_path=%q orig_path=%q%s />\n", name, "proj/"+name, comp)
			for d := filepath.ToSlash(filepath.Dir(name)); d != "."; d = filepath.ToSlash(filepath.Dir(d)) {
				dirs[d] = true
			}
		}
		fmt.Fprintf(&list, "<file targ_path=\"sce_sys/param.sfo\" orig_path=\"proj/sce_sys/param.sfo\" />\n")
		gp4 := `<?xml version="1.0"?>
<psproject fmt="gp4" version="1000">
  <volume>
    <volume_type>pkg_ps4_app</volume_type>
    <volume_id>PS4VOLUME</volume_id>
    <volume_ts>2020-01-01 00:00:00</volume_ts>
    <package content_id="UP0000-TEST00000_00-0000000000000000" passcode="` + tc.passcode + `" storage_type="digital50" app_type="full" />
    <chunk_info chunk_count="1" scenario_count="1">
      <chunks><chunk id="0" layer_no="0" label="Chunk #0" /></chunks>
      <scenarios default_id="0"><scenario id="0" type="sp" initial_chunk_count="1" label="Scenario #0">0</scenario></scenarios>
    </chunk_info>
  </volume>
  <files img_no="0">
` + list.String() + `  </files>
  <rootdir>` + dirTree(dirs, "") + `<dir targ_name="sce_sys" /></rootdir>
</psproject>
`
		os.WriteFile(filepath.Join(dir, "p.gp4"), []byte(gp4), 0o644)
		os.Mkdir(filepath.Join(dir, "out"), 0o755)
		run("pkg_build", "p.gp4", "out")
		path := filepath.Join(dir, "out", "UP0000-TEST00000_00-0000000000000000.pkg")

		p, err := Open(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.Fake != tc.fake || p.Title() != "Test Game" {
			t.Fatalf("passcode %s: fake %v, title %q", tc.passcode, p.Fake, p.Title())
		}
		if !tc.fake {
			continue
		}
		if p.Files != len(files)+1 {
			t.Errorf("files %d, want %d", p.Files, len(files)+1)
		}
		out := filepath.Join(dir, "x")
		if _, err := p.Extract(context.Background(), out, func(int64, int64) {}); err != nil {
			t.Fatal(err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s: %d bytes, want %d (%v)", name, len(got), len(want), err)
			}
		}
		if got, _ := os.ReadFile(filepath.Join(out, "sce_sys", "param.sfo")); !bytes.HasPrefix(got, []byte("\x00PSF")) {
			t.Error("sce_sys/param.sfo missing")
		}
	}
}

// dirTree writes the <dir> elements of a GP4 for the folders under prefix.
func dirTree(dirs map[string]bool, prefix string) string {
	var b strings.Builder
	for d := range dirs {
		rest := strings.TrimPrefix(d, prefix)
		if !strings.HasPrefix(d, prefix) || rest == "" || strings.Contains(rest, "/") {
			continue
		}
		fmt.Fprintf(&b, `<dir targ_name=%q>%s</dir>`, rest, dirTree(dirs, d+"/"))
	}
	return b.String()
}
