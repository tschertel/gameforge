package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSystemDarkMacOS(t *testing.T) {
	output, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleInterfaceStyle").Output()
	want := err == nil && strings.TrimSpace(string(output)) == "Dark"
	if got := systemDark(); got != want {
		t.Fatalf("systemDark() = %v, macOS preference is %v", got, want)
	}
}
