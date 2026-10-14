package main

import "testing"

// #336: an FB instance named like a library FUNCTION hides it in that
// program: a warning on the declaration, and the file still compiles.
func TestCheckGoldenInstanceShadowsFunction(t *testing.T) {
	out, code := checkGolden(t, map[string]string{
		"nautilus.yaml": "tasks:\n  - program: program.st\ntags:\n  - { name: Out, role: output, type: REAL }\n",
		"lib/blocks.st": "FUNCTION Scale : REAL\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nScale := IN * 2.0;\nEND_FUNCTION\n\n" +
			"FUNCTION_BLOCK Ramp\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nVAR_OUTPUT\n    Q : REAL;\nEND_VAR\nQ := Q + IN;\nEND_FUNCTION_BLOCK\n",
		"program.st": "PROGRAM Main\nVAR\n    scale : Ramp;\nEND_VAR\nscale(IN := 1.0);\nOut := scale.Q;\nEND_PROGRAM\n",
	})
	assertGolden(t, out, code, 0, `program.st:3:5: warning: instance scale shadows FUNCTION Scale — scale(...) runs the instance here, never the function; rename one
naut check: 2 file(s), 0 with errors, 1 warning(s)`)
}
