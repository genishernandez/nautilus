package main

import (
	"strings"
	"testing"
)

// ST-050: the dereference operator (^) is not supported, so naut check
// refuses a program that uses it, as it refuses a REF_TO declaration. (The
// ST lexer used to drop the character, so `r^` read as `r` and exit was 0.)
func TestCheckRejectsDereference(t *testing.T) {
	// The control: the same program without the ^ is accepted, so any
	// rejection below is the ^ and nothing else.
	const head = "PROGRAM p\nVAR\n  r : REAL;\nEND_VAR\n"
	if out, code := checkIn(t, map[string]string{"p.st": head + "r := 1.0;\nEND_PROGRAM\n"}); code != 0 {
		t.Fatalf("control program rejected (exit %d):\n%s", code, out)
	}
	for name, body := range map[string]string{
		"read":  "r := r^;\n",
		"write": "r^ := 1.0;\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, code := checkIn(t, map[string]string{"p.st": head + body + "END_PROGRAM\n"})
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (rejected)\n%s", code, out)
			}
			if !strings.Contains(out, "the ^ dereference operator is not supported") {
				t.Errorf("check output does not name the dereference operator:\n%s", out)
			}
		})
	}
}
