package acceptance_test

// `always:` is an invariant, not an endpoint: it is checked after every
// scan of the step. The only way to tell that from a check at the end of
// the step is a violation that happens partway through and is gone again
// by the end, which is what the program here produces: Spike is TRUE on
// the third scan and on no other.

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/joyautomation/nautilus/acceptance"
	"github.com/joyautomation/nautilus/internal/project"
)

const spikeProgram = `PROGRAM Main
VAR_EXTERNAL
    N     : INT;
    Spike : BOOL;
END_VAR
N := N + 1;
Spike := N = 3;
END_PROGRAM`

func runSpike(t *testing.T, suite string) []acceptance.Result {
	t.Helper()
	fsys := fstest.MapFS{
		"nautilus.yaml": &fstest.MapFile{Data: []byte(`
tasks:
  - program: program.st
    scan: 100ms
tags:
  - { name: N, role: state, init: 0 }
  - { name: Spike, role: state, init: false }
`)},
		"program.st":      &fstest.MapFile{Data: []byte(spikeProgram)},
		"spike_test.yaml": &fstest.MapFile{Data: []byte(suite)},
	}
	proj, err := project.Load(fsys, "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := acceptance.RunDir(fsys, proj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func TestAlwaysCatchesAViolationThatRecovers(t *testing.T) {
	results := runSpike(t, `
tests:
  - name: the spike is over by the end of the step
    steps:
      - scans: 10
        expect: { Spike: false, N: 10 }
  - name: always sees the spike on the scan it happens
    steps:
      - scans: 10
        always: { Spike: false }
  - name: and so does always beside until
    steps:
      - until: 2s
        expect: "N >= 10"
        always: { Spike: false }
`)
	if len(results) != 3 {
		t.Fatalf("%d results; want 3", len(results))
	}
	// The premise: at the end of the step the invariant holds again, so a
	// check made only there would pass.
	if !results[0].Passed {
		t.Fatalf("premise: %s", acceptance.FormatFailure(results[0]))
	}
	for _, r := range results[1:] {
		if r.Passed {
			t.Errorf("%s: passed; the invariant broke on scan 3", r.Name)
			continue
		}
		if r.Failure.Reason != "invariant broke" {
			t.Errorf("%s: reason %q, want invariant broke", r.Name, r.Failure.Reason)
		}
		// Scan 3 of a 100 ms task lands at 300 ms of virtual time: the
		// failure is stamped on the scan that broke it, not the step's end.
		if r.Failure.At != 300*time.Millisecond {
			t.Errorf("%s: failed at %v, want 300ms (the third scan)", r.Name, r.Failure.At)
		}
	}
}
