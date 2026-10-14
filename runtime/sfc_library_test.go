package runtime_test

import (
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// A step's action can instantiate and call a library FUNCTION_BLOCK, and
// the instance keeps its state between scans (the hysteresis band holds).
func TestLibrariesUserFBFromSFCAction(t *testing.T) {
	lib := `FUNCTION_BLOCK Hysteresis
VAR_INPUT IN : REAL; HI : REAL; LO : REAL; END_VAR
VAR_OUTPUT Q : BOOL; END_VAR
IF IN >= HI THEN Q := TRUE;
ELSIF IN <= LO THEN Q := FALSE;
END_IF;
END_FUNCTION_BLOCK`
	sfcProg := `PROGRAM Main
VAR_EXTERNAL x : REAL; Out : BOOL; END_VAR
VAR h1 : Hysteresis; END_VAR
SFC
  INITIAL_STEP Watch:
    N Band;
  END_STEP
  ACTION Band:
    h1(IN := x, HI := 10.0, LO := 5.0);
    Out := h1.Q;
  END_ACTION
END_SFC
END_PROGRAM`
	drv := nio.NewMemory()
	rt, err := runtime.New(runtime.Options{
		Program:   sfcProg,
		Libraries: []string{lib},
		Driver:    drv,
		Tags:      []runtime.TagDef{runtime.Input("x"), runtime.Output("Out", runtime.Init(false))},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	step := func(x float64) bool {
		t.Helper()
		_ = drv.WriteOutputs(nio.Values{"x": x})
		rt.Scan()
		out, _ := drv.ReadInputs()
		b, _ := out["Out"].(bool)
		return b
	}
	step(0) // enter the initial step
	if !step(12) {
		t.Fatal("above HI the library block must latch on")
	}
	if !step(7) {
		t.Fatal("inside the band the library block instance must hold its state")
	}
	if step(3) {
		t.Fatal("below LO the library block must latch off")
	}
}
