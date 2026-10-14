package ld

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/fbd"
	"github.com/joyautomation/nautilus/lang/ir"
)

// runLD compiles a ladder program through both hops and returns a
// scanner: set inputs, scan once, read outputs.
func runLD(t *testing.T, src string, libs ...string) (scan func(in map[string]ir.Value), h *ldHost) {
	t.Helper()
	out, err := Transpile(src, libs...)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := fbd.Compile(out)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	frame := ir.NewFrame(prog)
	h = &ldHost{vals: map[string]ir.Value{}}
	return func(in map[string]ir.Value) {
		t.Helper()
		for k, v := range in {
			h.vals[k] = v
		}
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatalf("run: %v", err)
		}
	}, h
}

// LD-004: a plain (un-negated) function contact passes power exactly when
// the call returns TRUE, and the negated form passes exactly when it
// returns FALSE.
func TestFunctionContactPassesPowerWhenTrue(t *testing.T) {
	scan, h := runLD(t, `PROGRAM p
VAR_EXTERNAL Mode : DINT; Plain : BOOL; Negated : BOOL; END_VAR
LD
  RUNG plain
    EQ(Mode, 0) ( Plain )
  RUNG neg
    /EQ(Mode, 0) ( Negated )
END_LD
END_PROGRAM`)
	scan(map[string]ir.Value{"Mode": ir.IntVal(0)})
	if !h.vals["Plain"].B || h.vals["Negated"].B {
		t.Fatalf("Mode=0: EQ true, want Plain=true Negated=false; got %v %v", h.vals["Plain"].B, h.vals["Negated"].B)
	}
	scan(map[string]ir.Value{"Mode": ir.IntVal(3)})
	if h.vals["Plain"].B || !h.vals["Negated"].B {
		t.Fatalf("Mode=3: EQ false, want Plain=false Negated=true; got %v %v", h.vals["Plain"].B, h.vals["Negated"].B)
	}
}

// LD-015: all six comparisons work as contacts.
func TestSixComparisonsAsContacts(t *testing.T) {
	scan, h := runLD(t, `PROGRAM p
VAR_EXTERNAL A : DINT; B : DINT; oGT : BOOL; oGE : BOOL; oLT : BOOL; oLE : BOOL; oEQ : BOOL; oNE : BOOL; END_VAR
LD
  RUNG gt  GT(A, B) ( oGT )
  RUNG ge  GE(A, B) ( oGE )
  RUNG lt  LT(A, B) ( oLT )
  RUNG le  LE(A, B) ( oLE )
  RUNG eq  EQ(A, B) ( oEQ )
  RUNG ne  NE(A, B) ( oNE )
END_LD
END_PROGRAM`)
	cases := []struct {
		a, b int64
		want [6]bool // GT GE LT LE EQ NE
	}{
		{7, 5, [6]bool{true, true, false, false, false, true}},
		{5, 5, [6]bool{false, true, false, true, true, false}},
		{3, 5, [6]bool{false, false, true, true, false, true}},
	}
	names := [6]string{"oGT", "oGE", "oLT", "oLE", "oEQ", "oNE"}
	for _, c := range cases {
		scan(map[string]ir.Value{"A": ir.IntVal(c.a), "B": ir.IntVal(c.b)})
		for i, n := range names {
			if got := h.vals[n].B; got != c.want[i] {
				t.Errorf("A=%d B=%d %s = %v, want %v", c.a, c.b, n, got, c.want[i])
			}
		}
	}
}

// LD-014: a coil written on one rung reads back as a contact on the next
// rung in the same scan (and a rung ahead of the writer sees last scan's
// value).
func TestCoilReadsBackOnNextRungSameScan(t *testing.T) {
	scan, h := runLD(t, `PROGRAM p
VAR_EXTERNAL In : BOOL; Mid : BOOL; After : BOOL; Before : BOOL; END_VAR
LD
  RUNG before
    Mid ( Before )
  RUNG writer
    In ( Mid )
  RUNG reader
    Mid ( After )
END_LD
END_PROGRAM`)
	scan(map[string]ir.Value{"In": ir.BoolVal(true)})
	if !h.vals["After"].B {
		t.Error("the rung after the writer must see the coil's new value in the same scan")
	}
	if h.vals["Before"].B {
		t.Error("the rung ahead of the writer must still see the previous scan's value")
	}
	scan(map[string]ir.Value{"In": ir.BoolVal(false)})
	if h.vals["After"].B {
		t.Error("After must drop in the same scan the writer drops")
	}
	if !h.vals["Before"].B {
		t.Error("Before sees last scan's Mid (TRUE) one scan later")
	}
}

// LD-018: a coil on a non-BOOL target is refused.
func TestCoilOnNonBoolTargetRefused(t *testing.T) {
	for _, typ := range []string{"DINT", "REAL", "TIME"} {
		src := "PROGRAM p\nVAR_EXTERNAL A : BOOL; N : " + typ + "; END_VAR\nLD\n  RUNG r\n    A ( N )\nEND_LD\nEND_PROGRAM"
		out, err := Transpile(src)
		if err == nil {
			_, err = fbd.Compile(out)
		}
		if err == nil {
			t.Errorf("a coil on a %s tag compiled", typ)
		}
		t.Logf("%s: %v", typ, err)
	}
	ok := "PROGRAM p\nVAR_EXTERNAL A : BOOL; N : BOOL; END_VAR\nLD\n  RUNG r\n    A ( N )\nEND_LD\nEND_PROGRAM"
	out, err := Transpile(ok)
	if err == nil {
		_, err = fbd.Compile(out)
	}
	if err != nil {
		t.Errorf("a coil on a BOOL tag must compile: %v", err)
	}
}

// LD-030: a coil inside a branch, or ahead of a later block, is an error.
func TestCoilZoneIsContiguousAtRightEnd(t *testing.T) {
	cases := []struct{ name, body string }{
		{"coil in a branch leg", "[ Start ( Run ) | Stop ] ( Alm )"},
		{"coil in the second leg", "[ Start | Stop ( Run ) ] ( Alm )"},
		{"coil ahead of a block", "Start ( Run ) t1:TON(PT := T#1S)"},
	}
	for _, tc := range cases {
		src := "PROGRAM p\nVAR_EXTERNAL Start : BOOL; Stop : BOOL; Run : BOOL; Alm : BOOL; END_VAR\nVAR t1 : TON; END_VAR\nLD\n  RUNG r\n    " + tc.body + "\nEND_LD\nEND_PROGRAM"
		_, err := Transpile(src)
		if err == nil {
			t.Errorf("%s: transpiled, want an error", tc.name)
		}
		t.Logf("%s: %v", tc.name, err)
	}
}

// LD-022: power lands on the first BOOL VAR_INPUT NOT bound by name — a
// first BOOL input bound by name is skipped and power lands on the next.
func TestLadderPowerSkipsBoundBoolInput(t *testing.T) {
	lib := `FUNCTION_BLOCK Pick
VAR_INPUT  X : BOOL; Y : BOOL; END_VAR
VAR_OUTPUT Q : BOOL; END_VAR
Q := Y AND NOT X;
END_FUNCTION_BLOCK`
	src := `PROGRAM p
VAR_EXTERNAL A : BOOL; Out : BOOL; END_VAR
VAR k1 : Pick; END_VAR
LD
  RUNG r  A k1:Pick(X := FALSE) ( Out )
END_LD
END_PROGRAM`
	got := toST(t, src, lib)
	if !strings.Contains(got, "Y := A") {
		t.Fatalf("power must land on Y, the first BOOL input not bound by name:\n%s", got)
	}
	prog := compileST(t, lib+"\n"+got)
	h := newHost()
	frame := ir.NewFrame(prog)
	for _, a := range []bool{true, false, true} {
		h.vals["A"] = ir.BoolVal(a)
		if err := ir.Run(prog, frame, h); err != nil {
			t.Fatal(err)
		}
		if h.vals["Out"].B != a {
			t.Errorf("A=%v: Out=%v, want Out to follow the rung power", a, h.vals["Out"].B)
		}
	}
}
