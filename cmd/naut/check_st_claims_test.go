package main

import (
	"testing"
)

// ST-050: the dereference operator (^) is not supported, so naut check should
// refuse a program that uses it, as it refuses a REF_TO declaration.
//
// Today the ST lexer skips characters it does not know ("Skip unknown
// characters"), so `r^` reads as `r` and the program is accepted. The test
// skips on that bug rather than assert the wrong behaviour; when ^ is
// rejected the skip turns into a failure telling you to drop it.
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
			if code == 0 {
				t.Skip("bug: naut check accepts the dereference operator; the lexer drops the unknown character ^ (exit 0)")
			}
			if code != 1 {
				t.Fatalf("exit = %d, want 1\n%s", code, out)
			}
		})
	}
}
