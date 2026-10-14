package lsp

import (
	"strings"
	"testing"
)

// TestAnalyzeLDHopErrorLandsOnRung: an error raised by the ladder hop itself
// (LD → FBD, before any ST exists) lands on the offending RUNG of the .ld
// source, like the ST-stage errors TestAnalyzeLDFunctionBlocks maps through
// the composed line map. Two rungs, so a diagnostic pinned to line 1 or to the
// first rung would be wrong.
func TestAnalyzeLDHopErrorLandsOnRung(t *testing.T) {
	src := `PROGRAM Main
VAR_EXTERNAL Start : BOOL; Run : BOOL; Alm : BOOL; END_VAR
LD
  RUNG ok    Start ( Run )
  RUNG bad   ( Run ) Start ( Alm )
END_LD
END_PROGRAM
`
	a := analyzeLD(src, "", 0)
	if len(a.Diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", a.Diags)
	}
	d := a.Diags[0]
	if !strings.Contains(d.Message, "right end") {
		t.Errorf("message = %q, want the coils-at-the-right-end error", d.Message)
	}
	if d.Range.Start.Line != 4 { // 0-based: the RUNG bad line
		t.Errorf("diagnostic on 0-based line %d, want 4 (RUNG bad)", d.Range.Start.Line)
	}
}
