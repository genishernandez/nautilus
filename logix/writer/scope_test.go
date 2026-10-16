package writer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// Options.AllTags (target.logix.all-tags, #370): a manifest tag no
// program names is still a controller tag — typed, seeded and described
// from the manifest exactly as a named one — while an untyped one (no
// type:, no init:, so not in Tags) and the side code's heartbeat are not
// the writer's to declare. Off, an unnamed tag is not written (LGA-001).
func TestAllTagsWritesTagsNoProgramNames(t *testing.T) {
	ladder := "PROGRAM P\nVAR_EXTERNAL\n  Run : BOOL;\nEND_VAR\nLD\n  RUNG r Run ( Run )\nEND_LD\nEND_PROGRAM\n"
	st := "PROGRAM P\nVAR_EXTERNAL\n  Run : BOOL;\nEND_VAR\nRun := NOT Run;\nEND_PROGRAM\n"
	opts := Options{
		Tags:  map[string]string{"Run": "BOOL", "BatchId": "DINT", "Level": "REAL", "Hist": "ARRAY [0..9] OF INT", "Nautilus_Scan": "DINT"},
		Inits: map[string]any{"Run": false, "BatchId": int64(42), "Level": 20.5},
		Descs: map[string]string{"BatchId": "stamped by the skid"},
		Side:  Side{Heartbeat: "Nautilus_Scan"},
	}
	for _, c := range []struct {
		name  string
		write func(string, Options) ([]byte, []Diag, error)
		src   string
	}{{"ladder", Write, ladder}, {"st", WriteST, st}} {
		t.Run(c.name, func(t *testing.T) {
			tags := func(o Options) map[string]*l5x.Tag {
				t.Helper()
				doc, diags, err := c.write(c.src, o)
				if err != nil || len(diags) > 0 {
					t.Fatalf("write: %v %v", err, diags)
				}
				f, err := l5x.Parse(doc)
				if err != nil {
					t.Fatal(err)
				}
				out := map[string]*l5x.Tag{}
				for _, tg := range f.Controller.Tags {
					out[tg.Name] = tg
				}
				if n := len(f.Controller.Programs[0].Tags); n != 0 {
					t.Errorf("%d program tags, want none", n)
				}
				return out
			}
			off := tags(opts)
			for _, n := range []string{"BatchId", "Level", "Hist"} {
				if off[n] != nil {
					t.Errorf("all-tags off: %s, which no program names, was written", n)
				}
			}
			on := opts
			on.AllTags = true
			got := tags(on)
			if b := got["BatchId"]; b == nil || b.DataType != "DINT" || b.Value != int64(42) || b.Description != "stamped by the skid" {
				t.Errorf("BatchId = %+v", b)
			}
			if l := got["Level"]; l == nil || l.DataType != "REAL" || l.Value != 20.5 {
				t.Errorf("Level = %+v", l)
			}
			if h := got["Hist"]; h == nil || h.DataType != "INT" || h.Dimensions != "10" {
				t.Errorf("Hist = %+v", h)
			}
			if hb := got["Nautilus_Scan"]; hb == nil || !strings.HasPrefix(hb.Description, "nautilus side code") {
				t.Errorf("heartbeat = %+v", hb)
			}
			if len(got) != 5 {
				t.Errorf("controller tags %v, want Run, BatchId, Level, Hist and the heartbeat", keys(got))
			}
		})
	}
}

// An unnamed manifest tag the Logix subset cannot carry is not written,
// and refuses nothing: the program does not use it (an import reports
// each such tag). Named, the same tag is still refused by name.
func TestAllTagsSkipsWhatLogixCannotCarry(t *testing.T) {
	src := "PROGRAM P\nVAR_EXTERNAL\n  Run : BOOL;\nEND_VAR\nLD\n  RUNG r Run ( Run )\nEND_LD\nEND_PROGRAM\n"
	opts := Options{AllTags: true, Tags: map[string]string{"Run": "BOOL", "Label": "STRING", "Big": "LINT", "N": "DINT"}}
	f := mustWrite(t, src, opts)
	var got []string
	for _, tg := range f.Controller.Tags {
		got = append(got, tg.Name)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "N Run" {
		t.Errorf("controller tags %v, want N and Run", got)
	}
	named := strings.Replace(src, "Run : BOOL;", "Run : BOOL;\n  Label : STRING;", 1)
	if _, diags, _ := Write(named, opts); len(diags) != 1 || diags[0].Rule != ruleType || !strings.Contains(diags[0].Message, "Label") {
		t.Errorf("named STRING: diags %v, want one %s naming Label", diags, ruleType)
	}
}

// Options.ControllerScoped (target.logix.controller-scoped, #371): a
// program VAR named there is written as a controller tag, with the
// description Descs gives it, and the rungs that use it are unchanged.
func TestControllerScopedVarBecomesAControllerTag(t *testing.T) {
	src := `PROGRAM P
VAR_EXTERNAL
    Go : BOOL;
    Done : BOOL;
END_VAR
VAR
    Settle : TON;
    Local : DINT;
END_VAR
LD
  RUNG r0 Go Settle:TON(PT := T#2S)
  RUNG r1 Settle.Q ( Done )
END_LD
END_PROGRAM
`
	plain := mustWrite(t, src, Options{})
	f := mustWrite(t, src, Options{ControllerScoped: []string{"settle"}, Descs: map[string]string{"settle": "handshake settling delay"}})
	ctrl, prog := map[string]*l5x.Tag{}, map[string]*l5x.Tag{}
	for _, tg := range f.Controller.Tags {
		ctrl[tg.Name] = tg
	}
	for _, tg := range f.Controller.Programs[0].Tags {
		prog[tg.Name] = tg
	}
	if s := ctrl["Settle"]; s == nil || s.DataType != "TIMER" || s.Description != "handshake settling delay" {
		t.Errorf("controller tag Settle = %+v (controller %v, program %v)", s, keys(ctrl), keys(prog))
	}
	if prog["Settle"] != nil || prog["Local"] == nil || ctrl["Local"] != nil {
		t.Errorf("program tags %v, want Local only", keys(prog))
	}
	a, b := plain.Controller.Programs[0].Routines[0].Rungs, f.Controller.Programs[0].Routines[0].Rungs
	for i := range a {
		if a[i].Text != b[i].Text {
			t.Errorf("rung %d: %s, program-scoped %s", i, b[i].Text, a[i].Text)
		}
	}
}

// A controller-scoped entry that names no program VAR is a stale manifest
// entry: refused by name, never ignored.
func TestControllerScopedRefusesWhatIsNotAProgramVar(t *testing.T) {
	src := "PROGRAM P\nVAR_EXTERNAL\n  Run : BOOL;\nEND_VAR\nVAR\n  T1 : TON;\nEND_VAR\nLD\n  RUNG r Run T1:TON(PT := T#1S)\nEND_LD\nEND_PROGRAM\n"
	_, diags, err := Write(src, Options{ControllerScoped: []string{"T1", "Run", "Gone"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range diags {
		got = append(got, d.Message)
	}
	all := strings.Join(got, "\n")
	if len(diags) != 2 || !strings.Contains(all, "names Run, which is VAR_EXTERNAL") || !strings.Contains(all, "names Gone, which program P does not declare") {
		t.Errorf("diags:\n%s", all)
	}
}

func keys(m map[string]*l5x.Tag) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// An Add-On Instruction instance listed controller-scoped is a controller
// tag of the AOI's type, as an export that had it there carries it.
func TestControllerScopedAOIInstance(t *testing.T) {
	dir := filepath.Join("testdata", "conformance", "aoi")
	src, err := os.ReadFile(filepath.Join(dir, "Main.ld"))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := os.ReadFile(filepath.Join(dir, "lib", "motor.ld"))
	if err != nil {
		t.Fatal(err)
	}
	f := mustWrite(t, string(src), Options{Libs: []string{string(lib)}, ControllerScoped: []string{"m101"}, Descs: map[string]string{"m101": "P-101 starter"}})
	var ctrl, prog *l5x.Tag
	for _, tg := range f.Controller.Tags {
		if tg.Name == "m101" {
			ctrl = tg
		}
	}
	for _, tg := range f.Controller.Programs[0].Tags {
		if tg.Name == "m101" {
			prog = tg
		}
	}
	if ctrl == nil || ctrl.DataType != "MotorStarter" || ctrl.Description != "P-101 starter" || prog != nil {
		t.Errorf("m101: controller %+v, program %+v", ctrl, prog)
	}
}
