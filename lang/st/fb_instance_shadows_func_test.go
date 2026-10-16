package st

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// #336: an FB instance named like a FUNCTION (identifiers are
// case-insensitive, so `f` and `F` are one name). The instance is the
// declaration in scope, so `f(...)` runs the block — the function never
// stole the call. The function and the block share an input name, so the
// call lowers either way; only which body runs tells them apart.
const instanceVsFunctionPOUs = `
FUNCTION F : INT
VAR_INPUT
  IN : INT;
END_VAR
  F := IN * 100;
END_FUNCTION

FUNCTION_BLOCK Counter
VAR_INPUT
  IN : INT;
END_VAR
VAR_OUTPUT
  Q : INT;
END_VAR
  Q := Q + IN;
END_FUNCTION_BLOCK
`

func runInstanceVsFunction(t *testing.T, src string, scans int) map[string]ir.Value {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	irProg, err := Lower(prog)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	host := newFakeHost()
	frame := ir.NewFrame(irProg)
	for i := 0; i < scans; i++ {
		if err := ir.Run(irProg, frame, host); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	return host.vals
}

func TestFBInstanceNamedLikeFunctionRunsItsBody(t *testing.T) {
	src := instanceVsFunctionPOUs + `
PROGRAM main
VAR
  f : Counter;
END_VAR
VAR_GLOBAL
  result : INT;
END_VAR
  f(IN := 1);
  result := f.Q;
END_PROGRAM
`
	got := runInstanceVsFunction(t, src, 3)["result"]
	if got.I != 3 {
		t.Fatalf("result = %+v after 3 scans, want 3 (the Counter instance f runs, not FUNCTION F)", got)
	}
}

// The same inside a FUNCTION_BLOCK body: the block's own instance wins.
func TestFBInstanceNamedLikeFunctionRunsItsBodyInsideABlock(t *testing.T) {
	src := instanceVsFunctionPOUs + `
FUNCTION_BLOCK Outer
VAR_OUTPUT
  Q : INT;
END_VAR
VAR
  f : Counter;
END_VAR
  f(IN := 2);
  Q := f.Q;
END_FUNCTION_BLOCK

PROGRAM main
VAR
  o : Outer;
END_VAR
VAR_GLOBAL
  result : INT;
END_VAR
  o();
  result := o.Q;
END_PROGRAM
`
	got := runInstanceVsFunction(t, src, 2)["result"]
	if got.I != 4 {
		t.Fatalf("result = %+v after 2 scans, want 4 (Outer's Counter instance f runs, not FUNCTION F)", got)
	}
}

// With no instance of that name in scope the call still reaches the
// FUNCTION, as before; a non-FB variable named like it is not callable
// and does not hide it.
func TestFunctionCallUnaffectedWithoutShadowingInstance(t *testing.T) {
	src := instanceVsFunctionPOUs + `
PROGRAM main
VAR_GLOBAL
  result : INT;
END_VAR
  result := f(IN := 2);
END_PROGRAM
`
	if got := runInstanceVsFunction(t, src, 1)["result"]; got.I != 200 {
		t.Fatalf("result = %+v, want 200 from FUNCTION F", got)
	}
}

// An instance in scope hides the FUNCTION in an expression too: the call
// is refused with a message that names the clash, instead of silently
// calling the function.
func TestFBInstanceNamedLikeFunctionInExpressionIsAnError(t *testing.T) {
	src := instanceVsFunctionPOUs + `
PROGRAM main
VAR
  f : Counter;
END_VAR
VAR_GLOBAL
  result : INT;
END_VAR
  result := F(IN := 2);
END_PROGRAM
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Lower(prog)
	if err == nil {
		t.Fatal("lower succeeded; want an error: the instance f hides FUNCTION F")
	}
	if !strings.Contains(err.Error(), "can't be used as an expression") || !strings.Contains(err.Error(), "FUNCTION F") {
		t.Fatalf("err = %v, want the FB-instance-as-expression error naming FUNCTION F", err)
	}
}

// The `naut check` / editor warning: an instance named like a FUNCTION is
// reported on its declaration; one in a block is reported too; a non-FB
// local and an unrelated instance are not.
func TestInstancesShadowingFunctions(t *testing.T) {
	lib, err := Parse(instanceVsFunctionPOUs)
	if err != nil {
		t.Fatal(err)
	}
	own, err := Parse(`
FUNCTION_BLOCK Outer
VAR
  F : Counter;
END_VAR
  F(IN := 1);
END_FUNCTION_BLOCK

PROGRAM main
VAR
  f : Counter;
  c : Counter;
  scale : INT;
  t : TON;
END_VAR
  f(IN := 1);
END_PROGRAM
`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range InstancesShadowingFunctions(own, lib) {
		got = append(got, s.Decl.Name+"/"+s.Func)
	}
	if strings.Join(got, ",") != "F/F,f/F" {
		t.Fatalf("shadows = %v, want [F/F f/F]", got)
	}
	// A standard FB instance named like a project FUNCTION counts too.
	own2, _ := Parse("FUNCTION Tmr : INT\nTmr := 1;\nEND_FUNCTION\nPROGRAM main\nVAR\n  tmr : TON;\nEND_VAR\nEND_PROGRAM\n")
	if s := InstancesShadowingFunctions(own2, nil); len(s) != 1 || s[0].Decl.Name != "tmr" || s[0].Func != "Tmr" {
		t.Fatalf("shadows = %+v, want tmr/Tmr", s)
	}
}
