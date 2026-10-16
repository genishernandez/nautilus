package ld

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/fbd"
	"github.com/joyautomation/nautilus/lang/ir"
)

// cScanner compiles composed ST and returns a scan function over one frame
// and host, so a test can drive inputs and read outputs across scans.
func cScanner(t *testing.T, stSrc string) (*host, func()) {
	t.Helper()
	prog := compileST(t, stSrc)
	h := newHost()
	frame := ir.NewFrame(prog)
	return h, func() {
		t.Helper()
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
}

// A coil ahead of a later function block in the same rung is the compile
// error "coils must sit at the rung's right end" — the coil-before-FB case,
// as well as a coil ahead of a contact (TestRungErrors).
func TestCoilBeforeFunctionBlockRefused(t *testing.T) {
	for name, body := range map[string]string{
		"coil before a rung-final FB":      "Start ( Run ) t1:TON(PT := T#1S)",
		"coil before an FB and its coil":   "Start ( Run ) t1:TON(PT := T#1S) ( Alm )",
		"coil before a contact and a coil": "( Run ) Start ( Alm )",
	} {
		src := "PROGRAM p\nVAR_EXTERNAL Start : BOOL; Run : BOOL; Alm : BOOL; END_VAR\nLD\n  RUNG r\n    " + body + "\nEND_LD\nEND_PROGRAM"
		_, err := Transpile(src)
		if err == nil || !strings.Contains(err.Error(), "coils must sit at the rung's right end") {
			t.Errorf("%s: err = %v, want \"coils must sit at the rung's right end\"", name, err)
		}
	}
	// The same elements with the output zone contiguous at the right end
	// are fine: a rung-final FB, or an FB followed by its coils.
	for _, body := range []string{"Start t1:TON(PT := T#1S)", "Start t1:TON(PT := T#1S) ( Run ) ( Alm )"} {
		src := "PROGRAM p\nVAR_EXTERNAL Start : BOOL; Run : BOOL; Alm : BOOL; END_VAR\nLD\n  RUNG r\n    " + body + "\nEND_LD\nEND_PROGRAM"
		if _, err := Transpile(src); err != nil {
			t.Errorf("%q: unexpected error %v", body, err)
		}
	}
}

// The BOOL rule: a comparison is a valid contact and runs; a numeric
// function used as a contact is a compile error with the page's wording —
// "operator AND on BOOL and REAL" in series with another contact, "cannot
// assign REAL to BOOL" alone on the rung — never a pass-through.
func TestNumericFunctionContactIsACompileError(t *testing.T) {
	compile := func(rung string) error {
		src := "PROGRAM p\nVAR_EXTERNAL TempC : REAL; Run : BOOL; Out : BOOL; END_VAR\nLD\n  RUNG r\n    " + rung + "\nEND_LD\nEND_PROGRAM"
		out, err := Transpile(src)
		if err != nil {
			return err
		}
		_, err = fbd.Compile(out)
		return err
	}
	if err := compile("Run ADD(TempC, 0.0) ( Out )"); err == nil || !strings.Contains(err.Error(), "operator AND on BOOL and REAL") {
		t.Errorf("ADD in series: err = %v, want \"operator AND on BOOL and REAL\"", err)
	}
	if err := compile("ADD(TempC, 0.0) ( Out )"); err == nil || !strings.Contains(err.Error(), "cannot assign REAL to BOOL") {
		t.Errorf("ADD alone: err = %v, want \"cannot assign REAL to BOOL\"", err)
	}

	// Every comparison is a valid contact, and passes power exactly when it
	// is TRUE.
	for _, c := range []struct {
		fn         string
		lo, eq, hi bool // result for TempC below, at, above 50
	}{
		{"GT", false, false, true},
		{"GE", false, true, true},
		{"LT", true, false, false},
		{"LE", true, true, false},
		{"EQ", false, true, false},
		{"NE", true, false, true},
	} {
		src := "PROGRAM p\nVAR_EXTERNAL TempC : REAL; Out : BOOL; END_VAR\nLD\n  RUNG r\n    " + c.fn + "(TempC, 50.0) ( Out )\nEND_LD\nEND_PROGRAM"
		h, scan := cScanner(t, toST(t, src))
		for i, temp := range []float64{40, 50, 60} {
			h.vals["TempC"] = ir.RealVal(temp)
			scan()
			want := []bool{c.lo, c.eq, c.hi}[i]
			if got := h.vals["Out"].B; got != want {
				t.Errorf("%s(TempC=%v, 50.0) contact: Out = %v, want %v", c.fn, temp, got, want)
			}
		}
	}
}

// Two instances of one ladder FUNCTION_BLOCK are two pumps: each keeps its
// own seal-in and its own t1 across scans, and driving one never moves the
// other.
func TestLadderFunctionBlockTwoInstancesIndependent(t *testing.T) {
	src := pumpSeq + `

PROGRAM Main
VAR_EXTERNAL Cmd1 : BOOL; Cmd2 : BOOL; Lvl : REAL; Run1 : BOOL; Run2 : BOOL; Warm1 : BOOL; Warm2 : BOOL; END_VAR
VAR p1 : PumpSeq; p2 : PumpSeq; END_VAR
LD
  RUNG pump1  Cmd1 p1:PumpSeq(Level := Lvl, Run => Run1)
  RUNG pump2  Cmd2 p2:PumpSeq(Level := Lvl, Run => Run2)
  RUNG warm1  p1.t1.Q ( Warm1 )
  RUNG warm2  p2.t1.Q ( Warm2 )
END_LD
END_PROGRAM`
	h, scan := cScanner(t, toST(t, src))
	want := func(step string, run1, run2, warm1, warm2 bool) {
		t.Helper()
		got := [4]bool{h.vals["Run1"].B, h.vals["Run2"].B, h.vals["Warm1"].B, h.vals["Warm2"].B}
		if got != [4]bool{run1, run2, warm1, warm2} {
			t.Fatalf("%s: Run1, Run2, Warm1, Warm2 = %v, want %v", step, got, [4]bool{run1, run2, warm1, warm2})
		}
	}
	h.vals["Lvl"] = ir.RealVal(50)

	// Start pump 1 only, then release its command: its seal-in holds and
	// pump 2 never started.
	h.vals["Cmd1"] = ir.BoolVal(true)
	scan()
	want("start p1", true, false, false, false)
	h.vals["Cmd1"] = ir.BoolVal(false)
	scan()
	want("p1 sealed in", true, false, false, false)

	// Three seconds later start pump 2: p1's t1 has been timing for three
	// seconds, p2's starts now.
	h.now += 3000
	h.vals["Cmd2"] = ir.BoolVal(true)
	scan()
	want("start p2", true, true, false, false)
	h.vals["Cmd2"] = ir.BoolVal(false)

	// Five and a half seconds after p1 started, only p1's own timer is done.
	h.now += 2500
	scan()
	want("p1's t1 done, p2's not", true, true, true, false)

	// Five seconds after p2 started, p2's timer is done too.
	h.now += 2600
	scan()
	want("both timers done", true, true, true, true)
}

// User-block power pins beyond the first-BOOL rule: a declared EN takes
// the power even when an earlier BOOL input is free, and an earlier BOOL
// input the call binds by name is skipped for the next free one.
func TestLadderFunctionBlockPowerPinPreference(t *testing.T) {
	lib := `FUNCTION_BLOCK Gate
VAR_INPUT  Arm : BOOL; EN : BOOL; END_VAR
VAR_OUTPUT Q : BOOL; ENO : BOOL; END_VAR
Q := TRUE;
ENO := EN;
END_FUNCTION_BLOCK

FUNCTION_BLOCK Latch
VAR_INPUT  Set : BOOL; Clear : BOOL; END_VAR
VAR_OUTPUT Q : BOOL; END_VAR
Q := (Q OR Set) AND NOT Clear;
END_FUNCTION_BLOCK`

	t.Run("a declared EN wins over an earlier free BOOL input; ENO over the first BOOL output", func(t *testing.T) {
		got := toST(t, `PROGRAM p
VAR_EXTERNAL A : BOOL; Out : BOOL; END_VAR
VAR g : Gate; END_VAR
LD
  RUNG r  A g:Gate() ( Out )
END_LD
END_PROGRAM`, lib)
		if !strings.Contains(got, "g(EN := A") || strings.Contains(got, "Arm := A") {
			t.Fatalf("power must land on the declared EN, not the first BOOL input Arm:\n%s", got)
		}
		h, scan := cScanner(t, lib+"\n"+got)
		for _, a := range []bool{true, false, true} {
			h.vals["A"] = ir.BoolVal(a)
			scan()
			if h.vals["Out"].B != a {
				t.Fatalf("A=%v: Out=%v; power should enter on EN and leave on ENO (= EN)", a, h.vals["Out"].B)
			}
		}
	})

	t.Run("an earlier BOOL input bound by name is skipped", func(t *testing.T) {
		got := toST(t, `PROGRAM p
VAR_EXTERNAL A : BOOL; S : BOOL; Out : BOOL; END_VAR
VAR l1 : Latch; END_VAR
LD
  RUNG r  A l1:Latch(Set := S) ( Out )
END_LD
END_PROGRAM`, lib)
		if !strings.Contains(got, "Clear := A") || strings.Contains(got, "Set := A") {
			t.Fatalf("power must land on Clear, the first BOOL input the call leaves free:\n%s", got)
		}
		h, scan := cScanner(t, lib+"\n"+got)
		step := func(a, s, want bool) {
			t.Helper()
			h.vals["A"], h.vals["S"] = ir.BoolVal(a), ir.BoolVal(s)
			scan()
			if h.vals["Out"].B != want {
				t.Fatalf("A=%v S=%v: Out=%v, want %v", a, s, h.vals["Out"].B, want)
			}
		}
		step(false, true, true)  // Set by name latches
		step(false, false, true) // holds
		step(true, false, false) // rung power clears
	})
}

// A block with no BOOL input at all has no power-in: behind a contact it is
// the error "no free BOOL input for the rung's power"; on a rail-driven
// rung it is called unconditionally, every scan.
func TestLadderFunctionBlockWithNoBoolInputs(t *testing.T) {
	lib := `FUNCTION_BLOCK Acc
VAR_INPUT  Rate : REAL; END_VAR
VAR_OUTPUT Total : REAL; END_VAR
Total := Total + Rate;
END_FUNCTION_BLOCK`
	gated := `PROGRAM p
VAR_EXTERNAL A : BOOL; R : REAL; Sum : REAL; END_VAR
VAR a1 : Acc; END_VAR
LD
  RUNG r  A a1:Acc(Rate := R, Total => Sum)
END_LD
END_PROGRAM`
	if _, err := Transpile(gated, lib); err == nil || !strings.Contains(err.Error(), "no free BOOL input for the rung's power") {
		t.Fatalf("err = %v, want \"no free BOOL input for the rung's power\"", err)
	}
	rail := strings.Replace(gated, "RUNG r  A a1:", "RUNG r  a1:", 1)
	got := toST(t, rail, lib)
	h, scan := cScanner(t, lib+"\n"+got)
	h.vals["R"] = ir.RealVal(2)
	for i, want := range []float64{2, 4, 6} {
		scan()
		if got := h.vals["Sum"].F; got != want {
			t.Fatalf("scan %d: Sum = %v, want %v (called on every scan)", i+1, got, want)
		}
	}
}

// A block whose type nothing in the compile declares falls back to IN/Q:
// power lands on IN and continues from Q.
func TestLadderUndeclaredBlockFallsBackToINQ(t *testing.T) {
	out, err := Transpile(`PROGRAM p
VAR_EXTERNAL A : BOOL; Out : BOOL; END_VAR
LD
  RUNG r  A x:Mystery(PT := T#1S) ( Out )
END_LD
END_PROGRAM`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "IN := A") || !strings.Contains(out, "Out := x.Q") {
		t.Fatalf("an undeclared block should take power on IN and pass it on from Q:\n%s", out)
	}
}

// An element of an array of instances called by a VARIABLE index in a rung:
// each element keeps its own timer across scans, and the index picks the
// element at run time.
func TestFBInstanceArrayVariableIndexInLadderRuns(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Run : BOOL; K : INT; Q0 : BOOL; Q1 : BOOL; Q2 : BOOL; END_VAR
VAR Timers : ARRAY [0..3] OF TON; END_VAR
LD
  RUNG call  Run Timers[K]:TON(PT := T#2S)
  RUNG q0    Timers[0].Q ( Q0 )
  RUNG q1    Timers[1].Q ( Q1 )
  RUNG q2    Timers[2].Q ( Q2 )
END_LD
END_PROGRAM`
	h, scan := cScanner(t, toST(t, src))
	h.vals["Run"] = ir.BoolVal(true)
	at := func(k int64, q0, q1, q2 bool) {
		t.Helper()
		h.vals["K"] = ir.IntVal(k)
		scan()
		got := [3]bool{h.vals["Q0"].B, h.vals["Q1"].B, h.vals["Q2"].B}
		if got != [3]bool{q0, q1, q2} {
			t.Fatalf("t+%dms K=%d: Q0..Q2 = %v, want %v", h.now-1_700_000_000_000, k, got, [3]bool{q0, q1, q2})
		}
	}
	at(1, false, false, false) // t=0: Timers[1] starts
	h.now += 1000
	at(2, false, false, false) // t=1s: Timers[2] starts; Timers[1] not called
	h.now += 1000
	at(1, false, true, false) // t=2s: Timers[1] has its own 2 s
	at(2, false, true, false) // same instant: Timers[2] has only 1 s
	h.now += 1000
	at(2, false, true, true) // t=3s: Timers[2] reaches its own 2 s
}
