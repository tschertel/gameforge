package main

import "modernc.org/tk9.0/extensions/themedetector"

// systemDark reads the macOS preference independently of this window's appearance.
func systemDark() bool {
	preference, err := themedetector.GetPreference()
	return err == nil && preference == themedetector.DarkTheme
}

// setTitleBarDark keeps Aqua widgets and the title bar in the selected appearance.
func setTitleBarDark(dark bool) {
	appearance := "aqua"
	if dark {
		appearance = "darkaqua"
	}
	tcl("wm", "attributes", ".", "-appearance", appearance)
}
