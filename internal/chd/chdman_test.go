package chd

import (
	"bytes"
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests compare with MAME's chdman. They run only when CHDMAN names
// the chdman executable, e.g. CHDMAN=C:\mame\chdman.exe go test ./internal/chd
func chdman(t *testing.T) string {
	p := os.Getenv("CHDMAN")
	if p == "" {
		t.Skip("CHDMAN not set")
	}
	return p
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", filepath.Base(name), args, err, out)
	}
	return string(out)
}

var shaLine = regexp.MustCompile(`(?m)^(SHA1|Data SHA1):\s+([0-9a-f]{40})`)

func infoSHA(t *testing.T, tool, path string) map[string]string {
	out := run(t, tool, "info", "-i", path)
	m := map[string]string{}
	for _, s := range shaLine.FindAllStringSubmatch(out, -1) {
		m[s[1]] = s[2]
	}
	if len(m) != 2 {
		t.Fatalf("no SHA-1 in chdman info:\n%s", out)
	}
	return m
}

// testDisc writes a disc with Mode 1 sectors (valid parity), Mode 2
// sectors, empty areas and audio tracks.
func testDisc(t *testing.T, dir string, rng *rand.Rand) {
	var data []byte
	for i := range 2203 {
		var s []byte
		switch {
		case i < 1200:
			s = mode1Sector(rng, i+150)
		case i < 1800:
			s = make([]byte, SectorSize)
			copy(s, syncHeader[:])
			s[15] = 2
			for k := 16; k < SectorSize; k++ {
				s[k] = byte(rng.IntN(8))
			}
			if i%2 == 0 { // Mode 2 Form 1 with valid parity, as on PS1 discs
				s[12], s[13], s[14] = byte(i/4500), byte(i/75%60), byte(i%75)
				eccGenerate(s)
			}
		default:
			s = make([]byte, SectorSize)
		}
		data = append(data, s...)
	}
	audio := func(n int) []byte {
		b := make([]byte, n*SectorSize)
		v := 0
		for i := 0; i < len(b); i += 2 {
			v += rng.IntN(64) - 32
			b[i], b[i+1] = byte(v), byte(v>>8)
		}
		return b
	}
	os.WriteFile(filepath.Join(dir, "multi (Track 1).bin"), data, 0o644)
	os.WriteFile(filepath.Join(dir, "multi (Track 2).bin"), audio(751), 0o644)
	os.WriteFile(filepath.Join(dir, "multi (Track 3).bin"), audio(302), 0o644)
	os.WriteFile(filepath.Join(dir, "multi.cue"), []byte(`FILE "multi (Track 1).bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
FILE "multi (Track 2).bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
FILE "multi (Track 3).bin" BINARY
  TRACK 03 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
`), 0o644)

	single := append(append([]byte(nil), data[:1500*SectorSize]...), audio(600)...)
	os.WriteFile(filepath.Join(dir, "single.bin"), single, 0o644)
	os.WriteFile(filepath.Join(dir, "single.cue"), []byte("FILE \"single.bin\" BINARY\r\n  TRACK 01 MODE1/2352\r\n    INDEX 01 00:00:00\r\n"+
		"  TRACK 02 AUDIO\r\n    PREGAP 00:02:00\r\n    INDEX 01 00:20:00\r\n"), 0o644)

	// Track 2 starts at 00:20:00 = frame 1500.
	iso := make([]byte, 3001*DVDSectorSize)
	for i := range len(iso) / 2 {
		iso[i] = byte(rng.IntN(16))
	}
	os.WriteFile(filepath.Join(dir, "dvd.iso"), iso, 0o644)

	// Dreamcast: a .gdi and the same disc as a Redump multi-cue. The
	// high-density area starts at sector 45000.
	gd1, gd3 := data[:300*SectorSize], data[300*SectorSize:800*SectorSize]
	gd2, gd4 := audio(75), audio(100)
	os.WriteFile(filepath.Join(dir, "gd1.bin"), gd1, 0o644)
	os.WriteFile(filepath.Join(dir, "gd2.raw"), gd2, 0o644)
	os.WriteFile(filepath.Join(dir, "gd 3.bin"), gd3, 0o644)
	os.WriteFile(filepath.Join(dir, "gd4.raw"), gd4, 0o644)
	os.WriteFile(filepath.Join(dir, "disc.gdi"), []byte("4\r\n1 0 4 2352 gd1.bin 0\r\n2 450 0 2352 gd2.raw 0\r\n"+
		"3 45000 4 2352 \"gd 3.bin\" 0\r\n4 45650 0 2352 gd4.raw 0\r\n"), 0o644)
	pregap := make([]byte, 150*SectorSize)
	os.WriteFile(filepath.Join(dir, "rd1.bin"), gd1, 0o644)
	os.WriteFile(filepath.Join(dir, "rd2.bin"), append(append([]byte(nil), pregap...), gd2...), 0o644)
	os.WriteFile(filepath.Join(dir, "rd3.bin"), gd3, 0o644)
	os.WriteFile(filepath.Join(dir, "rd4.bin"), append(append([]byte(nil), pregap...), gd4...), 0o644)
	os.WriteFile(filepath.Join(dir, "gdrom.cue"), []byte(`REM SINGLE-DENSITY AREA
FILE "rd1.bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 00:00:00
FILE "rd2.bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
REM HIGH-DENSITY AREA
FILE "rd3.bin" BINARY
  TRACK 03 MODE1/2352
    INDEX 01 00:00:00
FILE "rd4.bin" BINARY
  TRACK 04 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
`), 0o644)
}

func TestChdmanCompat(t *testing.T) {
	tool := chdman(t)
	dir := t.TempDir()
	testDisc(t, dir, rand.New(rand.NewPCG(11, 12)))

	for _, c := range []struct{ name, in, create, extract string }{
		{"multi", "multi.cue", "createcd", "extractcd"},
		{"single", "single.cue", "createcd", "extractcd"},
		{"dvd", "dvd.iso", "createdvd", "extractdvd"},
		{"isocd", "dvd.iso", "createcd", "extractcd"},
		{"gdi", "disc.gdi", "createcd", "extractcd"},
		{"gdicue", "gdrom.cue", "createcd", "extractcd"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := filepath.Join(dir, c.in)
			ours := filepath.Join(dir, c.name+".ours.chd")
			theirs := filepath.Join(dir, c.name+".chdman.chd")
			var err error
			switch {
			case c.name == "isocd":
				_, err = ConvertISOAsCD(context.Background(), in, ours, Options{})
			case c.name == "gdi":
				_, err = ConvertGDI(context.Background(), in, ours, Options{})
			case c.create == "createcd":
				_, err = ConvertCue(context.Background(), in, ours, Options{})
			default:
				_, err = ConvertISO(context.Background(), in, ours, Options{})
			}
			if err != nil {
				t.Fatal(err)
			}
			run(t, tool, "verify", "-i", ours)
			for _, l := range regexp.MustCompile(`(?m)^\s+\d+\s+[\d.]+%.*$`).FindAllString(run(t, tool, "info", "-v", "-i", ours), -1) {
				t.Logf("ours: %s", l)
			}
			run(t, tool, c.create, "-i", in, "-o", theirs)
			a, b := infoSHA(t, tool, ours), infoSHA(t, tool, theirs)
			if a["Data SHA1"] != b["Data SHA1"] {
				t.Errorf("data SHA-1: ours %s, chdman %s", a["Data SHA1"], b["Data SHA1"])
			}
			if a["SHA1"] != b["SHA1"] {
				t.Errorf("overall SHA-1 (metadata): ours %s, chdman %s", a["SHA1"], b["SHA1"])
				meta := regexp.MustCompile(`(?m)^\s+(TRACK:.*|Logical size.*)$`)
				t.Logf("ours:\n%s\nchdman:\n%s",
					strings.Join(meta.FindAllString(run(t, tool, "info", "-v", "-i", ours), -1), "\n"),
					strings.Join(meta.FindAllString(run(t, tool, "info", "-v", "-i", theirs), -1), "\n"))
			}
			// chdman's own files use huff and flac (DVD) or cdfl (CD) too.
			if _, err := Verify(context.Background(), theirs, nil); err != nil {
				t.Errorf("reading chdman's CHD: %v", err)
			}
			sa, _ := os.Stat(ours)
			sb, _ := os.Stat(theirs)
			t.Logf("size: ours %d, chdman %d (%+.1f%%)", sa.Size(), sb.Size(), 100*float64(sa.Size()-sb.Size())/float64(sb.Size()))

			// Extracting both must give the same image.
			outA, outB := filepath.Join(dir, c.name+".a"), filepath.Join(dir, c.name+".b")
			ext := ".cue"
			if c.extract == "extractdvd" {
				ext = ".iso"
			}
			run(t, tool, c.extract, "-i", ours, "-o", outA+ext, "-f")
			run(t, tool, c.extract, "-i", theirs, "-o", outB+ext, "-f")
			for _, sfx := range []string{ext, ".bin"} {
				x, errA := os.ReadFile(outA + sfx)
				y, errB := os.ReadFile(outB + sfx)
				if errA != nil || errB != nil {
					continue
				}
				if sfx == ".cue" {
					x = bytes.ReplaceAll(x, []byte(filepath.Base(outA)), []byte("X"))
					y = bytes.ReplaceAll(y, []byte(filepath.Base(outB)), []byte("X"))
				}
				if !bytes.Equal(x, y) {
					t.Errorf("extracted %s differs:\n%s\n---\n%s", sfx, x[:min(len(x), 600)], y[:min(len(y), 600)])
				}
			}
		})
	}
}

// CHDs that chdman made with the zstd codecs (cdzs for CDs, zstd for
// DVDs) are read and extracted like chdman's own extraction.
func TestChdmanZstd(t *testing.T) {
	tool := chdman(t)
	dir := t.TempDir()
	testDisc(t, dir, rand.New(rand.NewPCG(13, 14)))
	for _, c := range []struct{ name, in, create, codecs string }{
		{"cd", "multi.cue", "createcd", "cdzs"},
		{"cdmix", "multi.cue", "createcd", "cdzs,cdfl"},
		{"gdi", "disc.gdi", "createcd", "cdzs,cdfl"},
		{"dvd", "dvd.iso", "createdvd", "zstd"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := filepath.Join(dir, c.in)
			theirs := filepath.Join(dir, c.name+".chd")
			run(t, tool, c.create, "-i", in, "-o", theirs, "-c", c.codecs)
			if _, err := Verify(context.Background(), theirs, nil); err != nil {
				t.Fatalf("reading chdman's CHD: %v", err)
			}
		})
	}
}
