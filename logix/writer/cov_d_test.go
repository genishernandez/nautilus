package writer

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// dRungsAndTag writes src and returns its rung texts and the program tag
// named name.
func dRungsAndTag(t *testing.T, src, name string) ([]string, *l5x.Tag) {
	t.Helper()
	f := mustWrite(t, src, Options{})
	var rungs []string
	for _, rg := range f.Controller.Programs[0].Routines[0].Rungs {
		rungs = append(rungs, rg.Text)
	}
	for _, tg := range f.Controller.Programs[0].Tags {
		if tg.Name == name {
			return rungs, tg
		}
	}
	t.Fatalf("no program tag %q", name)
	return nil, nil
}

// dPresets reads PRE out of every element of an array-of-structures tag.
func dPresets(t *testing.T, tg *l5x.Tag) []any {
	t.Helper()
	arr, ok := tg.Value.([]any)
	if !ok {
		t.Fatalf("%s value = %#v, want an array", tg.Name, tg.Value)
	}
	out := make([]any, len(arr))
	for i, e := range arr {
		m, _ := e.(map[string]any)
		out[i] = m["PRE"]
	}
	return out
}

// docs/functions.md#arrays-of-instances: "On a Logix target the array is
// one TIMER[4] tag; an element called with a literal index carries its
// preset in the tag's data, one called with a computed index takes a MOVE
// to its .PRE ahead of the rung."
func TestTimerInstanceArrayOnLogix(t *testing.T) {
	t.Run("literal indices preset in the tag's data", func(t *testing.T) {
		src := `PROGRAM P
VAR_EXTERNAL
    a : BOOL; b : BOOL; c : BOOL;
END_VAR
VAR
    Timers : ARRAY [0..3] OF TON;
END_VAR
LD
  RUNG r0
    a Timers[0]:TON(PT := T#2S) ( b )
  RUNG r1
    a Timers[3]:TON(PT := T#500MS) ( c )
END_LD
END_PROGRAM
`
		rungs, tg := dRungsAndTag(t, src, "Timers")
		if tg.DataType != "TIMER" || tg.Dimensions != "4" {
			t.Fatalf("Timers = %s[%s], want one TIMER[4] tag", tg.DataType, tg.Dimensions)
		}
		want := []any{int64(2000), int64(0), int64(0), int64(500)}
		got := dPresets(t, tg)
		for i := range want {
			if i >= len(got) || got[i] != want[i] {
				t.Fatalf("PRE per element = %v, want %v", got, want)
			}
		}
		wantRungs := []string{
			"XIC(a)TON(Timers[0],?,?)XIC(Timers[0].DN)OTE(b);",
			"XIC(a)TON(Timers[3],?,?)XIC(Timers[3].DN)OTE(c);",
		}
		if strings.Join(rungs, "\n") != strings.Join(wantRungs, "\n") {
			t.Errorf("rungs (no MOVE for a literal index)\n got %s\nwant %s", strings.Join(rungs, "\n     "), strings.Join(wantRungs, "\n     "))
		}
	})

	t.Run("a computed index takes a MOVE to .PRE ahead of the rung", func(t *testing.T) {
		src := `PROGRAM P
VAR_EXTERNAL
    a : BOOL; b : BOOL; i : DINT;
END_VAR
VAR
    Timers : ARRAY [0..3] OF TON;
END_VAR
LD
  RUNG r0
    a Timers[i]:TON(PT := T#1S) ( b )
END_LD
END_PROGRAM
`
		rungs, tg := dRungsAndTag(t, src, "Timers")
		if tg.DataType != "TIMER" || tg.Dimensions != "4" {
			t.Fatalf("Timers = %s[%s], want one TIMER[4] tag", tg.DataType, tg.Dimensions)
		}
		wantRungs := []string{
			"MOVE(1000,Timers[i].PRE);",
			"XIC(a)TON(Timers[i],?,?)XIC(Timers[i].DN)OTE(b);",
		}
		if strings.Join(rungs, "\n") != strings.Join(wantRungs, "\n") {
			t.Errorf("rungs\n got %s\nwant %s", strings.Join(rungs, "\n     "), strings.Join(wantRungs, "\n     "))
		}
		for i, p := range dPresets(t, tg) {
			if p != int64(0) {
				t.Errorf("Timers[%d].PRE = %v in the data, want 0 (the MOVE sets it)", i, p)
			}
		}
	})
}
