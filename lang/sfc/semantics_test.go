package sfc

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// runScans compiles src, runs len(times) scans setting inputs[i] and now=times[i]
// before each, and returns the program+frame for assertions.
func runScans(t *testing.T, src string, times []int64, inputs []map[string]ir.Value) (*ir.Program, *ir.Frame, *clockHost) {
	t.Helper()
	prog := mustCompile(t, src)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame := ir.NewFrame(prog)
	for i, now := range times {
		if i < len(inputs) {
			for k, v := range inputs[i] {
				h.vals[k] = v
			}
		}
		h.now = now
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
	}
	return prog, frame, h
}

// TestAltPriorityMutualExclusion: two transitions from the same step both
// enabled → only the first (declared) one fires.
func TestAltPriorityMutualExclusion(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; END_VAR
SFC
  INITIAL_STEP S:
  END_STEP
  STEP A:
  END_STEP
  STEP B:
  END_STEP
  TRANSITION t1 FROM S TO A := Go;
  END_TRANSITION
  TRANSITION t2 FROM S TO B := Go;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog, frame, _ := runScans(t, src, []int64{1000}, []map[string]ir.Value{
		{"Go": ir.BoolVal(true)},
	})
	if !stepActive(t, prog, frame, "A") {
		t.Error("higher-priority t1 should fire → A active")
	}
	if stepActive(t, prog, frame, "B") {
		t.Error("t2 must be suppressed → B inactive")
	}
	if stepActive(t, prog, frame, "S") {
		t.Error("S should have cleared")
	}
}

// TestEnabledNotCondGuard is the regression of §2.3: a higher-priority
// convergence whose OTHER source is inactive (so cond can be true but it is not
// enabled) must NOT suppress a lower-priority branch sharing the step.
func TestEnabledNotCondGuard(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; END_VAR
SFC
  INITIAL_STEP S:
  END_STEP
  STEP Other:
  END_STEP
  STEP Z:
  END_STEP
  STEP W:
  END_STEP
  TRANSITION t_hi FROM (S, Other) TO Z := Go;
  END_TRANSITION
  TRANSITION t_lo FROM S TO W := Go;
  END_TRANSITION
  TRANSITION seed FROM Z TO Other := FALSE;
  END_TRANSITION
END_SFC
END_PROGRAM`
	// S is active (initial), Other is NOT. Go=TRUE so cond(t_hi) is true, but
	// enabled(t_hi) is false (Other inactive) → t_lo must fire.
	prog, frame, _ := runScans(t, src, []int64{1000}, []map[string]ir.Value{
		{"Go": ir.BoolVal(true)},
	})
	if !stepActive(t, prog, frame, "W") {
		t.Error("lower branch must fire when higher convergence is not enabled")
	}
	if stepActive(t, prog, frame, "Z") {
		t.Error("t_hi cannot fire with Other inactive")
	}
}

// TestSetDominatesClear: a self-loop FROM S TO S leaves S active (§2.4).
func TestSetDominatesClear(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; END_VAR
SFC
  INITIAL_STEP S:
  END_STEP
  TRANSITION t FROM S TO S := Go;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog, frame, _ := runScans(t, src, []int64{1000}, []map[string]ir.Value{
		{"Go": ir.BoolVal(true)},
	})
	if !stepActive(t, prog, frame, "S") {
		t.Error("self-loop must leave S active (set dominates clear)")
	}
}

// TestP1FiresOncePerActivation: a P1 body runs exactly once per activation,
// including on re-entry.
func TestP1FiresOncePerActivation(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Fwd : BOOL; Back : BOOL; Count : INT; END_VAR
SFC
  INITIAL_STEP Home:
  END_STEP
  STEP Work:
    P1 Bump;
  END_STEP
  TRANSITION t1 FROM Home TO Work := Fwd;
  END_TRANSITION
  TRANSITION t2 FROM Work TO Home := Back;
  END_TRANSITION
  ACTION Bump:
    Count := Count + 1;
  END_ACTION
END_SFC
END_PROGRAM`
	prog := mustCompile(t, src)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame := ir.NewFrame(prog)
	run := func(in map[string]ir.Value) {
		for k, v := range in {
			h.vals[k] = v
		}
		h.now += 1000
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
	}
	run(map[string]ir.Value{"Fwd": ir.BoolVal(true)})               // Home→Work, pulse #1
	run(map[string]ir.Value{"Fwd": ir.BoolVal(false)})              // Work, no pulse
	run(nil)                                                        // Work, no pulse
	if h.vals["Count"].I != 1 {
		t.Fatalf("after first activation Count=%d, want 1", h.vals["Count"].I)
	}
	run(map[string]ir.Value{"Back": ir.BoolVal(true)})              // Work→Home
	run(map[string]ir.Value{"Back": ir.BoolVal(false), "Fwd": ir.BoolVal(true)}) // Home→Work, pulse #2
	if h.vals["Count"].I != 2 {
		t.Fatalf("after re-entry Count=%d, want 2 (one pulse per activation)", h.vals["Count"].I)
	}
}

// TestStoredLatchAcrossSteps: S on one step sets a stored boolean action; R on
// a later step clears it; the value latches across the steps between.
func TestStoredLatchAcrossSteps(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Next : BOOL; Clear : BOOL; Lamp : BOOL; END_VAR
SFC
  INITIAL_STEP S0:
  END_STEP
  STEP S1:
    S Lamp;
  END_STEP
  STEP S2:
  END_STEP
  STEP S3:
    R Lamp;
  END_STEP
  TRANSITION a FROM S0 TO S1 := Go;
  END_TRANSITION
  TRANSITION b FROM S1 TO S2 := Next;
  END_TRANSITION
  TRANSITION c FROM S2 TO S3 := Clear;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog := mustCompile(t, src)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame := ir.NewFrame(prog)
	run := func(in map[string]ir.Value) {
		for k, v := range in {
			h.vals[k] = v
		}
		h.now += 1000
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
	}
	run(map[string]ir.Value{"Go": ir.BoolVal(true)}) // S0→S1: sets Lamp
	if !h.vals["Lamp"].B {
		t.Fatal("S Lamp should set on S1 activation")
	}
	run(map[string]ir.Value{"Go": ir.BoolVal(false), "Next": ir.BoolVal(true)}) // S1→S2
	if !h.vals["Lamp"].B {
		t.Fatal("Lamp should latch across S2 (no R yet)")
	}
	run(map[string]ir.Value{"Next": ir.BoolVal(false), "Clear": ir.BoolVal(true)}) // S2→S3: clears
	if h.vals["Lamp"].B {
		t.Fatal("R Lamp on S3 should clear the latch")
	}
}

// TestFinalScanResetsTON is the §2.5.1 hazard in isolation: a body action that
// drives a TON from the step's .X must, on the falling edge, get one final scan
// that calls the TON with IN:=FALSE, resetting it for a clean (fresh-timing)
// re-entry.
func TestFinalScanResetsTON(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; Reset : BOOL; Q : BOOL; END_VAR
VAR mt : TON; END_VAR
SFC
  INITIAL_STEP Home:
  END_STEP
  STEP Run:
    N Timing;
  END_STEP
  TRANSITION t1 FROM Home TO Run := Go;
  END_TRANSITION
  TRANSITION t2 FROM Run TO Home := Reset;
  END_TRANSITION
  ACTION Timing:
    mt(IN := Run.X, PT := T#10S);
    Q := mt.Q;
  END_ACTION
END_SFC
END_PROGRAM`
	prog := mustCompile(t, src)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame := ir.NewFrame(prog)
	mtIdx := slotIdx(prog, "mt")
	et := func() int64 { return frame.Slots[mtIdx].FB.Slots[3].I }
	run := func(in map[string]ir.Value, now int64) {
		for k, v := range in {
			h.vals[k] = v
		}
		h.now = now
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
	}
	// mt is a global (VAR_EXTERNAL) here so ET is observable via the host.
	run(map[string]ir.Value{"Go": ir.BoolVal(true)}, 1000)  // Home→Run, mt starts
	run(map[string]ir.Value{"Go": ir.BoolVal(false)}, 4000) // Run, mt at 3s
	if et() == 0 {
		t.Fatal("mt should be counting mid-run")
	}
	run(map[string]ir.Value{"Reset": ir.BoolVal(true)}, 6000) // Run→Home: final scan resets mt
	if et() != 0 {
		t.Fatalf("final scan must reset the TON, ET=%d", et())
	}
	// Re-enter: timing must start fresh, not from a stale value.
	run(map[string]ir.Value{"Reset": ir.BoolVal(false), "Go": ir.BoolVal(true)}, 8000) // Home→Run
	if et() != 0 {
		t.Fatalf("fresh re-entry should start timing at 0, ET=%d", et())
	}
	run(nil, 9000) // Run, 1s
	if got := et(); got < 900 || got > 1100 {
		t.Errorf("re-entry timing should be ~1s, ET=%d", got)
	}
}

// TestStepTimerGatesTransition: Step.T via a hidden TON gates a transition once
// the clock advances past the compared bound.
func TestStepTimerGatesTransition(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL Go : BOOL; END_VAR
SFC
  INITIAL_STEP Idle:
  END_STEP
  STEP Wait:
  END_STEP
  STEP Done:
  END_STEP
  TRANSITION t1 FROM Idle TO Wait := Go;
  END_TRANSITION
  TRANSITION t2 FROM Wait TO Done := Wait.T >= T#5S;
  END_TRANSITION
END_SFC
END_PROGRAM`
	prog := mustCompile(t, src)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame := ir.NewFrame(prog)
	run := func(in map[string]ir.Value, now int64) {
		for k, v := range in {
			h.vals[k] = v
		}
		h.now = now
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
	}
	run(map[string]ir.Value{"Go": ir.BoolVal(true)}, 1000) // Idle→Wait; timer starts
	if !stepActive(t, prog, frame, "Wait") {
		t.Fatal("expected Wait active")
	}
	run(map[string]ir.Value{"Go": ir.BoolVal(false)}, 3000) // Wait, .T=2s < 5s
	if stepActive(t, prog, frame, "Done") {
		t.Fatal("must not advance before 5s")
	}
	run(nil, 7000) // .T=6s ≥ 5s (read next scan via pipeline)
	run(nil, 8000)
	if !stepActive(t, prog, frame, "Done") {
		t.Fatal("Step.T should gate the transition once past 5s")
	}
}

// TestWarmMigration: transpile+lower, run scans to move the token, then lower
// an edited variant and MigrateFrame (what runtime.Program.SwapWarm does) —
// the chart's live state must survive (§2.7 warm restart).
func TestWarmMigration(t *testing.T) {
	t.Run("token", testWarmMigrationToken)
	t.Run("timers-latches-pulses", testWarmMigrationTimersLatchesPulses)
	t.Run("renamed-step-resets", testWarmMigrationRenamedStep)
}

// warmChart has one of each piece of retained chart state on step Fill: its
// Step.T timer, a MAXTIME overrun flag, a stored (S) action whose body
// counts while the latch holds, a D-timed association (its own TON), an SL
// association (a timed-qualifier latch and its TON), and a P action whose
// once-per-activation pulse rides the step's edge memory.
const warmChart = `PROGRAM P
VAR_EXTERNAL Go : BOOL; Done : BOOL; Valve : BOOL; Ovr : BOOL; Pulses : INT; Ticks : INT; Pump : BOOL; END_VAR
SFC
INITIAL_STEP Idle:
END_STEP
STEP Fill (MAXTIME := T#5S, ERROR := Ovr):
  S Agitate;
  D Valve(T#10S);
  SL Pump(T#20S);
  P Bump;
END_STEP
STEP Drain:
  R Agitate;
END_STEP
TRANSITION FROM Idle TO Fill := Go;
END_TRANSITION
TRANSITION FROM Fill TO Drain := Done;
END_TRANSITION
ACTION Agitate:
  Ticks := Ticks + 1;
END_ACTION
ACTION Bump:
  Pulses := Pulses + 1;
END_ACTION
END_SFC
END_PROGRAM
`

// fbET reads a TON instance's ET (slot 3: IN, PT, Q, ET) from a frame.
func fbET(t *testing.T, prog *ir.Program, frame *ir.Frame, name string) int64 {
	t.Helper()
	i := slotIdx(prog, name)
	if i < 0 || frame.Slots[i].FB == nil {
		t.Fatalf("no TON slot %q", name)
	}
	return frame.Slots[i].FB.Slots[3].I
}

func frameBool(t *testing.T, prog *ir.Program, frame *ir.Frame, name string) bool {
	t.Helper()
	i := slotIdx(prog, name)
	if i < 0 {
		t.Fatalf("no slot %q", name)
	}
	return frame.Slots[i].B
}

func testWarmMigrationTimersLatchesPulses(t *testing.T) {
	prog1 := mustCompile(t, warmChart)
	h := &clockHost{vals: map[string]ir.Value{}}
	frame1 := ir.NewFrame(prog1)
	run := func(prog *ir.Program, frame *ir.Frame, in map[string]ir.Value, now int64) {
		t.Helper()
		for k, v := range in {
			h.vals[k] = v
		}
		h.now = now
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
	}
	// Idle→Fill at 1 s; Fill stays active to 7 s, past its 5 s MAXTIME.
	run(prog1, frame1, map[string]ir.Value{"Go": ir.BoolVal(true)}, 1000)
	for now := int64(2000); now <= 7000; now += 1000 {
		run(prog1, frame1, map[string]ir.Value{"Go": ir.BoolVal(false)}, now)
	}
	if !stepActive(t, prog1, frame1, "Fill") {
		t.Fatal("token should be at Fill before the edit")
	}
	if got := fbET(t, prog1, frame1, "_S_Fill_t"); got != 6000 {
		t.Fatalf("Fill.T before the edit = %d, want 6000", got)
	}
	if !h.vals["Ovr"].B || h.vals["Pulses"].I != 1 || h.vals["Ticks"].I != 7 || h.vals["Valve"].B || !h.vals["Pump"].B {
		t.Fatalf("before the edit: Ovr=%v Pulses=%d Ticks=%d Valve=%v Pump=%v, want true/1/7/false/true",
			h.vals["Ovr"].B, h.vals["Pulses"].I, h.vals["Ticks"].I, h.vals["Valve"].B, h.vals["Pump"].B)
	}

	// A real edit: the P action's body changes (so a re-fired pulse shows
	// as +10), and a new step + transition are added after Drain.
	edited := strings.Replace(warmChart, "Pulses := Pulses + 1;", "Pulses := Pulses + 10;", 1)
	edited = strings.Replace(edited, "STEP Drain:\n  R Agitate;\nEND_STEP",
		"STEP Drain:\n  R Agitate;\nEND_STEP\nSTEP Spare:\nEND_STEP", 1)
	edited = strings.Replace(edited, "ACTION Agitate:",
		"TRANSITION FROM Drain TO Spare := Go;\nEND_TRANSITION\nACTION Agitate:", 1)
	prog2 := mustCompile(t, edited)
	frame2, resets := ir.MigrateFrame(prog2, prog1, frame1)

	// Every piece of chart state is carried by name, and none is reported
	// as reset; the new step is.
	for _, name := range []string{"_S_Fill_X", "_S_Fill_prev", "_S_Fill_t", "_S_Fill_err",
		"_q_Fill_Valve_D_tmr", "_q_Fill_Pump_SL_ff", "_q_Fill_Pump_SL_tmr",
		"_act_Agitate_stored", "_act_Agitate_prev"} {
		for _, r := range resets {
			if strings.EqualFold(r, name) {
				t.Errorf("%s reported as reset (resets %v)", name, resets)
			}
		}
	}
	if !containsFold(resets, "_S_Spare_X") {
		t.Errorf("the added step's token should be reported as reset: %v", resets)
	}
	if !frameBool(t, prog2, frame2, "_S_Fill_X") || frameBool(t, prog2, frame2, "_S_Idle_X") {
		t.Fatal("warm migration lost the token at Fill")
	}
	// The MAXTIME flag is carried as is, not left for the next scan to
	// recompute: a transition reading Fill.ERR on the first scan after the
	// swap reads it before supervision runs.
	if !frameBool(t, prog2, frame2, "_S_Fill_err") {
		t.Error("Fill's MAXTIME overrun flag (Fill.ERR) was dropped by the swap")
	}
	if !frameBool(t, prog2, frame2, "_act_Agitate_stored") {
		t.Error("the stored (S) latch on Agitate was dropped by the swap")
	}
	if !frameBool(t, prog2, frame2, "_S_Fill_prev") {
		t.Error("Fill's edge memory (P pulses, S/MAXTIME edges) was dropped by the swap")
	}
	if got := fbET(t, prog2, frame2, "_S_Fill_t"); got != 6000 {
		t.Errorf("Fill.T right after the swap = %d, want 6000", got)
	}
	if got := fbET(t, prog2, frame2, "_q_Fill_Valve_D_tmr"); got != 6000 {
		t.Errorf("D Valve timer right after the swap = %d, want 6000", got)
	}
	if !frameBool(t, prog2, frame2, "_q_Fill_Pump_SL_ff") {
		t.Error("the SL Pump latch was dropped by the swap")
	}
	if got := fbET(t, prog2, frame2, "_q_Fill_Pump_SL_tmr"); got != 6000 {
		t.Errorf("SL Pump timer right after the swap = %d, want 6000", got)
	}

	// And it keeps running from there on the edited program.
	run(prog2, frame2, nil, 8000)
	if got := fbET(t, prog2, frame2, "_S_Fill_t"); got != 7000 {
		t.Errorf("Fill.T one scan after the swap = %d, want 7000 (continues, not restarted)", got)
	}
	if h.vals["Pulses"].I != 1 {
		t.Errorf("Pulses = %d after the swap, want 1: the P action re-fired, so its edge memory did not carry", h.vals["Pulses"].I)
	}
	if h.vals["Ticks"].I != 8 {
		t.Errorf("Ticks = %d after the swap, want 8: the stored action stopped running", h.vals["Ticks"].I)
	}
	if !h.vals["Ovr"].B {
		t.Error("ERROR := Ovr cleared across the swap")
	}
	run(prog2, frame2, nil, 10000)
	if h.vals["Valve"].B {
		t.Error("D Valve(T#10S) on at 10 s: 9 s after activation")
	}
	run(prog2, frame2, nil, 11000)
	if !h.vals["Valve"].B {
		t.Error("D Valve(T#10S) off at 11 s, 10 s after activation: its timer restarted at the swap")
	}
	// The latch still holds three scans on (a dropped latch gets one final
	// scan from its falling-edge memory, then stops).
	if h.vals["Ticks"].I != 10 {
		t.Errorf("Ticks = %d three scans after the swap, want 10: the stored action stopped running", h.vals["Ticks"].I)
	}
	// SL Pump(T#20S): on from activation (1 s) until 21 s, on the latch and
	// timer that crossed the swap.
	if !h.vals["Pump"].B {
		t.Error("SL Pump off at 11 s: its latch did not carry")
	}
	run(prog2, frame2, nil, 20000)
	if !h.vals["Pump"].B {
		t.Error("SL Pump off at 20 s, inside its 20 s limit")
	}
	run(prog2, frame2, nil, 21000)
	if h.vals["Pump"].B {
		t.Error("SL Pump still on at 21 s, 20 s after activation: its timer restarted at the swap")
	}
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// testWarmMigrationRenamedStep: renaming the active step is a new slot name,
// so its token resets — the chart ends up with no active step — and the
// renamed step's token is listed in the swap report.
func testWarmMigrationRenamedStep(t *testing.T) {
	prog1 := mustCompile(t, warmChart)
	h := &clockHost{vals: map[string]ir.Value{"Go": ir.BoolVal(true)}, now: 1000}
	frame1 := ir.NewFrame(prog1)
	if err := ir.Run(prog1, frame1, h); err != nil {
		t.Fatal(err)
	}
	if !stepActive(t, prog1, frame1, "Fill") {
		t.Fatal("token should be at Fill before the rename")
	}
	renamed := strings.NewReplacer("STEP Fill ", "STEP Filling ", "FROM Idle TO Fill ", "FROM Idle TO Filling ",
		"FROM Fill TO", "FROM Filling TO").Replace(warmChart)
	prog2 := mustCompile(t, renamed)
	frame2, resets := ir.MigrateFrame(prog2, prog1, frame1)
	if stepActive(t, prog2, frame2, "Filling") {
		t.Error("a renamed step must not inherit the old step's token")
	}
	if stepActive(t, prog2, frame2, "Idle") {
		t.Error("Idle must stay inactive: the rename resets the token, it does not restart the chart")
	}
	if !containsFold(resets, "_S_Filling_X") {
		t.Errorf("the renamed step's token should be listed in the swap report: %v", resets)
	}
}

func testWarmMigrationToken(t *testing.T) {
	prog1 := mustCompile(t, tankBatchSrc)
	h := &clockHost{vals: map[string]ir.Value{
		"FillSP": ir.RealVal(100), "HeatSP": ir.RealVal(80), "EmptySP": ir.RealVal(5),
	}}
	frame1 := ir.NewFrame(prog1)
	run := func(in map[string]ir.Value, now int64) {
		for k, v := range in {
			h.vals[k] = v
		}
		h.now = now
		if err := ir.Run(prog1, frame1, h); err != nil {
			t.Fatal(err)
		}
	}
	run(map[string]ir.Value{"Start": ir.BoolVal(true), "Abort": ir.BoolVal(false)}, 1000) // →Fill
	run(map[string]ir.Value{"Start": ir.BoolVal(false), "Level": ir.RealVal(40)}, 2000)   // Fill
	if !stepActive(t, prog1, frame1, "Fill") {
		t.Fatal("token should be at Fill before edit")
	}

	// A trivially-edited variant (an added comment line changes nothing
	// structural but recompiles a fresh program).
	edited := strings.Replace(tankBatchSrc, "  STEP Fill:", "  (* edited *)\n  STEP Fill:", 1)
	prog2 := mustCompile(t, edited)
	frame2, resets := ir.MigrateFrame(prog2, prog1, frame1)
	if !frame2.Slots[slotIdx(prog2, "_S_Fill_X")].B {
		t.Fatalf("warm migration lost the token at Fill (resets=%v)", resets)
	}
	if frame2.Slots[slotIdx(prog2, "_S_Idle_X")].B {
		t.Error("Idle should not be active after migration")
	}
}
