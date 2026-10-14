package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// run compiles FBD source and executes one scan against a tag host, returning
// the host so the test can assert output values.
func run(t *testing.T, src string, seed map[string]ir.Value) *host {
	t.Helper()
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v\n--- transpiled ---\n%s", err, mustTranspile(src))
	}
	h := &host{vals: map[string]ir.Value{}}
	for k, v := range seed {
		h.vals[k] = v
	}
	frame := ir.NewFrame(prog)
	if err := ir.Run(prog, frame, h); err != nil {
		t.Fatalf("run: %v", err)
	}
	return h
}

// scanner compiles src once and keeps its frame and host across scans, so a
// test can drive retained state (a latch, a timer) the way the runtime does:
// nothing is fed back by hand.
type scanner struct {
	t     *testing.T
	prog  *ir.Program
	frame *ir.Frame
	h     *host
}

func newScanner(t *testing.T, src string) *scanner {
	t.Helper()
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v\n--- transpiled ---\n%s", err, mustTranspile(src))
	}
	return &scanner{t: t, prog: prog, frame: ir.NewFrame(prog), h: &host{vals: map[string]ir.Value{}}}
}

// scan writes set into the tag store, advances the clock to nowMs, and runs
// one scan.
func (s *scanner) scan(nowMs int64, set map[string]ir.Value) {
	s.t.Helper()
	for k, v := range set {
		s.h.vals[k] = v
	}
	s.h.now = nowMs
	if err := ir.Run(s.prog, s.frame, s.h); err != nil {
		s.t.Fatalf("run: %v", err)
	}
}

func mustTranspile(src string) string {
	s, err := Transpile(src)
	if err != nil {
		return "TRANSPILE ERROR: " + err.Error()
	}
	return s
}

// host is a minimal ir.Host backed by a map.
type host struct {
	vals map[string]ir.Value
	now  int64
}

func (h *host) ReadGlobal(name string) (ir.Value, error) {
	if v, ok := h.vals[name]; ok {
		return v, nil
	}
	return ir.Value{}, nil // undefined reads as zero, like a fresh tag
}
func (h *host) WriteGlobal(name string, v ir.Value) error { h.vals[name] = v; return nil }
func (h *host) NowMs() int64                              { return h.now }

func TestSealInLatch(t *testing.T) {
	// Classic seal-in: Run = (Start OR Run) AND NOT Stop. The feedback is
	// through the Run variable, which the netlist reads before it writes.
	// One program and one tag store across all scans: nothing re-seeds Run,
	// so it can only stay on if the latch really holds it.
	src := `PROGRAM Latch
VAR_EXTERNAL
  Start : BOOL; Stop : BOOL; Run : BOOL;
END_VAR
FBD
  seal  = OR(Start, Run)
  Run  := AND(seal, NOT Stop)
END_FBD
END_PROGRAM`
	s := newScanner(t, src)
	tru, fls := ir.BoolVal(true), ir.BoolVal(false)

	s.scan(0, map[string]ir.Value{"Start": fls, "Stop": fls})
	if s.h.vals["Run"].B {
		t.Fatal("Run must start off")
	}
	// Start pressed → latches on.
	s.scan(0, map[string]ir.Value{"Start": tru})
	if !s.h.vals["Run"].B {
		t.Fatal("Start should latch Run on")
	}
	// Start released → stays on (seal-in), scan after scan.
	s.scan(0, map[string]ir.Value{"Start": fls})
	s.scan(0, nil)
	if !s.h.vals["Run"].B {
		t.Fatal("Run should seal in when Start released")
	}
	// Stop pressed → drops out, and stays out once Stop is released.
	s.scan(0, map[string]ir.Value{"Stop": tru})
	if s.h.vals["Run"].B {
		t.Fatal("Stop should drop Run")
	}
	s.scan(0, map[string]ir.Value{"Stop": fls})
	if s.h.vals["Run"].B {
		t.Fatal("Run must stay off after Stop is released")
	}
}

func TestArithmeticAndFanout(t *testing.T) {
	// A wire feeding two coils (fan-out) plus an arithmetic block.
	src := `PROGRAM Calc
VAR_EXTERNAL
  A : REAL; B : REAL; Sum : REAL; Doubled : REAL; Big : BOOL;
END_VAR
FBD
  s = ADD(A, B)
  Sum := s
  Doubled := MUL(s, 2.0)
  Big := GT(s, 100.0)
END_FBD
END_PROGRAM`
	h := run(t, src, map[string]ir.Value{"A": ir.RealVal(60), "B": ir.RealVal(50)})
	if h.vals["Sum"].F != 110 {
		t.Errorf("Sum = %v, want 110", h.vals["Sum"].F)
	}
	if h.vals["Doubled"].F != 220 {
		t.Errorf("Doubled = %v, want 220", h.vals["Doubled"].F)
	}
	if !h.vals["Big"].B {
		t.Errorf("Big = %v, want true (110 > 100)", h.vals["Big"].B)
	}
}

func TestFunctionBlockInstance(t *testing.T) {
	// A TON instance whose output pin feeds a coil; ordering must place the
	// call before the read.
	src := `PROGRAM Timed
VAR_EXTERNAL
  Run : BOOL; Elapsed : BOOL;
END_VAR
FBD
  Elapsed := t1.Q
  t1 : TON(IN := Run, PT := T#5S)
END_FBD
END_PROGRAM`
	st := mustTranspile(src)
	if strings.Index(st, "t1(IN") > strings.Index(st, "Elapsed := t1.Q") {
		t.Fatalf("FB call must be ordered before its pin read:\n%s", st)
	}
	if !strings.Contains(st, "VAR\n  t1 : TON;") {
		t.Fatalf("FB instance not declared:\n%s", st)
	}
	// Run it: the TON's Q reaches the coil only once PT has elapsed.
	sc := newScanner(t, src)
	sc.scan(1000, map[string]ir.Value{"Run": ir.BoolVal(true)})
	if sc.h.vals["Elapsed"].B {
		t.Fatal("Q must be false at the first scan of IN")
	}
	sc.scan(5999, nil)
	if sc.h.vals["Elapsed"].B {
		t.Fatal("Q must be false until PT (5 s) has elapsed")
	}
	sc.scan(6000, nil)
	if !sc.h.vals["Elapsed"].B {
		t.Fatal("Q must reach the coil once PT has elapsed")
	}
	sc.scan(6001, map[string]ir.Value{"Run": ir.BoolVal(false)})
	if sc.h.vals["Elapsed"].B {
		t.Fatal("Q must drop with IN")
	}
}

func TestTranspileLineMap(t *testing.T) {
	// Each transpiled line must map back to the .fbd line it came from:
	// header verbatim, FB decls and statements to their netlist lines.
	src := `PROGRAM Timed
VAR_EXTERNAL
  Run : BOOL; Elapsed : BOOL;
END_VAR
FBD
  Elapsed := t1.Q
  t1 : TON(IN := Run, PT := T#5S)
END_FBD
END_PROGRAM`
	stSrc, lineMap, err := TranspileWithLines(src)
	if err != nil {
		t.Fatalf("TranspileWithLines: %v", err)
	}
	if got, want := stSrc, mustTranspile(src); got != want {
		t.Fatalf("TranspileWithLines output differs from Transpile:\n%s\n---\n%s", got, want)
	}
	stLines := strings.Split(stSrc, "\n")
	if len(lineMap) != len(stLines) {
		t.Fatalf("lineMap has %d entries for %d lines", len(lineMap), len(stLines))
	}
	find := func(prefix string) int {
		for i, l := range stLines {
			if strings.HasPrefix(strings.TrimSpace(l), prefix) {
				return i
			}
		}
		t.Fatalf("no transpiled line starts with %q:\n%s", prefix, stSrc)
		return -1
	}
	// Header maps 1:1; the t1 decl and call map to .fbd line 7; the coil to 6.
	for i, want := range map[int]int{
		0:                     1, // PROGRAM Timed
		2:                     3, // Run : BOOL; ...
		find("t1 : TON;"):     7,
		find("t1(IN"):         7,
		find("Elapsed := t1"): 6,
		find("END_PROGRAM"):   9,
	} {
		if lineMap[i] != want {
			t.Errorf("lineMap[%d] (%q) = %d, want %d", i, stLines[i], lineMap[i], want)
		}
	}
}

func TestCombinationalLoopRejected(t *testing.T) {
	src := `PROGRAM Loop
VAR_EXTERNAL X : BOOL; END_VAR
FBD
  a = AND(b, X)
  b = OR(a, X)
  X := a
END_FBD
END_PROGRAM`
	if _, err := Compile(src); err == nil || !strings.Contains(err.Error(), "combinational loop") {
		t.Fatalf("expected combinational-loop error, got %v", err)
	}
}

func TestSharedTypes(t *testing.T) {
	// An FBD POU using a UDT — proves it lowers through the same type system.
	src := `TYPE
  Motor_Type : STRUCT
    Speed : REAL;
    Run : BOOL;
  END_STRUCT;
END_TYPE
PROGRAM UsesUDT
VAR_EXTERNAL
  M : Motor_Type; FastOut : BOOL;
END_VAR
FBD
  FastOut := GT(M.Speed, 50.0)
END_FBD
END_PROGRAM`
	h := run(t, src, map[string]ir.Value{
		"M": {Kind: ir.TypeStruct, Fld: []ir.Value{ir.RealVal(75), ir.BoolVal(true)}},
	})
	if !h.vals["FastOut"].B {
		t.Errorf("FastOut = %v, want true", h.vals["FastOut"].B)
	}
}

// `pin => target` output bindings (IEC formal-call outputs) pass through
// the netlist verbatim — the one way to capture ET/CV into a variable at
// the call site (coils are the BOOL path).
func TestOutputBinding(t *testing.T) {
	src := `PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; Elapsed : TIME; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S, ET => Elapsed)
  Done := t1.Q
END_FBD
END_PROGRAM`
	out, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ET => Elapsed") {
		t.Fatalf("binding must emit verbatim:\n%s", out)
	}
	if _, err := Compile(src); err != nil {
		t.Fatalf("does not compile: %v", err)
	}
	m, err := Graph(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range m.Nodes {
		if n.Kind == "fb" && n.Label == "t1" {
			has := false
			for _, o := range n.Outputs {
				if o == "ET" {
					has = true
				}
			}
			if !has {
				t.Fatalf("ET must render as an output pin: %+v", n)
			}
		}
	}
	// The binding runs: ET lands in Elapsed at the call site.
	sc := newScanner(t, src)
	sc.scan(1000, map[string]ir.Value{"Run": ir.BoolVal(true)})
	sc.scan(3000, nil)
	if got := sc.h.vals["Elapsed"]; got.I != 2000 {
		t.Fatalf("Elapsed = %+v, want 2000 (ET bound to the variable)", got)
	}
	if sc.h.vals["Done"].B {
		t.Fatal("Done must wait for PT")
	}
	sc.scan(6000, nil)
	if got := sc.h.vals["Elapsed"]; got.I != 5000 || !sc.h.vals["Done"].B {
		t.Fatalf("Elapsed = %+v Done = %v, want 5000 and true", got, sc.h.vals["Done"].B)
	}
	// A non-variable target is rejected at parse.
	bad := `PROGRAM p
VAR_EXTERNAL Run : BOOL; END_VAR
FBD
  t1 : TON(IN := Run, ET => 5)
END_FBD
END_PROGRAM`
	if _, err := Transpile(bad); err == nil {
		t.Fatal("literal binding target must be rejected")
	}
}

// A file may hold several netlist blocks, one per POU — how a
// FUNCTION_BLOCK carries an FBD (or, through the LD hop, a ladder) body
// alongside the file's PROGRAM. Each block becomes its own POU's
// statements; everything between them passes through as ST verbatim.
func TestTranspileMultipleBlocks(t *testing.T) {
	src := `FUNCTION_BLOCK Scale
VAR_INPUT  IN : REAL; END_VAR
VAR_OUTPUT OUT : REAL; END_VAR
FBD
  OUT := MUL(IN, 2.0)
END_FBD
END_FUNCTION_BLOCK

PROGRAM Main
VAR_EXTERNAL Raw : REAL; Eng : REAL; END_VAR
FBD
  s1 : Scale(IN := Raw)
  Eng := s1.OUT
END_FBD
END_PROGRAM
`
	out, lineMap, err := TranspileWithLines(src)
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	for _, want := range []string{
		"FUNCTION_BLOCK Scale",
		"OUT := (IN * 2.0);",
		"END_FUNCTION_BLOCK",
		"PROGRAM Main",
		"  s1 : Scale;",
		"s1(IN := Raw);",
		"Eng := s1.OUT;",
		"END_PROGRAM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "FBD"); n != 0 {
		t.Errorf("no FBD keyword should survive, found %d:\n%s", n, out)
	}
	if got, want := len(lineMap), len(strings.Split(out, "\n")); got != want {
		t.Fatalf("lineMap has %d entries for %d lines", got, want)
	}
	// The second POU's statement maps back to ITS netlist line, not the
	// first block's — the map composes across POUs.
	srcLines := strings.Split(src, "\n")
	for i, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Eng := s1.OUT;") {
			if !strings.Contains(srcLines[lineMap[i]-1], "Eng := s1.OUT") {
				t.Fatalf("maps to source line %d (%q)", lineMap[i], srcLines[lineMap[i]-1])
			}
			return
		}
	}
	t.Fatal("statement not found")
}

func TestSplitBlocksRejectsMalformed(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"unclosed", "PROGRAM p\nFBD\n  a := b\nEND_PROGRAM\n", "no END_FBD"},
		{"orphan end", "PROGRAM p\nEND_FBD\nEND_PROGRAM\n", "END_FBD with no FBD"},
		{"nested", "PROGRAM p\nFBD\nFBD\nEND_FBD\nEND_PROGRAM\n", "inside an FBD block"},
		{"none", "PROGRAM p\nEND_PROGRAM\n", "must contain an FBD"},
	}
	for _, tc := range cases {
		_, err := Transpile(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

// An instance the POU's header already declares is CALLED by the netlist,
// not re-declared — writing both reads naturally and must not collide.
func TestInstanceDeclaredInHeaderIsNotRedeclared(t *testing.T) {
	src := `PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
VAR t1 : TON; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S)
  Done := t1.Q
END_FBD
END_PROGRAM
`
	out, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "t1 : TON"); n != 1 {
		t.Fatalf("t1 declared %d times, want 1:\n%s", n, out)
	}
	if _, err := Compile(src); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
}
