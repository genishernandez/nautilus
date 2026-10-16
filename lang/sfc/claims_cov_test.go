package sfc

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// Behaviour tests that pin individual promises of the SFC language page (see
// docs/claims/sfc.yaml). Everything runs in virtual time: the host clock is
// set by hand before each scan.

// logHost is a clockHost that also records every global write, in order, so a
// test can tell "written FALSE once" from "held FALSE".
type logHost struct {
	*clockHost
	writes []string // "Name=true|false"
}

func (h *logHost) WriteGlobal(name string, v ir.Value) error {
	if v.Kind == ir.TypeBool {
		s := "false"
		if v.B {
			s = "true"
		}
		h.writes = append(h.writes, name+"="+s)
	}
	return h.clockHost.WriteGlobal(name, v)
}

// atScanner compiles src and returns a driver that sets inputs, sets the
// clock to an absolute time, runs one scan, and returns the writes that scan
// made.
func atScanner(t *testing.T, src string) (*ir.Program, *ir.Frame, *logHost, func(now int64, in map[string]ir.Value) []string) {
	t.Helper()
	prog := mustCompile(t, src)
	h := &logHost{clockHost: &clockHost{vals: map[string]ir.Value{}}}
	frame := ir.NewFrame(prog)
	run := func(now int64, in map[string]ir.Value) []string {
		t.Helper()
		for k, v := range in {
			h.vals[k] = v
		}
		h.now = now
		h.writes = nil
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
		return append([]string(nil), h.writes...)
	}
	return prog, frame, h, run
}

func count(writes []string, w string) int {
	n := 0
	for _, x := range writes {
		if x == w {
			n++
		}
	}
	return n
}

// SFC-027: only a step whose .T is read gets a hidden TON.
func TestUnreadStepTimerHasNoHiddenTON(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Slow : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Read:
    N Watch;
  END_STEP
  STEP Plain:
  END_STEP
  TRANSITION FROM Idle TO Read := Go;
  END_TRANSITION
  TRANSITION FROM Read TO Plain := Read.T >= T#2S;
  END_TRANSITION
  TRANSITION FROM Plain TO Idle := TRUE;
  END_TRANSITION
  ACTION Watch:
    Slow := Idle.X;
  END_ACTION
END_SFC
END_PROGRAM`
	out, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_S_Read_t : TON;") {
		t.Errorf("Read.T is read; want a hidden TON for it:\n%s", out)
	}
	for _, step := range []string{"Idle", "Plain"} {
		if strings.Contains(out, "_S_"+step+"_t") {
			t.Errorf("%s.T is never read; it must not get a hidden timer:\n%s", step, out)
		}
	}
	// A chart that reads no .T at all has no hidden step timer.
	plain, err := Transpile(strings.Replace(src, "Read.T >= T#2S", "Go", 1))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "TON") {
		t.Errorf("no step reads .T; want no TON at all:\n%s", plain)
	}
}

// SFC-003: Step.X is retained (it holds while the step is active, with no
// transition and no input event), is legal in a transition condition and in
// an action body; Step.T is legal in an action body.
func TestStepXRetainedAndStepTInBody(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Back : BOOL; Echo : BOOL; Slow : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Run:
    N Body;
  END_STEP
  TRANSITION FROM Idle TO Run := Go;
  END_TRANSITION
  TRANSITION FROM Run TO Idle := Run.X AND Back;
  END_TRANSITION
  ACTION Body:
    Echo := Run.X;
    Slow := Run.T >= T#1S;
  END_ACTION
END_SFC
END_PROGRAM`
	prog, frame, h, run := atScanner(t, src)
	run(1000, nil)
	run(1100, map[string]ir.Value{"Go": b(true)}) // Idle -> Run
	run(1200, map[string]ir.Value{"Go": b(false)})
	if !stepActive(t, prog, frame, "Run") || !h.vals["Echo"].B {
		t.Fatalf("Run.X should be TRUE in the body once Run is active (Echo=%v)", h.vals["Echo"])
	}
	if h.vals["Slow"].B {
		t.Fatal("Run.T is 100 ms; Slow must be FALSE")
	}
	run(2300, nil) // no event at all — X must still be set; T is now 1.2 s
	if !stepActive(t, prog, frame, "Run") || !h.vals["Echo"].B {
		t.Fatal("Run.X must stay set across scans with no event (retained)")
	}
	run(2400, nil)
	if !h.vals["Slow"].B {
		t.Fatal("Run.T >= T#1S in the body must be TRUE after 1 s in Run")
	}
	run(2500, map[string]ir.Value{"Back": b(true)}) // Run.X read in a condition
	if stepActive(t, prog, frame, "Run") || !stepActive(t, prog, frame, "Idle") {
		t.Fatal("Run.X AND Back should have fired Run -> Idle")
	}
}

// SFC-006: one scan is one fixed-order pass. Transitions are evaluated against
// the pre-scan activity (a chain of always-true transitions advances one hop
// per scan), sources clear and targets set, the step timer starts from the new
// activity (ET is 0 on the activation scan), and actions are computed from
// the new activity in that same scan (the source's N output drops and the
// target's rises together).
func TestScanOrderOneFixedPass(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Go2 : BOOL; AOut : BOOL; BOut : BOOL; END_VAR
SFC
  INITIAL_STEP A:
    N AOut;
  END_STEP
  STEP B:
    N BOut;
  END_STEP
  STEP C:
  END_STEP
  TRANSITION FROM A TO B := Go;
  END_TRANSITION
  TRANSITION FROM B TO C := TRUE;
  END_TRANSITION
  TRANSITION FROM C TO A := Go2 OR B.T >= T#10S;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog, frame, h, run := atScanner(t, src)
	run(1000, nil)
	if !stepActive(t, prog, frame, "A") || !h.vals["AOut"].B || h.vals["BOut"].B {
		t.Fatalf("scan 1: want A active, AOut set, BOut clear; got AOut=%v BOut=%v", h.vals["AOut"].B, h.vals["BOut"].B)
	}
	run(2000, map[string]ir.Value{"Go": b(true)})
	// A -> B fired. B -> C is also TRUE but B was inactive in the snapshot.
	if stepActive(t, prog, frame, "A") || !stepActive(t, prog, frame, "B") || stepActive(t, prog, frame, "C") {
		t.Fatalf("scan 2: a chain must advance one hop per scan; want only B active")
	}
	if h.vals["AOut"].B || !h.vals["BOut"].B {
		t.Fatalf("scan 2: actions are computed from the new activity: want AOut clear, BOut set; got AOut=%v BOut=%v", h.vals["AOut"].B, h.vals["BOut"].B)
	}
	if et := fbET(t, prog, frame, "_S_B_t"); et != 0 {
		t.Fatalf("scan 2: B's timer starts on its activation scan; ET = %d, want 0", et)
	}
	run(3000, map[string]ir.Value{"Go": b(false)})
	if !stepActive(t, prog, frame, "C") || stepActive(t, prog, frame, "B") {
		t.Fatalf("scan 3: B -> C should have fired")
	}
	if h.vals["BOut"].B {
		t.Fatal("scan 3: BOut must drop on the scan B is left")
	}
	if et := fbET(t, prog, frame, "_S_B_t"); et != 0 {
		t.Fatalf("scan 3: B was just left, so its timer (updated from the new activity) must be reset; ET = %d", et)
	}
}

// SFC-006 (timer phase): the step timer is updated from the post-transition
// activity in the same scan, so it starts counting on the activation scan —
// 500 ms later it reads 500, not 0 (a timer updated from the pre-scan
// snapshot would start one scan late) — and is already reset on the scan the
// step is left.
func TestStepTimerStartsOnActivationScan(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Out : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Wait:
  END_STEP
  TRANSITION FROM Idle TO Wait := Go;
  END_TRANSITION
  TRANSITION FROM Wait TO Idle := Wait.T >= T#5S;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog, frame, _, run := atScanner(t, src)
	run(1000, nil)
	run(2000, map[string]ir.Value{"Go": b(true)}) // Wait activates
	run(2500, map[string]ir.Value{"Go": b(false)})
	if et := fbET(t, prog, frame, "_S_Wait_t"); et != 500 {
		t.Fatalf("Wait.T 500 ms after activation = %d, want 500", et)
	}
	run(7100, nil) // 5.1 s: Wait -> Idle fires this scan
	run(7200, nil)
	if stepActive(t, prog, frame, "Wait") {
		t.Fatal("Wait.T >= T#5S should have fired")
	}
	if et := fbET(t, prog, frame, "_S_Wait_t"); et != 0 {
		t.Fatalf("Wait is left; its timer must be reset, ET = %d", et)
	}
}

// SFC-017: a timed qualifier takes a TIME variable as its duration; the
// variable is read at run time and the association's own timer starts when its
// step activates.
func TestTimedQualifierTimeVariableDuration(t *testing.T) {
	src := `PROGRAM P
VAR tDelay : TIME := T#2S; END_VAR
VAR_EXTERNAL Go : BOOL; Late : BOOL; Early : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Fill:
    D Late(tDelay);
    D Early(T#500MS);
  END_STEP
  TRANSITION FROM Idle TO Fill := Go;
  END_TRANSITION
END_SFC
END_PROGRAM`
	_, _, h, run := atScanner(t, src)
	run(1000, nil)
	run(10000, map[string]ir.Value{"Go": b(true)}) // Fill activates at 10 s
	run(10400, nil)
	if h.vals["Early"].B || h.vals["Late"].B {
		t.Fatal("neither delay has elapsed at 0.4 s")
	}
	run(10600, nil)
	if !h.vals["Early"].B || h.vals["Late"].B {
		t.Fatalf("at 0.6 s the T#500MS delay is up and the tDelay (2 s) one is not: Early=%v Late=%v", h.vals["Early"].B, h.vals["Late"].B)
	}
	run(11900, nil)
	if h.vals["Late"].B {
		t.Fatal("tDelay (2 s) must not fire at 1.9 s")
	}
	run(12100, nil)
	if !h.vals["Late"].B {
		t.Fatal("D Late(tDelay) must fire 2 s after Fill activates")
	}
}

// SFC-021: N, pulse and timed associations on one BOOL are OR-combined —
// held TRUE while any is active, written FALSE exactly once, on the scan the
// last one drops.
func TestAssociationsOROnOneVariable(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; ExitA : BOOL; ExitB : BOOL; ExitC : BOOL; Out : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP A:
    L Out(T#1S);
  END_STEP
  STEP B:
    N Out;
  END_STEP
  STEP C:
    P1 Out;
  END_STEP
  STEP Gone:
  END_STEP
  TRANSITION FROM Idle TO (A, B, C) := Go;
  END_TRANSITION
  TRANSITION FROM A TO Gone := ExitA;
  END_TRANSITION
  TRANSITION FROM B TO Gone := ExitB;
  END_TRANSITION
  TRANSITION FROM C TO Gone := ExitC;
  END_TRANSITION
END_SFC
END_PROGRAM`
	_, _, h, run := atScanner(t, src)
	run(1000, nil)
	run(2000, map[string]ir.Value{"Go": b(true)})
	if !h.vals["Out"].B {
		t.Fatal("Out must be TRUE once A, B and C are active")
	}
	// The pulse scan is over and the L window (1 s) closes at 3 s; B keeps
	// Out TRUE throughout, with no FALSE written.
	falses := 0
	for _, now := range []int64{2100, 2500, 3100, 3500, 4000} {
		w := run(now, map[string]ir.Value{"Go": b(false)})
		falses += count(w, "Out=false")
		if !h.vals["Out"].B {
			t.Fatalf("at %d ms Out dropped while step B's N is still active", now)
		}
	}
	if falses != 0 {
		t.Fatalf("Out was written FALSE %d times while N was still holding it", falses)
	}
	// Leaving A and C (already past their windows) changes nothing either.
	w := run(4100, map[string]ir.Value{"ExitA": b(true), "ExitC": b(true)})
	if count(w, "Out=false") != 0 || !h.vals["Out"].B {
		t.Fatalf("Out must stay TRUE while B is active (writes %v)", w)
	}
	// The last one drops: FALSE once, on that scan, and not again.
	w = run(4200, map[string]ir.Value{"ExitA": b(false), "ExitC": b(false), "ExitB": b(true)})
	if h.vals["Out"].B || count(w, "Out=false") != 1 {
		t.Fatalf("the scan the last association drops: want Out FALSE written once; Out=%v writes %v", h.vals["Out"].B, w)
	}
	w = run(4300, map[string]ir.Value{"ExitB": b(false)})
	if count(w, "Out=false") != 0 || count(w, "Out=true") != 0 {
		t.Fatalf("after the last one dropped nothing should write Out again; writes %v", w)
	}
}

// SFC-023: an association wins over an ACTION body that assigns the same
// variable on the scan it acts — the activation scan of S and R, the pulse scan
// of P1, and inside a timed window — and the ACTION owns it again afterwards.
func TestAssociationBeatsActionWrite(t *testing.T) {
	mk := func(assoc, body string) string {
		return `PROGRAM P
VAR_EXTERNAL Go : BOOL; Out : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Run:
    ` + assoc + `
    N Writer;
  END_STEP
  TRANSITION FROM Idle TO Run := Go;
  END_TRANSITION
  ACTION Writer:
    Out := ` + body + `;
  END_ACTION
END_SFC
END_PROGRAM`
	}
	for _, c := range []struct {
		name, assoc, actionWrites string
		onActivation              bool // Out on the activation scan
		later                     bool // Out two scans on, past any window
		midWindow                 bool // check a scan inside the window (timed only)
	}{
		{name: "S wins on its activation scan", assoc: "S Out;", actionWrites: "FALSE", onActivation: true, later: false},
		{name: "R wins on its activation scan", assoc: "R Out;", actionWrites: "TRUE", onActivation: false, later: true},
		{name: "P1 wins on its pulse scan", assoc: "P1 Out;", actionWrites: "FALSE", onActivation: true, later: false},
		{name: "L wins inside its window", assoc: "L Out(T#500MS);", actionWrites: "FALSE", onActivation: true, later: false, midWindow: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, h, run := atScanner(t, mk(c.assoc, c.actionWrites))
			if c.assoc == "R Out;" {
				h.vals["Out"] = b(true) // something for R to clear
			}
			run(1000, nil)
			run(2000, map[string]ir.Value{"Go": b(true)})
			if h.vals["Out"].B != c.onActivation {
				t.Fatalf("activation scan: Out = %v, want %v (the association, not the ACTION's %s)", h.vals["Out"].B, c.onActivation, c.actionWrites)
			}
			if c.midWindow {
				run(2300, map[string]ir.Value{"Go": b(false)})
				if !h.vals["Out"].B {
					t.Fatal("inside the L window the association must still win over the ACTION")
				}
			}
			run(3000, map[string]ir.Value{"Go": b(false)})
			run(3100, nil)
			if h.vals["Out"].B != c.later {
				t.Fatalf("after the association's turn the ACTION owns Out: Out = %v, want %v", h.vals["Out"].B, c.later)
			}
		})
	}
}

// SFC-024: the association-vs-ACTION-write warning covers every qualifier.
func TestCheckAssocVsActionWriteEveryQualifier(t *testing.T) {
	src := `PROGRAM P
VAR Xs : BOOL; Xp1 : BOOL; Xp0 : BOOL; Xp : BOOL; Xsd : BOOL; Xds : BOOL; Xsl : BOOL; END_VAR
SFC
INITIAL_STEP Idle:
END_STEP
STEP Run:
  S Xs;
  P1 Xp1;
  P0 Xp0;
  P Xp;
  SD Xsd(T#1S);
  Xds(DS, T#1S);
  Xsl(SL, T#1S);
END_STEP
TRANSITION FROM Idle TO Run := TRUE;
END_TRANSITION
TRANSITION FROM Run TO Idle := TRUE;
END_TRANSITION
ACTION Writer:
  Xs := TRUE; Xp1 := TRUE; Xp0 := TRUE; Xp := TRUE;
  Xsd := TRUE; Xds := TRUE; Xsl := TRUE;
END_ACTION
END_SFC
END_PROGRAM
`
	d := Check(mustParse(t, src))
	for _, w := range []string{
		"Xs is driven by a qualifier association (S) on step Run and assigned in ACTION Writer",
		"Xp1 is driven by a qualifier association (P1) on step Run and assigned in ACTION Writer",
		"Xp0 is driven by a qualifier association (P0) on step Run and assigned in ACTION Writer",
		"Xp is driven by a qualifier association (P) on step Run and assigned in ACTION Writer",
		"Xsd is driven by a qualifier association (SD) on step Run and assigned in ACTION Writer",
		"Xds is driven by a qualifier association (DS) on step Run and assigned in ACTION Writer",
		"Xsl is driven by a qualifier association (SL) on step Run and assigned in ACTION Writer",
	} {
		wantDiag(t, d, SeverityWarning, w)
	}
}

// SFC-025: an ACTION body under S, D, SD, DS or SL runs one final scan when
// its active signal falls. The body mirrors Probe into Echo; the test drops
// Probe on the falling scan, so Echo goes FALSE only if the body ran then,
// and Echo stays FALSE afterwards because the body has stopped.
func TestActionBodyFinalScanEveryLevelQualifier(t *testing.T) {
	// Each chart reaches `Run`, where the association starts the body, then
	// drops its active signal. fall(now) is the time of the falling scan.
	chart := func(assocRun, steps, trans string) string {
		return `PROGRAM P
VAR_EXTERNAL Go : BOOL; Next : BOOL; Probe : BOOL; Echo : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Run:
    ` + assocRun + `
  END_STEP
  ` + steps + `
  TRANSITION FROM Idle TO Run := Go;
  END_TRANSITION
  ` + trans + `
  ACTION Body:
    Echo := Probe;
  END_ACTION
END_SFC
END_PROGRAM`
	}
	reset := "STEP Stop:\n    R Body;\n  END_STEP"
	toStop := "TRANSITION FROM Run TO Stop := Next;\n  END_TRANSITION"
	for _, c := range []struct {
		name  string
		src   string
		drive map[string]ir.Value // inputs that cause the signal to fall (besides Probe)
		fall  int64               // time of the falling scan
	}{
		{"S", chart("S Body;", reset, toStop), map[string]ir.Value{"Next": b(true)}, 3000},
		{"D", chart("D Body(T#300MS);", "", "TRANSITION FROM Run TO Idle := Next;\n  END_TRANSITION"), map[string]ir.Value{"Next": b(true)}, 3000},
		{"SD", chart("SD Body(T#300MS);", reset, toStop), map[string]ir.Value{"Next": b(true)}, 3000},
		{"DS", chart("Body(DS, T#300MS);", reset, toStop), map[string]ir.Value{"Next": b(true)}, 3000},
		{"SL", chart("Body(SL, T#300MS);", "", "TRANSITION FROM Run TO Idle := Next;\n  END_TRANSITION"), nil, 2300},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, h, run := atScanner(t, c.src)
			run(1000, nil)
			run(2000, map[string]ir.Value{"Go": b(true), "Probe": b(true)}) // Run activates
			run(2100, map[string]ir.Value{"Go": b(false)})
			run(2200, nil)
			if c.name != "SL" {
				run(2500, nil) // past any delay: the body is running
			} else {
				run(2250, nil) // still inside the SL window
			}
			if !h.vals["Echo"].B {
				t.Fatal("the body should be running and mirroring Probe=TRUE")
			}
			in := map[string]ir.Value{"Probe": b(false)}
			for k, v := range c.drive {
				in[k] = v
			}
			run(c.fall, in)
			if h.vals["Echo"].B {
				t.Fatal("no final scan: Echo still TRUE after the signal fell with Probe=FALSE")
			}
			run(c.fall+100, map[string]ir.Value{"Probe": b(true), "Next": b(false)})
			run(c.fall+200, nil)
			if h.vals["Echo"].B {
				t.Fatal("the body kept running after its final scan")
			}
		})
	}
}

// SFC-029: an overrunning step raises its flag and nothing else — it stays
// active and no transition fires unless one reads the flag.
func TestStepOverrunDoesNothingElse(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Done : BOOL; Ovr : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Fill (MAXTIME := T#1S, ERROR := Ovr):
  END_STEP
  STEP After:
  END_STEP
  TRANSITION FROM Idle TO Fill := Go;
  END_TRANSITION
  TRANSITION FROM Fill TO After := Done;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog, frame, h, run := atScanner(t, src)
	run(1000, nil)
	run(2000, map[string]ir.Value{"Go": b(true)})
	run(2500, map[string]ir.Value{"Go": b(false)})
	if h.vals["Ovr"].B {
		t.Fatal("0.5 s in Fill, MAXTIME is 1 s: no overrun yet")
	}
	for _, now := range []int64{3500, 5000, 20000} {
		run(now, nil)
	}
	if !h.vals["Ovr"].B {
		t.Fatal("Fill ran past MAXTIME: the flag must be set")
	}
	if !stepActive(t, prog, frame, "Fill") || stepActive(t, prog, frame, "After") || stepActive(t, prog, frame, "Idle") {
		t.Fatal("an overrun must not move the token; no transition reads the flag")
	}
}

// SFC-033: a .T reference to an unknown step is a structural error, like .X.
func TestCheckUnknownStepTRef(t *testing.T) {
	src := `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP A:
END_STEP
STEP B:
END_STEP
TRANSITION FROM A TO B := Typo.T >= T#1S;
END_TRANSITION
TRANSITION FROM B TO A := TRUE;
END_TRANSITION
END_SFC
END_PROGRAM
`
	wantDiag(t, Check(mustParse(t, src)), SeverityError, `references unknown step "Typo"`)
}
