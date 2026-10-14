package main

import (
	"strings"
	"testing"
)

// A compile error in the ST a rung (or an SFC step / transition) produced
// is reported by `naut check` at the line of the diagram source that wrote
// it, not at a line of generated code.
func TestCheckLandsDiagramErrorsOnSourceLine(t *testing.T) {
	ld := "PROGRAM p\n" + // 1
		"VAR_EXTERNAL A : BOOL; N : DINT; Ok : BOOL; END_VAR\n" + // 2
		"LD\n" + // 3
		"  RUNG fine\n" + // 4
		"    A ( Ok )\n" + // 5
		"  RUNG bad\n" + // 6
		"    A ( N )\n" + // 7
		"END_LD\n" +
		"END_PROGRAM\n"
	// The same chart twice, each with one type error: the first in a
	// transition condition (line 11), the second in a step's action (line 14).
	chart := func(cond, act string) string {
		return "PROGRAM s\n" + // 1
			"VAR_EXTERNAL A : BOOL; N : DINT; END_VAR\n" + // 2
			"SFC\n" + // 3
			"  INITIAL_STEP Idle:\n" + // 4
			"  END_STEP\n" + // 5
			"  STEP Run:\n" + // 6
			"    N Bump;\n" + // 7
			"  END_STEP\n" + // 8
			"  TRANSITION go FROM Idle TO Run := A;\n" + // 9
			"  END_TRANSITION\n" + // 10
			"  TRANSITION back FROM Run TO Idle := " + cond + ";\n" + // 11
			"  END_TRANSITION\n" + // 12
			"  ACTION Bump:\n" + // 13
			"    " + act + "\n" + // 14
			"  END_ACTION\n" + // 15
			"END_SFC\n" +
			"END_PROGRAM\n"
	}
	out, code := checkIn(t, map[string]string{
		"rungs.ld":  ld,
		"trans.sfc": chart("A AND N", "N := N + 1;"),
		"act.sfc":   chart("A", "A := N;"),
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	t.Log(out)
	if !strings.Contains(out, "rungs.ld:6:") {
		t.Errorf("the type error in rung `bad` must report at its RUNG line (rungs.ld:6):\n%s", out)
	}
	if strings.Contains(out, "rungs.ld:4:") {
		t.Errorf("the good rung must not be blamed:\n%s", out)
	}
	if !strings.Contains(out, "trans.sfc:11:") {
		t.Errorf("the type error in a transition must report at its TRANSITION line (trans.sfc:11):\n%s", out)
	}
	if !strings.Contains(out, "act.sfc:14:") {
		t.Errorf("the type error in a step's action must report at the action body line (act.sfc:14):\n%s", out)
	}
}
