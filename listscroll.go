package main

import (
	. "modernc.org/tk9.0"
)

// The file lists scroll sideways: each column is kept at least as wide as
// its longest text (its minimum width), so that a long name or detail is
// never cut off; when the columns together are wider than the list, a
// horizontal scroll bar shows the rest.

// listProcs measures the rows of a list in Tcl, where it is fast even for
// long lists. fitColumns sets every column's minimum width to its widest
// cell (in the #0 column with the indent and the row's image);
// growColumn widens one column for a new text, if needed.
const listProcs = `
namespace eval ::gameforge {}
proc ::gameforge::fonts {} {
	set f [ttk::style lookup Treeview -font]
	if {$f eq ""} {set f TkDefaultFont}
	set h [ttk::style lookup Heading -font]
	if {$h eq ""} {set h TkHeadingFont}
	return [list $f $h]
}
proc ::gameforge::fitColumns {tv} {
	lassign [::gameforge::fonts] font heading
	set indent [ttk::style lookup Treeview -indent]
	if {$indent eq ""} {set indent 20}
	set indent [winfo pixels $tv $indent] ;# Tk 9 may give "15p"
	set pad [expr {[winfo pixels $tv 3m]}]
	set cols [$tv cget -columns]
	set w(#0) [expr {[font measure $heading [$tv heading #0 -text]] + $pad}]
	foreach c $cols {set w($c) [expr {[font measure $heading [$tv heading $c -text]] + $pad}]}
	set todo [list {} 0]
	while {[llength $todo]} {
		set todo [lassign $todo parent depth]
		foreach id [$tv children $parent] {
			set f $font
			foreach tag [$tv item $id -tags] {
				set tf [$tv tag cget $tag -font]
				if {$tf ne ""} {set f $tf}
			}
			set x [expr {$indent * ($depth + 1) + [font measure $f [$tv item $id -text]] + $pad}]
			set img [lindex [$tv item $id -image] 0]
			if {$img ne ""} {incr x [image width $img]}
			if {$x > $w(#0)} {set w(#0) $x}
			foreach c $cols v [$tv item $id -values] {
				if {$c eq ""} break
				set x [expr {[font measure $f $v] + $pad}]
				if {$x > $w($c)} {set w($c) $x}
			}
			lappend todo $id [expr {$depth + 1}]
		}
	}
	foreach c [concat #0 $cols] {
		$tv column $c -minwidth $w($c)
		# Tk widens only stretching columns to their minimum width.
		if {[$tv column $c -width] < $w($c)} {$tv column $c -width $w($c)}
	}
}
proc ::gameforge::growColumn {tv col text} {
	lassign [::gameforge::fonts] font heading
	set x [expr {[font measure $font $text] + [winfo pixels $tv 3m]}]
	if {$x > [$tv column $col -minwidth]} {$tv column $col -minwidth $x}
	if {$x > [$tv column $col -width]} {$tv column $col -width $x}
}
`

var listProcsDefined bool

// scrolledTree creates a list with vertical and horizontal scroll bars in
// parent, which holds nothing else.
func scrolledTree(parent *TFrameWidget, opts ...Opt) *TTreeviewWidget {
	var sy, sx *TScrollbarWidget
	opts = append(opts,
		Yscrollcommand(func(e *Event) { e.ScrollSet(sy) }),
		Xscrollcommand(func(e *Event) { e.ScrollSet(sx) }))
	tv := parent.TTreeview(opts...)
	sy = parent.TScrollbar(Command(func(e *Event) { e.Yview(tv) }))
	sx = parent.TScrollbar(Orient("horizontal"), Command(func(e *Event) { e.Xview(tv) }))
	Grid(tv, Row(0), Column(0), Sticky("news"))
	Grid(sy, Row(0), Column(1), Sticky("ns"))
	Grid(sx, Row(1), Column(0), Sticky("we"))
	GridColumnConfigure(parent.Window, 0, Weight(1))
	GridRowConfigure(parent.Window, 0, Weight(1))
	return tv
}

func defineListProcs() {
	if !listProcsDefined {
		evalTcl(listProcs)
		listProcsDefined = true
	}
}

// fitColumns makes every column of a list at least as wide as its longest
// text; call it after filling the list.
func fitColumns(tv *TTreeviewWidget) {
	defineListProcs()
	tcl("::gameforge::fitColumns", tv.String())
}

// growColumn widens a column of a list, if needed, for a text just shown
// in it.
func growColumn(tv *TTreeviewWidget, col, text string) {
	defineListProcs()
	tcl("::gameforge::growColumn", tv.String(), col, text)
}
