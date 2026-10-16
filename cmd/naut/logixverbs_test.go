package main

// More `naut logix` verbs, each against the fake agent or a public fixture:
// what the CLI itself promises, with the agent's half faked.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// LGX-003: --scope '*' brings program-scope tags in beside the
// controller-scope ones; the default leaves them out.
func TestLogixImportScopeStarTakesProgramTags(t *testing.T) {
	read := func(args ...string) string {
		dir := t.TempDir()
		if code := runLogixImport(append([]string{"--out", dir}, append(args, varietyL5X)...)); code != 0 {
			t.Fatalf("import %v exited %d", args, code)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "tags", "logix.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	all := read("--scope", "*")
	for _, want := range []string{"name: MainProgram_Scratch", "name: StartPB"} {
		if !strings.Contains(all, want) {
			t.Errorf("--scope '*' tag file is missing %q:\n%s", want, all)
		}
	}
	if def := read(); strings.Contains(def, "MainProgram_Scratch") {
		t.Errorf("the default scope took a program tag, so the flag proves nothing:\n%s", def)
	}
}

// LGX-021: drift compares what the controller holds with the repo file and
// exits 1 on a mismatch, 0 on a match: a CI gate.
func TestLogixDriftExitsOneOnAMismatch(t *testing.T) {
	repo := filepath.Join("..", "..", "lang", "l5x", "testdata", "demoline.L5X")
	same, err := os.ReadFile(repo)
	if err != nil {
		t.Fatal(err)
	}
	// The same project with one setpoint changed (85.0 -> 80.0).
	changed, err := os.ReadFile(filepath.Join("..", "..", "lang", "l5x", "testdata", "demoline.v80.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, controller string
		code             int
		says             string
	}{
		{"controller matches the repo", string(same), 0, "no drift"},
		{"controller differs from the repo", string(changed), 1, "DRIFT"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, url, stop := newFakeLogixd(t)
			defer stop()
			f.controllerL5X = c.controller
			var code int
			out := captureStdout(t, func() int {
				code = runLogixDrift([]string{"--agent", url, "--comm-path", `AB_ETH-1\10.0.0.5\Backplane\0`, repo})
				return code
			})
			if code != c.code || !strings.Contains(out, c.says) {
				t.Errorf("drift exit %d, want %d, saying %q:\n%s", code, c.code, c.says, out)
			}
			// What it compared is what it uploaded from that controller.
			if got, _ := f.uploadToNew["commPath"].(string); got != `AB_ETH-1\10.0.0.5\Backplane\0` {
				t.Errorf("uploaded from %q (body %v)", got, f.uploadToNew)
			}
		})
	}
}

// LGX-022: browse lists the controllers in the FT Linx configuration the
// agent hands back (path, controller name, catalog number), and asks the
// agent for nothing but that configuration: no scan.
func TestLogixBrowseListsWhatFTLinxDiscovered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "logix", "logixd", "testdata", "RSLinxNG.xml"))
	if err != nil {
		t.Fatal(err)
	}
	// The agent reads the file as text (UTF-16 with a BOM on disk) and
	// sends it as a JSON string.
	u := make([]uint16, 0, len(raw)/2)
	for i := 2; i+1 < len(raw); i += 2 {
		u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	f, url, stop := newFakeLogixd(t)
	defer stop()
	f.linxXML = string(utf16.Decode(u))

	var code int
	out := captureStdout(t, func() int { code = runLogixBrowse([]string{"--agent", url}); return code })
	if code != 0 {
		t.Fatalf("browse exit %d:\n%s", code, out)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, `AB_ETH-1\10.0.0.5\Backplane\0`) {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "DemoLine") || !strings.Contains(line, "(9310-WED300ENT)") {
		t.Errorf("want the controller's path, name and catalog number on one line:\n%s", out)
	}
	if len(f.seen) != 1 || f.seen[0] != "GET /v1/linx-config" {
		t.Errorf("browse asked the agent for %v, want only its FT Linx configuration", f.seen)
	}
}

// LGX-018: a wrong token is answered the way logixd answers it (HTTP 401,
// {"error":"unauthorized"}), and the CLI says so in the words the Logix
// guide quotes, naming the agent and where the token comes from.
func TestLogixAgentReportsAWrongToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
	}))
	defer srv.Close()
	out, code := captureStderr(t, func() int { return runLogixAgent([]string{"--agent", srv.URL, "--token", "wrong"}) })
	if code == 0 {
		t.Error("a refused token should be an error")
	}
	want := "logixd: unauthorized: " + srv.URL + " refused the token; check --token or NAUTILUS_LOGIXD_TOKEN against " +
		`%ProgramData%\logixd\logixd.token on the agent's machine`
	if !strings.Contains(out, want) {
		t.Errorf("stderr missing %q:\n%s", want, out)
	}
}

// LGA-032: `naut logix mode` reads the controller's mode, and with --yes
// changes it, through a project uploaded from that controller.
func TestLogixModeReadsAndChangesTheMode(t *testing.T) {
	const path = `AB_ETH-1\10.0.0.5\Backplane\0`
	t.Run("read", func(t *testing.T) {
		f, url, stop := newFakeLogixd(t)
		defer stop()
		f.mode = "Program"
		var code int
		out := captureStdout(t, func() int {
			code = runLogixMode([]string{"--agent", url, "--comm-path", path})
			return code
		})
		if code != 0 || !strings.Contains(out, path+" is Program") {
			t.Errorf("mode read exit %d:\n%s", code, out)
		}
		if got, _ := f.uploadToNew["commPath"].(string); got != path {
			t.Errorf("uploaded from %q, want the controller named", got)
		}
		for _, s := range f.seen {
			if strings.HasPrefix(s, "POST") && strings.HasSuffix(s, "/mode") {
				t.Errorf("a read changed the mode: %v", f.seen)
			}
		}
	})
	t.Run("change", func(t *testing.T) {
		f, url, stop := newFakeLogixd(t)
		defer stop()
		f.mode = "Run"
		var code int
		out := captureStdout(t, func() int {
			code = runLogixMode([]string{"--agent", url, "--comm-path", path, "--yes", "program"})
			return code
		})
		if code != 0 || !strings.Contains(out, path+": Run → Program") {
			t.Errorf("mode change exit %d:\n%s", code, out)
		}
		if f.mode != "Program" {
			t.Errorf("controller mode = %q, want Program", f.mode)
		}
	})
}
