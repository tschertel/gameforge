package arcade

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gameforge/internal/datfile"
)

// A small list in FBNeo's format: a BIOS, a parent with a file from the
// BIOS, and a clone sharing the parent's files.
const sample = `<?xml version="1.0"?>
<!DOCTYPE datafile PUBLIC "-//FinalBurn Neo//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">
<datafile>
	<header>
		<name>FinalBurn Neo - Arcade Games</name>
		<version>1.0.0.03</version>
	</header>
	<game isbios="yes" name="neogeo">
		<description>Neo Geo</description>
		<rom name="sp-s2.sp1" size="131072" crc="9036d879"/>
		<rom name="sfix.sfix" size="131072" crc="c2ea0cfd"/>
		<driver status="good"/>
	</game>
	<game name="blazstar" romof="neogeo">
		<description>Blazing Star</description>
		<year>1998</year>
		<manufacturer>Yumekobo</manufacturer>
		<rom name="239-p1.p1" size="1048576" crc="183682f8"/>
		<rom name="239-c1.c1" size="4194304" crc="84f6d584"/>
		<rom name="239-x1.x1" size="1024" status="nodump"/>
		<rom name="sp-s2.sp1" merge="sp-s2.sp1" size="131072" crc="9036d879"/>
		<driver status="good"/>
	</game>
	<game name="blazstara" cloneof="blazstar" romof="blazstar">
		<description>Blazing Star (alt)</description>
		<rom name="239-p1a.p1" size="1048576" crc="11111111"/>
		<rom name="239-c1.c1" merge="239-c1.c1" size="4194304" crc="84f6d584"/>
		<rom name="sp-s2.sp1" merge="sp-s2.sp1" size="131072" crc="9036d879"/>
		<driver status="preliminary"/>
	</game>
</datafile>
`

var (
	bios   = []File{{"sp-s2.sp1", 131072, 0x9036d879}, {"sfix.sfix", 131072, 0xc2ea0cfd}}
	parent = []File{{"239-p1.p1", 1048576, 0x183682f8}, {"239-c1.c1", 4194304, 0x84f6d584}}
)

func list(t *testing.T) *List {
	l, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestParse(t *testing.T) {
	l := list(t)
	if l.Version != "1.0.0.03" || len(l.Sets) != 3 {
		t.Fatalf("version %q, %d sets", l.Version, len(l.Sets))
	}
	s := l.Sets["blazstar"]
	if s.Title != "Blazing Star" || s.Year != "1998" || s.RomOf != "neogeo" || !s.Works || len(s.Roms) != 4 || !s.Roms[2].NoDump {
		t.Errorf("blazstar %+v", s)
	}
	if !l.Sets["neogeo"].Bios || l.Sets["blazstara"].Works || l.Sets["blazstara"].CloneOf != "blazstar" {
		t.Errorf("flags")
	}
}

func TestAudit(t *testing.T) {
	l := list(t)
	folder := func(sets map[string][]File) func(string) ([]File, bool) {
		return func(name string) ([]File, bool) { f, ok := sets[name]; return f, ok }
	}
	none := folder(nil)
	withBios := folder(map[string][]File{"neogeo": bios})

	// Non-merged: everything inside, no other set needed.
	r := l.Audit("BlazStar", append(parent, bios[0]), none)
	if !r.OK() || len(r.Uses) != 0 {
		t.Errorf("non-merged %+v", r)
	}
	// Split: the BIOS file comes from neogeo.zip, if it is there.
	if r := l.Audit("blazstar", parent, withBios); !r.OK() || !slicesEq(r.Uses, "neogeo") {
		t.Errorf("split %+v", r)
	}
	// Without the BIOS the game is complete; the BIOS is only noted.
	if r := l.Audit("blazstar", parent, none); !r.OK() || len(r.Needs) != 0 || !slicesEq(r.Bios, "neogeo") || r.Missing != 0 {
		t.Errorf("no BIOS %+v", r)
	}
	// A file missing, and one with another CRC.
	r = l.Audit("blazstar", []File{parent[0], {"239-c1.c1", 4194304, 0xdeadbeef}}, withBios)
	if r.OK() || r.Wrong != 1 || r.Missing != 0 {
		t.Errorf("wrong CRC %+v", r)
	}
	if r := l.Audit("blazstar", parent[:1], withBios); r.Missing != 1 {
		t.Errorf("missing %+v", r)
	}
	// A split clone needs its parent; the BIOS is only noted.
	clone := []File{{"239-p1a.p1", 1048576, 0x11111111}}
	if r := l.Audit("blazstara", clone, none); r.OK() || !slicesEq(r.Needs, "blazstar") || !slicesEq(r.Bios, "neogeo") {
		t.Errorf("clone without parent %+v", r)
	}
	r = l.Audit("blazstara", clone, folder(map[string][]File{"blazstar": parent, "neogeo": bios}))
	if !r.OK() || !slicesEq(r.Uses, "blazstar", "neogeo") {
		t.Errorf("clone %+v", r)
	}
	// An unknown name: what the files are.
	if r := l.Audit("Blazing Star (MAME)", parent, none); r.Set != nil || r.Like == nil || r.Like.Name != "blazstar" {
		t.Errorf("unknown %+v", r)
	}
	if r := l.Audit("other", []File{{"x", 1, 2}}, none); r.Like != nil {
		t.Errorf("nothing %+v", r)
	}
}

func slicesEq(a []string, b ...string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

func TestDownloadAndCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/FinalBurn Neo (ClrMame Pro XML, Arcade only).dat" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	old := FBNeoBase
	FBNeoBase = srv.URL + "/"
	defer func() { FBNeoBase = old }()

	db, err := Download(context.Background(), nil, []string{"fbneo"}, nil)
	if err != nil || !db.Has("fbneo") || len(db.Lists["fbneo"].Sets) != 3 {
		t.Fatalf("download: %v", err)
	}
	db, err = Download(context.Background(), db, []string{"fbneo"}, nil)
	if err != nil || calls != 2 || !db.Has("fbneo") {
		t.Fatalf("second download: %v, %d calls", err, calls)
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := db.Save(path); err != nil {
		t.Fatal(err)
	}
	db2, err := Load(path)
	if err != nil || db2.Lists["fbneo"].Sets["blazstar"].Title != "Blazing Star" {
		t.Fatalf("load: %v", err)
	}
	if r := db2.Lists["fbneo"].Audit("blazstar", parent, func(string) ([]File, bool) { return bios, true }); !r.OK() {
		t.Errorf("audit after load %+v", r)
	}
}

// TestRealList reads FBNeo's real list, if FBNEO_DAT points to it, and
// checks the ZIPs of ARCADE_DIR, if set.
func TestRealList(t *testing.T) {
	path := os.Getenv("FBNEO_DAT")
	if path == "" {
		t.Skip("FBNEO_DAT not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d sets", l.Version, len(l.Sets))
	if s := l.Sets["blazstar"]; s == nil || s.Title != "Blazing Star" || s.RomOf != "neogeo" {
		t.Errorf("blazstar %+v", s)
	}
	dir := os.Getenv("ARCADE_DIR")
	if dir == "" {
		return
	}
	read := func(p string) ([]File, bool) {
		zr, err := zip.OpenReader(p)
		if err != nil {
			return nil, false
		}
		defer zr.Close()
		var fs []File
		for _, f := range zr.File {
			fs = append(fs, File{f.Name, int64(f.UncompressedSize64), f.CRC32})
		}
		return fs, true
	}
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	for _, z := range zips {
		files, _ := read(z)
		name := strings.TrimSuffix(filepath.Base(z), ".zip")
		r := l.Audit(name, files, func(set string) ([]File, bool) { return read(filepath.Join(dir, set+".zip")) })
		t.Logf("%s: ok=%v missing=%d wrong=%d needs=%v uses=%v like=%v", name, r.OK(), r.Missing, r.Wrong, r.Needs, r.Uses, r.Like != nil)
	}
}

// MAME's own -listxml (the lists of MAME 2003 to 2010) and a Logiqx list
// of <machine>s that states its version only in the description (2016).
const listxml = `<?xml version="1.0"?>
<!DOCTYPE mame [
<!ELEMENT mame (game+)>
]>
<mame build="0.139 (Jan  2 2019)" debug="no">
	<game name="neogeo" isbios="yes">
		<description>Neo-Geo</description>
		<rom name="sp-s2.sp1" size="131072" crc="9036d879" sha1="x" region="mainbios"/>
	</game>
	<game name="mslug" romof="neogeo">
		<description>Metal Slug</description>
		<rom name="201-p1.p1" size="2097152" crc="08d8daa5" region="maincpu"/>
		<rom name="sp-s2.sp1" merge="sp-s2.sp1" size="131072" crc="9036d879" region="mainbios"/>
		<chip type="cpu" name="68000"/>
		<disk name="none" sha1="y"/>
		<driver status="imperfect"/>
	</game>
</mame>
`

const machines = `<?xml version="1.0" encoding="UTF-8"?>
<datafile>
	<header>
		<description>MAME Arcade 0.174</description>
		<version>-not specified-</version>
	</header>
	<machine name="mslug" romof="neogeo">
		<description>Metal Slug - Super Vehicle-001</description>
		<rom name="201-p1.p1" size="2097152" crc="08d8daa5" sha1="z"/>
		<driver status="good"/>
	</machine>
</datafile>
`

func TestMameFormats(t *testing.T) {
	l, err := Parse(strings.NewReader(listxml))
	if err != nil {
		t.Fatal(err)
	}
	s := l.Sets["mslug"]
	if l.Version != "0.139" || s == nil || s.Works || s.Disks != 1 || len(s.Roms) != 2 || !l.Sets["neogeo"].Bios {
		t.Errorf("listxml: %q %+v", l.Version, s)
	}
	l, err = Parse(strings.NewReader(machines))
	if err != nil {
		t.Fatal(err)
	}
	if s := l.Sets["mslug"]; l.Version != "0.174" || s == nil || s.Title != "Metal Slug - Super Vehicle-001" || !s.Works {
		t.Errorf("machines: %q %+v", l.Version, s)
	}
}

// MAME 2015's list is the only file of a ZIP; MAME 2003's states no
// version, which the emulator's entry gives.
func TestDownloadMame(t *testing.T) {
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, _ := zw.Create("mame2015.xml")
	w.Write([]byte(machines))
	zw.Close()
	plain := strings.Replace(listxml, ` build="0.139 (Jan  2 2019)"`, "", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadat/mame/MAME 2015 XML.zip":
			w.Write(zipped.Bytes())
		case "/metadat/mame/MAME 2003 XML.xml":
			w.Write([]byte(plain))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := datfile.BaseURL
	datfile.BaseURL = srv.URL + "/"
	defer func() { datfile.BaseURL = old }()
	db, err := Download(context.Background(), nil, []string{"mame2015", "mame2003"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l := db.Lists["mame2015"]; l == nil || l.Sets["mslug"] == nil || l.Version != "0.174" {
		t.Errorf("2015: %+v", l)
	}
	if l := db.Lists["mame2003"]; l == nil || l.Version != "0.78" {
		t.Errorf("2003: %+v", l)
	}
}

// TestSetByName: a set is found by its short name in any case, in the list
// of the first emulator that has it.
func TestSetByName(t *testing.T) {
	db := &DB{Lists: map[string]*List{
		"mame2003": {Sets: map[string]*Set{"sfa3": {Name: "sfa3", Title: "MAME 2003 title"}}},
		"fbneo":    {Sets: map[string]*Set{"sfa3": {Name: "sfa3", Title: "Street Fighter Alpha 3"}}},
	}}
	if s := db.Set("SFA3"); s == nil || s.Title != "Street Fighter Alpha 3" {
		t.Fatalf("got %+v", s)
	}
	if db.Set("nope") != nil || (*DB)(nil).Set("sfa3") != nil {
		t.Fatal("found a set that is not there")
	}
}

func TestSetSystem(t *testing.T) {
	for needs, want := range map[string]string{"": "Arcade", "awbios": "Atomiswave", "naomi": "Naomi", "naomi2": "Naomi 2", "hod2bios": "Naomi", "segasp": "Sega System SP"} {
		s := &Set{}
		if needs != "" {
			s.Needs = []string{needs}
		}
		if got := s.System(); got != want {
			t.Errorf("System with %q = %q, want %q", needs, got, want)
		}
	}
}
