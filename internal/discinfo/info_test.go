package discinfo

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gameforge/internal/chd"
)

// isoTree builds an ISO 9660 image with files in the root and in one level
// of folders ("PSP_GAME/PARAM.SFO").
func isoTree(files map[string][]byte, sectors int) []byte {
	img := make([]byte, sectors*2048)
	pvd := img[16*2048:]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	rec := func(name string, lba, size int, dir bool) []byte {
		r := make([]byte, 33+len(name)+(1-len(name)%2))
		r[0] = byte(len(r))
		binary.LittleEndian.PutUint32(r[2:], uint32(lba))
		binary.LittleEndian.PutUint32(r[10:], uint32(size))
		if dir {
			r[25] = 2
		}
		r[32] = byte(len(name))
		copy(r[33:], name)
		return r
	}
	next := 20
	alloc := func(size int) int {
		lba := next
		next += max(1, (size+2047)/2048)
		return lba
	}
	byDir := map[string][]string{}
	for name := range files {
		d, f, ok := strings.Cut(name, "/")
		if !ok {
			d, f = "", name
		}
		byDir[d] = append(byDir[d], f)
	}
	rootLBA := alloc(2048)
	var root []byte
	root = append(root, rec("\x00", rootLBA, 2048, true)...)
	var dirs []string
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		names := byDir[d]
		sort.Strings(names)
		var listing []byte
		dirLBA := rootLBA
		if d != "" {
			dirLBA = alloc(2048)
			listing = append(listing, rec("\x00", dirLBA, 2048, true)...)
			root = append(root, rec(d, dirLBA, 2048, true)...)
		}
		for _, n := range names {
			content := files[n]
			if d != "" {
				content = files[d+"/"+n]
			}
			lba := alloc(len(content))
			copy(img[lba*2048:], content)
			r := rec(n+";1", lba, len(content), false)
			if d == "" {
				root = append(root, r...)
			} else {
				listing = append(listing, r...)
			}
		}
		if d != "" {
			copy(img[dirLBA*2048:], listing)
		}
	}
	copy(img[rootLBA*2048:], root)
	copy(pvd[156:], rec("\x00", rootLBA, 2048, true))
	return img
}

// sfo builds a PARAM.SFO with text and number values.
func sfo(text map[string]string, nums map[string]uint32) []byte {
	var keys []string
	for k := range text {
		keys = append(keys, k)
	}
	for k := range nums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var ktab, dtab, entries []byte
	for _, k := range keys {
		e := make([]byte, 16)
		binary.LittleEndian.PutUint16(e[0:], uint16(len(ktab)))
		binary.LittleEndian.PutUint32(e[12:], uint32(len(dtab)))
		ktab = append(append(ktab, k...), 0)
		if v, ok := nums[k]; ok {
			binary.LittleEndian.PutUint16(e[2:], 0x0404)
			binary.LittleEndian.PutUint32(e[4:], 4)
			binary.LittleEndian.PutUint32(e[8:], 4)
			dtab = binary.LittleEndian.AppendUint32(dtab, v)
		} else {
			v := append([]byte(text[k]), 0)
			binary.LittleEndian.PutUint16(e[2:], 0x0204)
			binary.LittleEndian.PutUint32(e[4:], uint32(len(v)))
			binary.LittleEndian.PutUint32(e[8:], uint32(len(v)))
			dtab = append(dtab, v...)
		}
		entries = append(entries, e...)
	}
	for len(ktab)%4 != 0 {
		ktab = append(ktab, 0)
	}
	h := make([]byte, 20)
	copy(h, "\x00PSF")
	binary.LittleEndian.PutUint32(h[4:], 0x101)
	binary.LittleEndian.PutUint32(h[8:], uint32(20+len(entries)))
	binary.LittleEndian.PutUint32(h[12:], uint32(20+len(entries)+len(ktab)))
	binary.LittleEndian.PutUint32(h[16:], uint32(len(keys)))
	return append(append(append(h, entries...), ktab...), dtab...)
}

func put(b []byte, off int, s string, size int) {
	copy(b[off:off+size], s+strings.Repeat(" ", size))
}

func writeFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInfoFromISO(t *testing.T) {
	icon := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 3000)...)
	psp := isoTree(map[string][]byte{
		"UMD_DATA.BIN": []byte("ULUS-10041|"),
		"PSP_GAME/PARAM.SFO": sfo(
			map[string]string{"TITLE": "Grand Theft Auto:  Liberty City Stories", "DISC_ID": "ULUS10041", "DISC_VERSION": "1.02", "PSP_SYSTEM_VER": "02.60", "CATEGORY": "UG"},
			map[string]uint32{"DISC_NUMBER": 1, "DISC_TOTAL": 1}),
		"PSP_GAME/ICON0.PNG": icon,
	}, 60)

	saturn := make([]byte, 40*2048)
	put(saturn, 0, "SEGA SEGASATURN", 16)
	put(saturn, 0x20, "MK-81009", 10)
	put(saturn, 0x2A, "V1.003", 6)
	put(saturn, 0x30, "19951117", 8)
	put(saturn, 0x38, "CD-2/2", 8)
	put(saturn, 0x40, "JTUE", 10)
	put(saturn, 0x60, "PANZER DRAGOON  ZWEI", 112)

	segacd := make([]byte, 40*2048)
	put(segacd, 0, "SEGADISCSYSTEM", 16)
	put(segacd, 0x100, "SEGA MEGA DRIVE", 16)
	put(segacd, 0x120, "SONIC THE HEDGEHOG CD", 48)
	put(segacd, 0x150, "SONIC CD", 48)
	put(segacd, 0x180, "GM MK-4407 -00", 14)
	put(segacd, 0x1F0, "U", 3)

	gc := make([]byte, 40*2048)
	copy(gc, "GALE01")
	gc[7] = 2
	binary.BigEndian.PutUint32(gc[0x1c:], 0xc2339f3d)
	copy(gc[0x20:], "Super Smash Bros. Melee")

	for _, c := range []struct {
		name string
		img  []byte
		want Info
	}{
		{"psp.iso", psp, Info{Platform: PSP, Serial: "ULUS-10041", Title: "Grand Theft Auto: Liberty City Stories", Version: "1.02", Regions: []string{"USA"}, Firmware: "2.60", Icon: icon}},
		{"ps2.iso", isoTree(map[string][]byte{"SYSTEM.CNF": []byte("BOOT2 = cdrom0:\\SLES_504.90;1\r\nVER = 1.01\r\nVMODE = PAL\r\n")}, 40),
			Info{Platform: PS2, Serial: "SLES-50490", Version: "1.01", Regions: []string{"Europe"}}},
		{"ps1.iso", isoTree(map[string][]byte{"SYSTEM.CNF": []byte("BOOT=cdrom:SLPS_012.34;1\r\nTCB=4\r\n")}, 40),
			Info{Platform: PS1, Serial: "SLPS-01234", Regions: []string{"Japan"}}},
		{"saturn.iso", saturn, Info{Platform: Saturn, Serial: "MK-81009", Title: "PANZER DRAGOON ZWEI", Version: "1.003", Regions: []string{"Japan", "Asia", "USA", "Europe"}, Date: "1995-11-17", Disc: 2, Discs: 2}},
		{"segacd.iso", segacd, Info{Platform: SegaCD, Serial: "MK-4407", Title: "SONIC CD", Version: "00", Regions: []string{"USA"}}},
		{"gc.iso", gc, Info{Platform: GameCube, Serial: "GALE01", Title: "Super Smash Bros. Melee", Version: "1.02", Regions: []string{"USA"}}},
	} {
		got, err := InfoFromISO(writeFile(t, c.name, c.img))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			got.Icon, c.want.Icon = got.Icon[:min(8, len(got.Icon))], c.want.Icon[:min(8, len(c.want.Icon))]
			t.Errorf("%s:\n got %+v (%v)\nwant %+v", c.name, got, err, c.want)
		}
	}
}

func TestDreamcastInfo(t *testing.T) {
	ip := make([]byte, 2048)
	put(ip, 0, "SEGA SEGAKATANA", 16)
	put(ip, 0x10, "SEGA ENTERPRISES", 16)
	put(ip, 0x20, "5D4B GD-ROM1/1", 16)
	put(ip, 0x30, "JUE", 8)
	put(ip, 0x40, "MK-51000", 10)
	put(ip, 0x4A, "V1.005", 6)
	put(ip, 0x50, "19990909", 16)
	put(ip, 0x80, "SONIC ADVENTURE", 128)
	empty := make([]byte, 2048)
	// IP.BIN after a few empty sectors, as when a pregap is stored.
	got := dreamcastInfo(func(lba int) ([]byte, error) {
		if lba == 3 {
			return ip, nil
		}
		return empty, nil
	})
	want := Info{Platform: Dreamcast, Serial: "MK-51000", Title: "SONIC ADVENTURE", Version: "1.005", Regions: []string{"Japan", "USA", "Europe"}, Date: "1999-09-09"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// The information read through a CHD is the same as from the image it was
// made from: an ISO as DVD and as CD, and a raw Mode 2 CUE/BIN.
func TestInfoFromCHD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	ps2 := isoTree(map[string][]byte{"SYSTEM.CNF": []byte("BOOT2 = cdrom0:\\SLUS_214.09;1\r\nVER = 1.01\r\n")}, 300)
	iso := filepath.Join(dir, "ps2.iso")
	os.WriteFile(iso, ps2, 0o644)
	want, err := InfoFromISO(iso)
	if err != nil || want.Serial != "SLUS-21409" {
		t.Fatalf("ISO: %+v %v", want, err)
	}
	for name, convert := range map[string]func(context.Context, string, string, chd.Options) (*chd.Result, error){
		"dvd.chd": chd.ConvertISO, "cd.chd": chd.ConvertISOAsCD,
	} {
		out := filepath.Join(dir, name)
		if _, err := convert(ctx, iso, out, chd.Options{}); err != nil {
			t.Fatal(err)
		}
		got, err := InfoFromCHD(out)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v (%v), want %+v", name, got, err, want)
		}
	}

	ps1 := isoTree(map[string][]byte{"SYSTEM.CNF": []byte("BOOT = cdrom:\\SCES_000.01;1\r\n")}, 300)
	var raw []byte
	for s := 0; s < 300; s++ {
		sec := make([]byte, 2352)
		copy(sec[1:11], bytes.Repeat([]byte{0xFF}, 10))
		sec[15] = 2
		copy(sec[24:], ps1[s*2048:(s+1)*2048])
		raw = append(raw, sec...)
	}
	os.WriteFile(filepath.Join(dir, "ps1.bin"), raw, 0o644)
	// An audio track after the data, with a pregap stored in the file.
	os.WriteFile(filepath.Join(dir, "ps1 (Track 2).bin"), make([]byte, 2352*200), 0o644)
	cue := filepath.Join(dir, "ps1.cue")
	os.WriteFile(cue, []byte("FILE \"ps1.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"+
		"FILE \"ps1 (Track 2).bin\" BINARY\n  TRACK 02 AUDIO\n    INDEX 00 00:00:00\n    INDEX 01 00:02:00\n"), 0o644)
	want = Info{Platform: PS1, Serial: "SCES-00001", Regions: []string{"Europe"}}
	if got, err := InfoFromCue(cue); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CUE: %+v (%v)", got, err)
	}
	out := filepath.Join(dir, "ps1.chd")
	if _, err := chd.ConvertImage(ctx, cue, out, chd.Options{}); err != nil {
		t.Fatal(err)
	}
	if got, err := InfoFromCHD(out); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CHD from CUE: %+v (%v)", got, err)
	}
}

// A self-booting Dreamcast CD-R with a data track holding no game in its
// first session: the game is read from IP.BIN in the second session's
// track, also through a CHD.
func TestInfoFromCueDataData(t *testing.T) {
	dir := t.TempDir()
	ip := make([]byte, 2048)
	put(ip, 0, "SEGA SEGAKATANA", 16)
	put(ip, 0x40, "MK-51063", 10)
	put(ip, 0x4A, "V1.008", 6)
	put(ip, 0x80, "NBA2K1", 128)
	game := append(ip, make([]byte, 2048*299)...)
	os.WriteFile(filepath.Join(dir, "t1.iso"), make([]byte, 2048*300), 0o644)
	os.WriteFile(filepath.Join(dir, "t2.iso"), game, 0o644)
	cue := filepath.Join(dir, "dc.cue")
	os.WriteFile(cue, []byte("FILE \"t1.iso\" BINARY\n  TRACK 01 MODE1/2048\n    INDEX 01 00:00:00\n"+
		"FILE \"t2.iso\" BINARY\n  TRACK 02 MODE1/2048\n    INDEX 01 00:00:00\n"), 0o644)
	got, err := InfoFromCue(cue)
	if err != nil || got.Platform != Dreamcast || got.Serial != "MK-51063" || got.Title != "NBA2K1" {
		t.Fatalf("CUE: %+v %v", got, err)
	}
	out := filepath.Join(dir, "dc.chd")
	if _, err := chd.ConvertImage(context.Background(), cue, out, chd.Options{}); err != nil {
		t.Fatal(err)
	}
	if got2, err := InfoFromCHD(out); err != nil || !reflect.DeepEqual(got2, got) {
		t.Errorf("CHD: %+v %v, want %+v", got2, err, got)
	}
}
