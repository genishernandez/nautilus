package main

// The brownfield round trip on the public yardstick, at the CLI: the four
// commands the video beat shows (#276), run as a user runs them, on
// examples/batch-skid/line/Line.L5X. logix/importer's
// TestRoundTripPublicYardstick is the library-level form of the rung
// comparison; this pins the user's flow and everything it puts back —
// controller, task, routine, initial values, descriptions — and names
// every difference that remains: none in the logic, none in a tag's
// value or description.

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

func TestLogixRoundTripYardstickCLI(t *testing.T) {
	src := filepath.Join("..", "..", "examples", "batch-skid", "line", "Line.L5X")
	dir := t.TempDir()
	proj := filepath.Join(dir, "line")
	back := filepath.Join(dir, "back.L5X")

	// 1. naut logix import --project line Line.L5X
	out, code := capture(t, func() int { return runLogix([]string{"import", "--project", proj, src}) })
	if code != 0 || !strings.Contains(out, "1 ladder routine, 1 complete; 5 rungs, 5 carried") {
		t.Fatalf("import: exit %d\n%s", code, out)
	}
	// The project it writes checks clean: no errors, and no warning (the
	// timer the program owns is not also a project tag).
	for _, args := range [][]string{{proj}, {"--target", "logix", proj}} {
		out, code = capture(t, func() int { return runCheck(args) })
		if code != 0 || strings.Contains(out, "warning") {
			t.Errorf("naut check %v: exit %d\n%s", args, code, out)
		}
	}

	// 2. naut logix write -o back.L5X line/MainProgram.ld — no flags: the
	// envelope and the tags' seeds come from the imported nautilus.yaml.
	out, code = capture(t, func() int {
		return runLogix([]string{"write", "-o", back, filepath.Join(proj, "MainProgram.ld")})
	})
	if code != 0 {
		t.Fatalf("write: exit %d\n%s", code, out)
	}

	// 3. naut logix normalize --logic --check back.L5X Line.L5X — the
	// logic is the same: every tag back at its own scope (AcceptTmr at
	// controller scope, #371; Line_BatchId, which no routine names, #370)
	// and every rung byte-identical (#372). A difference fails here
	// (docs/design/logix-authoring.md, "Round trip on the yardstick").
	out, code = capture(t, func() int { return runLogix([]string{"normalize", "--logic", "--check", back, src}) })
	if want := fmt.Sprintf("%s and %s have the same logic\n", src, back); code != 0 || out != want {
		t.Errorf("logic check: exit %d\n%s\nwant\n%s", code, out, want)
	}

	// What the logic comparison does not look at, compared here: the
	// controller and its task, and each tag's initial value and description.
	orig, err := l5x.ParseFile(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := l5x.ParseFile(back)
	if err != nil {
		t.Fatal(err)
	}
	oc, gc := orig.Controller, got.Controller
	if gc.Name != oc.Name || gc.ProcessorType != oc.ProcessorType || gc.MajorRev != oc.MajorRev || gc.MinorRev != oc.MinorRev {
		t.Errorf("controller %s %s %s.%s, want %s %s %s.%s", gc.Name, gc.ProcessorType, gc.MajorRev, gc.MinorRev,
			oc.Name, oc.ProcessorType, oc.MajorRev, oc.MinorRev)
	}
	if len(gc.Tasks) != 1 || !reflect.DeepEqual(*gc.Tasks[0], *oc.Tasks[0]) {
		t.Errorf("tasks %+v, want %+v", gc.Tasks, oc.Tasks)
	}
	if p := gc.Programs[0]; p.Name != "MainProgram" || p.MainRoutineName != "Receive" || p.Routines[0].Name != "Receive" {
		t.Errorf("program %s main routine %s routine %s, want MainProgram/Receive", p.Name, p.MainRoutineName, p.Routines[0].Name)
	}
	gotTags := map[string]*l5x.Tag{}
	for _, tg := range gc.Tags {
		gotTags[tg.Name] = tg
	}
	for _, p := range gc.Programs {
		for _, tg := range p.Tags {
			gotTags[tg.Name] = tg
		}
	}
	var diffs []string
	for _, o := range oc.Tags {
		g := gotTags[o.Name]
		switch {
		case g == nil:
			diffs = append(diffs, o.Name+": missing")
			continue
		case !reflect.DeepEqual(g.Value, o.Value):
			diffs = append(diffs, fmt.Sprintf("%s: value %v, want %v", o.Name, g.Value, o.Value))
		}
		if g.Description != o.Description {
			diffs = append(diffs, fmt.Sprintf("%s: description %q, want %q", o.Name, g.Description, o.Description))
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Errorf("tag differences:\n  %s", strings.Join(diffs, "\n  "))
	}
	// Each written where the export had it: every tag at controller
	// scope, none in the program.
	if n := len(gc.Programs[0].Tags); n != 0 || len(gc.Tags) != len(oc.Tags) {
		t.Errorf("%d controller tags and %d program tags, want %d and 0", len(gc.Tags), n, len(oc.Tags))
	}

	// A flag still overrides the manifest.
	again := filepath.Join(dir, "again.L5X")
	if _, code := capture(t, func() int {
		return runLogix([]string{"write", "-o", again, "--routine", "Main", "--period", "50", filepath.Join(proj, "MainProgram.ld")})
	}); code != 0 {
		t.Fatalf("write with flags: exit %d", code)
	}
	f, err := l5x.ParseFile(again)
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Controller.Programs[0].Routines[0].Name; r != "Main" || f.Controller.Tasks[0].Type != "PERIODIC" || f.Controller.Tasks[0].RateMs != 50 || f.Controller.Name != "LineController" {
		t.Errorf("flags: routine %s task %+v controller %s", r, *f.Controller.Tasks[0], f.Controller.Name)
	}
}
