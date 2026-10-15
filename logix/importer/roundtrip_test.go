package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/logix/writer"
)

// TestRoundTripPublicYardstick is the offline, public form of the corpus
// round-trip measurement in TestCorpus (docs/design/logix-authoring.md):
// import batch-skid's examples/batch-skid/line/Line.L5X, write the
// imported nautilus project back as Logix, and compare every complete
// program routine's rung text with the original's. "Identical" is the
// corpus comparison, rung text equal modulo generated names (norm); a
// routine that is only textually different is "equivalent" by the design
// doc's definition (the writer moved a rung, e.g. RES ahead of its timer)
// and must be named here, not silently accepted.
//
// Current, documented state: Line.L5X has one program routine,
// MainProgram/Receive (5 rungs), imported complete (no notes) and written
// back EQUIVALENT, not identical: 4 rungs are byte-identical and rung 0
// differs only in the comparison's caption, the export's LES(a,b) coming
// back as LT(a,b) (the same spelling difference TestYardstickMatchesLineL5X
// in logix/writer pins). Both are the less-than instruction. Nothing
// differs beyond that; the comparison below is not loosened for it, the
// LES spelling is the single named exception.
//
// The user's flow at the CLI — import --project, write, normalize --logic
// --check — and the whole-project differences it leaves (tags, values,
// descriptions, the envelope) are pinned by cmd/naut's
// TestLogixRoundTripYardstickCLI (#276).
func TestRoundTripPublicYardstick(t *testing.T) {
	orig, err := l5x.ParseFile(filepath.Join("..", "..", "examples", "batch-skid", "line", "Line.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Import(orig, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != 0 {
		t.Errorf("import notes: %+v", p.Notes)
	}
	libs := libsOf(p)
	// The manifest says where the program goes back to (the routine, a
	// CONTINUOUS task), and the timer the program now owns is not also a
	// project tag.
	for _, want := range []string{"    routine: Receive\n", "    task: MainTask\n", "    continuous: true\n"} {
		if !strings.Contains(string(p.Files["nautilus.yaml"]), want) {
			t.Errorf("nautilus.yaml lacks %q:\n%s", want, p.Files["nautilus.yaml"])
		}
	}
	if strings.Contains(string(p.Files["tags/logix.yaml"]), "AcceptTmr") {
		t.Errorf("tags/logix.yaml carries the program's timer:\n%s", p.Files["tags/logix.yaml"])
	}

	var identical, equivalent, differ []string
	var total int
	for _, prog := range orig.Controller.Programs {
		for _, r := range prog.Routines {
			name := prog.Name + "/" + r.Name
			if r.Type != "RLL" {
				t.Errorf("%s: unexpected %s routine", name, r.Type)
				continue
			}
			total++
			file := l5x.Ident(prog.Name) + ".ld"
			src, ok := p.Files[file]
			if !ok {
				t.Errorf("%s: not imported (no %s)", name, file)
				continue
			}
			if strings.Contains(string(src), "NOT deployable") {
				t.Errorf("%s: imported incomplete", name)
				continue
			}
			out, diags, err := writer.Write(string(src), writer.Options{Controller: orig.Controller.Name, Libs: libs})
			if err != nil || len(diags) > 0 {
				t.Errorf("%s: writer refused: %v %v", name, err, diags)
				continue
			}
			back, err := l5x.Parse(out)
			if err != nil {
				t.Fatalf("%s: written export does not parse: %v", name, err)
			}
			a := rungTexts(orig, prog.Name, r.Name)
			b := rungTexts(back, l5x.Ident(prog.Name), "MainRoutine")
			if strings.Join(a, "\n") == strings.Join(b, "\n") {
				identical = append(identical, name)
				continue
			}
			// The one named equivalence: LES spelled LT by the writer.
			if len(a) == len(b) {
				same := true
				for i := range a {
					if strings.Replace(a[i], "LES(", "LT(", 1) != b[i] {
						same = false
						t.Errorf("%s rung %d differs:\n orig %s\n back %s", name, i, a[i], b[i])
					}
				}
				if same {
					equivalent = append(equivalent, name)
					continue
				}
			}
			differ = append(differ, name)
		}
	}
	if total != 1 || len(identical) != 0 || len(equivalent) != 1 || equivalent[0] != "MainProgram/Receive" || len(differ) != 0 {
		t.Errorf("routines %d; identical %v; equivalent %v; differ %v; want 1 routine, MainProgram/Receive equivalent (LES->LT)", total, identical, equivalent, differ)
	}
}

// An array of timers called by literal and by computed index (the writer's
// arrays golden): written, imported back, and written again, every call
// keeps its PT := — the MOVE helpers fold back into presets, and the
// literal-index elements' presets in the tag's data agree with them.
func TestRoundTripArrayPresets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "writer", "testdata", "arrays.ld"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	out, diags, err := writer.Write(src, writer.Options{})
	if err != nil || len(diags) > 0 {
		t.Fatalf("writer: %v %v", err, diags)
	}
	f, err := l5x.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Import(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != 0 {
		t.Errorf("import notes: %+v", p.Notes)
	}
	ld := string(p.Files["MainProgram.ld"])
	for _, want := range []string{
		"Delays[0]:TON(PT := T#2S)",
		"Delays[3]:TON(PT := T#500MS)",
		"Delays[Sel]:TON(PT := T#1S)",
		"Starts[1]:CTU(PV := 3)",
	} {
		if !strings.Contains(ld, want) {
			t.Errorf("imported program lacks %q:\n%s", want, ld)
		}
	}
	if strings.Contains(ld, "MOVE") {
		t.Errorf("a preset helper MOVE was imported as a rung:\n%s", ld)
	}
	again, diags, err := writer.Write(ld, writer.Options{Libs: libsOf(p)})
	if err != nil || len(diags) > 0 {
		t.Fatalf("write-back: %v %v", err, diags)
	}
	back, err := l5x.Parse(again)
	if err != nil {
		t.Fatal(err)
	}
	a, b := rungTexts(f, "MainProgram", "MainRoutine"), rungTexts(back, "MainProgram", "MainRoutine")
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("write-back differs:\n first %s\n again %s", strings.Join(a, "\n       "), strings.Join(b, "\n       "))
	}
}
