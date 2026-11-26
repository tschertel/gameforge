package main

import (
	"testing"
	"time"

	"gameforge/internal/config"
)

// TestUpdateChoice reads the settings of the update check as the About
// window's list shows them, and how often the check runs.
func TestUpdateChoice(t *testing.T) {
	on, off := true, false
	cases := []struct {
		check *bool
		every string
		want  int
		dur   time.Duration
	}{
		{nil, "", 0, 24 * time.Hour},
		{&off, "weekly", 0, 24 * time.Hour},
		{&on, "", 1, 24 * time.Hour},
		{&on, "daily", 1, 24 * time.Hour},
		{&on, "weekly", 2, 7 * 24 * time.Hour},
		{&on, "monthly", 3, 30 * 24 * time.Hour},
		{&on, "hourly", 1, 24 * time.Hour},
	}
	for _, c := range cases {
		u := &ui{cfg: &config.Config{UpdateCheck: c.check, UpdateEvery: c.every}}
		if got := u.updateChoice(); got != c.want {
			t.Errorf("%v %q: choice %d, want %d", c.check, c.every, got, c.want)
		}
		if got := u.updateEvery(); got != c.dur {
			t.Errorf("%v %q: every %v, want %v", c.check, c.every, got, c.dur)
		}
	}
}

// TestUpdateQuestion: asked the first time; asked again only by who said
// yes before the check was counted, once.
func TestUpdateQuestion(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		cfg  config.Config
		want string
	}{
		{config.Config{}, "upd.ask"},
		{config.Config{UpdateCheck: &yes}, "upd.ask_again"},
		{config.Config{UpdateCheck: &no}, ""},
		{config.Config{UpdateCheck: &yes, UpdateConsent: updateConsent}, ""},
		{config.Config{UpdateCheck: &no, UpdateConsent: updateConsent}, ""},
	} {
		if got := updateQuestion(&c.cfg); got != c.want {
			t.Errorf("%+v: %q, want %q", c.cfg, got, c.want)
		}
	}
}
