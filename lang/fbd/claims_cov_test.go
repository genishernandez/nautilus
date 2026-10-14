package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// prog wraps netlist lines in a PROGRAM with the given external vars.
func prog(vars, body string) string {
	return "PROGRAM P\nVAR_EXTERNAL\n" + vars + "\nEND_VAR\nFBD\n" + body + "\nEND_FBD\nEND_PROGRAM"
}

// A bare `lic : TON` in the body declares the instance; a later `lic(...)`
// calls it. The call is not repeated in the declaration.
func TestSplitDeclareThenCall(t *testing.T) {
	src := prog("  Run : BOOL; Done : BOOL;", `  t1 : TON
  Done := t1.Q
  t1(IN := Run, PT := T#5S)`)
	sc := newScanner(t, src)
	sc.scan(1000, map[string]ir.Value{"Run": ir.BoolVal(true)})
	sc.scan(5999, nil)
	if sc.h.vals["Done"].B {
		t.Fatal("Q must be false until PT has elapsed")
	}
	sc.scan(6000, nil)
	if !sc.h.vals["Done"].B {
		t.Fatal("the later t1(...) call must drive the declared instance")
	}
}

// BOOL and STRING constants are literal inputs.
func TestBoolAndStringLiteralInputs(t *testing.T) {
	src := prog("  Flag : BOOL; Name : STRING; Both : BOOL; IsOk : BOOL; Joined : STRING;",
		`  Both := AND(TRUE, Flag)
  IsOk := EQ(Name, 'ok')
  Joined := CONCAT(Name, '!')`)
	h := run(t, src, map[string]ir.Value{"Flag": ir.BoolVal(true), "Name": ir.StringVal("ok")})
	if !h.vals["Both"].B || !h.vals["IsOk"].B || h.vals["Joined"].S != "ok!" {
		t.Fatalf("Both=%v IsOk=%v Joined=%q, want true true \"ok!\"", h.vals["Both"].B, h.vals["IsOk"].B, h.vals["Joined"].S)
	}
	h = run(t, src, map[string]ir.Value{"Flag": ir.BoolVal(false), "Name": ir.StringVal("no")})
	if h.vals["Both"].B || h.vals["IsOk"].B {
		t.Fatalf("Both=%v IsOk=%v, want false false", h.vals["Both"].B, h.vals["IsOk"].B)
	}
}

// A statement may end with ';' or a newline.
func TestSemicolonTerminatedStatements(t *testing.T) {
	src := prog("  A : REAL; Sum : REAL; Twice : REAL;", `  s = ADD(A, 1.0);
  Sum := s;
  Twice := MUL(s, 2.0)`)
	h := run(t, src, map[string]ir.Value{"A": ir.RealVal(4)})
	if h.vals["Sum"].F != 5 || h.vals["Twice"].F != 10 {
		t.Fatalf("Sum=%v Twice=%v, want 5 10", h.vals["Sum"].F, h.vals["Twice"].F)
	}
}

// A standard function called with every input named, and no EN.
func TestAllNamedStandardCallWithoutEN(t *testing.T) {
	src := prog("  X : REAL; Y : REAL;", `  Y := LIMIT(IN := X, MX := 9.0, MN := 0.0)`)
	for _, c := range []struct{ in, want float64 }{{-3, 0}, {4, 4}, {12, 9}} {
		h := run(t, src, map[string]ir.Value{"X": ir.RealVal(c.in)})
		if h.vals["Y"].F != c.want {
			t.Errorf("LIMIT(0, %v, 9) = %v, want %v", c.in, h.vals["Y"].F, c.want)
		}
	}
}

// A wire is inlined at every read: an expression read after one of its
// inputs has been rewritten sees the new input, which a value held in shared
// state could not.
func TestWireInlinedAtEveryRead(t *testing.T) {
	src := prog("  A : REAL; First : REAL; Second : REAL;", `  w = ADD(A, 1.0)
  First := w
  A := 100.0
  Second := w`)
	// Fan-out duplicates the expression in the generated source: no temporary
	// holds the wire.
	if st := mustTranspile(src); strings.Count(st, "(A + 1.0)") != 2 || strings.Contains(st, "w :=") {
		t.Fatalf("the wire's expression must appear at both reads, with no w variable:\n%s", st)
	}
	h := run(t, src, map[string]ir.Value{"A": ir.RealVal(4)})
	if h.vals["First"].F != 5 {
		t.Fatalf("First = %v, want 5", h.vals["First"].F)
	}
	if h.vals["Second"].F != 101 {
		t.Fatalf("Second = %v, want 101 (the wire re-evaluates at its second read)", h.vals["Second"].F)
	}
}

// Arities: n-ary AND/OR/XOR/ADD/MUL, binary SUB/DIV/MOD/comparisons, MOVE
// passes one input through.
func TestFunctionArities(t *testing.T) {
	vars := "  a : BOOL; b : BOOL; c : BOOL; x : INT; y : INT; z : INT; And3 : BOOL; Or3 : BOOL; Xor3 : BOOL; Add3 : INT; Mul3 : INT; Mv : INT;"
	body := `  And3 := AND(a, b, c)
  Or3 := OR(a, b, c)
  Xor3 := XOR(a, b, c)
  Add3 := ADD(x, y, z)
  Mul3 := MUL(x, y, z)
  Mv := MOVE(z)`
	T, F := ir.BoolVal(true), ir.BoolVal(false)
	seed := func(a, b, c bool) map[string]ir.Value {
		bv := func(v bool) ir.Value {
			if v {
				return T
			}
			return F
		}
		return map[string]ir.Value{"a": bv(a), "b": bv(b), "c": bv(c), "x": ir.IntVal(2), "y": ir.IntVal(3), "z": ir.IntVal(4)}
	}
	h := run(t, prog(vars, body), seed(true, true, false))
	if h.vals["And3"].B || !h.vals["Or3"].B || h.vals["Xor3"].B {
		t.Errorf("(T,T,F): And3=%v Or3=%v Xor3=%v, want false true false", h.vals["And3"].B, h.vals["Or3"].B, h.vals["Xor3"].B)
	}
	h = run(t, prog(vars, body), seed(true, true, true))
	if !h.vals["And3"].B || !h.vals["Xor3"].B {
		t.Errorf("(T,T,T): And3=%v Xor3=%v, want true true (the third input counts)", h.vals["And3"].B, h.vals["Xor3"].B)
	}
	if h.vals["Add3"].I != 9 || h.vals["Mul3"].I != 24 || h.vals["Mv"].I != 4 {
		t.Errorf("Add3=%v Mul3=%v Mv=%v, want 9 24 4", h.vals["Add3"].I, h.vals["Mul3"].I, h.vals["Mv"].I)
	}

	// Too few: one input is refused for the n-ary blocks; two are enough.
	for _, c := range []struct{ fn, out string }{{"AND", "a"}, {"OR", "a"}, {"XOR", "a"}, {"ADD", "x"}, {"MUL", "x"}} {
		_, err := Compile(prog(vars, "  "+c.out+" := "+c.fn+"("+c.out+")"))
		if err == nil || !strings.Contains(err.Error(), c.fn+" needs at least 2 inputs") {
			t.Errorf("%s with one input: err = %v, want %q", c.fn, err, c.fn+" needs at least 2 inputs")
		}
		if _, err := Compile(prog(vars, "  "+c.out+" := "+c.fn+"("+c.out+", "+c.out+")")); err != nil {
			t.Errorf("%s with two inputs failed to compile: %v", c.fn, err)
		}
	}

	// MOVE takes exactly one.
	if _, err := Compile(prog(vars, "  x := MOVE(x, y)")); err == nil || !strings.Contains(err.Error(), "MOVE needs exactly 1 input") {
		t.Errorf("MOVE with two inputs: err = %v, want %q", err, "MOVE needs exactly 1 input")
	}

	// Exactly two: a third input is refused for SUB, DIV, MOD and each comparison.
	for _, fn := range []string{"SUB", "DIV", "MOD", "GT", "GE", "LT", "LE", "EQ", "NE"} {
		out := "x"
		if fn != "SUB" && fn != "DIV" && fn != "MOD" {
			out = "a"
		}
		_, err := Compile(prog(vars, "  "+out+" := "+fn+"(x, y, z)"))
		if err == nil || !strings.Contains(err.Error(), fn+" needs exactly 2 inputs") {
			t.Errorf("%s with three inputs: err = %v, want %q", fn, err, fn+" needs exactly 2 inputs")
		}
		if _, err := Compile(prog(vars, "  "+out+" := "+fn+"(x, y)")); err != nil {
			t.Errorf("%s with two inputs failed to compile: %v", fn, err)
		}
		if _, err := Compile(prog(vars, "  "+out+" := "+fn+"(x)")); err == nil {
			t.Errorf("%s with one input compiled, want an error", fn)
		}
	}
}

// An input written by an earlier call and then omitted from a later one keeps
// the value that call wrote, not the declared initial value.
func TestOmittedFBInputKeepsLastWrittenValue(t *testing.T) {
	src := "FUNCTION_BLOCK Offset\nVAR_INPUT X : INT; Bias : INT := 7; END_VAR\nVAR_OUTPUT Y : INT; END_VAR\nY := X + Bias;\nEND_FUNCTION_BLOCK\n" +
		"PROGRAM Main\nVAR_EXTERNAL In : INT; Out : INT; END_VAR\nFBD\n  o : Offset(X := In, Bias := 20)\n  o(X := In)\n  Out := o.Y\nEND_FBD\nEND_PROGRAM"
	sc := newScanner(t, src)
	sc.scan(0, map[string]ir.Value{"In": ir.IntVal(1)})
	sc.scan(0, map[string]ir.Value{"In": ir.IntVal(2)})
	if got := sc.h.vals["Out"]; got.I != 22 {
		t.Fatalf("Out = %+v, want 22 (X 2 + Bias 20 written by the first call; the omitting call must not reset it to 7)", got)
	}
}
