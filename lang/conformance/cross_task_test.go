package main

// #327 (FN-021): a function-block instance is a local of the program that
// declares it, and tasks share only tags. Another task's program cannot
// name the instance — not as `t1.Q`, not through the owning program's
// name (`Owner.t1.Q`) nor its task's (`timing.t1.Q`) — and the error says
// so and says what to do: publish the output to a tag in the owner and
// read the tag. The recipe itself is pinned below, with its timing: a
// reader sees the owner's last committed scan.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/joyautomation/nautilus/acceptance"
	"github.com/joyautomation/nautilus/internal/project"
)

// ownerLD is the issue's shape: an inline TON in a ladder rung (the
// rung's only output), whose Q a second rung publishes to a tag; a third
// reads the non-BOOL pin ET as a contact's argument.
const ownerLD = `PROGRAM Owner
LD
  RUNG timer
    Run t1:TON(PT := T#30ms)
  RUNG publish
    t1.Q ( T1Done )
  RUNG warm
    GE(t1.ET, T#20ms) ( Warm )
END_LD
END_PROGRAM
`

// crossTaskProject is a resource of two or more tasks, all at 10 ms: main
// runs mainFile, then each of tasks (name → file) in order. files holds
// every program; suite, when set, is the acceptance suite.
func crossTaskProject(mainFile string, tasks [][2]string, files map[string]string, suite string) fstest.MapFS {
	var y strings.Builder
	y.WriteString("name: cross-task\ntasks:\n  - program: " + mainFile + "\n    scan: 10ms\n")
	for _, t := range tasks {
		y.WriteString("  - name: " + t[0] + "\n    program: " + t[1] + "\n    scan: 10ms\n")
	}
	y.WriteString(`tags:
  - { name: Run,       role: state, init: false }
  - { name: T1Done,    role: state, init: false }
  - { name: Warm,      role: state, init: false }
  - { name: Done,      role: state, init: false }
  - { name: EarlySeen, role: state, init: false }
  - { name: LateSeen,  role: state, init: false }
driver:
  type: memory
`)
	fsys := fstest.MapFS{"nautilus.yaml": {Data: []byte(y.String())}}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	if suite == "" {
		suite = "tests:\n  - name: one scan\n    steps:\n      - scans: 1\n"
	}
	fsys["p_test.yaml"] = &fstest.MapFile{Data: []byte(suite)}
	return fsys
}

// loadAndRun loads fsys and runs its suite: the load/compile error text,
// or the first failing step's, or "".
func loadAndRun(t *testing.T, fsys fstest.MapFS) string {
	t.Helper()
	proj, err := project.Load(fsys, "")
	if err != nil {
		return err.Error()
	}
	results, err := acceptance.RunDir(fsys, proj.Runtime)
	if err != nil {
		return err.Error()
	}
	for _, r := range results {
		if !r.Passed {
			return r.Failure.Reason + ": " + r.Failure.Detail
		}
	}
	return ""
}

// Every way of naming another task's instance fails to compile, and the
// error names the owner and points at a tag — in ST and in ladder.
func TestCrossTaskInstanceIsDiagnosed(t *testing.T) {
	for name, c := range map[string]struct{ file, src, want string }{
		"bare instance from ST": {"reader.st",
			"PROGRAM Reader\nDone := t1.Q;\nEND_PROGRAM\n",
			`undeclared identifier "t1": t1 is a local of program Owner (task main), and one program cannot read another's variables`},
		"through the program name": {"reader.st",
			"PROGRAM Reader\nDone := Owner.t1.Q;\nEND_PROGRAM\n",
			`undeclared identifier "Owner": Owner is a program (task main), and a program's variables cannot be addressed through its name`},
		"bare instance from a ladder contact": {"reader.ld",
			"PROGRAM Reader\nLD\n  RUNG r\n    t1.Q ( Done )\nEND_LD\nEND_PROGRAM\n",
			`undeclared identifier "t1": t1 is a local of program Owner (task main)`},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := crossTaskProject("owner.ld", [][2]string{{"reader", c.file}},
				map[string]string{"owner.ld": ownerLD, c.file: c.src}, "")
			got := loadAndRun(t, fsys)
			wantCompileErr(t, got, c.want)
			wantCompileErr(t, got, "Tasks share only tags: in Owner, write the value to a tag (an assignment or a coil) and read that tag here")
		})
	}
	// A plain VAR is as private as an instance.
	t.Run("a plain VAR of another program", func(t *testing.T) {
		fsys := crossTaskProject("owner.st", [][2]string{{"reader", "reader.st"}}, map[string]string{
			"owner.st":  "PROGRAM Owner\nVAR armed : BOOL; END_VAR\narmed := Run;\nEND_PROGRAM\n",
			"reader.st": "PROGRAM Reader\nDone := armed;\nEND_PROGRAM\n",
		}, "")
		wantCompileErr(t, loadAndRun(t, fsys),
			`undeclared identifier "armed": armed is a local of program Owner (task main), and one program cannot read another's variables. Tasks share only tags`)
	})
	// Through the owning task's name, with the owner a named task.
	t.Run("through the task name", func(t *testing.T) {
		fsys := crossTaskProject("reader.st", [][2]string{{"timing", "owner.ld"}}, map[string]string{
			"owner.ld":  ownerLD,
			"reader.st": "PROGRAM Reader\nDone := timing.t1.Q;\nEND_PROGRAM\n",
		}, "")
		wantCompileErr(t, loadAndRun(t, fsys),
			`undeclared identifier "timing": timing is the task running program Owner, and a program's variables cannot be addressed through its task. Tasks share only tags`)
	})
}

// A name that is no other program's variable keeps the plain message.
func TestUndeclaredIdentifierWithoutPeerKeepsItsMessage(t *testing.T) {
	fsys := crossTaskProject("owner.ld", [][2]string{{"reader", "reader.st"}}, map[string]string{
		"owner.ld":  ownerLD,
		"reader.st": "PROGRAM Reader\nDone := t9.Q;\nEND_PROGRAM\n",
	}, "")
	wantCompileErr(t, loadAndRun(t, fsys), `undeclared identifier "t9" (declare in VAR_* or VAR_GLOBAL block)`)
}

// Inside its own program the instance reads as inst.Pin from any rung — t1.Q
// as a coil's condition, the TIME pin t1.ET as a contact's argument.
//
// The page's recipe: the owner publishes t1.Q to a tag, and other tasks
// read the tag. A reader sees the value of the owner's last committed
// scan: one that scans after the owner in a tick (late) sees the rise on
// that tick; one that scans before it (main, here) sees it a scan later.
func TestCrossTaskPublishedInstanceOutput(t *testing.T) {
	suite := `tests:
  - name: readers see the published output as of the owner's last committed scan
    given: { Run: true }
    steps:
      - scans: 2 # ET 0 ms, then 10 ms
        expect: { Warm: false, T1Done: false, EarlySeen: false, LateSeen: false }
      - scans: 1 # ET 20 ms: the ET contact passes
        expect: { Warm: true, T1Done: false, EarlySeen: false, LateSeen: false }
      - scans: 1 # the owner's TON reaches PT and publishes
        expect: { T1Done: true, LateSeen: true, EarlySeen: false }
      - scans: 1
        expect: { T1Done: true, LateSeen: true, EarlySeen: true }
      - given: { Run: false }
        scans: 1 # the owner drops Q; main read before it committed
        expect: { T1Done: false, LateSeen: false, EarlySeen: true }
      - scans: 1
        expect: { EarlySeen: false }
`
	fsys := crossTaskProject("early.st", [][2]string{{"owner", "owner.ld"}, {"late", "late.st"}}, map[string]string{
		"early.st": "PROGRAM Early\nEarlySeen := T1Done;\nEND_PROGRAM\n",
		"owner.ld": ownerLD,
		"late.st":  "PROGRAM Late\nLateSeen := T1Done;\nEND_PROGRAM\n",
	}, suite)
	if got := loadAndRun(t, fsys); got != "" {
		t.Fatal(got)
	}
}
