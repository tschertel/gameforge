//go:build !windows

package main

import "strconv"

// onScreen checks the point against the screen; window managers usually
// move windows back into view themselves.
func onScreen(x, y int) bool {
	w, _ := strconv.Atoi(tcl("winfo", "vrootwidth", "."))
	h, _ := strconv.Atoi(tcl("winfo", "vrootheight", "."))
	return x >= 0 && y >= 0 && (w == 0 || x < w) && (h == 0 || y < h)
}
