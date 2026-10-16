package lsp

import (
	"strings"
	"testing"
)

// A library holding a FUNCTION Scale and a FUNCTION_BLOCK Ramp that share
// an input name.
const shadowFuncLib = "FUNCTION Scale : REAL\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nScale := IN * 2.0;\nEND_FUNCTION\n\n" +
	"FUNCTION_BLOCK Ramp\nVAR_INPUT\n    IN : REAL;\nEND_VAR\nVAR_OUTPUT\n    Q : REAL;\nEND_VAR\nQ := Q + IN;\nEND_FUNCTION_BLOCK\n"

// #336: an FB instance named like a library FUNCTION hides it in that POU;
// the editor warns on the declaration.
func TestInstanceShadowsFunctionWarning(t *testing.T) {
	src := "PROGRAM Main\nVAR\n    scale : Ramp;\nEND_VAR\nscale(IN := TempC);\nEND_PROGRAM\n"
	files := map[string]string{"nautilus.yaml": implicitManifest, "main.st": src, "sim.st": implicitSim, "lib/blocks.st": shadowFuncLib}
	_, paths := refProject(t, files)
	s := startSession(t)
	s.recvResponse(s.send("initialize", map[string]any{}, true))
	d := s.openDiags(pathToURI(paths["main.st"]), src)
	if len(d) != 1 || d[0].Severity != SeverityWarning || d[0].Range.Start.Line != 2 ||
		!strings.Contains(d[0].Message, "instance scale shadows FUNCTION Scale") {
		t.Fatalf("diagnostics = %+v", d)
	}
}
