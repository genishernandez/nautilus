package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joyautomation/nautilus/internal/k8sapi"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/lang/st"
	"github.com/joyautomation/nautilus/retain"
)

// #300: a setpoint table kept in an ARRAY must survive a restart and a
// failover like every other setpoint. Before the fix retainState skipped
// TypeArray, so each of these tables silently reset to its seed.

const tableLib = `
TYPE
  Mode : (Idle, Run := 10, Fault) := Idle;
  Step : STRUCT
    Mode : Mode;
    Secs : REAL;
  END_STRUCT;
END_TYPE
`

// The program never writes the tables, so anything the test reads back
// after a scan came from the seed or from the store.
const tableProg = `
PROGRAM Main
VAR_EXTERNAL
    Tbl    : ARRAY[1..4] OF REAL;
    Counts : ARRAY[0..2] OF DINT;
    Steps  : ARRAY[1..2] OF Step;
    Grid   : ARRAY[1..2, 1..2] OF INT;
    Flags  : ARRAY[1..2] OF BOOL;
    Delays : ARRAY[1..2] OF TIME;
    Names  : ARRAY[1..2] OF STRING;
    SP     : REAL;
    Enable : BOOL;
    Delay  : TIME;
    Batch  : STRING;
    Rec    : Step;
    Cmd    : Mode;
END_VAR
END_PROGRAM
`

func tableRuntime(t *testing.T, store retain.Store, coord Coordinator) *Runtime {
	t.Helper()
	rt, err := New(Options{
		Program:   tableProg,
		Libraries: []string{tableLib},
		Tags: []TagDef{
			Typed("Tbl", RoleSetpoint, "ARRAY[1..4] OF REAL", Init([]any{1.0, 2.0, 3.0, 4.0})),
			Typed("Counts", RoleSetpoint, "ARRAY[0..2] OF DINT", Init([]any{100, 101, 102})),
			Typed("Steps", RoleSetpoint, "ARRAY[1..2] OF Step"),
			Typed("Grid", RoleSetpoint, "ARRAY[1..2, 1..2] OF INT"),
			Typed("Flags", RoleSetpoint, "ARRAY[1..2] OF BOOL"),
			Typed("Delays", RoleSetpoint, "ARRAY[1..2] OF TIME"),
			Typed("Names", RoleSetpoint, "ARRAY[1..2] OF STRING"),
			Setpoint("SP", 65.0),
			Typed("Enable", RoleSetpoint, "BOOL"),
			Typed("Delay", RoleSetpoint, "TIME"),
			Typed("Batch", RoleSetpoint, "STRING"),
			Typed("Rec", RoleSetpoint, "Step"),
			Typed("Cmd", RoleSetpoint, "Mode"),
		},
		Retain:      store,
		Coordinator: coord,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// retune is an operator editing the tables element by element, the way an
// HMI or POST /api/tags does.
func retune(t *testing.T, rt *Runtime) {
	t.Helper()
	for path, v := range map[string]any{
		"Tbl[2]":        12.5,
		"Tbl[4]":        40.25,
		"Counts[1]":     int64(7),
		"Steps[2].Mode": "Run",
		"Steps[2].Secs": 30.0,
		"Grid[2][1]":    int64(21),
		"Grid[1][2]":    int64(12),
		"Flags[2]":      true,
		"Delays[1]":     int64(1500),
		"Names[2]":      "batch-7",
		"SP":            72.5,
		"Enable":        true,
		"Delay":         int64(2500),
		"Batch":         "batch-9",
		"Rec.Mode":      "Fault",
		"Rec.Secs":      12.0,
		"Cmd":           "Run",
	} {
		if err := rt.Tags().SetPath(path, v); err != nil {
			t.Fatalf("SetPath(%s): %v", path, err)
		}
	}
}

// assertRetuned checks every retuned element — and that the kinds came
// back as declared: an INT element must not return as a REAL, and an
// enumeration element must come back named.
func assertRetuned(t *testing.T, rt *Runtime) {
	t.Helper()
	want := map[string]any{
		"Tbl[1]":        1.0, // untouched: the seed
		"Tbl[2]":        12.5,
		"Tbl[4]":        40.25,
		"Counts[1]":     int64(7),
		"Steps[1].Mode": "Idle",
		"Steps[2].Mode": "Run",
		"Steps[2].Secs": 30.0,
		"Grid[2][1]":    int64(21),
		"Grid[1][2]":    int64(12),
		"Flags[1]":      false,
		"Flags[2]":      true,
		"Delays[1]":     int64(1500),
		"Names[2]":      "batch-7",
		"SP":            72.5,
		"Enable":        true,
		"Delay":         int64(2500),
		"Batch":         "batch-9",
		"Rec.Mode":      "Fault",
		"Rec.Secs":      12.0,
		"Cmd":           "Run",
	}
	for path, w := range want {
		got, ok := rt.Tags().ReadPath(path)
		if !ok {
			t.Errorf("%s: unreadable", path)
			continue
		}
		if got != w {
			t.Errorf("%s = %v (%T), want the retained %v (%T)", path, got, got, w, w)
		}
	}
	// ReadPath collapses INT and TIME alike to int64; the kind is what
	// says a TIME came back as a TIME.
	for name, kind := range map[string]ir.TypeKind{"Delay": ir.TypeTime, "Counts": ir.TypeArray, "Grid": ir.TypeArray} {
		if v, err := rt.Tags().ReadGlobal(name); err != nil || v.Kind != kind {
			t.Errorf("%s: kind %v (err %v), want %v", name, v.Kind, err, kind)
		}
	}
	for path, kind := range map[string]ir.TypeKind{"Delays[1]": ir.TypeTime, "Counts[1]": ir.TypeInt, "Grid[2][1]": ir.TypeInt} {
		root, segs := ir.SplitAddress(path)
		v, _ := rt.Tags().ReadGlobal(root)
		if e, _, err := ir.FieldAt(v, rt.Tags().TypeOf(root), segs, root); err != nil || e.Kind != kind {
			t.Errorf("%s: kind %v (err %v), want %v", path, e.Kind, err, kind)
		}
	}
	if s := rt.Stats(); s.RetainErrors != 0 {
		t.Errorf("the load reported %d retain errors: %s", s.RetainErrors, s.LastRetainError)
	}
}

func fileStore(t *testing.T, path string) retain.Store {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "") // the file store, whatever the CI box is
	return retain.New("unused", path)
}

// Save from one runtime, load into a fresh one from the same file: the
// restart a power cycle or a redeploy is.
func TestRetainedArraySetpointsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retain.json")

	before := tableRuntime(t, fileStore(t, path), nil)
	before.Scan()
	retune(t, before)
	before.saveRetained(nil)

	after := tableRuntime(t, fileStore(t, path), nil)
	after.Scan() // the first scan's takeover is what loads the store
	assertRetuned(t, after)
}

// The failover path in a cluster: the leader saves to the ConfigMap, steps
// down, and the standby's takeover re-reads the ConfigMap before its first
// scan. Both replicas talk to the same (fake) API server, exactly as two
// pods talk to one.
func TestRetainedArraySetpointsSurviveFailover(t *testing.T) {
	srv := httptest.NewServer(newFakeConfigMaps())
	defer srv.Close()
	client := &k8sapi.Client{Base: srv.URL, Namespace: "plant", HTTP: srv.Client()}

	leaderFlag, standbyFlag := &flag{lead: true}, &flag{lead: false}
	leader := tableRuntime(t, retain.NewConfigMap(client, "line1-retain"), leaderFlag)
	standby := tableRuntime(t, retain.NewConfigMap(client, "line1-retain"), standbyFlag)

	leader.Scan()
	standby.Scan() // standing by: no takeover, no load
	retune(t, leader)
	leader.saveRetained(nil)
	if n := standby.Stats().Count; n != 0 {
		t.Fatalf("standby scanned %d times before the failover", n)
	}

	// The lease moves.
	leaderFlag.lead, standbyFlag.lead = false, true
	leader.Scan()
	standby.Scan()
	assertRetuned(t, standby)
}

// A forced element is retained at its ACTUAL value: forces are not
// retained, so the forced value must not ride into the store under the
// array's name and come back as a plain value after a restart.
func TestRetainedArraySavesActualNotForced(t *testing.T) {
	rt := tableRuntime(t, &fakeStore{}, nil)
	rt.Scan()
	if err := rt.Tags().SetPath("Tbl[2]", 12.5); err != nil {
		t.Fatal(err)
	}
	if err := rt.Tags().Force("Tbl[2]", 99.0); err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.Tags().ReadPath("Tbl[2]"); got != 99.0 {
		t.Fatalf("Tbl[2] reads %v, want the forced 99 (the force did not take)", got)
	}
	list, ok := rt.retainState().Tags["Tbl"].([]any)
	if !ok {
		t.Fatalf("Tbl was not persisted: %+v", rt.retainState().Tags)
	}
	if want := []any{1.0, 12.5, 3.0, 4.0}; !sameList(list, want) {
		t.Fatalf("retained Tbl = %v, want the actual %v (not the forced 99)", list, want)
	}
}

// A store written before arrays were retained has no entry for them: it
// must load as before — the scalar setpoint restored, the arrays at their
// seeds, no retain error. A regression guard on the compatibility promise;
// it passed before the fix as well, by construction.
func TestRetainFileWithoutArraysStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retain.json")
	old := `{
  "tags": {
    "SP": 72.5
  },
  "programs": {}
}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := tableRuntime(t, fileStore(t, path), nil)
	rt.Scan()
	if got := rt.Tags().Real("SP"); got != 72.5 {
		t.Fatalf("SP = %v, want the retained 72.5", got)
	}
	if got, _ := rt.Tags().ReadPath("Tbl[2]"); got != 2.0 {
		t.Fatalf("Tbl[2] = %v, want its seed 2.0", got)
	}
	if s := rt.Stats(); s.RetainErrors != 0 {
		t.Fatalf("an old-format file raised a retain error: %s", s.LastRetainError)
	}
}

// A retained list longer than the declared array (the project shrank the
// table) must not half-restore: the tag keeps its seed and the mismatch
// shows in diagnostics. A shorter list (the table grew) restores its
// prefix and the new elements keep their seeds.
func TestRetainedArrayLengthChange(t *testing.T) {
	store := &fakeStore{state: retain.State{Tags: map[string]any{
		"Tbl":    []any{9.0, 9.0, 9.0, 9.0, 9.0},
		"Counts": []any{5.0},
	}}}
	rt := tableRuntime(t, store, nil)
	rt.Scan()
	if got, _ := rt.Tags().ReadPath("Tbl[1]"); got != 1.0 {
		t.Errorf("Tbl[1] = %v, want its seed: a too-long retained list must not restore", got)
	}
	if s := rt.Stats(); s.RetainErrors == 0 || !strings.Contains(s.LastRetainError, "Tbl") {
		t.Errorf("the length mismatch left no trace in diagnostics: retainErrors=%d lastRetainError=%q",
			s.RetainErrors, s.LastRetainError)
	}
	if got, _ := rt.Tags().ReadPath("Counts[0]"); got != int64(5) {
		t.Errorf("Counts[0] = %v (%T), want the retained INT 5", got, got)
	}
	for path, seed := range map[string]int64{"Counts[1]": 101, "Counts[2]": 102} {
		if got, _ := rt.Tags().ReadPath(path); got != seed {
			t.Errorf("%s = %v, want its seed %d: an element the store did not hold must keep it", path, got, seed)
		}
	}
}

// An array or struct holding function-block instances has no value to
// persist and cannot be written back: it is left out (and logged once), and
// the rest of the state still saves.
//
// No project reaches this: the tag model refuses a block, or a type
// holding one, both as a manifest tag's type: and as the type a program
// declares for a manifest tag. The skip stays as defense in depth, so this
// drives retain directly, writing the values into the store.
func TestRetainSkipsArrayOfFunctionBlocks(t *testing.T) {
	fbs := ir.Value{Kind: ir.TypeArray, Arr: []ir.Value{{Kind: ir.TypeFB}}}
	if retainable(fbs) {
		t.Fatal("an array of FB instances reads as retainable")
	}
	holder := ir.Value{Kind: ir.TypeStruct, Fld: []ir.Value{ir.IntVal(1), {Kind: ir.TypeFB}}}
	if retainable(holder) {
		t.Fatal("a struct with an FB member reads as retainable")
	}
	nested := ir.Value{Kind: ir.TypeArray, Arr: []ir.Value{{Kind: ir.TypeArray, Arr: []ir.Value{ir.IntVal(1)}}}}
	if !retainable(nested) {
		t.Fatal("an array of arrays of INT reads as unretainable")
	}
	// The manifest routes are closed: typed, and untyped with the program
	// declaring the block.
	for _, d := range []TagDef{Typed("Timers", RoleSetpoint, "ARRAY[1..2] OF TON"), {Name: "Timers", Role: RoleSetpoint}} {
		if _, err := New(Options{
			Program: `PROGRAM Main VAR_EXTERNAL Timers : ARRAY[1..2] OF TON; END_VAR END_PROGRAM`,
			Tags:    []TagDef{d},
		}); err == nil || !errors.Is(err, st.ErrFunctionBlockType) {
			t.Fatalf("tag %+v: err = %v, want the function-block refusal", d, err)
		}
	}
	rt, err := New(Options{
		Program:    `PROGRAM Main VAR_EXTERNAL SP : REAL; END_VAR END_PROGRAM`,
		Tags:       []TagDef{Setpoint("SP", 65.0)},
		RetainTags: []string{"Timers", "Station", "SP"},
		Retain:     &fakeStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = rt.Tags().WriteGlobal("Timers", fbs)
	_ = rt.Tags().WriteGlobal("Station", holder)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	rt.Scan()
	state := rt.retainState()
	for _, name := range []string{"Timers", "Station"} {
		if _, ok := state.Tags[name]; ok {
			t.Errorf("%s, which holds an FB instance, was persisted", name)
		}
	}
	if _, ok := state.Tags["SP"]; !ok {
		t.Fatal("skipping one tag dropped the rest of the state")
	}
	rt.retainState() // a second saver tick
	for _, name := range []string{"Timers", "Station"} {
		if n := strings.Count(logs.String(), "tag="+name); n != 1 {
			t.Errorf("the skipped %s was named %d times in the log, want once:\n%s", name, n, logs.String())
		}
	}
}

func sameList(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeConfigMaps is the slice of the Kubernetes API the ConfigMap store
// speaks: GET and PUT one ConfigMap by name, POST to create it.
type fakeConfigMaps struct {
	mu   sync.Mutex
	maps map[string][]byte
}

func newFakeConfigMaps() *fakeConfigMaps { return &fakeConfigMaps{maps: map[string][]byte{}} }

func (f *fakeConfigMaps) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet, http.MethodPut:
		name := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		b, ok := f.maps[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPut {
			b, _ = io.ReadAll(r.Body)
			f.maps[name] = b
		}
		_, _ = w.Write(b)
	case http.MethodPost:
		b, _ := io.ReadAll(r.Body)
		var cm struct {
			Metadata struct{ Name string } `json:"metadata"`
		}
		if err := json.Unmarshal(b, &cm); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.maps[cm.Metadata.Name] = b
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}
