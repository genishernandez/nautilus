package runtime_test

import (
	"testing"

	"github.com/joyautomation/nautilus/runtime"
)

// TestB1FBDDivModZeroENOAndDivZero: FBD's DIV and MOD with EN/ENO report a
// zero divisor as ENO FALSE, the result still follows the ÷0 rule (0), and
// each one is counted in the controller's divZero stat while the scan keeps
// running.
func TestB1FBDDivModZeroENOAndDivZero(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: "PROGRAM P\nVAR_EXTERNAL N : DINT; D : DINT; Q : DINT; M : DINT; QOk : BOOL; MOk : BOOL; END_VAR\n" +
			"FBD\n" +
			"  q = DIV(EN := TRUE, N, D)\n  Q := q\n  QOk := q.ENO\n" +
			"  m = MOD(EN := TRUE, N, D)\n  M := m\n  MOk := m.ENO\n" +
			"END_FBD\nEND_PROGRAM\n",
		Tags: []runtime.TagDef{
			runtime.State("N", int64(17)), runtime.State("D", int64(5)),
			runtime.State("Q", int64(99)), runtime.State("M", int64(99)),
			runtime.State("QOk", false), runtime.State("MOk", false),
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rt.Scan()
	snap := rt.Tags().Snapshot()
	if snap["Q"].I != 3 || snap["M"].I != 2 || !snap["QOk"].B || !snap["MOk"].B {
		t.Fatalf("17 / 5: Q=%v M=%v QOk=%v MOk=%v, want 3 2 true true", snap["Q"].I, snap["M"].I, snap["QOk"].B, snap["MOk"].B)
	}
	if st := rt.Stats(); st.DivZero != 0 {
		t.Fatalf("a good divisor counts nothing: divZero=%d", st.DivZero)
	}

	rt.Tags().Set("D", int64(0))
	rt.Scan()
	snap = rt.Tags().Snapshot()
	if snap["Q"].I != 0 || snap["M"].I != 0 || snap["QOk"].B || snap["MOk"].B {
		t.Fatalf("17 / 0: Q=%v M=%v QOk=%v MOk=%v, want 0 0 false false", snap["Q"].I, snap["M"].I, snap["QOk"].B, snap["MOk"].B)
	}
	st := rt.Stats()
	if st.LogicErrors != 0 || st.DivZero != 2 {
		t.Fatalf("17 / 0: want 0 logic errors and divZero=2 (DIV and MOD), got %d / %d (%q)", st.LogicErrors, st.DivZero, st.LastError)
	}
	rt.Scan()
	if st = rt.Stats(); st.DivZero != 4 || st.LogicErrors != 0 {
		t.Fatalf("the scan keeps running and counting: divZero=%d logicErrors=%d, want 4 0", st.DivZero, st.LogicErrors)
	}
}
