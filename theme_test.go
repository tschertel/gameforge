package main

import (
	"gameforge/internal/config"
	"testing"
)

func TestDarkMode(t *testing.T) {
	for _, tc := range []struct {
		theme string
		want  bool
	}{
		{"dark", true}, {"light", false}, {"auto", systemDark()},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			u := &ui{cfg: &config.Config{Theme: tc.theme}}
			if got := u.darkMode(); got != tc.want {
				t.Fatalf("darkMode() = %v, want %v", got, tc.want)
			}
		})
	}
}
