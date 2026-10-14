package runtime_test

import (
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// ST-021: a struct-typed VAR_EXTERNAL tag is written one field at a time
// (P101.Cmd := ...) or whole (P102 := P101), and a field write changes that
// member of the tag in the store and leaves every other member as it was,
// including a member an operator set between scans.
func TestStructTagWrittenFieldwiseAndWhole(t *testing.T) {
	const prog = `
PROGRAM Main
VAR_EXTERNAL
    P101     : Motor;
    P102     : Motor;
    StartCmd : BOOL;
END_VAR
P101.Cmd := StartCmd;
P101.Starts := P101.Starts + 1;
IF StartCmd THEN
    P102 := P101;
END_IF;
END_PROGRAM
`
	rt, err := runtime.New(runtime.Options{
		Program:   prog,
		Libraries: []string{structPinLib},
		Driver:    nio.NewMemory(),
		Tags: []runtime.TagDef{
			runtime.Typed("P101", runtime.RoleState, "Motor"),
			runtime.Typed("P102", runtime.RoleState, "Motor"),
			runtime.State("StartCmd", false),
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// An operator sets one member of P101 before the program has run.
	if err := rt.Tags().SetPath("P101.Speed", 12.5); err != nil {
		t.Fatal(err)
	}
	rt.Tags().WriteGlobal("StartCmd", ir.BoolVal(true))
	rt.Scan()

	p101, _ := rt.Tags().ReadGlobal("P101")
	// Motor is {Cmd, Running, Starts, Speed}.
	if !p101.Fld[0].B || p101.Fld[1].B || p101.Fld[2].I != 1 || p101.Fld[3].F != 12.5 {
		t.Fatalf("P101 = Cmd %v Running %v Starts %d Speed %v; want Cmd true (written), Running false, Starts 1 (written), Speed 12.5 (untouched)",
			p101.Fld[0].B, p101.Fld[1].B, p101.Fld[2].I, p101.Fld[3].F)
	}
	// The whole-struct write copied every member.
	p102, _ := rt.Tags().ReadGlobal("P102")
	if !p102.Fld[0].B || p102.Fld[2].I != 1 || p102.Fld[3].F != 12.5 {
		t.Fatalf("P102 = Cmd %v Starts %d Speed %v; want a whole copy of P101 (true, 1, 12.5)", p102.Fld[0].B, p102.Fld[2].I, p102.Fld[3].F)
	}

	// A member the operator changes between scans survives the program's next
	// field writes.
	if err := rt.Tags().SetPath("P101.Speed", 20.0); err != nil {
		t.Fatal(err)
	}
	rt.Scan()
	p101, _ = rt.Tags().ReadGlobal("P101")
	if p101.Fld[2].I != 2 || p101.Fld[3].F != 20.0 {
		t.Fatalf("after the second scan P101.Starts = %d, Speed = %v; want 2 and 20", p101.Fld[2].I, p101.Fld[3].F)
	}
}
