package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// #332: a netlist's inline `t1 : TON(...)` is declared in its POU even when
// a library FUNCTION_BLOCK earlier in the same file has a VAR named t1.
// Only the POU's own header can declare the instance for it; another
// block's locals are another scope.
func TestInlineInstanceNotHiddenByLibraryBlockVar(t *testing.T) {
	cases := map[string]string{
		// An ST library block ahead of the program, before the first FBD.
		"ST block ahead of the program": `FUNCTION_BLOCK Helper
VAR_INPUT x : BOOL; END_VAR
VAR_OUTPUT y : BOOL; END_VAR
VAR t1 : BOOL; END_VAR
t1 := x;
y := t1;
END_FUNCTION_BLOCK

PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S)
  Done := t1.Q
END_FBD
END_PROGRAM
`,
		// An FBD-bodied block, then an ST block with t1, then the program:
		// the text between the first END_FBD and the program's FBD holds
		// the ST block's VAR section.
		"ST block between two FBD bodies": `FUNCTION_BLOCK Pass
VAR_INPUT x : BOOL; END_VAR
VAR_OUTPUT y : BOOL; END_VAR
FBD
  y := x
END_FBD
END_FUNCTION_BLOCK

FUNCTION_BLOCK Helper
VAR t1 : TON; END_VAR
t1(IN := TRUE, PT := T#1S);
END_FUNCTION_BLOCK

PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S)
  Done := t1.Q
END_FBD
END_PROGRAM
`,
		// An FBD-bodied library block whose own header declares t1 as a
		// plain VAR, then the program.
		"FBD block's own VAR t1": `FUNCTION_BLOCK Helper
VAR_INPUT x : BOOL; END_VAR
VAR_OUTPUT y : BOOL; END_VAR
VAR t1 : BOOL; END_VAR
FBD
  y := x
END_FBD
END_FUNCTION_BLOCK

PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S)
  Done := t1.Q
END_FBD
END_PROGRAM
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := Transpile(src)
			if err != nil {
				t.Fatal(err)
			}
			prog := out[strings.Index(out, "PROGRAM p"):]
			if n := strings.Count(prog, "t1 : TON;"); n != 1 {
				t.Fatalf("program declares t1 %d times, want 1:\n%s", n, out)
			}
			s := newScanner(t, src)
			tru := ir.BoolVal(true)
			s.scan(1000, map[string]ir.Value{"Run": tru})
			if s.h.vals["Done"].B {
				t.Fatal("Done TRUE before PT elapsed")
			}
			s.scan(6500, nil)
			if !s.h.vals["Done"].B {
				t.Fatal("Done FALSE after PT elapsed: the program's t1 didn't time")
			}
		})
	}
}

// The POU's own header still wins: a program that declares t1 itself gets
// no second declaration, with a library block's t1 in the file too.
func TestInlineInstanceDeclaredInOwnHeaderWithLibraryVar(t *testing.T) {
	src := `FUNCTION_BLOCK Helper
VAR t1 : TON; END_VAR
t1(IN := TRUE, PT := T#1S);
END_FUNCTION_BLOCK

PROGRAM p
VAR_EXTERNAL Run : BOOL; Done : BOOL; END_VAR
VAR t1 : TON; END_VAR
FBD
  t1 : TON(IN := Run, PT := T#5S)
  Done := t1.Q
END_FBD
END_PROGRAM
`
	out, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	prog := out[strings.Index(out, "PROGRAM p"):]
	if n := strings.Count(prog, "t1 : TON;"); n != 1 {
		t.Fatalf("program declares t1 %d times, want 1:\n%s", n, out)
	}
	if _, err := Compile(src); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
}
