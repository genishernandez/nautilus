package runtime

import (
	"strings"
	"testing"
)

// An edge contact's one-shot memory lives in an instance whose name is
// derived from the rung and tag, so an unrelated online edit (here a new
// rung ahead of it, with an edge contact of its own on the same tag, so a
// name counted across the program would shift) must not reset it: Cmd held TRUE across the swap
// produces no second pulse. A reset memory would see the held input as a
// fresh rising edge.
func TestSwapWarmLadderKeepsEdgeContactMemory(t *testing.T) {
	const v1 = `PROGRAM Main
VAR_EXTERNAL Cmd : BOOL; Rise : BOOL; PRise : BOOL; Halt : BOOL; Lamp : BOOL; Ticks : INT; END_VAR
VAR cnt : CTU; END_VAR
LD
  RUNG start  +Cmd ( Rise )
  RUNG coil   Cmd ( P PRise )
  RUNG count  Rise cnt:CTU(PV := 100, CV => Ticks)
END_LD
END_PROGRAM`
	v2 := strings.Replace(v1, "  RUNG start", "  RUNG lamp   /Halt [ +Cmd | /Halt ] ( Lamp )\n  RUNG start", 1)
	if v2 == v1 {
		t.Fatal("edit did not apply")
	}
	p, err := Compile(v1)
	if err != nil {
		t.Fatal(err)
	}
	tags := NewTags()
	tags.SetBool("Halt", false)
	scan := func(cmd bool) {
		t.Helper()
		tags.SetBool("Cmd", cmd)
		if err := p.Run(tags); err != nil {
			t.Fatal(err)
		}
	}
	scan(false)
	scan(true)
	if !readTag(t, tags, "Rise").B {
		t.Fatal("Rise should pulse on the first scan Cmd is TRUE")
	}
	scan(true)
	if readTag(t, tags, "Rise").B {
		t.Fatal("Rise should be a one-shot")
	}
	if _, err := p.SwapWarm(v2); err != nil {
		t.Fatalf("warm swap: %v", err)
	}
	scan(true) // Cmd still held: not an edge
	if readTag(t, tags, "Rise").B {
		t.Error("Rise pulsed again after the swap: the edge contact's memory was reset by an unrelated edit")
	}
	if readTag(t, tags, "PRise").B {
		t.Error("the P coil pulsed again after the swap: the edge coil's memory was reset by an unrelated edit")
	}
	if !readTag(t, tags, "Lamp").B {
		t.Error("the added rung did not go live")
	}
	scan(false)
	scan(true) // a genuine new edge still fires
	if !readTag(t, tags, "Rise").B {
		t.Error("a real rising edge after the swap should still pulse")
	}
	if !readTag(t, tags, "PRise").B {
		t.Error("a real rising edge after the swap should still pulse the P coil")
	}
	if got := readTag(t, tags, "Ticks").I; got != 2 {
		t.Errorf("Ticks = %d, want 2 (one pulse before the swap, one after)", got)
	}
}
