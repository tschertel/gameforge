// Package config stores the user's options in a JSON file next to the
// program, creating it with defaults when it does not exist.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the name of the configuration file.
const FileName = "gameforge.json"

// Names used before the app was renamed GameForge; their configuration is
// taken over on the first run.
const (
	oldFileName = "nspmerge.json"
	oldDirName  = "nspmerge"
	dirName     = "gameforge"
)

// Config holds the persisted options.
type Config struct {
	// Language is a language code ("en", "pt", ...) or "auto" to follow the system.
	Language string `json:"language"`
	// Theme is "light", "dark" or "auto" to follow the system.
	Theme     string `json:"theme"`
	KeysPath  string `json:"keys_path"`
	OutputDir string `json:"output_dir"` // older versions: one for all tabs
	// OutputDirs holds the output folder of each tab ("" = next to each
	// file); it follows the folder last opened in the tab.
	OutputDirs map[string]string `json:"output_dirs,omitempty"`
	LastDir    string            `json:"last_dir"`
	// Format is "nsz" (compressed, default) or "nsp".
	Format string `json:"format"`
	// Level is the NSZ compression level: "fast", "normal" (default) or "max".
	Level      string `json:"level"`
	SkipVerify bool   `json:"skip_verify"`
	// Subfolders makes opened and dropped folders add the files in their
	// subfolders too; by default only the folder's own files are added.
	Subfolders bool `json:"subfolders,omitempty"`
	// ApplyUpdate builds one patched base game (for emulators) instead of
	// keeping base, update and DLC as separate titles.
	ApplyUpdate bool `json:"apply_update"`
	// ChdNoM3U turns off the .m3u playlists written for games on several
	// discs converted to CHD.
	ChdNoM3U bool `json:"chd_no_m3u,omitempty"`
	// IsoFormat is what the Discs tab converts ISO images to: "" (CHD),
	// "cso" or "zso".
	IsoFormat string `json:"iso_format,omitempty"`
	// CisoFormat was the format of the ISO → CSO/ZSO tab, before it became
	// part of the Discs tab; it is only read, to carry it over.
	CisoFormat string `json:"ciso_format,omitempty"`
	// WiiKeyPath is a Wii common key file chosen by the user (the keys
	// folder is also searched).
	WiiKeyPath string `json:"wii_key_path,omitempty"`
	// RvzChunk is the RVZ block size in KiB (0: 128).
	RvzChunk int `json:"rvz_chunk_kib,omitempty"`
	// SevenZipDelete removes the original files after their 7z archive is
	// written and verified.
	SevenZipDelete bool `json:"sevenzip_delete,omitempty"`
	// SevenZipJobs is how many files the 7z tab compresses at a time (0: 1).
	SevenZipJobs int `json:"sevenzip_jobs,omitempty"`
	// SaveLog writes the log of each run to a file in the logs folder.
	SaveLog bool `json:"save_log,omitempty"`
	// ROMArchive is what the ROMs tab writes: "zip" for ZIP, else 7z.
	ROMArchive string `json:"rom_archive,omitempty"`
	// DupesKeep is the order of the extensions whose file is kept of the
	// duplicates of a game in the ROMs tab, such as "7z, neo, zip" (the
	// default when empty).
	DupesKeep string `json:"dupes_keep,omitempty"`
	// RegionOrder is the order of regions whose version of a game is kept
	// by 1G1R in the ROMs tab, such as "USA, Europe, Japan" (the default
	// when empty).
	RegionOrder string `json:"region_order,omitempty"`
	// UpdateCheck turns on the daily check for a new version on GitHub;
	// nil until the user has been asked.
	UpdateCheck *bool `json:"update_check,omitempty"`
	// UpdateEvery is how often the check runs: "daily" (the default when
	// empty), "weekly" or "monthly".
	UpdateEvery string `json:"update_every,omitempty"`
	// ListsCheck turns on the question, at startup, whether to update the
	// downloaded lists that have grown old; nil until the user has been
	// asked.
	ListsCheck *bool `json:"lists_check,omitempty"`
	// ListsChecked is when the app last asked whether to update the
	// downloaded lists that had grown old.
	ListsChecked time.Time `json:"lists_checked,omitempty"`
	// UpdateChecked is when the last check was tried; UpdateLatest and
	// UpdatePage are the newest version found and its web page.
	UpdateChecked time.Time `json:"update_checked,omitempty"`
	UpdateLatest  string    `json:"update_latest,omitempty"`
	UpdatePage    string    `json:"update_page,omitempty"`
	// ShowHidden shows hidden files and folders (names starting with a
	// dot) in Tk's file dialogs; they are hidden by default.
	ShowHidden bool `json:"show_hidden,omitempty"`
	// RAKey is the user's RetroAchievements web API key, used to check
	// games against RetroAchievements; it stays in this file.
	RAKey string `json:"retroachievements_key,omitempty"`
	// RAReport saves an HTML report of each RetroAchievements check (off
	// by default).
	RAReport bool `json:"ra_report,omitempty"`

	// Window layout of the last run: geometry "WxH+X+Y" of the normal
	// (not maximized) window, whether it was maximized, the widths of the
	// list's columns and the position of the divider above the log.
	Window    string `json:"window,omitempty"`
	Maximized bool   `json:"maximized,omitempty"`
	Columns   []int  `json:"columns,omitempty"`
	Divider   int    `json:"divider,omitempty"`
	// Tab is the tab shown last: "switch" or a converter id ("chd", "7z").
	Tab string `json:"tab,omitempty"`

	path string
}

// AppDir returns the program's folder. Under "go run" the executable lives
// in a temporary build folder, so the working directory is used instead.
func AppDir() string {
	exe, err := os.Executable()
	if err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			dir := filepath.Dir(exe)
			if !strings.Contains(dir, "go-build") {
				return dir
			}
		}
	}
	wd, _ := os.Getwd()
	return wd
}

// Load reads the configuration from the program folder, creating the file
// with defaults when missing. If the program folder is not writable (e.g.
// installed under /usr/bin), the user's config folder is used instead.
func Load() (*Config, error) {
	primary := filepath.Join(AppDir(), FileName)
	migrate(filepath.Join(AppDir(), oldFileName), primary)
	c, err := loadOrCreate(primary)
	if err == nil || c != nil {
		return c, err
	}
	if dir, uerr := os.UserConfigDir(); uerr == nil {
		user := filepath.Join(dir, dirName, FileName)
		migrate(filepath.Join(dir, oldDirName, oldFileName), user)
		if c, ferr := loadOrCreate(user); c != nil {
			return c, ferr
		}
	}
	// Keep running with defaults; saving will report the error again.
	d := &Config{path: primary}
	d.fillDefaults()
	return d, err
}

// migrate renames the configuration file of an older version to its new
// name, if there is no file with the new name yet. Failures are ignored:
// the defaults are used then.
func migrate(old, path string) {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.Rename(old, path)
}

// fillDefaults sets options missing from the file (e.g. written by an
// older version) to their defaults.
func (c *Config) fillDefaults() {
	if c.Language == "" {
		c.Language = "auto"
	}
	switch c.Theme {
	case "light", "dark":
	default:
		c.Theme = "auto"
	}
	if c.Format != "nsp" {
		c.Format = "nsz"
	}
	switch c.Level {
	case "fast", "normal", "max":
	default:
		c.Level = "normal"
	}
	// The Xbox → XISO and GameCube/Wii → RVZ tabs became part of the Discs
	// tab: if one of them was the tab shown, the Discs tab is shown
	// instead, with its output folder.
	for _, old := range []string{"xiso", "rvz"} {
		if c.Tab == old {
			c.Tab = "chd"
			if d, ok := c.OutputDirs[old]; ok {
				if _, has := c.OutputDirs["chd"]; !has {
					c.OutputDirs["chd"] = d
				}
			}
		}
		delete(c.OutputDirs, old)
	}
	// The ISO → CSO/ZSO tab became part of the Discs tab: if it was the
	// tab shown, the Discs tab is shown instead and converts ISOs to its
	// format, into its output folder.
	if c.Tab == "ciso" {
		c.Tab = "chd"
		if c.IsoFormat == "" {
			c.IsoFormat = "cso"
			if c.CisoFormat == "zso" {
				c.IsoFormat = "zso"
			}
		}
		if d, ok := c.OutputDirs["ciso"]; ok {
			if _, has := c.OutputDirs["chd"]; !has {
				c.OutputDirs["chd"] = d
			}
		}
	}
	delete(c.OutputDirs, "ciso")
	c.CisoFormat = ""
	if c.IsoFormat != "cso" && c.IsoFormat != "zso" {
		c.IsoFormat = ""
	}
}

func loadOrCreate(path string) (*Config, error) {
	// Temporary files of saves that were cut off (see Save).
	if left, _ := filepath.Glob(path + ".*.tmp"); len(left) > 0 {
		for _, f := range left {
			os.Remove(f)
		}
	}
	c := &Config{path: path}
	c.fillDefaults()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := c.Save(); err != nil {
			return nil, err // not writable: let Load try the fallback
		}
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		// Readable but invalid: keep it as .bad, since the next save writes
		// the defaults, and use the defaults here.
		os.Rename(path, path+".bad")
		d := &Config{path: path}
		d.fillDefaults()
		return d, err
	}
	c.fillDefaults()
	return c, nil
}

// Path returns the location of the configuration file.
func (c *Config) Path() string { return c.path }

// Save writes the configuration file.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Written to a temporary file (readable by the user only, as it may
	// hold the RetroAchievements key) that then replaces the old one, so an
	// interrupted save leaves the old file, not half a new one.
	f, err := os.CreateTemp(filepath.Dir(c.path), filepath.Base(c.path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), c.path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
