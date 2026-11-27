package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	. "modernc.org/tk9.0"
	"modernc.org/tk9.0/extensions/eval"
)

// Light and dark themes. The light theme is Tk's default look; the dark one
// is a ttk theme derived from "clam", plus option database entries for the
// classic widgets (the log, menus, the lists and canvas of Tk's dialogs).

// Dark colors.
const (
	darkBg      = "#202020"
	darkField   = "#2b2b2b"
	darkButton  = "#323232"
	darkHover   = "#3d3d3d"
	darkPressed = "#484848"
	darkBorder  = "#454545"
	darkFg      = "#e8e8e8"
	darkDim     = "#8a8a8a"
	darkSelect  = "#0067c0" // the accent of the check boxes
)

// darkOptions are the classic widgets' colors in the dark theme, as option
// database patterns.
var darkOptions = map[string]string{
	"*Text.background":          darkField,
	"*Text.foreground":          darkFg,
	"*Text.insertBackground":    darkFg,
	"*Text.selectBackground":    darkSelect,
	"*Text.selectForeground":    "#ffffff",
	"*Menu.background":          darkField,
	"*Menu.foreground":          darkFg,
	"*Menu.activeBackground":    darkSelect,
	"*Menu.activeForeground":    "#ffffff",
	"*Listbox.background":       darkField,
	"*Listbox.foreground":       darkFg,
	"*Listbox.selectBackground": darkSelect,
	"*Listbox.selectForeground": "#ffffff",
	"*Canvas.background":        darkField,
	"*Toplevel.background":      darkBg,
	"*Frame.background":         darkBg,
	"*Label.background":         darkBg,
	"*Label.foreground":         darkFg,
}

// lightLook holds Tk's own look, read at startup to switch back to it.
var lightLook struct {
	theme   string
	root    string
	options map[string]string
}

// initThemes records the light look and defines the dark ttk theme.
func initThemes() {
	lightLook.theme = tcl("ttk::style", "theme", "use")
	lightLook.root = tcl(".", "cget", "-background")
	// The defaults of each classic widget class, read from a probe widget.
	lightLook.options = map[string]string{}
	probes := map[string]string{}
	for pattern := range darkOptions {
		class, option, _ := strings.Cut(strings.TrimPrefix(pattern, "*"), ".")
		w, ok := probes[class]
		if !ok {
			w = ".gameforgeProbe" + class
			tcl(strings.ToLower(class), w)
			probes[class] = w
		}
		lightLook.options[pattern] = tcl(w, "cget", "-"+strings.ToLower(option))
	}
	for _, w := range probes {
		tcl("destroy", w)
	}

	c := map[string]string{
		"$bg": darkBg, "$field": darkField, "$btn": darkButton, "$hover": darkHover, "$pressed": darkPressed,
		"$border": darkBorder, "$fg": darkFg, "$dim": darkDim, "$sel": darkSelect,
	}
	script := `
ttk::style theme create gameforgeDark -parent clam -settings {
	ttk::style configure . -background $bg -foreground $fg -fieldbackground $field \
		-bordercolor $border -lightcolor $bg -darkcolor $bg -troughcolor $field \
		-selectbackground $sel -selectforeground #ffffff -insertcolor $fg -arrowcolor $fg -focuscolor $sel
	ttk::style map . -foreground {disabled $dim} -background {disabled $bg} -fieldbackground {disabled $bg}
	ttk::style configure TButton -background $btn -lightcolor $btn -darkcolor $btn
	ttk::style map TButton -background {disabled $bg pressed $pressed active $hover} \
		-lightcolor {pressed $pressed active $hover} -darkcolor {pressed $pressed active $hover} \
		-bordercolor {focus $sel}
	ttk::style configure TCheckbutton -indicatorbackground $field -indicatorforeground #ffffff \
		-upperbordercolor $border -lowerbordercolor $border
	ttk::style map TCheckbutton -indicatorbackground {disabled $bg selected $sel} -background {active $bg}
	ttk::style configure TRadiobutton -indicatorbackground $field -indicatorforeground #ffffff -bordercolor $dim
	ttk::style map TRadiobutton -indicatorbackground {disabled $bg selected $sel} -background {active $bg}
	ttk::style configure TEntry -lightcolor $field -darkcolor $field
	ttk::style configure TCombobox -background $btn -lightcolor $field -darkcolor $field
	ttk::style map TCombobox -fieldbackground {disabled $bg readonly $field} \
		-foreground {disabled $dim readonly $fg} -background {active $hover} \
		-selectbackground {readonly $field} -selectforeground {readonly $fg}
	ttk::style configure Treeview -background $field -fieldbackground $field -foreground $fg \
		-lightcolor $field -darkcolor $field
	ttk::style map Treeview -background {selected $sel} -foreground {selected #ffffff}
	ttk::style configure Heading -background $btn -foreground $fg -lightcolor $btn -darkcolor $btn
	ttk::style map Heading -background {active $hover}
	ttk::style configure TNotebook -background $bg -bordercolor $border -lightcolor $bg -darkcolor $bg
	ttk::style configure TNotebook.Tab -background $btn -bordercolor $border -lightcolor $btn -darkcolor $btn -padding {14 6}
	ttk::style map TNotebook.Tab -background {selected $bg active $hover} -lightcolor {selected $sel} \
		-padding {selected {14 8}} -expand {selected {2 2 2 0}}
	ttk::style configure TProgressbar -background $sel -lightcolor $sel -darkcolor $sel
	ttk::style configure TScrollbar -background $btn -lightcolor $btn -darkcolor $btn -troughcolor $bg
	ttk::style map TScrollbar -background {disabled $bg active $hover}
	ttk::style configure TSeparator -background $border -lightcolor $border -darkcolor $border -bordercolor $border
	ttk::style configure Sash -background $bg -lightcolor $bg -darkcolor $bg -bordercolor $bg
	# Scroll bars without arrow buttons, as in Windows 11.
	ttk::style layout Vertical.TScrollbar {Vertical.Scrollbar.trough -sticky ns -children {Vertical.Scrollbar.thumb -expand 1 -sticky nswe}}
	ttk::style layout Horizontal.TScrollbar {Horizontal.Scrollbar.trough -sticky ew -children {Horizontal.Scrollbar.thumb -expand 1 -sticky nswe}}
}`
	for k, v := range c {
		script = strings.ReplaceAll(script, k, v)
	}
	if _, err := eval.Eval(script); err != nil {
		panic(fmt.Sprintf("dark theme: %v", err))
	}

	// Check buttons use the same boxes as the file list, drawn dark.
	prev := darkTheme
	darkTheme = true
	on, off, disabled := NewPhoto(Data(checkBoxImage(1))), NewPhoto(Data(checkBoxImage(0))), NewPhoto(Data(checkBoxImage(-1)))
	gray := color.RGBA{0x5a, 0x5a, 0x5a, 0xff}
	disabledOn := NewPhoto(Data(drawCheckBox(checkBoxSize(), gray, gray, true)))
	darkTheme = prev
	// Clam draws separators as a light etched line; a flat line in the
	// border color fits the dark theme.
	line := image.NewRGBA(image.Rect(0, 0, 1, 1))
	line.Set(0, 0, color.RGBA{0x45, 0x45, 0x45, 0xff})
	evalTcl(fmt.Sprintf(`ttk::style theme settings gameforgeDark {
	ttk::style element create Checkbutton.indicator image {%s {disabled selected} %s selected %s disabled %s} -sticky w
	ttk::style element create Separator.separator image %s -sticky nswe -width 1 -height 1
}`, off, disabledOn, on, disabled, NewPhoto(Data(line))))

	// The dark theme is based on clam, whose controls are smaller than those
	// of the native light theme; without this the window layout shifts
	// when switching themes. Measured, since sizes depend on scaling.
	if _, err := eval.Eval(fmt.Sprintf(matchSizesTcl, lightLook.theme)); err != nil {
		panic(fmt.Sprintf("dark theme sizes: %v", err))
	}
}

// matchSizesTcl gives the controls of the dark theme the size they have in
// the light theme (%[1]s): extra padding and the same minimum button width.
const matchSizesTcl = `
apply {{light dark} {
	set current [ttk::style theme use]
	set w .gameforgeSizeProbe
	set text "Abcdefghijklmnop"
	foreach {class make} {
		TButton      {ttk::button $w -text $text}
		TCheckbutton {ttk::checkbutton $w -text $text}
		TEntry       {ttk::entry $w}
		TCombobox    {ttk::combobox $w}
		TMenubutton  {ttk::menubutton $w -text $text}
	} {
		foreach theme [list $light $dark] {
			ttk::style theme use $theme
			eval $make
			update idletasks
			set size($theme) [list [winfo reqwidth $w] [winfo reqheight $w]]
			destroy $w
		}
		lassign $size($light) lw lh
		lassign $size($dark) dw dh
		set x [expr {max(0, $lw - $dw)}]
		set y [expr {max(0, $lh - $dh)}]
		set pad [list [expr {$x / 2}] [expr {$y / 2}] [expr {$x - $x / 2}] [expr {$y - $y / 2}]]
		ttk::style theme settings $dark [list ttk::style configure $class -padding $pad]
	}
	ttk::style theme use $light
	set width [ttk::style lookup TButton -width]
	ttk::style theme settings $dark [list ttk::style configure TButton -width $width -anchor center]
	ttk::style theme use $current
}} %[1]s gameforgeDark
`

// darkMode reports whether the configured theme is dark ("auto" follows the
// system).
func (u *ui) darkMode() bool {
	switch u.cfg.Theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemDark()
}

// applyTheme switches the look; the window is then rebuilt by the caller so
// that the classic widgets pick up the new options.
func applyTheme(dark bool) {
	theme, root, options := lightLook.theme, lightLook.root, lightLook.options
	if dark {
		theme, root, options = "gameforgeDark", darkBg, darkOptions
	}
	tcl("ttk::style", "theme", "use", theme)
	if !dark {
		// Roomier tabs than the system default; the dark theme sets its own.
		// Some themes (clam, Tk's default on many Linux desktops) give the
		// selected tab a padding of its own, which would override this one
		// and make that tab narrower, with its text lower.
		tcl("ttk::style", "configure", "TNotebook.Tab", "-padding", "14 6")
		tcl("ttk::style", "map", "TNotebook.Tab", "-padding", "selected {14 6}")
	}
	tcl(".", "configure", "-background", root)
	for pattern, value := range options {
		tcl("option", "add", pattern, value, "interactive")
	}
	darkTheme = dark
	resetImages()
	setTitleBarDark(dark)
}

// darkTheme is the look in use; images drawn for it depend on it.
var darkTheme bool
