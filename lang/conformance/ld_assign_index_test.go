package main

// A ladder assignment element is compiled as a SEL on the rung condition,
// so its value is evaluated every scan like any block — docs/functions.md
// "Ladder power flow": "an array index in it must stay valid while the rung
// is false". A rung whose condition is FALSE still faults the scan when the
// index in its assignment is out of bounds; an in-range index on the same
// unpowered rung does not, and the target keeps its value. (A Logix box on
// a false rung would be skipped; this pins that nautilus does not skip.)

import (
	"testing"
	"testing/fstest"

	"github.com/joyautomation/nautilus/acceptance"
	"github.com/joyautomation/nautilus/internal/project"
)

const cAssignIndexLd = `PROGRAM P
VAR_EXTERNAL Run : BOOL; I : INT; Out : INT; END_VAR
VAR Arr : ARRAY[0..3] OF INT; END_VAR
LD
  RUNG fill
    { Arr[2] := 30 }
  RUNG pick
    Run { Out := Arr[I] }
END_LD
END_PROGRAM
`

const cAssignIndexTags = "  - { name: Run, role: state, init: false }\n" +
	"  - { name: I, role: state, init: 0 }\n" +
	"  - { name: Out, role: state, init: -1 }\n"

// cRunLadder runs one ladder program as the project's only task through the
// acceptance harness, and returns the first failing result (nil when every
// test passed).
func cRunLadder(t *testing.T, tags, program, suite string) *acceptance.Result {
	t.Helper()
	fsys := fstest.MapFS{
		"nautilus.yaml": {Data: []byte("name: ld-assign-index\ntasks:\n  - program: p.ld\n    scan: 10ms\ntags:\n" + tags + "\ndriver:\n  type: memory\n")},
		"p.ld":          {Data: []byte(program)},
		"p_test.yaml":   {Data: []byte(suite)},
	}
	proj, err := project.Load(fsys, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results, err := acceptance.RunDir(fsys, proj.Runtime)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no tests ran")
	}
	for i := range results {
		if !results[i].Passed {
			return &results[i]
		}
	}
	return nil
}

func TestLadderAssignmentIndexEvaluatedWhileRungFalse(t *testing.T) {
	t.Run("out-of-range index on an unpowered rung faults", func(t *testing.T) {
		r := cRunLadder(t, cAssignIndexTags, cAssignIndexLd,
			"tests:\n  - name: rung false, index 9\n    given: { Run: false, I: 9 }\n    steps:\n      - scans: 1\n")
		wantFault(t, r, "out of bounds")
	})
	t.Run("in-range index on an unpowered rung does not fault and leaves the target alone", func(t *testing.T) {
		r := cRunLadder(t, cAssignIndexTags, cAssignIndexLd,
			"tests:\n  - name: rung false, index 2\n    given: { Run: false, I: 2 }\n    steps:\n      - scans: 3\n        expect: { Out: -1 }\n")
		if r != nil {
			t.Fatalf("unexpected failure:\n%s", acceptance.FormatFailure(*r))
		}
	})
	t.Run("in-range index on a powered rung assigns", func(t *testing.T) {
		r := cRunLadder(t, cAssignIndexTags, cAssignIndexLd,
			"tests:\n  - name: rung true, index 2\n    given: { Run: true, I: 2 }\n    steps:\n      - scans: 1\n        expect: { Out: 30 }\n")
		if r != nil {
			t.Fatalf("unexpected failure:\n%s", acceptance.FormatFailure(*r))
		}
	})
}
