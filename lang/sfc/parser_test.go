package sfc

import (
	"strconv"
	"strings"
	"testing"
)

// workedExample is the §1.2 worked example from docs/design/sfc.md,
// reproduced VERBATIM (byte-for-byte, including its comments) — it must
// parse cleanly and check clean (zero diagnostics of either severity).
const workedExample = `PROGRAM TankBatch
VAR_EXTERNAL
  Start     : BOOL;
  Abort     : BOOL;
  Level     : REAL;
  TempC     : REAL;
  FillSP    : REAL;
  EmptySP   : REAL;
  HeatSP    : REAL;
  FillValve : BOOL;
  DrainValve: BOOL;
  Heater    : BOOL;
  Mixer     : BOOL;
  RunLamp   : BOOL;
  AbortLamp : BOOL;
  BatchCount: INT;
END_VAR
VAR
  mixT : TON;   (* referenced by the Mix action body *)
END_VAR
SFC

  INITIAL_STEP Idle:
    N  RunLamp;              (* boolean action: RunLamp := Idle.X ... see §2.5 *)
    R  AbortLamp;            (* clear the abort lamp on return to Idle *)
  END_STEP

  STEP Fill:
    N  RunLamp;
    N  FillValve;            (* valve open while Fill active *)
  END_STEP

  STEP Heat:
    N  RunLamp;
    N  Heater;
  END_STEP

  STEP Mix:
    N  RunLamp;
    N  Stir;                 (* ACTION block, below *)
  END_STEP

  STEP Drain:
    N   RunLamp;
    N   DrainValve;
    P1  CountBatch;          (* pulse: run its body once on activation *)
  END_STEP

  (* --- transitions --- *)

  TRANSITION t_start FROM Idle TO Fill := Start AND NOT Abort;
  END_TRANSITION

  (* alternative divergence out of Fill: abort has priority (declared first) *)
  TRANSITION t_abort FROM Fill TO Idle := Abort;
  END_TRANSITION
  TRANSITION t_full  FROM Fill TO (Heat, Mix) := Level >= FillSP;   (* simultaneous divergence *)
  END_TRANSITION

  (* simultaneous convergence: fires only when BOTH Heat and Mix are active *)
  TRANSITION t_done FROM (Heat, Mix) TO Drain := (TempC >= HeatSP) AND mixT.Q;
  END_TRANSITION

  TRANSITION t_empty FROM Drain TO Idle := Level <= EmptySP;
  END_TRANSITION

  (* --- action blocks (ST bodies) --- *)

  ACTION Stir:
    Mixer := Mix.X;                  (* track the step: drops to FALSE on the final scan *)
    mixT(IN := Mix.X, PT := T#30S);  (* mixing timer; falling edge resets it (§2.5.1) *)
  END_ACTION

  ACTION CountBatch:
    BatchCount := BatchCount + 1;     (* runs exactly once, on Drain activation *)
  END_ACTION

  (* S/R stored-action demo lives on the lamp: an operator-visible latch *)
  ACTION HoldAbort:
  END_ACTION

END_SFC
END_PROGRAM
`

func TestParseWorkedExample(t *testing.T) {
	prog, err := Parse(workedExample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if prog.Name != "TankBatch" {
		t.Errorf("Name = %q, want TankBatch", prog.Name)
	}

	// Header VAR blocks reuse lang/st's declaration grammar verbatim.
	var totalVars int
	for _, vb := range prog.VarBlocks {
		totalVars += len(vb.Variables)
	}
	if totalVars != 15 {
		t.Errorf("total declared vars = %d, want 15 (14 VAR_EXTERNAL + 1 VAR)", totalVars)
	}

	if len(prog.Steps) != 5 {
		t.Fatalf("Steps = %d, want 5", len(prog.Steps))
	}
	wantSteps := []struct {
		name    string
		initial bool
		line    int
		nAssoc  int
	}{
		{"Idle", true, 23, 2},
		{"Fill", false, 28, 2},
		{"Heat", false, 33, 2},
		{"Mix", false, 38, 2},
		{"Drain", false, 43, 3},
	}
	for i, want := range wantSteps {
		s := prog.Steps[i]
		if s.Name != want.name || s.Initial != want.initial {
			t.Errorf("Steps[%d] = %+v, want name=%s initial=%v", i, s, want.name, want.initial)
		}
		if s.Pos.Line != want.line {
			t.Errorf("Steps[%d] (%s) line = %d, want %d", i, s.Name, s.Pos.Line, want.line)
		}
		if len(s.Actions) != want.nAssoc {
			t.Errorf("Steps[%d] (%s) has %d associations, want %d", i, s.Name, len(s.Actions), want.nAssoc)
		}
	}

	if len(prog.Transitions) != 5 {
		t.Fatalf("Transitions = %d, want 5", len(prog.Transitions))
	}
	wantTrans := []struct {
		name string
		from []string
		to   []string
		line int
		cond string
	}{
		{"t_start", []string{"Idle"}, []string{"Fill"}, 51, "Start AND NOT Abort"},
		{"t_abort", []string{"Fill"}, []string{"Idle"}, 55, "Abort"},
		{"t_full", []string{"Fill"}, []string{"Heat", "Mix"}, 57, "Level >= FillSP"},
		{"t_done", []string{"Heat", "Mix"}, []string{"Drain"}, 61, "(TempC >= HeatSP) AND mixT.Q"},
		{"t_empty", []string{"Drain"}, []string{"Idle"}, 64, "Level <= EmptySP"},
	}
	for i, want := range wantTrans {
		tr := prog.Transitions[i]
		if tr.Name != want.name {
			t.Errorf("Transitions[%d].Name = %q, want %q", i, tr.Name, want.name)
		}
		if !equalStrs(tr.From, want.from) {
			t.Errorf("%s.From = %v, want %v", want.name, tr.From, want.from)
		}
		if !equalStrs(tr.To, want.to) {
			t.Errorf("%s.To = %v, want %v", want.name, tr.To, want.to)
		}
		if tr.Pos.Line != want.line {
			t.Errorf("%s line = %d, want %d", want.name, tr.Pos.Line, want.line)
		}
		if tr.Cond.Text != want.cond {
			t.Errorf("%s.Cond.Text = %q, want %q", want.name, tr.Cond.Text, want.cond)
		}
	}

	if len(prog.Actions) != 3 {
		t.Fatalf("Actions = %d, want 3", len(prog.Actions))
	}
	wantActions := []struct {
		name string
		line int
		body string
	}{
		{"Stir", 69, "Mixer := Mix.X;                  (* track the step: drops to FALSE on the final scan *)\n    mixT(IN := Mix.X, PT := T#30S);  (* mixing timer; falling edge resets it (§2.5.1) *)"},
		{"CountBatch", 74, "BatchCount := BatchCount + 1;     (* runs exactly once, on Drain activation *)"},
		{"HoldAbort", 79, ""},
	}
	for i, want := range wantActions {
		a := prog.Actions[i]
		if a.Name != want.name {
			t.Errorf("Actions[%d].Name = %q, want %q", i, a.Name, want.name)
		}
		if a.Pos.Line != want.line {
			t.Errorf("%s line = %d, want %d", want.name, a.Pos.Line, want.line)
		}
		if a.Body.Text != want.body {
			t.Errorf("%s.Body.Text = %q, want %q", want.name, a.Body.Text, want.body)
		}
	}

	// The worked example is designed to be structurally clean.
	if diags := Check(prog); len(diags) != 0 {
		t.Errorf("Check() on the worked example = %v, want no diagnostics", diags)
	}
}

func equalStrs(a, b []string) bool {
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

// TestParseAlternativeDivergence checks the alternative-divergence form in
// isolation: two transitions sharing a single FROM source, priority by
// declaration order.
func TestParseAlternativeDivergence(t *testing.T) {
	src := `PROGRAM P
VAR
  A : BOOL;
  B : BOOL;
END_VAR
SFC
INITIAL_STEP S0:
END_STEP
STEP S1:
END_STEP
STEP S2:
END_STEP
TRANSITION t1 FROM S0 TO S1 := A;
END_TRANSITION
TRANSITION t2 FROM S0 TO S2 := B;
END_TRANSITION
TRANSITION FROM S1 TO S0 := TRUE;
END_TRANSITION
TRANSITION FROM S2 TO S0 := TRUE;
END_TRANSITION
END_SFC
END_PROGRAM
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Transitions) != 4 {
		t.Fatalf("Transitions = %d, want 4", len(prog.Transitions))
	}
	if prog.Transitions[0].Name != "t1" || prog.Transitions[1].Name != "t2" {
		t.Errorf("declaration order not preserved: %q, %q", prog.Transitions[0].Name, prog.Transitions[1].Name)
	}
	// Unnamed transitions parse fine and get a positional label.
	if prog.Transitions[2].Name != "" {
		t.Errorf("Transitions[2].Name = %q, want unnamed", prog.Transitions[2].Name)
	}
	if !strings.Contains(trName(prog.Transitions[2]), "unnamed") {
		t.Errorf("trName(unnamed) = %q, want it to say so", trName(prog.Transitions[2]))
	}
}

// TestParseSimultaneousDivergenceConvergence exercises FROM/TO step-sets and
// a condition split across multiple lines (verbatim multi-line span
// capture).
func TestParseSimultaneousDivergenceConvergence(t *testing.T) {
	src := `PROGRAM P
VAR
  Go : BOOL;
  Done : BOOL;
END_VAR
SFC
INITIAL_STEP S0:
END_STEP
STEP A:
END_STEP
STEP B:
END_STEP
STEP C:
END_STEP
TRANSITION t1 FROM S0 TO (A, B) := Go;
END_TRANSITION
TRANSITION t2 FROM (A, B) TO C :=
    Done
    AND Go;
END_TRANSITION
TRANSITION FROM C TO S0 := TRUE;
END_TRANSITION
END_SFC
END_PROGRAM
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t1 := prog.Transitions[0]
	if !equalStrs(t1.To, []string{"A", "B"}) {
		t.Errorf("t1.To = %v, want [A B]", t1.To)
	}
	t2 := prog.Transitions[1]
	if !equalStrs(t2.From, []string{"A", "B"}) {
		t.Errorf("t2.From = %v, want [A B]", t2.From)
	}
	wantCond := "Done\n    AND Go"
	if t2.Cond.Text != wantCond {
		t.Errorf("t2.Cond.Text = %q, want %q", t2.Cond.Text, wantCond)
	}
	if t2.Cond.Line != 18 {
		t.Errorf("t2.Cond.Line = %d, want 18", t2.Cond.Line)
	}
}

// ─── malformed input / position assertions ─────────────────────────────────

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string
		line    int
	}{
		{
			name: "missing END_SFC",
			src: `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP S0:
END_STEP
END_PROGRAM
`,
			wantErr: "SFC ... END_SFC",
		},
		{
			name: "missing END_STEP",
			src: `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP S0:
END_SFC
END_PROGRAM
`,
			wantErr: "missing END_STEP",
			line:    4,
		},
		{
			name: "missing FROM",
			src: `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP S0:
END_STEP
TRANSITION S0 TO S0 := TRUE;
END_TRANSITION
END_SFC
END_PROGRAM
`,
			wantErr: "expected FROM",
			line:    6,
		},
		{
			name: "indicator on a transition",
			src: `PROGRAM P
VAR Ind : BOOL; END_VAR
SFC
INITIAL_STEP S0:
END_STEP
TRANSITION t FROM S0 TO S0 (Ind) := TRUE;
END_TRANSITION
END_SFC
END_PROGRAM
`,
			wantErr: "transition t: indicator variables on a transition are not supported — a transition is",
			line:    6,
		},
		{
			name: "missing END_ACTION",
			src: `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP S0:
END_STEP
ACTION Foo:
  X := TRUE;
END_SFC
END_PROGRAM
`,
			wantErr: "missing END_ACTION",
			line:    6,
		},
		{
			name: "unexpected top-level token",
			src: `PROGRAM P
VAR END_VAR
SFC
INITIAL_STEP S0:
END_STEP
GARBAGE
END_SFC
END_PROGRAM
`,
			wantErr: "expected STEP, INITIAL_STEP, TRANSITION, or ACTION",
			line:    6,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.src)
			if err == nil {
				t.Fatalf("Parse: expected an error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Parse error = %q, want it to contain %q", err.Error(), c.wantErr)
			}
			if c.line != 0 {
				wantLine := "line " + strconv.Itoa(c.line)
				if !strings.Contains(err.Error(), wantLine) {
					t.Errorf("Parse error = %q, want it to mention %q", err.Error(), wantLine)
				}
			}
		})
	}
}

func TestHasBlock(t *testing.T) {
	if !HasBlock(workedExample) {
		t.Error("HasBlock(workedExample) = false, want true")
	}
	if HasBlock("PROGRAM P\nEND_PROGRAM\n") {
		t.Error("HasBlock on plain ST = true, want false")
	}
}
