package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/lang/ir"
)

// totalizer is a user FUNCTION_BLOCK with internal state (Sum): the shape
// whose instance an online edit must carry, not only a builtin TON. Body
// is its last statement, so an edit can change the code without touching
// the variables.
const totalizer = `FUNCTION_BLOCK Totalizer
VAR_INPUT X : REAL; END_VAR
VAR_OUTPUT Y : REAL; END_VAR
VAR Sum : REAL; END_VAR
Sum := Sum + X;
Y := Sum;
END_FUNCTION_BLOCK

`

// mainTail is the part of Main both revisions share: a TON partway to its
// preset, a CTU counting Toggle's rising edges, and a Totalizer instance,
// each surfaced on an output tag so a test reads what a scan produced.
const mainTail = `Debounce(IN := In1 > 0.0, PT := T#10S);
DebET := Debounce.ET;
DebQ := Debounce.Q;
Toggle := NOT Toggle;
Pulses(CU := Toggle, PV := 100);
PulseCV := Pulses.CV;
Acc(X := In1);
AccY := Acc.Y;
END_PROGRAM`

const editV1 = totalizer + `PROGRAM Main
VAR_EXTERNAL
  In1 : REAL;
  Out1 : REAL;
  DebET : TIME;
  DebQ : BOOL;
  PulseCV : INT;
  AccY : REAL;
END_VAR
VAR
  Total : REAL;
  Count : INT;
  Debounce : TON;
  Pulses : CTU;
  Toggle : BOOL;
  Acc : Totalizer;
END_VAR
Total := Total + In1;
Count := Count + 1;
Out1 := Total;
` + mainTail

// editV2 keeps Total, Debounce, Pulses, Toggle and Acc, drops Count, adds
// Gain, and edits the Totalizer's body (not its variables) — a typical
// online tweak.
var editV2 = strings.Replace(totalizer, "Y := Sum;", "Y := Sum * 2.0;", 1) + `PROGRAM Main
VAR_EXTERNAL
  In1 : REAL;
  Out1 : REAL;
  DebET : TIME;
  DebQ : BOOL;
  PulseCV : INT;
  AccY : REAL;
END_VAR
VAR
  Total : REAL;
  Gain : REAL := 2.0;
  Debounce : TON;
  Pulses : CTU;
  Toggle : BOOL;
  Acc : Totalizer;
END_VAR
Total := Total + In1;
Out1 := Total * Gain;
` + mainTail

// readTag reads a tag the program wrote, failing the test if it never did.
func readTag(t *testing.T, tags *Tags, name string) ir.Value {
	t.Helper()
	v, err := tags.ReadGlobal(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return v
}

// TestSwapWarmCarriesRetainedState runs a program on a virtual clock until
// a TON is partway to its preset, a CTU has counted, and a user FB instance
// has integrated, then swaps in an edit and asserts each carries on from
// where it was rather than restarting.
func TestSwapWarmCarriesRetainedState(t *testing.T) {
	p, err := Compile(editV1)
	if err != nil {
		t.Fatal(err)
	}
	clk := &stepClock{t: time.Unix(1_700_000_000, 0)}
	t0 := clk.t
	tags := NewTags()
	tags.clock = clk // the TON times against clk, as under Options.Clock
	tags.SetReal("In1", 1.0)
	tags.SetReal("Out1", 0)
	scanAt := func(sec int) {
		t.Helper()
		clk.t = t0.Add(time.Duration(sec) * time.Second)
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	for s := 0; s < 5; s++ { // scans at 0..4 s
		scanAt(s)
	}
	if got := tags.Real("Out1"); got != 5.0 {
		t.Fatalf("Out1 after 5 scans = %v, want 5", got)
	}
	// The pre-swap baseline, so a reset after the swap cannot pass by luck.
	if got := readTag(t, tags, "DebET").I; got != 4000 {
		t.Fatalf("Debounce.ET before the edit = %d ms, want 4000", got)
	}
	if got := readTag(t, tags, "PulseCV").I; got != 3 {
		t.Fatalf("Pulses.CV before the edit = %d, want 3 (Toggle rose on scans 1, 3, 5)", got)
	}
	if got := readTag(t, tags, "AccY").F; got != 5.0 {
		t.Fatalf("Acc.Y before the edit = %v, want 5", got)
	}
	if p.Dirty() {
		t.Fatal("program dirty before any edit")
	}

	report, err := p.SwapWarm(editV2)
	if err != nil {
		t.Fatalf("warm swap: %v", err)
	}
	if !p.Dirty() {
		t.Error("program should be dirty after an online edit")
	}
	// Gain is new → reset; Count vanished (not reported: it has no slot in
	// the new program); Total, Debounce, Pulses, Toggle and Acc carried.
	if len(report.Resets) != 1 || report.Resets[0] != "Gain" {
		t.Errorf("resets = %v, want [Gain]", report.Resets)
	}

	scanAt(5)
	// Total carried its 5.0 across the swap: next scan yields (5+1)*2.
	if got := tags.Real("Out1"); got != 12.0 {
		t.Errorf("Out1 after warm swap scan = %v, want 12 (Total carried)", got)
	}
	// The TON kept its start time: ET continues to 5 s, not back to 0.
	if got := readTag(t, tags, "DebET").I; got != 5000 {
		t.Errorf("Debounce.ET after the swap = %d ms, want 5000 (timer carried, not restarted)", got)
	}
	// The CTU kept its count and its CU edge memory, and Toggle carried
	// TRUE so it falls this scan: no count. A reset CTU would read 0; a
	// reset Toggle would rise again and count to 4.
	if got := readTag(t, tags, "PulseCV").I; got != 3 {
		t.Errorf("Pulses.CV after the swap = %d, want 3 (count carried)", got)
	}
	// The Totalizer instance kept Sum (5 → 6) and runs the edited body (×2).
	if got := readTag(t, tags, "AccY").F; got != 12.0 {
		t.Errorf("Acc.Y after the swap = %v, want 12 (Sum carried, edited body live)", got)
	}
	scanAt(6)
	if got := readTag(t, tags, "PulseCV").I; got != 4 {
		t.Errorf("Pulses.CV on the first rising edge after the swap = %d, want 4", got)
	}
	// The preset lands 10 s after the ORIGINAL start, as if never edited.
	scanAt(9)
	if readTag(t, tags, "DebQ").B {
		t.Error("Debounce.Q at 9 s must still be FALSE")
	}
	scanAt(10)
	if q, et := readTag(t, tags, "DebQ").B, readTag(t, tags, "DebET").I; !q || et != 10000 {
		t.Errorf("Debounce at 10 s: Q=%v ET=%d, want TRUE/10000 (timed from before the edit)", q, et)
	}

	// Rollback restores V1 program AND its state (Total keeps counting
	// from where V2 left it? No — rollback restores the pre-swap frame:
	// Total is 5 again, and one scan makes it 6).
	if !p.CanRollback() {
		t.Fatal("rollback should be available")
	}
	if _, err := p.Rollback(); err != nil {
		t.Fatal(err)
	}
	if p.Dirty() {
		t.Error("rolled-back program should match boot source")
	}
	if err := p.Run(tags); err != nil {
		t.Fatal(err)
	}
	if got := tags.Real("Out1"); got != 6.0 {
		t.Errorf("Out1 after rollback scan = %v, want 6 (pre-swap frame restored)", got)
	}
	if p.CanRollback() {
		t.Error("rollback should be single-shot")
	}
}

func TestSwapWarmTypeChangeResets(t *testing.T) {
	p, err := Compile(editV1)
	if err != nil {
		t.Fatal(err)
	}
	tags := NewTags()
	tags.SetReal("In1", 1)
	tags.SetReal("Out1", 0)
	_ = p.Run(tags)

	// Total becomes INT: incompatible, must reset and be reported.
	v3 := strings.Replace(editV1, "Total : REAL;", "Total : INT;", 1)
	v3 = strings.Replace(v3, "Total := Total + In1;", "Total := Total + 1;", 1)
	v3 = strings.Replace(v3, "Out1 := Total;", "Out1 := 0.0;", 1)
	report, err := p.SwapWarm(v3)
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	found := false
	for _, r := range report.Resets {
		if r == "Total" {
			found = true
		}
	}
	if !found {
		t.Errorf("Total type change not reported in resets: %v", report.Resets)
	}
}

func TestSwapWarmCompileErrorLeavesRunning(t *testing.T) {
	p, err := Compile(editV1)
	if err != nil {
		t.Fatal(err)
	}
	hash := p.Hash()
	if _, err := p.SwapWarm("PROGRAM Broken\nOut1 := ;\nEND_PROGRAM"); err == nil {
		t.Fatal("expected compile error")
	}
	if p.Hash() != hash {
		t.Error("failed swap must not change the running program")
	}
	tags := NewTags()
	tags.SetReal("In1", 1)
	tags.SetReal("Out1", 0)
	if err := p.Run(tags); err != nil {
		t.Errorf("old program no longer runs: %v", err)
	}
}

// FBD sources are first-class programs: the runtime detects the netlist
// block, transpiles through lang/fbd, and keeps the ORIGINAL .fbd text as
// the program of record — so online edits and hashes speak .fbd end to end.

const fbdV1 = `PROGRAM Main
VAR_EXTERNAL
  In1 : REAL;
  Out1 : REAL;
END_VAR
VAR
  integral : REAL;
END_VAR
FBD
  integral := ADD(integral, In1)
  Out1 := integral
END_FBD
END_PROGRAM`

// fbdV2 keeps integral (retained across the swap) and scales the output.
const fbdV2 = `PROGRAM Main
VAR_EXTERNAL
  In1 : REAL;
  Out1 : REAL;
END_VAR
VAR
  integral : REAL;
END_VAR
FBD
  integral := ADD(integral, In1)
  Out1 := MUL(integral, 2.0)
END_FBD
END_PROGRAM`

func TestLanguageDetection(t *testing.T) {
	if l := Language(fbdV1); l != "fbd" {
		t.Errorf("Language(fbd source) = %q, want fbd", l)
	}
	if l := Language(editV1); l != "st" {
		t.Errorf("Language(st source) = %q, want st", l)
	}
}

func TestSwapWarmFBDCarriesRetainedState(t *testing.T) {
	p, err := Compile(fbdV1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source() != fbdV1 {
		t.Fatal("Source() must be the original .fbd text, not the transpiled ST")
	}
	tags := NewTags()
	tags.SetReal("In1", 1.0)
	tags.SetReal("Out1", 0)
	for i := 0; i < 5; i++ {
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	if got := tags.Real("Out1"); got != 5.0 {
		t.Fatalf("Out1 after 5 scans = %v, want 5", got)
	}

	report, err := p.SwapWarm(fbdV2)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Resets) != 0 {
		t.Errorf("resets = %v, want none (integral carries)", report.Resets)
	}
	if err := p.Run(tags); err != nil {
		t.Fatal(err)
	}
	// integral was 5, +1 = 6, doubled = 12: the edit went live warm.
	if got := tags.Real("Out1"); got != 12.0 {
		t.Fatalf("Out1 after warm fbd edit = %v, want 12", got)
	}
	if !p.Dirty() || p.Source() != fbdV2 {
		t.Error("swap must mark dirty and report the new .fbd source")
	}

	// Rollback restores the old program AND its pre-swap state snapshot
	// (integral back to 5), so the next scan integrates to 6, unscaled.
	if _, err := p.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := p.Run(tags); err != nil {
		t.Fatal(err)
	}
	if got := tags.Real("Out1"); got != 6.0 {
		t.Fatalf("Out1 after rollback = %v, want 6", got)
	}
	if p.Source() != fbdV1 {
		t.Error("rollback must restore the original .fbd source")
	}
}

func TestSwapWarmFBDCompileErrorLeavesRunning(t *testing.T) {
	p, err := Compile(fbdV1)
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(fbdV1, "Out1 := integral", "Out1 := bogus_wire", 1)
	if _, err := p.SwapWarm(bad); err == nil {
		t.Fatal("expected compile error")
	}
	if p.Source() != fbdV1 {
		t.Error("failed swap must leave the running program untouched")
	}
	tags := NewTags()
	tags.SetReal("In1", 1.0)
	tags.SetReal("Out1", 0)
	if err := p.Run(tags); err != nil {
		t.Fatal(err)
	}
}

// Ladder programs warm-swap like any other: a .ld source with a ladder
// FUNCTION_BLOCK (a seal-in plus its own TON), a program-level TON and a
// CTU. The edit adds a rung; every instance must carry across it.
const ldPumpSeq = `FUNCTION_BLOCK PumpSeq
VAR_INPUT  Start : BOOL; Stop : BOOL; Level : REAL; END_VAR
VAR_OUTPUT Run : BOOL; END_VAR
VAR        t1 : TON; END_VAR
LD
  RUNG seal  [ Start | Run ] /Stop GT(Level, 10.0) ( Run )
  RUNG dly   Run t1:TON(PT := T#5S)
END_LD
END_FUNCTION_BLOCK

`

const ldV1 = ldPumpSeq + `PROGRAM Main
VAR_EXTERNAL
  Cmd : BOOL; Halt : BOOL; Lvl : REAL; Tick : BOOL;
  PumpRun : BOOL; Warm : BOOL; Done : BOOL; Ticks : INT;
END_VAR
VAR p1 : PumpSeq; dwell : TON; cnt : CTU; END_VAR
LD
  RUNG call   Cmd p1:PumpSeq(Stop := Halt, Level := Lvl, Run => PumpRun)
  RUNG warm   p1.t1.Q ( Warm )
  RUNG dwell  PumpRun dwell:TON(PT := T#8S)
  RUNG done   dwell.Q ( Done )
  RUNG count  Tick cnt:CTU(PV := 100, CV => Ticks)
END_LD
END_PROGRAM`

// ldV2 adds a lamp rung — the everyday ladder online edit.
var ldV2 = strings.Replace(
	strings.Replace(ldV1, "Ticks : INT;", "Ticks : INT; Lamp : BOOL;", 1),
	"END_LD\nEND_PROGRAM", "  RUNG lamp   /Halt ( Lamp )\nEND_LD\nEND_PROGRAM", 1)

func TestSwapWarmLadderCarriesRetainedState(t *testing.T) {
	p, err := Compile(ldV1)
	if err != nil {
		t.Fatal(err)
	}
	clk := &stepClock{t: time.Unix(1_700_000_000, 0)}
	t0 := clk.t
	tags := NewTags()
	tags.clock = clk
	tags.SetBool("Halt", false)
	tags.SetReal("Lvl", 50)
	scanAt := func(sec int, cmd, tick bool) {
		t.Helper()
		clk.t = t0.Add(time.Duration(sec) * time.Second)
		tags.SetBool("Cmd", cmd)
		tags.SetBool("Tick", tick)
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	// Start at 0 s, release the command, tick the counter on three edges.
	scanAt(0, true, true)
	scanAt(1, false, false)
	scanAt(2, false, true)
	scanAt(3, false, false)
	scanAt(4, false, true)
	if !readTag(t, tags, "PumpRun").B {
		t.Fatal("the block's seal-in should hold PumpRun before the edit")
	}
	if readTag(t, tags, "Warm").B || readTag(t, tags, "Done").B {
		t.Fatal("neither timer may be done 4 s in")
	}
	if got := readTag(t, tags, "Ticks").I; got != 3 {
		t.Fatalf("Ticks before the edit = %d, want 3", got)
	}

	report, err := p.SwapWarm(ldV2)
	if err != nil {
		t.Fatalf("warm swap: %v", err)
	}
	for _, r := range report.Resets {
		switch strings.ToLower(r) {
		case "p1", "dwell", "cnt":
			t.Errorf("%s reported as reset by an edit that only added a rung (resets %v)", r, report.Resets)
		}
	}
	if p.Source() != ldV2 {
		t.Fatal("the ORIGINAL .ld source must be the program of record after the swap")
	}

	// 5 s: the block's t1 has seen Run since 0 s, so Warm. A reset instance
	// would have dropped Run (Cmd is released; the seal-in bit is instance
	// state) and restarted t1.
	scanAt(5, false, false)
	if !readTag(t, tags, "PumpRun").B {
		t.Error("PumpRun dropped across the swap: the block instance's seal-in did not carry")
	}
	if !readTag(t, tags, "Warm").B {
		t.Error("p1.t1 not done at 5 s: the block's own TON restarted at the swap")
	}
	if !readTag(t, tags, "Lamp").B {
		t.Error("the added rung did not go live")
	}
	if readTag(t, tags, "Done").B {
		t.Error("dwell done early at 5 s")
	}
	// The counter carried: one more edge makes 4, not 1.
	scanAt(6, false, true)
	if got := readTag(t, tags, "Ticks").I; got != 4 {
		t.Errorf("Ticks after the edit = %d, want 4 (count carried)", got)
	}
	// 8 s after the original start the program-level TON is done.
	scanAt(7, false, false)
	if readTag(t, tags, "Done").B {
		t.Error("dwell done early at 7 s")
	}
	scanAt(8, false, false)
	if !readTag(t, tags, "Done").B {
		t.Error("dwell not done at 8 s: the program's TON restarted at the swap")
	}
}
