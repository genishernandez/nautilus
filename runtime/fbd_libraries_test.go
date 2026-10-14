package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joyautomation/nautilus/internal/stproject"
	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// User function blocks authored in an .st, an .fbd and a .ld library file
// (PROGRAM-less files, one under lib/ and two in the project root) are
// instantiated from an FBD program the way a TON is, and every instance
// does its own block's work.
func TestFBDInstantiatesBlocksFromSTFBDAndLadderLibraries(t *testing.T) {
	files := map[string]string{
		"lib/hyst.st": `FUNCTION_BLOCK Hyst
VAR_INPUT IN : REAL; HI : REAL; LO : REAL; END_VAR
VAR_OUTPUT Q : BOOL; END_VAR
IF IN >= HI THEN Q := TRUE;
ELSIF IN <= LO THEN Q := FALSE;
END_IF;
END_FUNCTION_BLOCK
`,
		"gate.fbd": `FUNCTION_BLOCK Gate
VAR_INPUT A : BOOL; B : BOOL; END_VAR
VAR_OUTPUT Q : BOOL; END_VAR
FBD
  Q := AND(A, B)
END_FBD
END_FUNCTION_BLOCK
`,
		"seal.ld": `FUNCTION_BLOCK Seal
VAR_INPUT Start : BOOL; Stop : BOOL; END_VAR
VAR_OUTPUT Run : BOOL; END_VAR
LD
  RUNG seal  [ Start | Run ] /Stop ( Run )
END_LD
END_FUNCTION_BLOCK
`,
		"main.fbd": `PROGRAM Main
VAR_EXTERNAL
  x : REAL; Both2 : BOOL; Start : BOOL; Stop : BOOL;
  Hot : BOOL; Gated : BOOL; Sealed : BOOL;
END_VAR
VAR h1 : Hyst; g1 : Gate; s1 : Seal; END_VAR
FBD
  h1 : Hyst(IN := x, HI := 10.0, LO := 5.0)
  Hot := h1.Q
  g1 : Gate(A := Hot, B := Both2)
  Gated := g1.Q
  s1 : Seal(Start := Start, Stop := Stop)
  Sealed := s1.Run
END_FBD
END_PROGRAM
`,
	}
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := stproject.ComposeAll(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Programs) != 1 || m.Programs[0].File != "main.fbd" {
		t.Fatalf("programs = %+v, want only main.fbd (the PROGRAM-less files are libraries)", m.Programs)
	}
	if len(m.Libraries) != 3 {
		t.Fatalf("libraries = %d, want 3 (.st, .fbd, .ld)", len(m.Libraries))
	}

	drv := nio.NewMemory()
	rt, err := runtime.New(runtime.Options{
		Program:   m.Programs[0].Body,
		Libraries: m.Libraries,
		Driver:    drv,
		Tags: []runtime.TagDef{
			runtime.Input("x"), runtime.Input("Both2"), runtime.Input("Start"), runtime.Input("Stop"),
			runtime.Output("Hot", runtime.Init(false)),
			runtime.Output("Gated", runtime.Init(false)), runtime.Output("Sealed", runtime.Init(false)),
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	scan := func(in nio.Values) map[string]any {
		t.Helper()
		_ = drv.WriteOutputs(in)
		rt.Scan()
		out, _ := drv.ReadInputs()
		return out
	}
	out := scan(nio.Values{"x": 12.0, "Both2": true, "Start": true, "Stop": false})
	if out["Hot"] != true || out["Gated"] != true || out["Sealed"] != true {
		t.Fatalf("scan 1: %v, want the ST, FBD and ladder blocks all true", out)
	}
	// Inside the band, Start released: the ST block holds, the ladder block
	// seals in; the FBD block follows its input.
	out = scan(nio.Values{"x": 7.0, "Both2": false, "Start": false})
	if out["Hot"] != true || out["Sealed"] != true || out["Gated"] != false {
		t.Fatalf("scan 2: %v, want Hot and Sealed held, Gated false", out)
	}
	out = scan(nio.Values{"x": 3.0, "Stop": true})
	if out["Hot"] != false || out["Sealed"] != false {
		t.Fatalf("scan 3: %v, want Hot and Sealed dropped", out)
	}
	if rt.Stats().LogicErrors != 0 {
		t.Fatalf("scans must run clean, got %d logic errors", rt.Stats().LogicErrors)
	}
}
