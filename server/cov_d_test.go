package server

import (
	"encoding/json"
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// docs/functions.md#identifiers-are-case-insensitive: forces accept any
// casing. A force sent over HTTP by another casing lands on the declared
// tag (reported under its declared spelling), and an unforce by a third
// casing removes it.
func TestForceAPIAnyCasing(t *testing.T) {
	rt := newTestRuntime(t)
	srv := New(rt)
	before := len(rt.Tags().All())

	rec := call(t, srv, "POST", "/api/forces", `{"name":"level","value":12}`)
	if rec.Code != 200 {
		t.Fatalf("force by another casing: %d %s", rec.Code, rec.Body)
	}
	var got forcesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Forces) != 1 || got.Forces[0].Name != "Level" {
		t.Fatalf("force response = %s, want one force on the declared spelling Level", rec.Body)
	}
	rt.Scan() // the Memory driver re-delivers Level = 40; the force holds
	if lvl := rt.Tags().Real("Level"); lvl != 12 {
		t.Fatalf("Level = %v under a force of 12 sent as \"level\"", lvl)
	}
	if after := len(rt.Tags().All()); after != before {
		t.Errorf("a force by another casing created a tag: %d -> %d", before, after)
	}

	if rec := call(t, srv, "DELETE", "/api/forces/LEVEL", ""); rec.Code != 200 {
		t.Fatalf("unforce by another casing: %d %s", rec.Code, rec.Body)
	}
	rt.Scan()
	if lvl := rt.Tags().Real("Level"); lvl != 40 {
		t.Fatalf("Level = %v after unforce by \"LEVEL\"; want the field's 40", lvl)
	}
}

// docs/functions.md#operators: every divide-by-zero bumps the
// controller-wide divZero counter in the scan stats served at /api/state,
// counted since start. The program divides by zero once per scan, so after
// n scans the frame carries n.
func TestStateCarriesDivZeroCount(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: `PROGRAM DivZ
VAR_EXTERNAL
  Num : REAL;
  Den : REAL;
  Q : REAL;
END_VAR
Q := Num / Den;
END_PROGRAM
`,
		Driver: nio.NewMemory(),
		Seed:   nio.Values{"Num": 6.0, "Den": 0.0, "Q": 0.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(rt)
	divZero := func() float64 {
		t.Helper()
		var raw map[string]any
		if err := json.Unmarshal(call(t, srv, "GET", "/api/state", "").Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		scan, ok := raw["scan"].(map[string]any)
		if !ok {
			t.Fatalf("/api/state has no scan stats block: %v", raw)
		}
		n, ok := scan["divZero"].(float64)
		if !ok {
			t.Fatalf("/api/state scan stats have no divZero: %v", scan)
		}
		return n
	}
	if n := divZero(); n != 0 {
		t.Fatalf("divZero = %v before any scan, want 0", n)
	}
	for i := 0; i < 3; i++ {
		rt.Scan()
	}
	if n := divZero(); n != 3 {
		t.Fatalf("divZero = %v after 3 scans dividing by zero, want 3", n)
	}
	// A scan with a nonzero divisor adds nothing; the count is since start,
	// not per scan.
	if err := rt.Tags().SetPath("Den", 2.0); err != nil {
		t.Fatal(err)
	}
	rt.Scan()
	if n := divZero(); n != 3 {
		t.Fatalf("divZero = %v after a clean scan, want it to stay 3 (counted since start)", n)
	}
}
