package pkg

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// makeSFO builds a PARAM.SFO with string values.
func makeSFO(kv [][2]string) []byte {
	var keys, data, idx bytes.Buffer
	le := binary.LittleEndian
	for _, p := range kv {
		var e [16]byte
		le.PutUint16(e[0:], uint16(keys.Len()))
		le.PutUint16(e[2:], 0x0204)
		le.PutUint32(e[4:], uint32(len(p[1])+1))
		le.PutUint32(e[8:], uint32(len(p[1])+1))
		le.PutUint32(e[12:], uint32(data.Len()))
		idx.Write(e[:])
		keys.WriteString(p[0] + "\x00")
		data.WriteString(p[1] + "\x00")
	}
	h := make([]byte, 20)
	copy(h, "\x00PSF")
	le.PutUint32(h[4:], 0x101)
	le.PutUint32(h[8:], uint32(20+idx.Len()))
	le.PutUint32(h[12:], uint32(20+idx.Len()+keys.Len()))
	le.PutUint32(h[16:], uint32(len(kv)))
	return append(append(append(h, idx.Bytes()...), keys.Bytes()...), data.Bytes()...)
}

var png = []byte("\x89PNG\r\n\x1a\nfake icon")

func writeTemp(t *testing.T, name string, b []byte) string {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSFO(t *testing.T) {
	s, err := ParseSFO(makeSFO([][2]string{{"TITLE", "Game"}, {"APP_VER", "01.05"}}))
	if err != nil || s.Title() != "Game" || s.Version() != "1.05" {
		t.Fatalf("%v %v", s, err)
	}
}

// makePS4 builds a PS4 PKG with a PARAM.SFO, an icon, an encrypted entry
// and an internal entry.
func makePS4(sfo []byte) []byte {
	be := binary.BigEndian
	type ent struct {
		id, flags uint32
		data      []byte
	}
	ents := []ent{
		{0x0001, 0, []byte("digests")},
		{0x1000, 0, sfo},
		{0x1200, 0, png},
		{0x0400, ps4Encrypted, []byte("secret licence")},
		{0x1260, 0, []byte("<changeinfo/>")},
	}
	const table = 0x1000
	b := make([]byte, table+len(ents)*32)
	copy(b, "\x7fCNT")
	be.PutUint32(b[0x10:], uint32(len(ents)))
	be.PutUint32(b[0x18:], table)
	copy(b[0x40:], "EP9000-CUSA00001_00-ABCDEFGHIJKLMNOP")
	be.PutUint32(b[0x74:], 0x1A)
	for i, e := range ents {
		r := b[table+i*32:]
		be.PutUint32(r[0:], e.id)
		be.PutUint32(r[8:], e.flags)
		be.PutUint32(r[16:], uint32(len(b)))
		be.PutUint32(r[20:], uint32(len(e.data)))
		b = append(b, e.data...)
	}
	return b
}

func TestPS4(t *testing.T) {
	sfo := makeSFO([][2]string{{"TITLE", "Some Game: Deluxe"}, {"APP_VER", "01.05"}, {"CATEGORY", "gp"}})
	path := writeTemp(t, "x.pkg", makePS4(sfo))
	p, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Platform != "PS4" || p.TitleID != "CUSA00001" || p.Kind != KindUpdate || !bytes.Equal(p.Icon, png) {
		t.Fatalf("%+v", p)
	}
	if got, want := p.StandardName(), "Some Game - Deluxe [CUSA00001][Update][v1.05].pkg"; got != want {
		t.Fatalf("name %q, want %q", got, want)
	}
	if p.Files != 3 {
		t.Fatalf("files %d", p.Files)
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := p.Extract(context.Background(), dir, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{
		"sce_sys/param.sfo": sfo, "sce_sys/icon0.png": png,
		"sce_sys/changeinfo/changeinfo.xml": []byte("<changeinfo/>"),
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sce_sys", "license.dat")); err == nil {
		t.Fatal("encrypted entry extracted")
	}
}

type ps3File struct {
	name string
	data []byte // nil: a folder
}

// makePS3 builds a PS3/PSP/Vita PKG encrypted with s.
func makePS3(pkgType uint16, keyID uint32, debug bool, iv []byte, s func(digest []byte) *stream, files []ps3File) []byte {
	be := binary.BigEndian
	// Data area: item table, names, then contents, 16-byte aligned.
	var data []byte
	table := make([]byte, len(files)*32)
	data = append(data, table...)
	align := func() {
		for len(data)%16 != 0 {
			data = append(data, 0)
		}
	}
	for i, f := range files {
		align()
		r := data[i*32:]
		be.PutUint32(r[0:], uint32(len(data)))
		be.PutUint32(r[4:], uint32(len(f.name)))
		data = append(data, f.name...)
		align()
		r = data[i*32:]
		flags := uint32(3)
		if f.data == nil {
			flags = 4
		}
		be.PutUint32(r[24:], flags)
		be.PutUint64(r[8:], uint64(len(data)))
		be.PutUint64(r[16:], uint64(len(f.data)))
		data = append(data, f.data...)
	}
	align()

	h := make([]byte, ps3HeaderSize)
	copy(h, "\x7fPKG")
	if !debug {
		be.PutUint16(h[4:], 0x8000)
	}
	be.PutUint16(h[6:], pkgType)
	meta := []byte{0, 0, 0, 2, 0, 0, 0, 4, 0, 0, 0, 5} // content type 5
	be.PutUint32(h[8:], ps3HeaderSize)
	be.PutUint32(h[12:], 1)
	be.PutUint32(h[20:], uint32(len(files)))
	dataOff := uint64(ps3HeaderSize + 0x40)
	be.PutUint64(h[0x20:], dataOff)
	be.PutUint64(h[0x28:], uint64(len(data)))
	copy(h[0x30:], "UP0001-NPUB30001_00-0000000000000001")
	digest := bytes.Repeat([]byte{0x5a}, 16)
	copy(h[0x60:], digest)
	copy(h[0x70:], iv)
	if pkgType == 2 {
		copy(h[0xC0:], "\x7fext")
		be.PutUint32(h[0xE4:], keyID)
	}
	s(digest).xorAt(data, 0)
	out := append(h, meta...)
	out = append(out, make([]byte, int(dataOff)-len(out))...)
	return append(out, data...)
}

func TestPS3Retail(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 16)
	iv := bytes.Repeat([]byte{0xFF}, 16) // the counter wraps
	sfo := makeSFO([][2]string{{"TITLE", "Homebrew"}, {"APP_VER", "01.00"}, {"CATEGORY", "HG"}})
	big := bytes.Repeat([]byte("0123456789abcdef!"), 100000)
	files := []ps3File{{"USRDIR", nil}, {"PARAM.SFO", sfo}, {"ICON0.PNG", png}, {"USRDIR/EBOOT.BIN", big}}
	b := makePS3(1, 0, false, iv, func([]byte) *stream { return aesStream(key, iv) }, files)
	path := writeTemp(t, "a.pkg", b)

	p, err := Open(path, nil)
	if err != nil || p.NeedKey != "ps3" || p.CanExtract() || p.TitleID != "NPUB30001" {
		t.Fatalf("without key: %+v %v", p, err)
	}
	if _, err := Open(path, NewKeys(map[string][]byte{"ps3": bytes.Repeat([]byte{0x22}, 16)})); err == nil {
		t.Fatal("wrong key accepted")
	}
	p, err = Open(path, NewKeys(map[string][]byte{"ps3": key}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Platform != "PS3" || p.Title() != "Homebrew" || p.Kind != KindGame || !bytes.Equal(p.Icon, png) {
		t.Fatalf("%+v", p)
	}
	if got := p.StandardName(); got != "Homebrew [NPUB30001][v1.00].pkg" {
		t.Fatal(got)
	}
	checkExtract(t, p, files)
}

func TestPS3Debug(t *testing.T) {
	files := []ps3File{{"PARAM.SFO", makeSFO([][2]string{{"TITLE", "Debug"}})}, {"a/b.txt", []byte("hello, world, a few blocks long......")}}
	b := makePS3(1, 0, true, make([]byte, 16), func(d []byte) *stream { return &stream{debug: d} }, files)
	p, err := Open(writeTemp(t, "d.pkg", b), nil)
	if err != nil || !p.Debug || p.Title() != "Debug" {
		t.Fatalf("%+v %v", p, err)
	}
	checkExtract(t, p, files)
}

func TestVita(t *testing.T) {
	vitaKey := bytes.Repeat([]byte{0x33}, 16)
	iv := bytes.Repeat([]byte{0x01}, 16)
	c, _ := aes.NewCipher(vitaKey)
	pkgKey := make([]byte, 16)
	c.Encrypt(pkgKey, iv)
	files := []ps3File{{"sce_sys", nil}, {"sce_sys/param.sfo", makeSFO([][2]string{{"TITLE", "Vita Game"}, {"CATEGORY", "gd"}, {"APP_VER", "01.00"}})}, {"eboot.bin", []byte("eboot")}}
	b := makePS3(2, 2, false, iv, func([]byte) *stream { return aesStream(pkgKey, iv) }, files)
	path := writeTemp(t, "v.pkg", b)
	if p, err := Open(path, nil); err != nil || p.NeedKey != "vita2" || p.Platform != "PS Vita" {
		t.Fatalf("%+v %v", p, err)
	}
	p, err := Open(path, NewKeys(map[string][]byte{"vita2": vitaKey}))
	if err != nil || p.Title() != "Vita Game" || p.Kind != KindGame {
		t.Fatalf("%+v %v", p, err)
	}
	checkExtract(t, p, files)
}

func checkExtract(t *testing.T, p *Package, files []ps3File) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := p.Extract(context.Background(), dir, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		got := filepath.Join(dir, filepath.FromSlash(f.name))
		if f.data == nil {
			if st, err := os.Stat(got); err != nil || !st.IsDir() {
				t.Fatalf("%s: not a folder", f.name)
			}
			continue
		}
		b, err := os.ReadFile(got)
		if err != nil || !bytes.Equal(b, f.data) {
			t.Fatalf("%s: wrong contents (%v)", f.name, err)
		}
	}
}

func TestKeysFile(t *testing.T) {
	path := writeTemp(t, "pkg.keys", []byte("# mine\nps3 = 00112233445566778899aabbccddeeff\nVITA2=ffeeddccbbaa99887766554433221100\n"))
	k, err := LoadKeys(path)
	if err != nil || len(k.Names()) != 2 || k.Get("vita2")[0] != 0xff {
		t.Fatalf("%v %v", k, err)
	}
	if _, err := LoadKeys(writeTemp(t, "bad.keys", []byte("ps3 = 1234\n"))); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestSafePath(t *testing.T) {
	for _, bad := range []string{"../x", "a/../../x", "", "C:/x"} {
		if _, err := safePath("out", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
