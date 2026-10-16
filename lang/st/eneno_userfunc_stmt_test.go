package st

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// TestENOnUserFunctionCalledAsStatement: a user FUNCTION called as a
// statement of its own takes EN/ENO — with EN FALSE its body does not run
// (the counter it bumps through its own VAR_EXTERNAL holds) and ENO is
// FALSE; with EN TRUE it runs and ENO is TRUE. The same call with EN inside
// a larger expression is the "statement of its own" compile error.
func TestENOnUserFunctionCalledAsStatement(t *testing.T) {
	src := `
FUNCTION Bump : BOOL
VAR_INPUT Step : INT; END_VAR
VAR_EXTERNAL count : INT; END_VAR
count := count + Step;
Bump := TRUE;
END_FUNCTION
PROGRAM p
VAR_GLOBAL en : BOOL; count : INT; ok : BOOL; END_VAR
Bump(EN := en, Step := 5, ENO => ok);
END_PROGRAM`
	h := newFakeHost()
	h.vals["count"] = ir.IntVal(0)
	h.vals["ok"] = ir.BoolVal(true) // ENO must be written FALSE, not left alone
	p, f := runFrame(t, src)
	scan(t, p, f, h)
	if h.vals["count"].I != 0 || h.vals["ok"].B {
		t.Fatalf("EN FALSE: count=%d ok=%v, want 0 and FALSE (the body must not run)", h.vals["count"].I, h.vals["ok"].B)
	}
	h.vals["en"] = ir.BoolVal(true)
	scan(t, p, f, h)
	scan(t, p, f, h)
	if h.vals["count"].I != 10 || !h.vals["ok"].B {
		t.Fatalf("EN TRUE: count=%d ok=%v, want 10 and TRUE", h.vals["count"].I, h.vals["ok"].B)
	}
	h.vals["en"] = ir.BoolVal(false)
	scan(t, p, f, h)
	if h.vals["count"].I != 10 || h.vals["ok"].B {
		t.Fatalf("EN FALSE again: count=%d ok=%v, want 10 and FALSE", h.vals["count"].I, h.vals["ok"].B)
	}

	bad := strings.Replace(src, "Bump(EN := en, Step := 5, ENO => ok);",
		"ok := NOT Bump(EN := en, Step := 5);", 1)
	prog, err := Parse(bad)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Lower(prog); err == nil || !strings.Contains(err.Error(), "statement of its own") {
		t.Fatalf("EN on a user FUNCTION inside an expression: err = %v, want \"statement of its own\"", err)
	}
}
