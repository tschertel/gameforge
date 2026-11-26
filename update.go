package main

import (
	"context"
	"runtime"
	"slices"
	"time"

	. "modernc.org/tk9.0"

	"gameforge/internal/config"
	"gameforge/internal/update"
)

// The app can check whether a newer version is out, daily (the default),
// weekly or monthly: the user is asked once, and can change it or check at
// once in the About window. It asks the project's server, which counts the
// check (server/README.md), else GitHub. Only the app's version and the
// system are sent; nothing is downloaded or installed.

// updateConsent is the version of the question about the check: 2 since
// the check goes to the project's server and is counted. Who said yes to an
// older question is asked again; who said no is not.
const updateConsent = 2

// updateQuestion is the question to ask about the check at startup, or ""
// when it was answered: the first time, and again for whoever said yes to a
// question older than updateConsent (whoever said no is not asked again).
func updateQuestion(c *config.Config) string {
	switch {
	case c.UpdateCheck == nil:
		return "upd.ask"
	case *c.UpdateCheck && c.UpdateConsent < updateConsent:
		return "upd.ask_again"
	}
	return ""
}

// updateDetail is the explanation of the check, with exactly what this
// app sends.
func updateDetail(key string) string { return T(key, appVersion, runtime.GOOS, runtime.GOARCH) }

type updateInterval struct {
	id    string
	every time.Duration
	key   string
}

// updateIntervals are the choices of how often the check runs, as kept in
// the settings, with their message keys.
var updateIntervals = []updateInterval{
	{"daily", 24 * time.Hour, "upd.daily"},
	{"weekly", 7 * 24 * time.Hour, "upd.weekly"},
	{"monthly", 30 * 24 * time.Hour, "upd.monthly"},
}

// updateChoice is the position of the current setting in the About
// window's list: 0 is off, then the intervals.
func (u *ui) updateChoice() int {
	if u.cfg.UpdateCheck == nil || !*u.cfg.UpdateCheck {
		return 0
	}
	i := slices.IndexFunc(updateIntervals, func(v updateInterval) bool { return v.id == u.cfg.UpdateEvery })
	return max(i, 0) + 1
}

// setUpdateChoice stores a choice of the About window's list and checks
// if it is now time to.
func (u *ui) setUpdateChoice(i int) {
	on := i > 0
	u.cfg.UpdateCheck = &on
	u.cfg.UpdateConsent = updateConsent // the choice's tooltip tells what is sent
	if on {
		u.cfg.UpdateEvery = updateIntervals[min(i, len(updateIntervals))-1].id
		if u.cfg.UpdateEvery == "daily" {
			u.cfg.UpdateEvery = ""
		}
	}
	u.saveConfig()
	u.checkUpdate()
}

// updateEvery is how often the check runs.
func (u *ui) updateEvery() time.Duration {
	return updateIntervals[max(u.updateChoice(), 1)-1].every
}

// startUpdateCheck asks the first time whether to check for new versions,
// then checks if it is time to.
func (u *ui) startUpdateCheck() {
	if question := updateQuestion(u.cfg); question != "" {
		answer := MessageBox(Parent(App), Icon("question"), Type("yesno"), Title(T("app.title")),
			Msg(T(question)), Detail(updateDetail("upd.ask.detail")))
		on := answer == "yes"
		u.cfg.UpdateCheck = &on
		u.cfg.UpdateConsent = updateConsent
		u.saveConfig()
	}
	if u.cfg.ListsCheck == nil {
		u.askListsCheck()
	}
	u.checkUpdate()
	u.checkLists()
}

// checkUpdate asks GitHub in the background for the newest version, if the
// check is on and the last one was tried long enough ago. Errors (no
// network, GitHub blocked) are silent: the check is tried again later.
func (u *ui) checkUpdate() {
	if u.updateChoice() == 0 || time.Since(u.cfg.UpdateChecked) < u.updateEvery() {
		return
	}
	u.fetchUpdate(nil)
}

// fetchUpdate asks GitHub for the newest version now, keeps and shows it,
// then calls done (if any) on the GUI thread.
func (u *ui) fetchUpdate(done func(update.Release, error)) {
	u.cfg.UpdateChecked = time.Now()
	u.saveConfig()
	go func() {
		r, err := update.Latest(context.Background(), appVersion)
		onGUI(func() {
			if err == nil {
				u.cfg.UpdateLatest, u.cfg.UpdatePage = r.Version, r.Page
				u.saveConfig()
				u.showUpdateNote()
				if update.Newer(r.Version, appVersion) {
					u.logf("upd.log", r.Version, u.updatePage())
					u.flush()
				}
			}
			if done != nil {
				done(r, err)
			}
		})
	}()
}

// showUpdateNote shows, in the top row, the newest version found if it is
// newer than this one.
func (u *ui) showUpdateNote() {
	if u.updateNote == nil {
		return
	}
	if update.Newer(u.cfg.UpdateLatest, appVersion) {
		u.updateNote.Configure(Txt(T("upd.note", u.cfg.UpdateLatest)), Cursor("hand2"))
		setTooltip(u.updateNote, u.updatePage())
	} else {
		u.updateNote.Configure(Txt(""), Cursor(""))
	}
}

// updatePage is the web page of the newest version found.
func (u *ui) updatePage() string {
	if u.cfg.UpdatePage != "" {
		return u.cfg.UpdatePage
	}
	return repoURL + "/releases/latest"
}

// openUpdatePage opens the page of the newest version, if one was found.
func (u *ui) openUpdatePage() {
	if update.Newer(u.cfg.UpdateLatest, appVersion) {
		openURL(u.updatePage())
	}
}

// whenShown calls f once the window manager has shown the main window: when
// it has the focus (3 seconds at most), and a moment later. A modal dialog
// opened at startup from PostEvent, before that, got no mouse or keyboard
// input on KDE Wayland (XWayland) and could not even be closed, although Tk
// already reported the window as mapped.
func whenShown(f func()) {
	start := time.Now()
	var poll func()
	poll = func() {
		if tcl("focus") == "" && time.Since(start) < 3*time.Second {
			TclAfter(100*time.Millisecond, poll)
			return
		}
		TclAfter(250*time.Millisecond, f)
	}
	TclAfterIdle(poll)
}
