package runtime

import (
	"strings"
	"testing"
)

// A program that reads a tag nothing has ever written faults the scan with
// "undefined tag <name>" instead of running on a silent zero (ST-005, TAG-003).
func TestScanReadOfUnwrittenTagFaults(t *testing.T) {
	p, err := Compile(`PROGRAM Main
VAR_EXTERNAL
  Level : REAL;
  Out : REAL;
END_VAR
Out := Level;
END_PROGRAM`)
	if err != nil {
		t.Fatal(err)
	}
	tags := NewTags()
	tags.SetReal("Out", 0) // Level is never written
	err = p.Run(tags)
	if err == nil || !strings.Contains(err.Error(), "undefined tag Level") {
		t.Fatalf("Run = %v, want an undefined tag Level fault", err)
	}

	tags.SetReal("Level", 1.5) // once written, the same program runs
	if err := p.Run(tags); err != nil {
		t.Fatalf("Run after the tag is written: %v", err)
	}
	if got := tags.Real("Out"); got != 1.5 {
		t.Errorf("Out = %v, want 1.5", got)
	}
}

// An array index outside the declared bounds faults the scan (ST-040).
func TestScanArrayIndexOutOfBoundsFaults(t *testing.T) {
	const src = `PROGRAM Main
VAR_EXTERNAL
  Idx : INT;
  Out : REAL;
END_VAR
VAR
  Levels : ARRAY[0..3] OF REAL;
END_VAR
Out := Levels[Idx];
END_PROGRAM`
	for _, tc := range []struct {
		name string
		idx  int64
		bad  bool
	}{
		{"in range", 3, false},
		{"past the end", 4, true},
		{"negative", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Compile(src)
			if err != nil {
				t.Fatal(err)
			}
			tags := NewTags()
			tags.Set("Idx", tc.idx)
			tags.SetReal("Out", 0)
			err = p.Run(tags)
			if tc.bad {
				if err == nil || !strings.Contains(err.Error(), "out of bounds") {
					t.Fatalf("Run = %v, want an out of bounds fault", err)
				}
			} else if err != nil {
				t.Fatalf("Run: %v", err)
			}
		})
	}
}
