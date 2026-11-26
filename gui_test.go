package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	. "modernc.org/tk9.0"
	"modernc.org/tk9.0/extensions/eval"
)

// Run Tk on the main OS thread, in an isolated process with no user files.
func TestMain(m *testing.M) {
	if os.Getenv("GAMEFORGE_GUI_CHILD") == "1" {
		InitializeExtension("eval")
		if err := checkGUI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestGUI(t *testing.T) {
	if os.Getenv("GAMEFORGE_GUI_TESTS") != "1" {
		t.Skip("set GAMEFORGE_GUI_TESTS=1 in a desktop session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "GAMEFORGE_GUI_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GUI regression: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func checkGUI() error {
	defer Destroy(App)
	// The main window starts hidden on Windows, and a hidden window keeps
	// its size, so the text would never reflow.
	WmDeiconify(App)
	initThemes()
	for _, dark := range []bool{false, true, false} {
		applyTheme(dark)
		if tcl("tk", "windowingsystem") == "aqua" {
			got := tcl("tk::unsupported::MacWindowStyle", "isdark", ".")
			if got != tclBool(dark) {
				return fmt.Errorf("appearance = %s, want dark=%v", got, dark)
			}
		}
		label := TLabel(Txt(strings.Repeat("A long description of conversion options. ", 80)), Wraplength(240), Padding(12))
		Grid(label, Row(0), Column(0), Sticky("we"))
		GridColumnConfigure(App, 0, Weight(1))
		WmMinSize(App, 400, 200)
		wrapToWidth(label)
		settle := func() error {
			_, err := eval.Eval(`unset -nocomplain ::gameforgeSettled; after 100 {set ::gameforgeSettled 1}; vwait ::gameforgeSettled`)
			return err
		}
		if err := settle(); err != nil {
			return err
		}
		if got, _ := strconv.Atoi(tcl("winfo", "reqwidth", ".")); got > 400 {
			return fmt.Errorf("window requests width %d, want at most 400 (dark=%v)", got, dark)
		}
		var heights []string
		for _, width := range []string{"800x600", "400x600", "800x600"} {
			WmGeometry(App, width)
			if err := settle(); err != nil {
				return err
			}
			heights = append(heights, tcl("winfo", "reqheight", label.String()))
		}
		if heights[0] == heights[1] || heights[0] != heights[2] {
			return fmt.Errorf("text did not reflow consistently: %v", heights)
		}
		WmGeometry(App, "")
		if err := settle(); err != nil {
			return err
		}
		if got, _ := strconv.Atoi(tcl("winfo", "reqwidth", ".")); got > 400 {
			return fmt.Errorf("window retained enlarged width request %d", got)
		}
		fmt.Printf("dark=%v: stable width, wide/narrow/wide reflow, appearance OK\n", dark)
		Destroy(label)
	}
	return nil
}
