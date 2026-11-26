//go:build !windows && !darwin

package main

// systemDark is false: there is no common way to ask X11 desktops for their
// color scheme, so "auto" means light here.
func systemDark() bool { return false }

// setTitleBarDark does nothing: the window manager draws the title bar.
func setTitleBarDark(bool) {}
