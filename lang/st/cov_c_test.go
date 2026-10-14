package st

import (
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// An array of TON instances called by a VARIABLE index in ST: each element
// keeps its own elapsed time across scans (it is not called on the scans
// the index points elsewhere), the index picks the element at run time,
// and elements never called stay idle.
func TestFBInstanceArrayVariableIndexRuns(t *testing.T) {
	src := `
PROGRAM main
VAR_GLOBAL
  run : BOOL;
  k   : INT;
  q   : ARRAY[0..3] OF BOOL;
END_VAR
VAR
  Timers : ARRAY[0..3] OF TON;
  i : INT;
END_VAR
Timers[k](IN := run, PT := T#2S);
FOR i := 0 TO 3 DO
  q[i] := Timers[i].Q;
END_FOR;
END_PROGRAM
`
	host := newFakeHost()
	host.now = 1_700_000_000_000
	irProg := mustLower(t, src)
	frame := ir.NewFrame(irProg)
	host.vals["run"] = ir.BoolVal(true)
	at := func(k int64, want [4]bool) {
		t.Helper()
		host.vals["k"] = ir.IntVal(k)
		if err := ir.Run(irProg, frame, host); err != nil {
			t.Fatalf("run: %v", err)
		}
		q := host.vals["q"]
		var got [4]bool
		for i := range got {
			got[i] = q.Arr[i].B
		}
		if got != want {
			t.Fatalf("k=%d at t+%dms: q = %v, want %v", k, host.now-1_700_000_000_000, got, want)
		}
	}
	at(1, [4]bool{}) // t=0: Timers[1] starts
	host.now += 1000
	at(2, [4]bool{}) // t=1s: Timers[2] starts; Timers[1] is not called
	host.now += 1000
	at(1, [4]bool{false, true, false, false}) // t=2s: Timers[1]'s own 2 s are up
	at(2, [4]bool{false, true, false, false}) // same instant: Timers[2] has 1 s
	host.now += 1000
	at(2, [4]bool{false, true, true, false}) // t=3s: Timers[2]'s own 2 s are up
}
