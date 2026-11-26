package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gameforge/internal/arcade"
)

// TestScanCollection: one row per title and console folder, the other
// consoles of each title, and no rows for saves, playlists or hidden files.
func TestScanCollection(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"PS2/LEGO Star Wars II - The Original Trilogy (USA).chd",
		"gba/LEGO Star Wars II - The Original Trilogy (USA) (En,Fr,De).gba",
		"gba/LEGO Star Wars II - The Original Trilogy (USA) (En,Fr,De).srm", // a save
		"psp/Lego Star Wars II, The Original Trilogy (Europe).iso",          // other case and punctuation
		"PS1/Final Fantasy VII (USA)/Final Fantasy VII (USA) (Disc 1).cue",
		"PS1/Final Fantasy VII (USA)/Final Fantasy VII (USA) (Disc 1) (Track 1).bin",
		"PS1/Final Fantasy VII (USA)/Final Fantasy VII (USA) (Disc 2).cue",
		"PS1/Final Fantasy VII (USA).m3u",
		"PS1/.hidden/Other Game (USA).cue",
		"XBOX/LEGO Star Wars II - The Original Trilogy (USA).xiso.iso",
		"NEOGEO/neogeo.zip", // a BIOS
		"NEOGEO/mslug.zip",
		"CPS2/sfa3.zip", // a short name: the title of the arcade lists
		"PSX/Street Fighter Alpha 3 (USA).chd",
		"PS2/King of Fighters XI, The (USA).chd",
		"Atomiswave/The King of Fighters XI.zip",
		"Wii/User/Wii/fst.bin", // Dolphin's files
		"Wii/User/Cache/Shaders/Vulkan-uber.cache",
		"loose (USA).gba", // not in a console folder
	} {
		p := filepath.Join(root, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	rows, err := scanCollection(root, &arcade.DB{Lists: map[string]*arcade.List{"fbneo": {Sets: map[string]*arcade.Set{
		"neogeo": {Name: "neogeo", Title: "Neo-Geo", Bios: true},
		"sfa3":   {Name: "sfa3", Title: "Street Fighter Alpha 3 (USA 980904)"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Console+": "+r.Name+" | "+r.Others)
	}
	want := []string{
		"PS1: Final Fantasy VII | ",
		"Atomiswave: The King of Fighters XI | PS2",
		"PS2: King of Fighters XI, The | Atomiswave",
		"gba: LEGO Star Wars II - The Original Trilogy | PS2, psp, XBOX",
		"PS2: LEGO Star Wars II - The Original Trilogy | gba, psp, XBOX",
		"psp: Lego Star Wars II, The Original Trilogy | gba, PS2, XBOX",
		"XBOX: LEGO Star Wars II - The Original Trilogy | gba, PS2, psp",
		"NEOGEO: mslug | ",
		"CPS2: Street Fighter Alpha 3 | PSX",
		"PSX: Street Fighter Alpha 3 | CPS2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	html := string(collectionHTML(root, rows, time.Now()))
	if !strings.Contains(html, `data-n="4"`) || !strings.Contains(html, `<option value="psp">psp</option>`) {
		t.Error("report without the console count or the console filter")
	}
}
