package project

import "testing"

// #336: an FB instance named like a library FUNCTION runs its block. The
// function and the block take the same input, so the call also lowers as a
// call of the function — which is what it did before the fix: the block
// never ran and Out stayed 0.
func TestInstanceNamedLikeLibraryFunctionRunsItsBlock(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Out, role: output, type: REAL }\n",
		"lib/blocks.st": "FUNCTION Scale : REAL\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nScale := IN * 100.0;\nEND_FUNCTION\n\n" +
			"FUNCTION_BLOCK Ramp\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nVAR_OUTPUT\n    Q : REAL;\nEND_VAR\nQ := Q + IN;\nEND_FUNCTION_BLOCK\n",
		"main.st": "PROGRAM Main\nVAR\n    scale : Ramp;\nEND_VAR\nscale(IN := 1.0);\nOut := scale.Q;\nEND_PROGRAM\n",
	})
	rt.Scan()
	rt.Scan()
	if got := readReal(t, rt, "Out"); got != 2 {
		t.Errorf("Out = %v after 2 scans, want 2 (the Ramp instance scale runs, not FUNCTION Scale)", got)
	}
}
