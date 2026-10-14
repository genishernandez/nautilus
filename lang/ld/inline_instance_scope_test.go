package ld

import (
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// #332 through the LD → FBD hop: a rung's inline t1:TON is declared in its
// program even when an ST block earlier in the same file has a VAR t1.
func TestLadderInlineInstanceNotHiddenByLibraryBlockVar(t *testing.T) {
	src := `FUNCTION_BLOCK Helper
VAR_INPUT x : BOOL; END_VAR
VAR_OUTPUT y : BOOL; END_VAR
VAR t1 : BOOL; END_VAR
t1 := x;
y := t1;
END_FUNCTION_BLOCK

PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
LD
  RUNG timing  Run t1:TON(PT := T#5S)
  RUNG done    t1.Q ( Done )
END_LD
END_PROGRAM
`
	h, scan := cScanner(t, toST(t, src))
	h.vals["Run"] = ir.BoolVal(true)
	h.now = 1000
	scan()
	if h.vals["Done"].B {
		t.Fatal("Done TRUE before PT elapsed")
	}
	h.now = 6500
	scan()
	if !h.vals["Done"].B {
		t.Fatal("Done FALSE after PT elapsed: the program's t1 didn't time")
	}
}
