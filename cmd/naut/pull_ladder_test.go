package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/runtime"
	"github.com/joyautomation/nautilus/server"
)

// A controller running a ladder program holds the .ld text itself: the
// program download returns it, `naut pull --check` diffs against it, and
// `naut pull` writes it back into the project's .ld file.
func TestPullMovesLadderText(t *testing.T) {
	remote := "PROGRAM Main\nVAR_EXTERNAL Start : BOOL; Run : BOOL; END_VAR\nLD\n  RUNG seal\n    [ Start | Run ] ( Run )\nEND_LD\nEND_PROGRAM\n"
	rt, err := runtime.New(runtime.Options{Program: remote, Seed: map[string]any{"Start": false, "Run": false}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(rt, server.Options{OnlineEdits: true}).Handler())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	// Download over the controller API returns the ladder text, not ST.
	resp, err := http.Get(srv.URL + "/api/program")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "RUNG seal") || !strings.Contains(string(body), "[ Start | Run ] ( Run )") {
		t.Fatalf("download should carry the .ld text itself:\n%s", body)
	}

	stale := "PROGRAM Main\nVAR_EXTERNAL Start : BOOL; Run : BOOL; END_VAR\nLD\n  RUNG seal\n    Start ( Run )\nEND_LD\nEND_PROGRAM\n"
	dir := writeProject(t, map[string]string{"main.ld": stale})
	args := []string{"--host", u.Hostname(), "--port", u.Port(), "--dir", dir}

	if code := runPull(append(args, "--check")); code != 1 {
		t.Fatalf("--check against a drifted .ld = %d, want 1", code)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "main.ld")); string(raw) != stale {
		t.Fatal("--check must not write")
	}
	if code := runPull(args); code != 0 {
		t.Fatalf("pull = %d, want 0", code)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "main.ld"))
	if string(raw) != remote {
		t.Fatalf("main.ld should now be the controller's ladder text:\n%s", raw)
	}
	if code := runPull(append(args, "--check")); code != 0 {
		t.Fatalf("post-pull --check = %d, want 0", code)
	}
}
