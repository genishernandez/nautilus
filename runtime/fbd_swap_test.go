package runtime

import (
	"strings"
	"testing"
	"time"
)

// An FBD program holding a PID with an accumulated integral and a TON partway
// to its preset. fbdBlocksV2 adds a coil and changes nothing else, so both
// instances must ride through the warm swap.
const fbdBlocksV1 = `PROGRAM Main
VAR_EXTERNAL
  Run : BOOL;
  ITerm : REAL;
  Done : BOOL;
  Elapsed : TIME;
END_VAR
VAR
  loop : PID;
  dwell : TON;
END_VAR
FBD
  loop : PID(AUTO := TRUE, PV := 0.0, SP := 1.0, KP := 1.0, KI := 1.0, DT := 1.0)
  ITerm := loop.I_TERM
  dwell : TON(IN := Run, PT := T#10S)
  Done := dwell.Q
  Elapsed := dwell.ET
END_FBD
END_PROGRAM`

var fbdBlocksV2 = strings.Replace(
	strings.Replace(fbdBlocksV1, "Elapsed : TIME;", "Elapsed : TIME;\n  Lamp : BOOL;", 1),
	"END_FBD", "  Lamp := Run\nEND_FBD", 1)

func TestSwapWarmFBDKeepsPIDIntegralAndRunningTON(t *testing.T) {
	p, err := Compile(fbdBlocksV1)
	if err != nil {
		t.Fatal(err)
	}
	clk := &stepClock{t: time.Unix(1_700_000_000, 0)}
	t0 := clk.t
	tags := NewTags()
	tags.clock = clk
	tags.SetBool("Run", true)
	scanAt := func(sec int) {
		t.Helper()
		clk.t = t0.Add(time.Duration(sec) * time.Second)
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	for s := 0; s <= 4; s++ {
		scanAt(s)
	}
	before := readTag(t, tags, "ITerm").F
	if before <= 0 {
		t.Fatalf("integral term before the swap = %v, want accumulated and positive", before)
	}
	if readTag(t, tags, "Done").B {
		t.Fatal("TON done 4 s into a 10 s preset")
	}

	report, err := p.SwapWarm(fbdBlocksV2)
	if err != nil {
		t.Fatalf("warm swap: %v", err)
	}
	for _, r := range report.Resets {
		switch strings.ToLower(r) {
		case "loop", "dwell":
			t.Errorf("%s reset by an edit that only added a coil (resets %v)", r, report.Resets)
		}
	}
	if p.Source() != fbdBlocksV2 {
		t.Fatal("the swapped .fbd must be the program of record")
	}

	// One scan later the integral has grown by one more step: it carried, a
	// reset PID would restart from a single step.
	scanAt(5)
	after := readTag(t, tags, "ITerm").F
	if want := before + before/4; after != want {
		t.Fatalf("integral after the swap = %v, want %v (before %v, +1 step; carried)", after, want, before)
	}
	if !readTag(t, tags, "Lamp").B {
		t.Fatal("the added coil did not go live")
	}
	// The TON that started at 0 s must be done at 10 s: a reset would restart
	// its timing at the swap and still be running.
	scanAt(9)
	if readTag(t, tags, "Done").B {
		t.Fatal("TON done at 9 s, before its 10 s preset")
	}
	scanAt(10)
	if !readTag(t, tags, "Done").B {
		t.Fatal("TON not done at 10 s: its timing restarted at the swap instead of carrying")
	}
	if got := readTag(t, tags, "Elapsed"); got.I != int64(10*time.Second/time.Millisecond) {
		t.Fatalf("ET = %+v, want 10 s", got)
	}
}

// Migration is by name AND type: an FBD edit that changes a variable's type
// gets a fresh, reported variable instead of a carried one.
func TestSwapWarmFBDTypeChangeResets(t *testing.T) {
	p, err := Compile(fbdV1)
	if err != nil {
		t.Fatal(err)
	}
	tags := NewTags()
	tags.SetReal("In1", 1.0)
	tags.SetReal("Out1", 0)
	for i := 0; i < 3; i++ {
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	retyped := strings.Replace(fbdV1, "integral : REAL;", "integral : INT;", 1)
	retyped = strings.Replace(retyped, "ADD(integral, In1)", "ADD(integral, 1)", 1)
	retyped = strings.Replace(retyped, "Out1 := integral", "Out1 := INT_TO_REAL(integral)", 1)
	report, err := p.SwapWarm(retyped)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range report.Resets {
		if strings.EqualFold(r, "integral") {
			found = true
		}
	}
	if !found {
		t.Fatalf("resets = %v, want integral reported (REAL -> INT)", report.Resets)
	}
	if err := p.Run(tags); err != nil {
		t.Fatal(err)
	}
	if got := tags.Real("Out1"); got != 1.0 {
		t.Fatalf("Out1 after retyped swap = %v, want 1 (integral restarted, not carried 3)", got)
	}
}
