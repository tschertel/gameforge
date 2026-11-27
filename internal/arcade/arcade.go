// Package arcade reads the lists of arcade sets of emulators such as
// FBNeo (clrmamepro/Logiqx XML datafiles) and checks whether a set, the
// ZIP or 7z an emulator loads by its short name, holds the files the
// emulator expects.
package arcade

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gameforge/internal/cachefile"
	"gameforge/internal/datfile"
)

// FileName is the name of the cache file.
const FileName = "arcade.cache"

// Emulator is an emulator whose list of sets can be downloaded.
type Emulator struct {
	ID      string // kept in the cache, e.g. "fbneo"
	Name    string // shown, e.g. "FBNeo"
	Base    string // folder URL of the list
	File    string // the list's file name
	Size    string // about how much is downloaded, e.g. "14 MB"
	Version string // the MAME version, for lists that do not state it
	Zipped  bool   // the list is the only file of a ZIP
	Detail  string // what it runs, shown in the menu, e.g. "Naomi, Atomiswave"
	Flycast bool   // the list is Flycast's table of games, not a datafile
}

// Label is the emulator's name with what it runs, for the menu.
func (e Emulator) Label() string {
	if e.Detail == "" {
		return e.Name
	}
	return e.Name + " (" + e.Detail + ")"
}

// URL is where the list is downloaded from.
func (e Emulator) URL() string { return e.Base + url.PathEscape(e.File) }

// FBNeoBase is the folder of FBNeo's lists, FlycastBase that of Flycast's
// table of games; tests replace them.
var (
	FBNeoBase   = "https://raw.githubusercontent.com/libretro/FBNeo/master/dats/"
	FlycastBase = "https://raw.githubusercontent.com/flyinghead/flycast/master/core/hw/naomi/"
)

// Emulators are the emulators the app knows: FBNeo, with its own list,
// and the MAME cores of RetroArch, with the lists libretro keeps for them
// (each core follows an older MAME version, whose sets differ).
func Emulators() []Emulator {
	mame := datfile.BaseURL + "metadat/mame/"
	return []Emulator{
		{ID: "fbneo", Name: "FBNeo", Base: FBNeoBase, File: "FinalBurn Neo (ClrMame Pro XML, Arcade only).dat", Size: "14 MB"},
		{ID: "mame2000", Name: "MAME 2000", Base: mame, File: "MAME 2000 XML.dat", Size: "2 MB", Version: "0.37b5"},
		{ID: "mame2003", Name: "MAME 2003", Base: mame, File: "MAME 2003 XML.xml", Size: "19 MB", Version: "0.78"},
		{ID: "mame2003plus", Name: "MAME 2003-Plus", Base: mame, File: "MAME 2003-Plus XML.xml", Size: "20 MB", Version: "0.78+"},
		{ID: "mame2010", Name: "MAME 2010", Base: mame, File: "MAME 2010 XML.xml", Size: "43 MB", Version: "0.139"},
		{ID: "mame2015", Name: "MAME 2015", Base: mame, File: "MAME 2015 XML.zip", Size: "10 MB", Version: "0.160", Zipped: true},
		{ID: "mame2016", Name: "MAME 2016", Base: mame, File: "MAME 2016 XML (Arcade Only).xml", Size: "53 MB", Version: "0.174"},
		{ID: "flycast", Name: "Flycast", Base: FlycastBase, File: "naomi_roms.cpp", Size: "300 KB", Detail: "Naomi, Atomiswave", Flycast: true},
	}
}

// EmulatorByID returns an emulator, or false.
func EmulatorByID(id string) (Emulator, bool) {
	for _, e := range Emulators() {
		if e.ID == id {
			return e, true
		}
	}
	return Emulator{}, false
}

// Rom is a file of a set.
type Rom struct {
	Name   string
	Size   int64
	CRC    uint32
	Merge  string // set: the file comes from the parent or BIOS set
	NoDump bool   // no known dump: never required
}

// Set is a game (or BIOS) of an emulator's list.
type Set struct {
	Name    string // short name, as the ZIP is named: "blazstar"
	Title   string // "Blazing Star"
	Year    string
	Maker   string
	CloneOf string // parent set, for a clone
	RomOf   string // set some files come from: the parent or a BIOS
	Bios    bool
	Works   bool     // the emulator's driver is marked good
	Disks   int      // CHDs it also needs, which are not checked
	Chds    []string // names of the CHDs, found in a folder named after the set
	Needs   []string // BIOS sets the emulator also needs (Flycast), whatever they hold
	Roms    []Rom
}

// List is the list of sets of one emulator.
type List struct {
	Version string
	ETag    string
	Checked time.Time       // when it was last downloaded or found unchanged
	Sets    map[string]*Set // by lowercase short name

	index map[fileKey][]string // own files → sets, built on first use
}

// DB holds the downloaded lists.
type DB struct {
	Downloaded time.Time
	Lists      map[string]*List // by emulator ID
}

// Oldest is when the list checked longest ago was downloaded or found
// unchanged: one list can be fetched while the others are not. Lists saved
// by versions without a date of their own count from the last download.
func (db *DB) Oldest() time.Time {
	var t time.Time
	for _, l := range db.Lists {
		c := l.Checked
		if c.IsZero() {
			c = db.Downloaded
		}
		if t.IsZero() || c.Before(t) {
			t = c
		}
	}
	return t
}

// Has reports whether the list of an emulator is there.
func (db *DB) Has(id string) bool { return db != nil && db.Lists[id] != nil }

// Set finds a set by its short name ("sfa3", any case) in the lists, the
// emulators in the order of Emulators, or nil.
func (db *DB) Set(name string) *Set {
	if db == nil {
		return nil
	}
	name = strings.ToLower(name)
	for _, e := range Emulators() {
		if l := db.Lists[e.ID]; l != nil && l.Sets[name] != nil {
			return l.Sets[name]
		}
	}
	return nil
}

// flycastSystems names the hardware of Flycast's games by the BIOS set
// they need; the others (naomi, f355bios, hod2bios…) are Naomi games.
var flycastSystems = map[string]string{"awbios": "Atomiswave", "naomi2": "Naomi 2", "segasp": "Sega System SP"}

// System is what the set runs on, as the ROMs tab shows it: "Atomiswave",
// "Naomi", "Naomi 2" or "Sega System SP" for Flycast's games, which need
// a BIOS set, and "Arcade" for the others, as the lists do not name the
// board.
func (s *Set) System() string {
	if len(s.Needs) == 0 {
		return "Arcade"
	}
	if sys := flycastSystems[s.Needs[0]]; sys != "" {
		return sys
	}
	return "Naomi"
}

// Load reads the cache file; the error wraps os.ErrNotExist if there is none.
func Load(path string) (*DB, error) {
	db := &DB{}
	if err := cachefile.Load(path, db); err != nil {
		return nil, err
	}
	return db, nil
}

// Save writes the cache file, replacing an old one only once complete.
func (db *DB) Save(path string) error { return cachefile.Save(path, db) }

// Download fetches the lists of the emulators and returns them merged
// with old (which may be nil). Lists that did not change since old are
// not downloaded again.
func Download(ctx context.Context, old *DB, ids []string, progress datfile.Progress) (*DB, error) {
	db := &DB{Lists: map[string]*List{}}
	if old != nil {
		for id, l := range old.Lists {
			db.Lists[id] = l
		}
	}
	for _, id := range ids {
		e, ok := EmulatorByID(id)
		if !ok {
			continue
		}
		etag := ""
		if l := db.Lists[id]; l != nil {
			etag = l.ETag
		}
		var list *List
		newTag, err := datfile.Fetch(ctx, e.URL(), e.File, etag, progress, func(r io.Reader) error {
			if e.Zipped {
				var err error
				if r, err = unzipOne(r); err != nil {
					return err
				}
			}
			var err error
			if e.Flycast {
				list, err = ParseFlycast(r)
			} else {
				list, err = Parse(r)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		if newTag == "" { // not modified
			l := *db.Lists[id]
			l.Checked = time.Now()
			db.Lists[id] = &l
			continue
		}
		list.ETag = newTag
		list.Checked = time.Now()
		if !versionLike(list.Version) {
			list.Version = e.Version
		}
		if list.Version == "" { // Flycast's table: the day it was downloaded
			list.Version = time.Now().Format("2006-01-02")
		}
		db.Lists[id] = list
	}
	db.Downloaded = time.Now()
	return db, nil
}

// unzipOne reads a downloaded ZIP and returns its only (or first) file.
func unzipOne(r io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("empty ZIP")
}

// versionLike reports whether a list's version looks like one ("0.174",
// "1.0.0.03"), and not like "-not specified-".
func versionLike(v string) bool {
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

// xmlGame is a <game> (or <machine>, in MAME's own lists) of a datafile.
type xmlGame struct {
	Name    string `xml:"name,attr"`
	CloneOf string `xml:"cloneof,attr"`
	RomOf   string `xml:"romof,attr"`
	IsBios  string `xml:"isbios,attr"`
	Desc    string `xml:"description"`
	Year    string `xml:"year"`
	Maker   string `xml:"manufacturer"`
	Driver  struct {
		Status string `xml:"status,attr"`
	} `xml:"driver"`
	Disks []struct {
		Name   string `xml:"name,attr"`
		Merge  string `xml:"merge,attr"`
		Status string `xml:"status,attr"`
	} `xml:"disk"`
	Roms []struct {
		Name   string `xml:"name,attr"`
		Size   string `xml:"size,attr"`
		CRC    string `xml:"crc,attr"`
		Merge  string `xml:"merge,attr"`
		Status string `xml:"status,attr"`
	} `xml:"rom"`
}

// Parse reads a clrmamepro (Logiqx) XML datafile.
func Parse(r io.Reader) (*List, error) {
	l := &List{Sets: map[string]*Set{}}
	d := xml.NewDecoder(r)
	d.Strict = false
	inHeader := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "header":
				inHeader = true
			case "mame":
				// MAME's own -listxml: <mame build="0.139 (Jan  2 2019)">.
				for _, a := range t.Attr {
					if a.Name.Local == "build" {
						if f := strings.Fields(a.Value); len(f) > 0 && versionLike(f[0]) {
							l.Version = f[0]
						}
					}
				}
			case "version", "description":
				if inHeader {
					var v string
					if err := d.DecodeElement(&v, &t); err != nil {
						return nil, err
					}
					v = strings.TrimSpace(v)
					if t.Name.Local == "description" {
						// "MAME Arcade 0.174", when the version is not stated
						f := strings.Fields(v)
						if len(f) == 0 || versionLike(l.Version) {
							break
						}
						v = f[len(f)-1]
					}
					if versionLike(v) {
						l.Version = v
					}
				}
			case "game", "machine":
				var g xmlGame
				if err := d.DecodeElement(&g, &t); err != nil {
					return nil, err
				}
				if s := g.set(); s != nil {
					l.Sets[strings.ToLower(s.Name)] = s
				}
			}
		case xml.EndElement:
			if t.Name.Local == "header" {
				inHeader = false
			}
		}
	}
	if len(l.Sets) == 0 {
		return nil, fmt.Errorf("no sets in the list")
	}
	return l, nil
}

func (g xmlGame) set() *Set {
	if g.Name == "" {
		return nil
	}
	s := &Set{
		Name: g.Name, Title: strings.TrimSpace(g.Desc), Year: g.Year, Maker: g.Maker,
		CloneOf: g.CloneOf, RomOf: g.RomOf, Bios: g.IsBios == "yes",
		Works: g.Driver.Status == "" || g.Driver.Status == "good", Disks: len(g.Disks),
	}
	for _, d := range g.Disks {
		if d.Name != "" && d.Status != "nodump" {
			s.Chds = append(s.Chds, d.Name)
		}
	}
	for _, r := range g.Roms {
		rom := Rom{Name: r.Name, Merge: r.Merge, NoDump: r.Status == "nodump"}
		if n, err := strconv.ParseInt(r.Size, 10, 64); err == nil {
			rom.Size = n
		}
		if c, err := strconv.ParseUint(r.CRC, 16, 32); err == nil {
			rom.CRC = uint32(c)
		} else {
			rom.NoDump = true
		}
		s.Roms = append(s.Roms, rom)
	}
	return s
}
