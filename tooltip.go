package main

// Tooltips, drawn by Tcl: shown after a short delay while the pointer rests
// on a widget, in the colors of the current theme (those of entry fields),
// and kept on the screen.
const tooltipTcl = `
namespace eval ::tip {
	variable text
	variable after ""
}
proc ::tip::schedule {w} {
	::tip::hide
	set ::tip::after [after 350 [list ::tip::show $w]]
}
proc ::tip::hide {} {
	after cancel $::tip::after
	catch {destroy .gameforgetip}
}
proc ::tip::show {w} {
	if {![winfo exists $w]} return
	set bg [ttk::style lookup TEntry -fieldbackground]
	set fg [ttk::style lookup TLabel -foreground]
	set border [ttk::style lookup TEntry -bordercolor]
	if {$bg eq ""} {set bg white}
	if {$fg eq ""} {set fg black}
	if {$border eq ""} {set border gray50}
	set t .gameforgetip
	toplevel $t -background $border
	wm overrideredirect $t 1
	catch {wm attributes $t -topmost 1}
	label $t.l -text $::tip::text($w) -justify left -wraplength 110m \
		-background $bg -foreground $fg -padx 3m -pady 2m -borderwidth 0
	pack $t.l -padx 1 -pady 1
	update idletasks
	set x [expr {[winfo rootx $w]}]
	set y [expr {[winfo rooty $w] + [winfo height $w] + 4}]
	set width [winfo reqwidth $t]
	set height [winfo reqheight $t]
	set right [expr {[winfo vrootx $w] + [winfo vrootwidth $w]}]
	set bottom [expr {[winfo vrooty $w] + [winfo vrootheight $w]}]
	if {$x + $width > $right} {set x [expr {$right - $width - 4}]}
	if {$y + $height > $bottom} {set y [expr {[winfo rooty $w] - $height - 4}]}
	wm geometry $t +$x+$y
}
`

func initTooltips() {
	evalTcl(tooltipTcl)
}

// setTooltip shows text while the pointer rests on w.
func setTooltip(w interface{ String() string }, text string) {
	path := w.String()
	tcl("set", "::tip::text("+path+")", text)
	tcl("bind", path, "<Enter>", "::tip::schedule %W")
	tcl("bind", path, "<Leave>", "::tip::hide")
	tcl("bind", path, "<ButtonPress>", "::tip::hide")
	tcl("bind", path, "<Destroy>", "+::tip::hide")
}
