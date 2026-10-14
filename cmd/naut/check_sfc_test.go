package main

import (
	"strings"
	"testing"
)

// `naut check` on a .sfc file: the structural errors and warnings the SFC
// page lists (SFC-032..036), the line mapping of a type error (SFC-037),
// and the constructs the page says are not supported (SFC-041..044). Each
// case runs the real CLI entry point (runCheck) on a chart on disk.

const sfcVars = "PROGRAM p\nVAR_EXTERNAL\n  A : BOOL;\n  Y : BOOL;\n  Cnt : INT;\n  Lamp : BOOL;\nEND_VAR\nSFC\n"

func TestCheckSFCDiagnostics(t *testing.T) {
	cases := []struct {
		name, body string
		code       int    // exit code expected
		want       string // substring that must appear
		wantNot    string // substring that must not appear
	}{
		{name: "duplicate step", code: 1, want: "duplicate step name",
			body: "INITIAL_STEP S0:\nEND_STEP\nSTEP S0:\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "duplicate action", code: 1, want: "duplicate ACTION name",
			body: "INITIAL_STEP S0:\n  N Act;\nEND_STEP\nACTION Act:\n  Y := TRUE;\nEND_ACTION\nACTION Act:\n  Y := FALSE;\nEND_ACTION\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "duplicate transition", code: 1, want: "duplicate TRANSITION name",
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION t1 FROM S0 TO S0 := A;\nEND_TRANSITION\nTRANSITION t1 FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "no initial step", code: 1, want: "no INITIAL_STEP declared",
			body: "STEP S0:\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "two initial steps", code: 1, want: "multiple INITIAL_STEP",
			body: "INITIAL_STEP S0:\nEND_STEP\nINITIAL_STEP S1:\nEND_STEP\nTRANSITION FROM S0 TO S1 := A;\nEND_TRANSITION\n"},
		{name: "FROM missing step", code: 1, want: `FROM references unknown step "Nope"`,
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION FROM Nope TO S0 := A;\nEND_TRANSITION\n"},
		{name: "TO missing step", code: 1, want: `TO references unknown step "Nope"`,
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION FROM S0 TO Nope := A;\nEND_TRANSITION\n"},
		{name: "empty condition", code: 1, want: "empty condition",
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION FROM S0 TO S0 :=;\nEND_TRANSITION\n"},
		{name: "association to nothing", code: 1, want: "references neither an ACTION block nor a declared variable",
			body: "INITIAL_STEP S0:\n  N Nowhere;\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "unknown step X ref", code: 1, want: `references unknown step "Typo"`,
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION FROM S0 TO S0 := Typo.X;\nEND_TRANSITION\n"},
		{name: "unknown step T ref", code: 1, want: `references unknown step "Typo"`,
			body: "INITIAL_STEP S0:\nEND_STEP\nTRANSITION FROM S0 TO S0 := Typo.T > T#1S;\nEND_TRANSITION\n"},
		{name: "unknown qualifier", code: 1, want: `unknown action qualifier "Q"`,
			body: "INITIAL_STEP S0:\n  Q Lamp;\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "timed qualifier without duration", code: 1, want: "needs a duration",
			body: "INITIAL_STEP S0:\n  D Lamp;\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "untimed qualifier with duration", code: 1, want: "does not take a time argument",
			body: "INITIAL_STEP S0:\n  N Lamp(T#1S);\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "unreachable step", code: 0, want: `warning: step "S1" is unreachable`,
			body: "INITIAL_STEP S0:\nEND_STEP\nSTEP S1:\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\nTRANSITION FROM S1 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "dead-end step", code: 0, want: `warning: step "S1" is a dead end`,
			body: "INITIAL_STEP S0:\nEND_STEP\nSTEP S1:\nEND_STEP\nTRANSITION FROM S0 TO S1 := A;\nEND_TRANSITION\n"},
		{name: "convergence from alternative branches", code: 0, want: "not structurally reachable from a common simultaneous divergence",
			body: "INITIAL_STEP S0:\nEND_STEP\nSTEP B1:\nEND_STEP\nSTEP B2:\nEND_STEP\nSTEP S3:\nEND_STEP\n" +
				"TRANSITION FROM S0 TO B1 := A;\nEND_TRANSITION\nTRANSITION FROM S0 TO B2 := A;\nEND_TRANSITION\n" +
				"TRANSITION FROM (B1, B2) TO S3 := A;\nEND_TRANSITION\nTRANSITION FROM S3 TO S0 := A;\nEND_TRANSITION\n"},
		{name: "convergence from a simultaneous divergence", code: 0, wantNot: "not structurally reachable",
			body: "INITIAL_STEP S0:\nEND_STEP\nSTEP B1:\nEND_STEP\nSTEP B2:\nEND_STEP\nSTEP S3:\nEND_STEP\n" +
				"TRANSITION FROM S0 TO (B1, B2) := A;\nEND_TRANSITION\n" +
				"TRANSITION FROM (B1, B2) TO S3 := A;\nEND_TRANSITION\nTRANSITION FROM S3 TO S0 := A;\nEND_TRANSITION\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := checkIn(t, map[string]string{"p.sfc": sfcVars + tc.body + sfcTail})
			if code != tc.code {
				t.Fatalf("exit = %d, want %d\n%s", code, tc.code, out)
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Errorf("check output missing %q:\n%s", tc.want, out)
			}
			if tc.wantNot != "" && strings.Contains(out, tc.wantNot) {
				t.Errorf("check output has unwanted %q:\n%s", tc.wantNot, out)
			}
		})
	}
}

// The page's "Not supported" list: a step nested in a step, a whole chart
// nested in a step, an explicit numeric transition priority (either spelling
// a vendor might use), and an indicator variable on a transition are each
// refused by `naut check` (exit 1), not accepted.
func TestCheckSFCNotSupported(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"step nested in a step",
			"INITIAL_STEP S0:\n STEP Inner:\n END_STEP\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n",
			"expected an action association"},
		{"chart nested in a step",
			"INITIAL_STEP S0:\n SFC\n INITIAL_STEP X:\n END_STEP\n END_SFC\nEND_STEP\nTRANSITION FROM S0 TO S0 := A;\nEND_TRANSITION\n",
			`association starting "SFC"`},
		{"numeric priority after TO",
			"INITIAL_STEP S0:\nEND_STEP\nTRANSITION t FROM S0 TO S0 PRIORITY 1 := A;\nEND_TRANSITION\n",
			"expected ':='"},
		{"numeric priority attribute",
			"INITIAL_STEP S0:\nEND_STEP\nTRANSITION t (PRIORITY := 1) FROM S0 TO S0 := A;\nEND_TRANSITION\n",
			"expected FROM"},
		{"indicator on a transition",
			"INITIAL_STEP S0:\nEND_STEP\nTRANSITION t FROM S0 TO S0 (Ind) := A;\nEND_TRANSITION\n",
			"expected ':='"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := checkIn(t, map[string]string{"p.sfc": sfcVars + tc.body + sfcTail})
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (rejected)\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("check output missing %q:\n%s", tc.want, out)
			}
		})
	}
}

// A type error in a transition condition is reported on the TRANSITION's
// line, one in an action body on the ACTION's line (SFC-037).
func TestCheckSFCTypeErrorLines(t *testing.T) {
	// Each chart carries exactly one error, so the reported line can only
	// be that one's.
	chart := func(cond, assign string) string {
		return sfcVars + // PROGRAM p .. SFC is 8 lines
			"INITIAL_STEP S0:\n" + // 9
			"  N Act;\n" + // 10
			"END_STEP\n" + // 11
			"STEP S1:\n" + // 12
			"END_STEP\n" + // 13
			"TRANSITION go FROM S0 TO S1 := " + cond + ";\n" + // 14
			"END_TRANSITION\n" + // 15
			"TRANSITION back FROM S1 TO S0 := A;\n" + // 16
			"END_TRANSITION\n" + // 17
			"ACTION Act:\n" + // 18
			"  Y := TRUE;\n" + // 19
			"  " + assign + "\n" + // 20
			"END_ACTION\n" + // 21
			sfcTail
	}
	t.Run("condition", func(t *testing.T) {
		out, code := checkIn(t, map[string]string{"p.sfc": chart("Cnt + 1", "Lamp := TRUE;")})
		if code != 1 || !strings.Contains(out, "p.sfc:14:") {
			t.Errorf("want a type error on the TRANSITION line (14), exit 1; got exit %d:\n%s", code, out)
		}
	})
	t.Run("action body", func(t *testing.T) {
		out, code := checkIn(t, map[string]string{"p.sfc": chart("A", "Lamp := Cnt + 1;")})
		// The diagnostic lands inside the ACTION block, on the offending
		// statement (line 20), not on the TRANSITION or anywhere else.
		if code != 1 || !strings.Contains(out, "p.sfc:20:") {
			t.Errorf("want a type error inside the ACTION block (line 20), exit 1; got exit %d:\n%s", code, out)
		}
	})
}
