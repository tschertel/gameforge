package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	. "modernc.org/tk9.0"

	"gameforge/internal/chd"
	"gameforge/internal/ciso"
	"gameforge/internal/discinfo"
	"gameforge/internal/playlist"
	"gameforge/internal/xiso"
)

// isoTarget is what the Discs tab converts ISO images to: "" for CHD,
// "cso" or "zso", from the tab's option.
var isoTarget = ""

// chdConverter compresses CD images (CUE + BIN) and ISO images to CHD, and
// ISO images to CSO or ZSO if chosen. An ISO becomes a CD CHD for CD games
// and a DVD CHD otherwise; the choice can be changed per file. CHD, CSO and
// ZSO files are listed for
// what they are, and can be extracted back. Xbox and Xbox 360 disc dumps
// are rebuilt as XISO instead.
var chdConverter = &converter{
	id:     "chd",
	title:  "tab.chd",
	info:   "tab.chd.info",
	button: "btn.convert",
	tip:    "btn.chd.tip",
	exts:   []string{".cue", ".gdi", ".iso", ".chd", ".cso", ".zso", ".gcm", ".rvz", ".nrg", ".cdi", ".ecm", ".zip", ".7z", ".wbfs"},
	accept: func(path string) bool {
		switch {
		case hasExt(path, []string{".gcm"}):
			return isNintendoDisc(path)
		case hasExt(path, []string{".ecm"}):
			return ecmListed(path)
		case isArchive(path):
			inner, _, _ := archiveDisc(path)
			return inner != ""
		}
		return true
	},
	types: []FileType{
		{TypeName: "CUE/GDI/ISO/CHD/CSO/ZSO/GCM/RVZ/WBFS/NRG/CDI/ECM/ZIP/7Z", Extensions: []string{".cue", ".gdi", ".iso", ".chd", ".cso", ".zso", ".gcm", ".rvz", ".wbfs", ".nrg", ".cdi", ".ecm", ".zip", ".7z"}},
		{TypeName: "CUE (CD)", Extensions: []string{".cue"}},
		{TypeName: "GDI (Dreamcast)", Extensions: []string{".gdi"}},
		{TypeName: "NRG (Nero)", Extensions: []string{".nrg"}},
		{TypeName: "CDI (DiscJuggler)", Extensions: []string{".cdi"}},
		{TypeName: "ECM", Extensions: []string{".ecm"}},
		{TypeName: "ZIP/7Z", Extensions: []string{".zip", ".7z"}},
		{TypeName: "ISO", Extensions: []string{".iso"}},
		{TypeName: "CHD", Extensions: []string{".chd"}},
		{TypeName: "CSO/ZSO", Extensions: []string{".cso", ".zso"}},
		{TypeName: "GameCube/Wii ISO", Extensions: []string{".iso", ".gcm"}},
		{TypeName: "RVZ", Extensions: []string{".rvz"}},
		{TypeName: "WBFS (Wii)", Extensions: []string{".wbfs"}},
	},
	describe: describeDisc,
	outName: func(it *convItem) string {
		if it.kind == "archive" {
			return archiveOutName(it)
		}
		if it.kind == "xiso" {
			return xisoName(it.path)
		}
		if it.kind == "rvz" {
			return strings.TrimSuffix(filepath.Base(it.path), filepath.Ext(it.path)) + ".rvz"
		}
		ext := ".chd"
		if isISO(it.path) && isoTarget != "" {
			ext = "." + isoTarget
		}
		return discStem(it.path) + ext
	},
	run: convertDisc,
	extra: func(u *ui, parent *TFrameWidget) []Widget {
		check := parent.TButton(Txt(T("btn.check_chd")), Command(func() {
			u.startCheck(u.openFiles([]FileType{{TypeName: "CHD", Extensions: []string{".chd"}}}))
		}))
		rename := parent.TButton(Txt(T("btn.rename")), Command(func() {
			for _, t := range u.tabs {
				if t.conv.id == "chd" {
					t.renameDiscs(u)
				}
			}
		}))
		setTooltip(rename, T("drename.tip"))
		raBtn := parent.TButton(Txt(T("btn.ra")), Command(func() {
			for _, t := range u.tabs {
				if t.conv.id == "chd" {
					t.checkRADiscs(u)
				}
			}
		}))
		setTooltip(raBtn, T("ra.tip_discs"))
		dbBtn := parent.TMenubutton(Txt(T("redump.menu")))
		menu := dbBtn.Menu(Tearoff(false))
		menu.AddCommand(Lbl(T("redump.update")), Command(u.updateRedump))
		menu.AddCommand(Lbl(T("ra.update")), Command(u.updateRA))
		menu.AddSeparator()
		menu.AddCommand(Lbl(T("ra.key_menu")), Command(func() {
			if !u.busy {
				u.askRAKey()
			}
		}))
		u.addRAReportItem(menu)
		dbBtn.Configure(Mnu(menu))
		discDB.status = parent.TLabel(Wraplength("45m"), Justify("left"))
		setTooltip(dbBtn, T("redump.menu.tip"))
		setTooltip(discDB.status, T("redump.menu.tip"))
		u.showRedumpStatus()
		return []Widget{check, rename, raBtn, parent.TSeparator(Orient("vertical")), dbBtn, discDB.status}
	},
	back: &convAction{
		button: "btn.extract_chd",
		tip:    "btn.extract_chd.tip",
		accept: func(it *convItem) bool { return (isCHD(it.path) || isCSO(it.path)) && it.kind != "" },
		outName: func(it *convItem) string {
			return strings.TrimSuffix(filepath.Base(it.path), filepath.Ext(it.path)) + "." + extractExt(it)
		},
		run: func(ctx context.Context, u *ui, it *convItem, out string, verify bool,
			progress func(bool, int64, int64)) (int64, int64, error) {
			if isCSO(it.path) {
				return extractCSO(ctx, u, it.path, out, verify, progress)
			}
			return extractCHD(ctx, u, it, out, verify, progress)
		},
	},
	options: func(u *ui, parent *TFrameWidget) []Widget {
		v := Variable(tclBool(!u.cfg.ChdNoM3U))
		m3u := parent.TCheckbutton(Txt(T("opt.m3u")), v, Command(func() {
			u.cfg.ChdNoM3U = v.Get() != "1"
			u.saveConfig()
		}))
		setTooltip(m3u, T("opt.m3u.tip"))
		isoTarget = u.cfg.IsoFormat
		iv := Variable(map[string]string{"": "chd", "cso": "cso", "zso": "zso"}[isoTarget])
		set := func() {
			isoTarget = strings.TrimPrefix(iv.Get(), "chd")
			u.cfg.IsoFormat = isoTarget
			u.saveConfig()
			for _, t := range u.tabs {
				if t.conv.id == "chd" {
					for _, it := range t.items {
						if isISO(it.path) && it.kind != "xiso" && it.kind != "rvz" {
							describeDisc(it)
						}
					}
					t.refresh(u)
				}
			}
		}
		label := parent.TLabel(Txt(T("opt.ciso_format")))
		toCHD := parent.TRadiobutton(Txt("CHD"), iv, Value("chd"), Command(set))
		cso := parent.TRadiobutton(Txt(T("opt.ciso_cso")), iv, Value("cso"), Command(set))
		zso := parent.TRadiobutton(Txt(T("opt.ciso_zso")), iv, Value("zso"), Command(set))
		setTooltip(toCHD, T("opt.iso_chd.tip"))
		setTooltip(cso, T("opt.ciso_cso.tip"))
		setTooltip(zso, T("opt.ciso_zso.tip"))
		return []Widget{m3u, label, toCHD, cso, zso}
	},
	options2: rvzOptions,
	menu: func(u *ui, t *convTab, it *convItem) []menuEntry {
		if isCHD(it.path) {
			entries := []menuEntry{{T("btn.check_chd"), false, func() { u.startCheck([]string{it.path}) }}}
			if choices := extractChoices(it.path); len(choices) > 1 {
				set := func(mode string) func() {
					return func() {
						it.mode = mode
						describeDisc(it)
						t.refresh(u)
					}
				}
				entries = append(entries, menuEntry{T("chd.menu_extract_auto", extractFormat(it.kind)), it.mode == "", set("")})
				for _, c := range choices {
					entries = append(entries, menuEntry{T("chd.menu_extract_" + c), it.mode == c, set(c)})
				}
			}
			return entries
		}
		if !isISO(it.path) || !it.ok || isoTarget != "" || it.kind == "xiso" || it.kind == "rvz" {
			return nil
		}
		_, autoCD, _ := discinfo.FromISO(it.path)
		auto := "DVD"
		if autoCD {
			auto = "CD"
		}
		set := func(mode string) func() {
			return func() {
				it.mode = mode
				describeDisc(it)
				t.refresh(u)
			}
		}
		return []menuEntry{
			{T("chd.menu_auto", auto), it.mode == "", set("")},
			{T("chd.menu_cd"), it.mode == "cd", set("cd")},
			{T("chd.menu_dvd"), it.mode == "dvd", set("dvd")},
		}
	},
	after: func(u *ui, done []converted) {
		if u.cfg.ChdNoM3U {
			return
		}
		seen := map[string]bool{}
		for _, d := range done {
			if !isCHD(d.out) {
				continue
			}
			game, _, ok := playlist.Disc(filepath.Base(d.out))
			key := strings.ToLower(filepath.Join(filepath.Dir(d.out), game))
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			path, discs, err := playlist.Update(d.out)
			switch {
			case err != nil:
				u.logf("log.merge_error", err)
			case path != "":
				u.logf("log.m3u", filepath.Base(path), len(discs))
			}
		}
	},
}

// extractExt is the extension, without the dot, of the image a CHD is
// extracted to: the one chosen, else the disc's own.
func extractExt(it *convItem) string {
	if it.mode != "" {
		return it.mode
	}
	return it.kind
}

// extractFormat names the files of an extension for the user.
func extractFormat(ext string) string {
	switch ext {
	case "cue":
		return "CUE + BIN"
	case "gdi":
		return "GDI"
	}
	return strings.ToUpper(ext)
}

// extractChoices are the formats a CHD can be extracted to.
func extractChoices(path string) []string {
	d, err := chd.OpenDisc(path)
	if err != nil {
		return nil
	}
	defer d.Close()
	switch {
	case d.DVD:
		return []string{"iso"}
	case d.GDROM:
		return []string{"gdi"}
	case d.CanISO():
		return []string{"cue", "iso"}
	}
	return []string{"cue"}
}

// extractCHD writes a CHD's disc image back as a CUE sheet and BIN files,
// a .gdi and its tracks, or an ISO.
func extractCHD(ctx context.Context, u *ui, it *convItem, out string, verify bool,
	progress func(bool, int64, int64)) (int64, int64, error) {
	res, err := chd.Extract(ctx, it.path, out, chd.ExtractOptions{
		Progress:       func(done, total int64) { progress(false, done, total) },
		Verify:         verify,
		VerifyProgress: func(done, total int64) { progress(true, done, total) },
	})
	var exists *chd.ExistsError
	if errors.As(err, &exists) {
		return 0, 0, errors.New(T("chd.extract_exists", exists.Name))
	}
	if err != nil {
		return 0, 0, err
	}
	switch ext := extractExt(it); {
	case ext == "cue":
		u.logf("chd.extracted_cue", res.Tracks)
	case ext == "gdi":
		u.logf("chd.extracted_gdi", res.Tracks)
	case it.kind != "iso":
		u.logf("chd.extracted_iso_data")
	default:
		u.logf("chd.extracted_iso")
	}
	return res.InputSize, res.OutputSize, nil
}

func isISO(path string) bool { return strings.EqualFold(filepath.Ext(path), ".iso") }
func isGDI(path string) bool { return strings.EqualFold(filepath.Ext(path), ".gdi") }
func isCHD(path string) bool { return strings.EqualFold(filepath.Ext(path), ".chd") }
func isCSO(path string) bool { return hasExt(path, []string{".cso", ".zso"}) }

// compressCSO compresses an ISO image to CSO or ZSO, by out's extension.
func compressCSO(ctx context.Context, in, out string, verify bool,
	progress func(bool, int64, int64)) (int64, int64, error) {
	format := ciso.CSO
	if strings.EqualFold(filepath.Ext(out), ".zso") {
		format = ciso.ZSO
	}
	res, err := ciso.Compress(ctx, in, out, format, func(done, total int64) { progress(false, done, total) })
	if err != nil {
		return 0, 0, err
	}
	if verify {
		sum, err := ciso.Hash(ctx, out, func(done, total int64) { progress(true, done, total) })
		if err == nil && sum != res.SHA1 {
			err = errors.New("content differs")
		}
		if err != nil {
			if ctx.Err() == nil {
				err = fmt.Errorf("%s: %w", T("log.verify_failed"), err)
			}
			return 0, 0, err
		}
	}
	return res.InputSize, res.OutputSize, nil
}

// extractCSO writes the ISO image of a CSO or ZSO.
func extractCSO(ctx context.Context, u *ui, in, out string, verify bool,
	progress func(bool, int64, int64)) (int64, int64, error) {
	res, err := ciso.Decompress(ctx, in, out, func(done, total int64) { progress(false, done, total) })
	if err != nil {
		return 0, 0, err
	}
	if verify {
		sum, err := ciso.HashFile(ctx, out, func(done, total int64) { progress(true, done, total) })
		if err == nil && sum != res.SHA1 {
			err = errors.New("content differs")
		}
		if err != nil {
			if ctx.Err() == nil {
				err = fmt.Errorf("%s: %w", T("log.verify_failed"), err)
			}
			return 0, 0, err
		}
	}
	u.logf("chd.extracted_iso")
	return res.InputSize, res.OutputSize, nil
}

// describeCSO tells what a CSO or ZSO holds; it is listed, not converted.
func describeCSO(it *convItem) {
	it.infoOnly, it.kind = true, "iso"
	st, err := os.Stat(it.path)
	if err != nil {
		it.detail = lazyf("⚠ %v", err)
		return
	}
	r, err := ciso.Open(it.path)
	if err != nil {
		it.detail = lazyf("⚠ %v", err)
		return
	}
	format, iso := r.Header.Format.String(), humanSize(r.Header.Size)
	r.Close()
	info, _ := discinfo.InfoFromCSO(it.path)
	it.platform, it.icon = info.Platform, info.Icon
	size := humanSize(st.Size())
	it.detail = withGame(info, msg("chd.row_cso", format, size, iso))
}

// withGame puts what the disc says about the game before a detail.
func withGame(info discinfo.Info, detail fmt.Stringer) fmt.Stringer {
	return lazyLine(func() string {
		if g := gameText(info); g != "" {
			return g + " — " + stringOf(detail)
		}
		return stringOf(detail)
	})
}

// gameText is the title, product code, version, regions and disc number
// of a game, as far as the disc tells them, in the current language.
func gameText(info discinfo.Info) string {
	var parts []string
	add := func(s string) {
		if s != "" {
			parts = append(parts, s)
		}
	}
	add(info.Title)
	add(info.Serial)
	if info.Version != "" {
		add("v" + info.Version)
	}
	var regions []string
	for _, r := range info.Regions {
		regions = append(regions, T("region."+r))
	}
	add(strings.Join(regions, ", "))
	switch {
	case info.Discs > 1:
		add(T("info.disc", info.Disc, info.Discs))
	case info.Disc > 1:
		add(T("info.disc1", info.Disc))
	}
	if info.Firmware != "" {
		add(T("file.firmware", info.Firmware))
	}
	return strings.Join(parts, " · ")
}

// describeCHD tells what a CHD holds; it is listed, not converted.
func describeCHD(it *convItem) {
	it.infoOnly = true
	st, err := os.Stat(it.path)
	if err != nil {
		it.detail = lazyf("⚠ %v", err)
		return
	}
	d, err := chd.OpenDisc(it.path)
	if err != nil {
		it.detail = lazyf("⚠ %v", err)
		return
	}
	it.kind = strings.TrimPrefix(d.DefaultExt(), ".")
	var kind fmt.Stringer
	switch {
	case d.DVD:
		kind = msg("chd.kind_dvd")
	case d.GDROM:
		kind = msg("chd.kind_gd", len(d.Tracks))
	default:
		kind = msg("chd.kind_cd", len(d.Tracks))
	}
	d.Close()
	info, _ := discinfo.InfoFromCHD(it.path)
	it.platform, it.icon = info.Platform, info.Icon
	if it.platform == discinfo.Dreamcast {
		it.icon = dreamcastIcon(it.path)
	}
	size, mode := humanSize(st.Size()), it.mode
	it.detail = withGame(info, lazyLine(func() string {
		s := T("chd.row_chd", stringOf(kind), size)
		if mode != "" {
			s += " " + T("chd.extract_chosen", extractFormat(mode))
		}
		return s
	}))
}

// describeDisc tells what a disc image is: its platform, and for an ISO

// convertDisc converts an image of the Discs tab: see chdConverter.
func convertDisc(ctx context.Context, u *ui, it *convItem, out string, verify bool,
	progress func(bool, int64, int64)) (int64, int64, error) {
	if it.kind == "archive" {
		return convertDiscArchive(ctx, u, it, out, verify, progress)
	}
	if it.kind == "xiso" {
		return rebuildXISO(ctx, u, it, out, verify, progress)
	}
	if it.kind == "rvz" {
		return compressRVZ(ctx, u, it, out, verify, progress)
	}
	if isISO(it.path) && isoTarget != "" {
		return compressCSO(ctx, it.path, out, verify, progress)
	}
	opt := chd.Options{Progress: func(done, total int64) { progress(false, done, total) }}
	var res *chd.Result
	var err error
	switch {
	case isGDI(it.path):
		if res, err = chd.ConvertGDI(ctx, it.path, out, opt); err == nil {
			u.logf("chd.gd", res.Tracks)
		}
	case !isISO(it.path):
		if res, err = chd.ConvertImage(ctx, it.path, out, opt); err == nil {
			if it.kind == "gd" {
				u.logf("chd.gd", res.Tracks)
			} else {
				u.logf("chd.cd", res.Tracks)
			}
		}
	case it.kind == "cd":
		if res, err = chd.ConvertISOAsCD(ctx, it.path, out, opt); err == nil {
			u.logf("chd.iso_cd")
		}
	default:
		if res, err = chd.ConvertISO(ctx, it.path, out, opt); err == nil {
			u.logf("chd.dvd")
		}
	}
	if err != nil {
		return 0, 0, err
	}
	if verify {
		if _, err := chd.Verify(ctx, out, func(done, total int64) { progress(true, done, total) }); err != nil {
			if ctx.Err() == nil {
				err = fmt.Errorf("%s: %w", T("log.verify_failed"), err)
			}
			return 0, 0, err
		}
	}
	return res.InputSize, res.OutputSize, nil
}

// whether it becomes a CD or a DVD CHD.
func describeDisc(it *convItem) {
	it.ok, it.platform, it.kind = false, "", ""
	if isCHD(it.path) {
		describeCHD(it)
		return
	}
	if isCSO(it.path) {
		describeCSO(it)
		return
	}
	if isRVZ(it.path) {
		describeRVZ(it)
		return
	}
	if isWBFS(it.path) {
		describeWBFS(it)
		return
	}
	if isISO(it.path) && xiso.IsXbox(it.path) {
		describeXbox(it)
		return
	}
	if gc, wii := nintendoDisc(it.path); gc || wii {
		describeNintendo(it, gc, wii)
		return
	}
	if isArchive(it.path) {
		describeDiscArchive(it)
		return
	}
	if !isISO(it.path) {
		tracks, err := chd.ParseImage(it.path)
		if err != nil {
			it.detail = lazyf("⚠ %v", err)
			return
		}
		var size int64
		seen := map[string]bool{}
		for _, t := range tracks {
			if !seen[t.File] {
				seen[t.File] = true
				size += chd.ImageFileSize(t.File)
			}
		}
		it.size = size
		info, _ := discinfo.InfoFromCue(it.path)
		if chd.IsGDROM(tracks) {
			it.ok, it.kind, it.platform = true, "gd", discinfo.Dreamcast
			if isGDI(it.path) {
				it.icon = dreamcastIcon(it.path)
			}
			it.detail = withGame(info, msg("chd.row_gd", len(tracks), humanSize(size)))
			return
		}
		it.platform = info.Platform
		it.ok, it.kind, it.detail = true, "cd", withGame(info, msg("chd.row_cd", len(tracks), humanSize(size)))
		return
	}
	st, err := os.Stat(it.path)
	switch {
	case err != nil:
		it.detail = lazyf("⚠ %v", err)
		return
	case st.Size() == 0 || st.Size()%chd.DVDSectorSize != 0:
		it.detail = msg("chd.row_bad_iso")
		return
	}
	platform, autoCD, _ := discinfo.FromISO(it.path)
	it.ok, it.platform = true, platform
	it.kind = map[bool]string{true: "cd", false: "dvd"}[autoCD]
	if it.mode != "" {
		it.kind = it.mode
	}
	size, kind, chosen := humanSize(st.Size()), strings.ToUpper(it.kind), it.mode != ""
	info, _ := discinfo.InfoFromISO(it.path)
	it.icon = info.Icon
	if isoTarget != "" {
		target := strings.ToUpper(isoTarget)
		it.detail = withGame(info, lazyLine(func() string { return T("ciso.row_iso", size, target) }))
		return
	}
	it.detail = withGame(info, lazyLine(func() string {
		how := T("chd.auto")
		if chosen {
			how = T("chd.chosen")
		}
		return T("chd.row_iso", size, kind, how)
	}))
}

// openFiles asks for several files, starting in and remembering the last
// folder used.
func (u *ui) openFiles(types []FileType) []string {
	opts := []string{"-parent", ".", "-title", T("app.title"), "-filetypes", fileTypes(types), "-multiple", "1"}
	if u.cfg.LastDir != "" {
		opts = append(opts, "-initialdir", u.cfg.LastDir)
	}
	cmd := "tk_getOpenFile"
	if !nativeDialogs() {
		cmd = "::tk::dialog::file::"
		opts = append([]string{"open"}, opts...)
		tcl("auto_load", cmd)
		patchDialogs()
	}
	u.prepareDialog()
	list := tcl(append([]string{cmd}, opts...)...)
	u.rememberHidden()
	n, _ := strconv.Atoi(tcl("llength", list))
	var paths []string
	for i := range n {
		paths = append(paths, filepath.FromSlash(tcl("lindex", list, strconv.Itoa(i))))
	}
	if len(paths) > 0 {
		u.cfg.LastDir = filepath.Dir(paths[0])
		u.saveConfig()
	}
	return paths
}
