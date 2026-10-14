package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// Arrays in the netlist: element reads (Levels[2], m[1][2], tbl[i].vals),
// element/member coil writes, verbatim transpile, chips in the render
// model, and the edit gestures that touch them.

const arraySrc = `PROGRAM Tanks
VAR_EXTERNAL
  Levels : ARRAY[1..4] OF REAL;
  m : ARRAY[1..2, 1..2] OF REAL;
  i : INT;
  HiAlm : BOOL;
END_VAR
FBD
  top = MAX(Levels[1], Levels[2], Levels[3])
  HiAlm := GT(top, 90.0)
  Levels[4] := top
  w = ADD(m[1][2], Levels[i])
  m[2][2] := w
END_FBD
END_PROGRAM`

func TestArrayTranspile(t *testing.T) {
	out, err := Transpile(arraySrc)
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	for _, want := range []string{
		"MAX(Levels[1], Levels[2], Levels[3])",
		"Levels[4] :=",
		"m[1][2]",
		"Levels[i]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("transpiled ST missing %q:\n%s", want, out)
		}
	}
}

func TestArrayGraph(t *testing.T) {
	m, err := Graph(arraySrc)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	labels := map[string]string{} // label -> kind
	for _, n := range m.Nodes {
		labels[n.Label] = n.Kind
	}
	for _, chip := range []string{"Levels[1]", "Levels[2]", "Levels[3]", "m[1][2]", "Levels[i]"} {
		if labels[chip] != "input" {
			t.Errorf("expected input chip %q, kinds: %v", chip, labels)
		}
	}
	if labels["Levels[4]"] != "coil" {
		t.Errorf("expected coil Levels[4], kinds: %v", labels)
	}
}

// Reading the exact element a coil writes draws feedback, like a plain
// seal-in; a different element of the same array is an ordinary chip.
func TestArrayElementFeedback(t *testing.T) {
	src := `PROGRAM p
VAR_EXTERNAL a : ARRAY[1..2] OF REAL; x : REAL; END_VAR
FBD
  a[1] := ADD(a[1], x)
  a[2] := a[1]
END_FBD
END_PROGRAM`
	m, err := Graph(src)
	if err != nil {
		t.Fatal(err)
	}
	feedback := 0
	for _, e := range m.Edges {
		if e.Feedback {
			feedback++
		}
	}
	if feedback == 0 {
		t.Error("a[1] reading its own coil must be a feedback edge")
	}

	// And it runs as an accumulator: a[1] keeps its value between scans, so
	// the element feeds itself while a[2] copies it.
	sc := newScanner(t, src)
	for i, want := range []float64{3, 6, 9} {
		sc.scan(0, map[string]ir.Value{"x": ir.RealVal(3)})
		a := sc.h.vals["a"]
		if len(a.Arr) != 2 || a.Arr[0].F != want || a.Arr[1].F != want {
			t.Fatalf("scan %d: a = %+v, want both elements %v", i+1, a.Arr, want)
		}
	}
}

func TestArrayEdits(t *testing.T) {
	// Retarget an element chip to a different element.
	out := apply(t, arraySrc, mustOp(t, arraySrc, EditOp{
		Type: "retarget", Node: "v:Levels[1]", NewName: "Levels[2]",
	}))
	if !strings.Contains(out, "MAX(Levels[2], Levels[2], Levels[3])") {
		t.Errorf("element retarget failed:\n%s", out)
	}
	// Retarget a plain chip to an element.
	out = apply(t, arraySrc, mustOp(t, arraySrc, EditOp{
		Type: "retarget", Node: "v:Levels[i]", NewName: "m[2][1]",
	}))
	if !strings.Contains(out, "ADD(m[1][2], m[2][1])") {
		t.Errorf("retarget to accessor failed:\n%s", out)
	}
	// Garbage accessor text is rejected by the parser-backed validation.
	if _, err := ApplyEdit(arraySrc, EditOp{Type: "retarget", Node: "v:Levels[i]", NewName: "Levels["}); err == nil {
		t.Error("unbalanced accessor must be rejected")
	}
	// Duplicating an element-target coil is skipped (a _copy rename would
	// not parse) — alone, the gesture reports nothing copyable.
	if _, err := ApplyEdit(arraySrc, EditOp{Type: "duplicate", Nodes: []string{"c:Levels[4]"}}); err == nil ||
		!strings.Contains(err.Error(), "nothing copyable") {
		t.Errorf("accessor coil duplicate should be skipped, got %v", err)
	}
	// Duplicating a wire severs its element reads to open pins.
	out = apply(t, arraySrc, mustOp(t, arraySrc, EditOp{Type: "duplicate", Nodes: []string{"b:w.top"}}))
	if !strings.Contains(out, "top_copy = MAX(_, _, _)") {
		t.Errorf("copied wire must sever element reads:\n%s", out)
	}
	// Disconnecting an element read placeholds like any fixed pin.
	out = apply(t, arraySrc, mustOp(t, arraySrc, EditOp{
		Type: "disconnect", To: "b:w.w", ToPin: "IN1",
	}))
	if !strings.Contains(out, "ADD(Levels[i])") && !strings.Contains(out, "ADD(_, Levels[i])") {
		t.Errorf("disconnect on element pin:\n%s", out)
	}
}

// The whole path: netlist with arrays compiles through ST to the IR.
func TestArrayCompilesEndToEnd(t *testing.T) {
	if _, err := Compile(arraySrc); err != nil {
		t.Fatalf("FBD with arrays does not compile: %v", err)
	}
}
