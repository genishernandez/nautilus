package ld

import (
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// TestEdgeStateSurvivesUnrelatedEdit: an edge contact's implicit R_TRIG is
// named from its rung's name, its reference and its position among repeats,
// so an online edit that inserts a rung ahead of it and changes another rung
// recompiles to the same instance name, and the warm swap (ir.MigrateFrame,
// what runtime.Program uses) carries the edge memory over. With A held TRUE
// across the swap the contact must not fire again; an instance that reset
// would see a fresh rise and pulse Y.
func TestEdgeStateSurvivesUnrelatedEdit(t *testing.T) {
	v1 := `PROGRAM p
VAR_EXTERNAL A : BOOL; B : BOOL; C : BOOL; Y : BOOL; Z : BOOL; W : BOOL; END_VAR
LD
  RUNG pulse  +A ( Y )
  RUNG other  B ( Z )
END_LD
END_PROGRAM`
	v2 := `PROGRAM p
VAR_EXTERNAL A : BOOL; B : BOOL; C : BOOL; Y : BOOL; Z : BOOL; W : BOOL; END_VAR
LD
  RUNG added  C ( W )
  RUNG pulse  +A ( Y )
  RUNG other  /B ( Z )
END_LD
END_PROGRAM`

	h := newHost()
	prog1 := compileST(t, toST(t, v1))
	frame1 := ir.NewFrame(prog1)
	run := func(p *ir.Program, f *ir.Frame) bool {
		t.Helper()
		if err := ir.Run(p, f, h); err != nil {
			t.Fatalf("run: %v", err)
		}
		return h.vals["Y"].B
	}

	h.vals["A"] = ir.BoolVal(false)
	if run(prog1, frame1) {
		t.Fatal("Y TRUE with A FALSE")
	}
	h.vals["A"] = ir.BoolVal(true)
	if !run(prog1, frame1) {
		t.Fatal("premise: +A should pass power on the scan A rises")
	}
	if run(prog1, frame1) {
		t.Fatal("premise: +A should pass power for one scan only")
	}

	prog2 := compileST(t, toST(t, v2))
	frame2, resets := ir.MigrateFrame(prog2, prog1, frame1)
	for _, r := range resets {
		if r == "rt_pulse_A" {
			t.Fatalf("the edit reset the edge instance rt_pulse_A (resets: %v)", resets)
		}
	}
	for i := 0; i < 3; i++ {
		if run(prog2, frame2) {
			t.Fatalf("scan %d after the edit: +A fired again with A held TRUE — the edge memory did not survive", i+1)
		}
	}
	// The migrated instance still sees the next real rise.
	h.vals["A"] = ir.BoolVal(false)
	run(prog2, frame2)
	h.vals["A"] = ir.BoolVal(true)
	if !run(prog2, frame2) {
		t.Fatal("after the edit, +A should still pass power on a new rise")
	}
}
