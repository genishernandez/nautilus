package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// b1ChainSrc chains EN/ENO through every kind of block the FBD editor
// places: a standard function (LIMIT), an operator block (ADD), a user
// FUNCTION, a standard function block (CTU), a user FUNCTION_BLOCK and a
// DIV. Each block's EN is the previous block's ENO, read as a wire, and the
// last ENO feeds a coil.
const b1ChainSrc = `FUNCTION Twice : REAL
VAR_INPUT IN : REAL; END_VAR
Twice := IN * 2.0;
END_FUNCTION

FUNCTION_BLOCK Acc
VAR_INPUT IN : REAL; END_VAR
VAR_OUTPUT SUM : REAL; END_VAR
SUM := SUM + IN;
END_FUNCTION_BLOCK

PROGRAM Chain
VAR_EXTERNAL
  En : BOOL; Pulse : BOOL; X : REAL; D : REAL;
  OutA : REAL; OutB : REAL; OutF : REAL; Cnt : INT; Sum : REAL; OutC : REAL; Ok : BOOL;
END_VAR
FBD
  a = LIMIT(EN := En, MN := 0.0, IN := X, MX := 10.0)
  OutA := a
  b = ADD(EN := a.ENO, X, 1.0)
  OutB := b
  f = Twice(EN := b.ENO, IN := X)
  OutF := f
  c1 : CTU(EN := f.ENO, CU := Pulse, R := FALSE, PV := 100)
  Cnt := c1.CV
  acc : Acc(EN := c1.ENO, IN := X)
  Sum := acc.SUM
  q = DIV(EN := acc.ENO, X, D)
  OutC := q
  Ok := q.ENO
END_FBD
END_PROGRAM`

// TestB1ENOFeedsTheNextBlocksEN: ENO read as a wire drives the next
// block's EN, through a function, an operator, a user FUNCTION, a standard
// FB, a user FB and DIV. EN FALSE at the head stops the whole chain (every
// result holds, the FBs do not run); a zero divisor at the tail drops only
// the last ENO.
func TestB1ENOFeedsTheNextBlocksEN(t *testing.T) {
	prog, err := Compile(b1ChainSrc)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, mustTranspile(b1ChainSrc))
	}
	h := &host{vals: map[string]ir.Value{
		"En": ir.BoolVal(true), "Pulse": ir.BoolVal(true), "X": ir.RealVal(4), "D": ir.RealVal(2),
	}}
	f := ir.NewFrame(prog)
	scan := func() {
		t.Helper()
		if err := ir.Run(prog, f, h); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	check := func(when string, outA, outB, outF float64, cnt int64, sum, outC float64, ok bool) {
		t.Helper()
		v := h.vals
		if v["OutA"].F != outA || v["OutB"].F != outB || v["OutF"].F != outF || v["Cnt"].I != cnt ||
			v["Sum"].F != sum || v["OutC"].F != outC || v["Ok"].B != ok {
			t.Fatalf("%s: OutA=%v OutB=%v OutF=%v Cnt=%v Sum=%v OutC=%v Ok=%v; want %v %v %v %v %v %v %v",
				when, v["OutA"].F, v["OutB"].F, v["OutF"].F, v["Cnt"].I, v["Sum"].F, v["OutC"].F, v["Ok"].B,
				outA, outB, outF, cnt, sum, outC, ok)
		}
	}

	scan()
	check("EN TRUE: the chain runs", 4, 5, 8, 1, 4, 2, true)

	// EN FALSE at the head: a.ENO is FALSE, so b does not run, so f does
	// not, and so on down the chain. Every result holds; the CTU misses the
	// new edge and the accumulator does not add.
	h.vals["En"], h.vals["X"], h.vals["Pulse"] = ir.BoolVal(false), ir.RealVal(6), ir.BoolVal(false)
	scan()
	h.vals["Pulse"] = ir.BoolVal(true)
	scan()
	check("EN FALSE: nothing downstream runs", 4, 5, 8, 1, 4, 2, false)

	// Back on: the chain runs again with the new X.
	h.vals["En"], h.vals["Pulse"] = ir.BoolVal(true), ir.BoolVal(false)
	scan()
	check("EN TRUE again", 6, 7, 12, 1, 10, 3, true)

	// A zero divisor at the tail: DIV's ENO is FALSE, the rest still ran.
	h.vals["D"] = ir.RealVal(0)
	scan()
	check("zero divisor at the tail", 6, 7, 12, 1, 16, 0, false)
}

// TestB1OperatorArityAndTypes: SUB, the comparisons and MOVE have a fixed
// arity (SUB and GT..NE two inputs, MOVE one), and an operator's result
// has the common type of its arguments: INT with INT is an integer (it
// drives an INT coil), INT with REAL is a REAL (it may not).
func TestB1OperatorArityAndTypes(t *testing.T) {
	prog := func(body string) string {
		return "PROGRAM P\nVAR_EXTERNAL X : INT; Y : INT; Z : INT; R : REAL; I : INT; Q : REAL; B : BOOL; END_VAR\nFBD\n" +
			body + "\nEND_FBD\nEND_PROGRAM"
	}
	for _, tc := range []struct{ body, want string }{
		{"  I := SUB(X, Y, Z)", "SUB needs exactly 2 inputs"},
		{"  B := GT(X, Y, Z)", "GT needs exactly 2 inputs"},
		{"  B := EQ(X)", "EQ needs exactly 2 inputs"},
		{"  I := MOVE(X, Y)", "MOVE needs exactly 1 input"},
		{"  I := ADD(X, R)", "cannot assign REAL to INT"},
		{"  I := MUL(X, Y, R)", "cannot assign REAL to INT"},
		{"  I := SUB(X, R)", "cannot assign REAL to INT"},
		{"  I := MOVE(R)", "cannot assign REAL to INT"},
		{"  I := GT(X, Y)", "cannot assign BOOL to INT"},
	} {
		if _, err := Compile(prog(tc.body)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", strings.TrimSpace(tc.body), err, tc.want)
		}
	}
	// The same shapes with matching types compile and run.
	h := run(t, prog("  I := ADD(X, Y, Z)\n  Q := ADD(X, R)\n  B := GT(R, Q)"), map[string]ir.Value{
		"X": ir.IntVal(2), "Y": ir.IntVal(3), "Z": ir.IntVal(4), "R": ir.RealVal(0.5),
	})
	if h.vals["I"].I != 9 || h.vals["Q"].F != 2.5 || h.vals["B"].B {
		t.Fatalf("I=%v Q=%v B=%v, want 9 2.5 false", h.vals["I"].I, h.vals["Q"].F, h.vals["B"].B)
	}
}
