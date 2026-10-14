package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// testHistory is two commits of the test program: HEAD (as booted) and an
// older revision whose gain differs — the one activation travels back to.
const oldTestProgram = `PROGRAM Test
VAR_EXTERNAL
  Level : REAL;
  SP : REAL;
  Out : REAL;
END_VAR
Out := (SP - Level) * 0.5;
END_PROGRAM
`

func testHistory() *ProgramHistory {
	return &ProgramHistory{
		Built: "aaaa111122223333aaaa111122223333aaaa1111",
		Commits: []ProgramCommit{
			{
				Sha: "aaaa111122223333aaaa111122223333aaaa1111", Short: "aaaa111",
				Author: "Test", Date: "2026-08-18T00:00:00Z", Subject: "head",
				Diff:  "diff --git a/program.st b/program.st\n",
				Files: map[string]string{"nautilus.yaml": "m1", "program.st": "b1"},
			},
			{
				Sha: "bbbb111122223333bbbb111122223333bbbb1111", Short: "bbbb111",
				Author: "Test", Date: "2026-08-17T00:00:00Z", Subject: "old gain",
				Files: map[string]string{"nautilus.yaml": "m1", "program.st": "b2"},
			},
		},
		Blobs: map[string]string{
			"m1": "name: t\ntasks:\n  - program: program.st\n",
			"b1": testProgram,
			"b2": oldTestProgram,
		},
	}
}

// sourcesFromHistory stands in for the runner's project.Sources wiring:
// resolve a sha to its snapshot's single main program.
func sourcesFromHistory(h *ProgramHistory) func(string) (map[string]string, error) {
	return func(sha string) (map[string]string, error) {
		c := h.Find(sha)
		if c == nil || len(c.Files) == 0 {
			return nil, fmt.Errorf("no snapshot for %s", sha)
		}
		return map[string]string{"main": h.Blobs[c.Files["program.st"]]}, nil
	}
}

// The history endpoint serves the captured commits plus the running layer,
// and degrades to an empty (never-null) history when nothing was captured.
func TestProgramHistoryEndpoint(t *testing.T) {
	rt := newTestRuntime(t)
	// No history wired at all — the endpoint still answers.
	w, body := doJSON(t, New(rt).Handler(), "GET", "/api/program/history", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d", w.Code)
	}
	if commits, ok := body["commits"].([]any); !ok || len(commits) != 0 {
		t.Fatalf("no capture should serve commits: [], got %v", body["commits"])
	}
	if body["editable"] != false {
		t.Fatal("no SourcesAt should report editable: false")
	}

	h := testHistory()
	srv := New(rt, Options{
		OnlineEdits: true,
		History:     func() *ProgramHistory { return h },
		SourcesAt:   sourcesFromHistory(h),
	}).Handler()
	w, body = doJSON(t, srv, "GET", "/api/program/history", nil)
	if w.Code != http.StatusOK || body["built"] != h.Built || body["editable"] != true {
		t.Fatalf("GET = %d %v", w.Code, body)
	}
	commits := body["commits"].([]any)
	if len(commits) != 2 {
		t.Fatalf("want 2 commits, got %d", len(commits))
	}
	first := commits[0].(map[string]any)
	if first["short"] != "aaaa111" || first["activatable"] != true || !strings.HasPrefix(first["diff"].(string), "diff --git") {
		t.Fatalf("first commit: %v", first)
	}
	progs := body["programs"].([]any)
	if len(progs) != 1 || progs[0].(map[string]any)["pou"] != "Test" {
		t.Fatalf("programs layer: %v", body["programs"])
	}
}

// Activation warm-swaps to a commit's source, reports per task, records the
// active sha, and a hand edit or rollback clears that claim.
func TestActivate(t *testing.T) {
	rt := newTestRuntime(t)
	h := testHistory()
	srv := New(rt, Options{
		OnlineEdits: true,
		History:     func() *ProgramHistory { return h },
		SourcesAt:   sourcesFromHistory(h),
	}).Handler()

	// Short shas resolve like full ones.
	w, body := doJSON(t, srv, "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusOK {
		t.Fatalf("activate = %d %v", w.Code, body)
	}
	if rt.Program().Source() != oldTestProgram {
		t.Fatal("activation should be running the old revision")
	}
	if !rt.Program().CanRollback() {
		t.Fatal("an activation is an online edit — rollback must be armed")
	}
	_, body = doJSON(t, srv, "GET", "/api/program/history", nil)
	if body["active"] != "bbbb111122223333bbbb111122223333bbbb1111" {
		t.Fatalf("active = %v", body["active"])
	}

	// Rollback clears the active claim and restores HEAD's source.
	w, _ = doJSON(t, srv, "POST", "/api/program/rollback", nil)
	if w.Code != http.StatusOK || rt.Program().Source() != testProgram {
		t.Fatalf("rollback = %d, source restored = %v", w.Code, rt.Program().Source() == testProgram)
	}
	_, body = doJSON(t, srv, "GET", "/api/program/history", nil)
	if active, ok := body["active"]; ok && active != "" {
		t.Fatalf("rollback should clear active, got %v", active)
	}
}

func TestActivateRejections(t *testing.T) {
	rt := newTestRuntime(t)
	h := testHistory()
	opts := Options{
		OnlineEdits: true,
		History:     func() *ProgramHistory { return h },
		SourcesAt:   sourcesFromHistory(h),
	}

	// Gated exactly like other online edits.
	w, _ := doJSON(t, New(rt).Handler(), "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("edits off: want 403, got %d", w.Code)
	}

	srv := New(rt, opts).Handler()
	w, _ = doJSON(t, srv, "POST", "/api/program/activate", map[string]string{"sha": "cccc111"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown sha: want 404, got %d", w.Code)
	}

	// A commit whose task set differs is a topology change, refused whole.
	opts.SourcesAt = func(sha string) (map[string]string, error) {
		return map[string]string{"main": oldTestProgram, "totals": "PROGRAM Totals\nEND_PROGRAM"}, nil
	}
	w, body := doJSON(t, New(rt, opts).Handler(), "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusConflict || !strings.Contains(body["error"].(string), "topology") {
		t.Fatalf("extra task: want 409 topology, got %d %v", w.Code, body)
	}
	opts.SourcesAt = func(sha string) (map[string]string, error) {
		return map[string]string{"other": oldTestProgram}, nil
	}
	w, _ = doJSON(t, New(rt, opts).Handler(), "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusConflict {
		t.Fatalf("missing task: want 409, got %d", w.Code)
	}

	// A commit that no longer compiles is rejected before any swap.
	opts.SourcesAt = func(sha string) (map[string]string, error) {
		return map[string]string{"main": "PROGRAM Broken\nOut := ;\nEND_PROGRAM"}, nil
	}
	w, _ = doJSON(t, New(rt, opts).Handler(), "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad compile: want 422, got %d", w.Code)
	}
	if rt.Program().Source() != testProgram || rt.Program().Dirty() {
		t.Fatal("a rejected activation must leave the running program untouched")
	}
}

// activateClock is a hand-moved runtime.Clock, so the TONs below time in
// exact virtual seconds.
type activateClock struct{ t time.Time }

func (c *activateClock) Now() time.Time { return c.t }

// Two-task programs for TestActivateEveryTask. Each carries a TON and an
// integrator; the older commit changes each program's output scaling, so
// the test can tell which revision every task is running.
const (
	twoTaskMain = `PROGRAM Test
VAR_EXTERNAL MainET : TIME; MainOut : REAL; END_VAR
VAR Dwell : TON; Total : REAL; END_VAR
Dwell(IN := TRUE, PT := T#1H);
MainET := Dwell.ET;
Total := Total + 1.0;
MainOut := Total;
END_PROGRAM
`
	twoTaskAux = `PROGRAM Totals
VAR_EXTERNAL AuxET : TIME; AuxOut : REAL; END_VAR
VAR Hold : TON; Sum : REAL; END_VAR
Hold(IN := TRUE, PT := T#1H);
AuxET := Hold.ET;
Sum := Sum + 1.0;
AuxOut := Sum;
END_PROGRAM
`
)

// Activation swaps EVERY task, not just main, and each swap is warm: both
// tasks run the commit's source afterwards, and both keep their timers and
// integrators. A handler that swapped only the main task, or swapped cold,
// fails here.
func TestActivateEveryTask(t *testing.T) {
	oldMain := strings.Replace(twoTaskMain, "MainOut := Total;", "MainOut := Total * 10.0;", 1)
	oldAux := strings.Replace(twoTaskAux, "AuxOut := Sum;", "AuxOut := Sum * 100.0;", 1)

	clk := &activateClock{t: time.Unix(1_700_000_000, 0)}
	t0 := clk.t
	rt, err := runtime.New(runtime.Options{
		Program: twoTaskMain,
		Clock:   clk,
		Seed:    nio.Values{"MainOut": 0.0, "AuxOut": 0.0},
		Tasks:   []runtime.Task{{Name: "totals", Program: twoTaskAux, Scan: time.Second}},
	})
	if err != nil {
		t.Fatal(err)
	}
	scanAt := func(sec int) {
		t.Helper()
		clk.t = t0.Add(time.Duration(sec) * time.Second)
		rt.Scan()
		if err := rt.ScanTask("totals"); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) ir.Value {
		t.Helper()
		v, err := rt.Tags().ReadGlobal(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return v
	}
	for s := 0; s < 5; s++ {
		scanAt(s)
	}
	if read("MainET").I != 4000 || read("AuxET").I != 4000 || read("MainOut").F != 5 || read("AuxOut").F != 5 {
		t.Fatalf("before activation: MainET=%d AuxET=%d MainOut=%v AuxOut=%v, want 4000/4000/5/5",
			read("MainET").I, read("AuxET").I, read("MainOut").F, read("AuxOut").F)
	}

	h := testHistory()
	sources := map[string]map[string]string{
		"aaaa111122223333aaaa111122223333aaaa1111": {"main": twoTaskMain, "totals": twoTaskAux},
		"bbbb111122223333bbbb111122223333bbbb1111": {"main": oldMain, "totals": oldAux},
	}
	srv := New(rt, Options{
		OnlineEdits: true,
		History:     func() *ProgramHistory { return h },
		SourcesAt: func(sha string) (map[string]string, error) {
			if s, ok := sources[sha]; ok {
				return s, nil
			}
			return nil, fmt.Errorf("no snapshot for %s", sha)
		},
	}).Handler()
	w, body := doJSON(t, srv, "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusOK {
		t.Fatalf("activate = %d %v", w.Code, body)
	}
	progs, _ := body["programs"].([]any)
	tasks := map[string]bool{}
	for _, p := range progs {
		tasks[p.(map[string]any)["task"].(string)] = true
	}
	if len(progs) != 2 || !tasks["main"] || !tasks["totals"] {
		t.Errorf("activation should report both tasks, got %v", body["programs"])
	}
	if rt.TaskProgram("main").Source() != oldMain {
		t.Error("main task is not running the activated revision")
	}
	if rt.TaskProgram("totals").Source() != oldAux {
		t.Error("totals task is not running the activated revision: only main was swapped")
	}

	// Both swaps were warm: each task's TON continues to 5 s and each
	// integrator steps 5 → 6 under the revision's scaling.
	scanAt(5)
	if got := read("MainET").I; got != 5000 {
		t.Errorf("main Dwell.ET after activation = %d, want 5000 (timer carried)", got)
	}
	if got := read("MainOut").F; got != 60 {
		t.Errorf("MainOut after activation = %v, want 60 (Total carried, ×10 live)", got)
	}
	if got := read("AuxET").I; got != 5000 {
		t.Errorf("totals Hold.ET after activation = %d, want 5000 (timer carried)", got)
	}
	if got := read("AuxOut").F; got != 600 {
		t.Errorf("AuxOut after activation = %v, want 600 (Sum carried, ×100 live)", got)
	}
}

// A program that names its tags without VAR_EXTERNAL (#177/#210) activates:
// the pre-flight compiles in the running program's context, where the
// project's tags are in scope.
func TestActivateImplicitTags(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: "PROGRAM Test\nOut := SP - Level;\nEND_PROGRAM\n",
		Driver:  nio.NewMemory(),
		Tags: []runtime.TagDef{
			runtime.Input("Level", runtime.Init(40.0)),
			runtime.Setpoint("SP", 65.0),
			runtime.Typed("Out", runtime.RoleOutput, "REAL"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := testHistory()
	h.Blobs["b2"] = "PROGRAM Test\nOut := (SP - Level) * 0.5;\nEND_PROGRAM\n"
	srv := New(rt, Options{
		OnlineEdits: true,
		History:     func() *ProgramHistory { return h },
		SourcesAt:   sourcesFromHistory(h),
	}).Handler()
	w, body := doJSON(t, srv, "POST", "/api/program/activate", map[string]string{"sha": "bbbb111"})
	if w.Code != http.StatusOK {
		t.Fatalf("activate = %d %v", w.Code, body)
	}
	rt.Scan()
	if v, _ := rt.Tags().ReadGlobal("Out"); v.F != 12.5 {
		t.Errorf("Out = %v, want 12.5 from the activated revision", v.F)
	}
}
