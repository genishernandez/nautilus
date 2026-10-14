package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
// snapshot and serving the dashboard at /.
//
// The scaffold's manifest is checked to name localhost:8080, and naut run is
// then shown to bind the manifest's address — moved to a free port, so the
// test never depends on 8080 being free on the machine running it.
func TestNautRunServesDashboardAndState(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the CLI")
	}
	bin := buildNautCLI(t, t.TempDir(), nil)
	dir := scaffoldInto(t, scaffold{Name: "runme", Program: "Runme", Template: tmplDemo})

	proj, err := project.Load(os.DirFS(dir), "")
	if err != nil {
		t.Fatal(err)
	}
	if proj.Addr != "localhost:8080" {
		t.Fatalf("the scaffold serves on %q, want localhost:8080", proj.Addr)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	manifest := filepath.Join(dir, "nautilus.yaml")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(string(raw), `addr: "localhost:8080"`, `addr: "`+addr+`"`, 1)
	if moved == string(raw) {
		t.Fatalf("the scaffold's manifest has no server addr line to move:\n%s", raw)
	}
	if err := os.WriteFile(manifest, []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}

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
		resp, err := http.Get("http://" + addr + path)
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
			t.Fatalf("naut run never answered on the manifest's address %s; stderr:\n%s", addr, stderr.String())
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

	// The scan loop is running: the scan count keeps climbing.
	scans := func() uint64 {
		_, body, _ := get("/api/state")
		var f struct {
			Scans uint64 `json:"scans"`
		}
		json.Unmarshal([]byte(body), &f)
		return f.Scans
	}
	first := scans()
	for deadline := time.Now().Add(10 * time.Second); scans() < first+3; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the scan count stayed at %d: naut run is not scanning", first)
		}
	}

	code, page, ok := get("/")
	if !ok || code != 200 || !strings.Contains(page, "/api/stream") {
		t.Errorf("GET / = %d (reached=%v), want the dashboard (it names /api/stream)", code, ok)
	}
}
