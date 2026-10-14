package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/internal/project"
	"testing/fstest"
)

// Getting started: a project that names no server address is served on
// localhost:8080 — the address `naut run` binds.
func TestProjectDefaultsToLocalhost8080(t *testing.T) {
	proj, err := project.Load(fstest.MapFS{
		"nautilus.yaml": {Data: []byte("tasks:\n  - program: main.st\ntags:\n  - { name: Out, role: state, init: 0.0 }\n")},
		"main.st":       {Data: []byte("PROGRAM Main\nOut := 1.0;\nEND_PROGRAM\n")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if proj.Addr != "localhost:8080" {
		t.Errorf("default address = %q, want localhost:8080", proj.Addr)
	}
}

// Getting started: `naut run` in a freshly scaffolded project starts the scan
// loop, the dashboard and the tag API on http://localhost:8080 — the real CLI
// binary, run with no NAUTILUS_ADDR, answering GET /api/state with the raw tag
// snapshot and serving the dashboard at /. Skipped (not failed) when something
// else already holds port 8080 on this machine.
func TestNautRunServesDashboardAndStateOnLocalhost8080(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the CLI")
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:8080", 200*time.Millisecond); err == nil {
		c.Close()
		t.Skip("port 8080 is already in use on this machine")
	}
	bin := buildNautCLI(t, t.TempDir(), nil)
	dir := scaffoldInto(t, scaffold{Name: "runme", Program: "Runme", Template: tmplDemo})

	cmd := exec.Command(bin, "run")
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NAUTILUS_ADDR=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-exited
	})

	get := func(path string) (int, string, bool) {
		resp, err := http.Get("http://localhost:8080" + path)
		if err != nil {
			return 0, "", false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), true
	}
	// Wait for the listener: poll until the controller answers or exits.
	deadline := time.Now().Add(30 * time.Second)
	var state string
	for {
		if code, body, ok := get("/api/state"); ok && code == 200 {
			state = body
			break
		}
		select {
		case <-exited:
			t.Fatalf("naut run exited before serving; stderr:\n%s", stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("naut run never answered on localhost:8080; stderr:\n%s", stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	var frame struct {
		Tags map[string]any `json:"tags"`
	}
	if err := json.Unmarshal([]byte(state), &frame); err != nil {
		t.Fatalf("/api/state is not JSON: %v\n%s", err, state)
	}
	if _, ok := frame.Tags["TempSP"]; !ok {
		t.Errorf("/api/state carries no tag snapshot (want the tour's TempSP): %s", state)
	}
	code, page, ok := get("/")
	if !ok || code != 200 || !strings.Contains(page, "/api/stream") {
		t.Errorf("GET / = %d (reached=%v), want the dashboard (it names /api/stream)", code, ok)
	}
}
