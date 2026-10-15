package main

// A project with a target: logix section, read the way `naut logix deploy`
// reads it: what the manifest decides (tag types, names, the task, the
// agent) and what it refuses. The deploys run against the fake agent; the
// L5X they hand it is the evidence.

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// fillLadder names four manifest tags it does not declare, and declares one
// program variable of its own.
const fillLadder = `PROGRAM Fill
VAR
    Step : DINT;
END_VAR
LD
  RUNG speed Start { Speed := 5 }
  RUNG count GT(Level, 50.0) { Count := Count + 1 }
  RUNG step Start { Step := 1 }
END_LD
END_PROGRAM
`

func fillManifest(agent, extra string) string {
	return `name: line
tasks:
  - program: Fill.ld
    scan: 250ms
tags:
  - { name: Speed, role: setpoint, type: INT, init: 0 }
  - { name: Level, role: input, init: 0.0 }
  - { name: Count, role: state, type: DINT }
  - { name: Start, role: input, init: false }
  - { name: Unused, role: state, init: 0 }
target:
  logix:
    controller: Line
    agent: ` + agent + `
` + extra
}

// deployed runs `naut logix deploy` (build only) on dir and returns the
// whole-project L5X it handed the agent.
func deployed(t *testing.T, f *fakeLogixd, args ...string) *l5x.File {
	t.Helper()
	captureStdout(t, func() int {
		_, code := captureStderr(t, func() int { return runLogixDeploy(args) })
		return code
	})
	for name, raw := range f.files {
		if strings.HasSuffix(name, "/Line.L5X") {
			doc, err := l5x.Parse(raw)
			if err != nil {
				t.Fatalf("deployed L5X does not parse: %v", err)
			}
			return doc
		}
	}
	t.Fatalf("no project L5X reached the agent; requests: %v", f.seen)
	return nil
}

func tagTypes(tags []*l5x.Tag) map[string]string {
	out := map[string]string{}
	for _, tg := range tags {
		out[tg.Name] = tg.DataType
	}
	return out
}

// LGA-002: a program that names manifest tags without declaring them gets
// their controller tags, typed from nautilus.yaml: `type:` when given, else
// the type `init:` implies. A tag no program names is not written.
func TestLogixDeployTypesUndeclaredTagsFromTheManifest(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()
	dir := writeFiles(t, map[string]string{"nautilus.yaml": fillManifest(url, ""), "Fill.ld": fillLadder})

	got := tagTypes(deployed(t, f, dir).Controller.Tags)
	for name, want := range map[string]string{
		"Speed": "INT",  // type: INT, though init: 0 alone would imply REAL
		"Level": "REAL", // no type: what init: 0.0 implies
		"Count": "DINT", // type: DINT, no init at all
		"Start": "BOOL", // no type: what init: false implies
	} {
		if got[name] != want {
			t.Errorf("controller tag %s is %q, want %q (all: %v)", name, got[name], want, got)
		}
	}
	if _, ok := got["Unused"]; ok {
		t.Errorf("a manifest tag no program names was written: %v", got)
	}

	// `naut logix write` in the project reads the same manifest.
	out := writeFiles(t, nil)
	if code := runLogixWrite([]string{"-o", out + "/w.L5X", dir + "/Fill.ld"}); code != 0 {
		t.Fatalf("naut logix write exit %d", code)
	}
	w, err := l5x.ParseFile(out + "/w.L5X")
	if err != nil {
		t.Fatal(err)
	}
	if wt := tagTypes(w.Controller.Tags); wt["Speed"] != "INT" || wt["Level"] != "REAL" || wt["Count"] != "DINT" {
		t.Errorf("naut logix write typed the tags %v", wt)
	}
}

// target.logix.all-tags and controller-scoped, as `naut logix import`
// writes them (#370, #371), reach both `naut logix deploy` and `naut logix
// write`: the manifest tag no program names is written too, typed from
// its init:, and the program's VAR listed under controller-scoped is a
// controller tag carrying the description the entry gives it.
func TestLogixDeployAllTagsAndControllerScoped(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()
	extra := "    all-tags: true\n    controller-scoped:\n      - { name: Step, desc: \"the fill step\" }\n"
	dir := writeFiles(t, map[string]string{"nautilus.yaml": fillManifest(url, extra), "Fill.ld": fillLadder})

	out := writeFiles(t, nil)
	if code := runLogixWrite([]string{"-o", out + "/w.L5X", dir + "/Fill.ld"}); code != 0 {
		t.Fatalf("naut logix write exit %d", code)
	}
	w, err := l5x.ParseFile(out + "/w.L5X")
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]*l5x.File{"deploy": deployed(t, f, dir), "write": w} {
		c := doc.Controller
		got := map[string]*l5x.Tag{}
		for _, tg := range c.Tags {
			got[tg.Name] = tg
		}
		if u := got["Unused"]; u == nil || u.DataType != "REAL" {
			t.Errorf("%s: the tag no program names = %+v, want a REAL controller tag", name, u)
		}
		if s := got["Step"]; s == nil || s.DataType != "DINT" || s.Description != "the fill step" {
			t.Errorf("%s: controller-scoped Step = %+v", name, s)
		}
		if n := len(c.Programs[0].Tags); n != 0 {
			t.Errorf("%s: %d program tags, want none", name, n)
		}
	}
}

// LGA-003: the task becomes a periodic Logix task at its scan rate, the
// program a Logix program with its VAR as program tags, the rungs its
// MainRoutine; program, routine and task under target.logix rename them.
func TestLogixDeployNamesTheTaskProgramAndRoutine(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()

	check := func(doc *l5x.File, program, routine, task string) {
		t.Helper()
		c := doc.Controller
		if len(c.Programs) != 1 || c.Programs[0].Name != program {
			t.Fatalf("programs = %+v, want one named %s", c.Programs, program)
		}
		p := c.Programs[0]
		if p.MainRoutineName != routine || len(p.Routines) != 1 || p.Routines[0].Name != routine || len(p.Routines[0].Rungs) != 3 {
			t.Errorf("program %s: main routine %q, routines %+v; want %s with the 3 rungs", p.Name, p.MainRoutineName, p.Routines, routine)
		}
		if pt := tagTypes(p.Tags); pt["Step"] != "DINT" {
			t.Errorf("the program's VAR Step is not a program tag: %v", pt)
		}
		if _, ok := tagTypes(c.Tags)["Step"]; ok {
			t.Error("the program's VAR Step became a controller tag")
		}
		if len(c.Tasks) != 1 {
			t.Fatalf("tasks = %+v", c.Tasks)
		}
		tk := c.Tasks[0]
		if tk.Name != task || tk.Type != "PERIODIC" || tk.RateMs != 250 || len(tk.Programs) != 1 || tk.Programs[0] != program {
			t.Errorf("task = %+v, want %s PERIODIC at 250 ms scheduling %s", tk, task, program)
		}
	}

	dir := writeFiles(t, map[string]string{"nautilus.yaml": fillManifest(url, ""), "Fill.ld": fillLadder})
	check(deployed(t, f, dir), "Fill", "MainRoutine", "MainTask")

	f.files = map[string][]byte{}
	dir = writeFiles(t, map[string]string{
		"nautilus.yaml": fillManifest(url, "    program: Filler\n    routine: Rungs\n    task: Fast\n"),
		"Fill.ld":       fillLadder,
	})
	check(deployed(t, f, dir), "Filler", "Rungs", "Fast")
}

// LGA-035: the agent URL comes from target.logix.agent, NAUTILUS_LOGIXD_URL
// overrides it, and the token comes from NAUTILUS_LOGIXD_TOKEN; the
// manifest has no place for one.
func TestLogixDeployFindsTheAgent(t *testing.T) {
	manifestAgent, mURL, stopM := newFakeLogixd(t)
	defer stopM()
	envAgent, eURL, stopE := newFakeLogixd(t)
	defer stopE()
	dir := writeFiles(t, map[string]string{"nautilus.yaml": fillManifest(mURL, ""), "Fill.ld": fillLadder})

	t.Setenv("NAUTILUS_LOGIXD_TOKEN", "tok-123")
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	deployed(t, manifestAgent, dir)
	if len(envAgent.seen) != 0 {
		t.Errorf("with no override the deploy reached the other agent: %v", envAgent.seen)
	}
	for i, a := range manifestAgent.auth {
		if a != "Bearer tok-123" {
			t.Fatalf("request %s carried Authorization %q, want the NAUTILUS_LOGIXD_TOKEN bearer", manifestAgent.seen[i], a)
		}
	}

	manifestAgent.seen = nil
	t.Setenv("NAUTILUS_LOGIXD_URL", eURL)
	deployed(t, envAgent, dir)
	if len(manifestAgent.seen) != 0 {
		t.Errorf("NAUTILUS_LOGIXD_URL did not override target.logix.agent: the manifest's agent saw %v", manifestAgent.seen)
	}

	// A token in the manifest is not a setting: the manifest is refused.
	bad := writeFiles(t, map[string]string{
		"nautilus.yaml": fillManifest(mURL, "    token: tok-123\n"),
		"Fill.ld":       fillLadder,
	})
	out, code := captureStderr(t, func() int { return runLogixDeploy([]string{bad}) })
	if code == 0 || !strings.Contains(out, "token") {
		t.Errorf("a manifest token was accepted (exit %d):\n%s", code, out)
	}
}

const seqSFC = `PROGRAM Seq
VAR
    A : BOOL;
    Y : BOOL;
END_VAR
SFC
  INITIAL_STEP S0:
    N Y;
  END_STEP
  STEP S1:
  END_STEP
  TRANSITION t1 FROM S0 TO S1 := A;
  END_TRANSITION
  TRANSITION t2 FROM S1 TO S0 := NOT A;
  END_TRANSITION
END_SFC
END_PROGRAM
`

// LGA-005 / LGA-036: one task running one ladder or ST program. An SFC
// program is refused by `naut check --target logix` and by the deploy, and
// so is a project with more than one task.
func TestLogixTargetRefusesSFCAndASecondTask(t *testing.T) {
	t.Run("SFC under naut check --target logix", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"seq.sfc": seqSFC})
		if out, code := capture(t, func() int { return runCheck([]string{dir}) }); code != 0 {
			t.Fatalf("the chart does not check clean without the target (%d):\n%s", code, out)
		}
		out, code := capture(t, func() int { return runCheck([]string{"--target", "logix", dir}) })
		if code != 1 || !strings.Contains(out, "seq.sfc: logix target: only ladder (.ld) and structured text (.st)") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("SFC task program under deploy", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{
			"nautilus.yaml": "name: s\ntasks:\n  - program: seq.sfc\n    scan: 100ms\ntarget:\n  logix:\n    controller: S\n",
			"seq.sfc":       seqSFC,
		})
		out, code := captureStderr(t, func() int { return runLogixDeploy([]string{"--agent", "http://127.0.0.1:1", dir}) })
		if code != 2 || !strings.Contains(out, "only ladder (.ld) and structured text (.st) programs are in the Logix subset") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("two tasks under deploy", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{
			"nautilus.yaml": "name: two\ntasks:\n  - program: a.ld\n    scan: 100ms\n  - program: b.ld\n    scan: 500ms\ntarget:\n  logix:\n    controller: Two\n",
			"a.ld":          "PROGRAM A\nVAR X : BOOL; Y : BOOL; END_VAR\nLD\n  RUNG r X ( Y )\nEND_LD\nEND_PROGRAM\n",
			"b.ld":          "PROGRAM B\nVAR X : BOOL; Y : BOOL; END_VAR\nLD\n  RUNG r X ( Y )\nEND_LD\nEND_PROGRAM\n",
		})
		out, code := captureStderr(t, func() int { return runLogixDeploy([]string{"--agent", "http://127.0.0.1:1", dir}) })
		if code != 2 || !strings.Contains(out, "takes exactly one task; nautilus.yaml declares 2") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
}

// LGA-021: one source, two places. The tags' alias: is the hardware
// binding: the deploy writes alias tags onto the rack points, while
// `naut test` runs the same program on the nautilus runtime, which ignores
// the binding and treats them as plain tags.
func TestAliasedTagsRunInSimulationAndDeployAsAliases(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()
	dir := writeFiles(t, map[string]string{
		"nautilus.yaml": `name: seal
tasks:
  - program: Seal.ld
    scan: 100ms
tags:
  - { name: StartPB, role: input, init: false, alias: "Local:1:I.Data.3" }
  - { name: StopPB, role: input, init: false, alias: "Local:1:I.Data.4" }
  - { name: RunCmd, role: output, init: false, alias: "Local:2:O.Data.0" }
target:
  logix:
    controller: Line
    agent: ` + url + `
`,
		"Seal.ld": `PROGRAM Seal
LD
  RUNG seal [ StartPB | RunCmd ] /StopPB ( RunCmd )
END_LD
END_PROGRAM
`,
		"seal_test.yaml": `tests:
  - name: start seals in and stop drops out
    steps:
      - given: { StartPB: true }
        scans: 1
        expect: { RunCmd: true }
      - given: { StartPB: false }
        scans: 1
        expect: { RunCmd: true }
      - given: { StopPB: true }
        scans: 1
        expect: { RunCmd: false }
`,
	})

	// Simulation: the runtime runs the program on the tags as tags.
	if out, code := capture(t, func() int { return runTest([]string{dir}) }); code != 0 || !strings.Contains(out, "1 passed") {
		t.Fatalf("naut test on the aliased project: exit %d\n%s", code, out)
	}

	// Hardware: the same tags go to the controller as alias tags.
	doc := deployed(t, f, dir)
	alias := map[string]string{}
	for _, tg := range doc.Controller.Tags {
		alias[tg.Name] = tg.AliasFor
	}
	for name, want := range map[string]string{"StartPB": "Local:1:I.Data.3", "StopPB": "Local:1:I.Data.4", "RunCmd": "Local:2:O.Data.0"} {
		if alias[name] != want {
			t.Errorf("controller tag %s aliases %q, want %q (all: %v)", name, alias[name], want, alias)
		}
	}
	rungs := doc.Controller.Programs[0].Routines[0].Rungs
	if len(rungs) != 1 || rungs[0].Text != "[XIC(StartPB) ,XIC(RunCmd) ]XIO(StopPB)OTE(RunCmd);" {
		t.Errorf("the rung does not name the alias tags: %+v", rungs)
	}
}
