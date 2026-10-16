package acceptance_test

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/joyautomation/nautilus/acceptance"
	"github.com/joyautomation/nautilus/internal/project"
)

// Getting started: "the fixture for tests comes from nautilus.yaml, so a
// retuned manifest gain changes what the tests run against." The same test
// file runs against two manifests that differ only in Kp: it passes where the
// manifest's Kp makes its expectation true and fails where Kp was retuned.
func TestAcceptanceFixtureComesFromTheManifest(t *testing.T) {
	const testFile = `tests:
  - name: gain doubles the setpoint
    steps:
      - scans: 1
        expect: { Out: 10.0 }
`
	run := func(kp float64) []acceptance.Result {
		t.Helper()
		fsys := fstest.MapFS{
			"nautilus.yaml": {Data: []byte(fmt.Sprintf(
				"tasks:\n  - program: main.st\ntags:\n  - { name: Kp, role: setpoint, init: %v }\n  - { name: Out, role: state, init: 0.0 }\n", kp))},
			"main.st":        {Data: []byte("PROGRAM Main\nOut := Kp * 2.0;\nEND_PROGRAM\n")},
			"gain_test.yaml": {Data: []byte(testFile)},
		}
		proj, err := project.Load(fsys, "")
		if err != nil {
			t.Fatal(err)
		}
		results, err := acceptance.RunDir(fsys, proj.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1", len(results))
		}
		return results
	}

	if r := run(5.0); !r[0].Passed {
		t.Fatalf("with the manifest's Kp = 5 the test must pass:\n%s", acceptance.FormatFailure(r[0]))
	}
	if r := run(6.0); r[0].Passed {
		t.Fatal("retuning Kp in the manifest to 6 left the test passing: the fixture is not coming from nautilus.yaml")
	}
}
