package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"2.12.0", "2.11.0", true}, {"v2.11.1", "2.11.0", true}, {"3.0.0", "2.99.99", true},
		{"2.11.0", "2.11.0", false}, {"2.10.9", "2.11.0", false}, {"2.9.0", "2.10.0", false},
		{"2.12", "2.11.0", false}, {"", "2.11.0", false}, {"2.12.0-beta", "2.11.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLatest(t *testing.T) {
	agent := ""
	body := `{"tag_name":"v2.12.0","html_url":"https://github.com/tschertel/gameforge/releases/tag/v2.12.0","assets":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	// GitHub only: tests never reach the project's server.
	oldCount, old := CountURL, URL
	CountURL, URL = "", srv.URL
	defer func() { CountURL, URL = oldCount, old }()

	r, err := Latest(context.Background(), "2.11.0")
	if err != nil || r.Version != "2.12.0" || r.Page != "https://github.com/tschertel/gameforge/releases/tag/v2.12.0" {
		t.Errorf("got %+v, %v", r, err)
	}
	if agent != "GameForge/2.11.0" {
		t.Errorf("User-Agent %q", agent)
	}
	// A page outside GitHub is not offered as a link.
	body = `{"tag_name":"v2.12.0","html_url":"https://example.com/x"}`
	if r, _ := Latest(context.Background(), "2.11.0"); r.Page != "" {
		t.Errorf("page %q", r.Page)
	}
	body = `{"tag_name":"nightly"}`
	if _, err := Latest(context.Background(), "2.11.0"); err == nil {
		t.Error("a tag without a version was taken")
	}
	srv.Close()
	if _, err := Latest(context.Background(), "2.11.0"); err == nil {
		t.Error("no error without a server")
	}
}

// TestCountURL: the project's server gets the version and the system and is
// asked first; GitHub gets only the User-Agent, and is asked when the server
// fails.
func TestCountURL(t *testing.T) {
	body := `{"tag_name":"v2.23.0","html_url":"https://github.com/tschertel/gameforge/releases/tag/v2.23.0"}`
	var counted, github string
	count := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counted = r.URL.RawQuery
		w.Write([]byte(body))
	}))
	defer count.Close()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		github = r.URL.RawQuery + "|" + r.Header.Get("User-Agent")
		w.Write([]byte(body))
	}))
	defer gh.Close()
	oldCount, oldURL := CountURL, URL
	CountURL, URL = count.URL+"/latest", gh.URL
	defer func() { CountURL, URL = oldCount, oldURL }()

	if r, err := Latest(context.Background(), "2.22.0"); err != nil || r.Version != "2.23.0" {
		t.Fatalf("got %+v, %v", r, err)
	}
	want := "arch=" + runtime.GOARCH + "&os=" + runtime.GOOS + "&version=2.22.0"
	if counted != want || github != "" {
		t.Errorf("server got %q, GitHub got %q", counted, github)
	}
	count.Close()
	if r, err := Latest(context.Background(), "2.22.0"); err != nil || r.Version != "2.23.0" {
		t.Fatalf("without the server: %+v, %v", r, err)
	}
	if github != "|GameForge/2.22.0" {
		t.Errorf("GitHub got %q", github)
	}
}
