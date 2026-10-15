package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// iolineExport is a Logix Designer export with I/O modules: Local:1
// DI_Slot1 (1756-IB16), Local:2 DO_Slot2 (1756-OB16E), and the adapter
// Remote1 (1734-AENT) with Remote1_DI (1734-IB8) in its slot 1
// (lang/l5x/testdata/README.md).
func iolineExport(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "lang", "l5x", "testdata", "ioline.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func iolineHardware(t *testing.T) *Hardware {
	t.Helper()
	h, err := ParseHardware("hardware.L5X", iolineExport(t))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

const iolineSrc = `PROGRAM MainProgram
VAR_EXTERNAL
    StartPB : BOOL;
    StopPB  : BOOL;
    FlowSw  : BOOL;
    RunCmd  : BOOL;
    FlowAlm : BOOL;
END_VAR
LD
  RUNG seal
    [ StartPB | RunCmd ] /StopPB ( RunCmd )
  RUNG flow
    RunCmd /FlowSw ( FlowAlm )
END_LD
END_PROGRAM
`

func iolineOpts(t *testing.T) Options {
	return Options{Controller: "IOLine", Hardware: iolineHardware(t), Aliases: map[string]string{
		"StartPB": "Local:1:I.Data.0", "StopPB": "Local:1:I.Data.1",
		"FlowSw": "Remote1:1:I.Data.0", "RunCmd": "Local:2:O.Data.0",
	}}
}

// LGA-052: the hardware configuration's <Modules> goes into the project
// as the export has it — every module, its configuration and connections
// — in place of the generated Local module, and the aliases name points
// on its modules.
func TestHardwareModulesWrittenVerbatim(t *testing.T) {
	export := iolineExport(t)
	src, err := l5x.Parse(export)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"ld", "st"} {
		t.Run(lang, func(t *testing.T) {
			var doc []byte
			var diags []Diag
			if lang == "ld" {
				doc, diags, err = Write(iolineSrc, iolineOpts(t))
			} else {
				doc, diags, err = WriteST("PROGRAM MainProgram\nVAR_EXTERNAL\n    StartPB : BOOL; StopPB : BOOL; RunCmd : BOOL;\nEND_VAR\nRunCmd := (StartPB OR RunCmd) AND NOT StopPB;\nEND_PROGRAM\n", iolineOpts(t))
			}
			if err != nil || len(diags) > 0 {
				t.Fatalf("%v %v", err, diags)
			}
			got, err := l5x.Parse(doc)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.ReplaceAll(src.Controller.ModulesXML, "\r\n", "\n")
			if got.Controller.ModulesXML != want {
				t.Errorf("<Modules> is not the export's:\n%s", got.Controller.ModulesXML)
			}
			if strings.Count(string(doc), "<Module ") != 5 {
				t.Errorf("%d modules written, want the export's 5", strings.Count(string(doc), "<Module "))
			}
			alias := map[string]string{}
			for _, tg := range got.Controller.Tags {
				alias[tg.Name] = tg.AliasFor
			}
			if alias["StartPB"] != "Local:1:I.Data.0" || alias["RunCmd"] != "Local:2:O.Data.0" {
				t.Errorf("aliases = %v", alias)
			}
		})
	}
	// Without a hardware configuration nothing changes: the Local module
	// from the processor.
	doc, _, err := Write(fixture(t, "demoline.ld"), demoOpts())
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(doc), "<Module "); n != 1 {
		t.Errorf("no hardware: %d modules, want Local only", n)
	}
}

// LGA-052: an alias of a rack point names a module the hardware
// configuration has, or the write stops with logix/io-module naming the
// module; an alias of a tag is not a rack point.
func TestIOModuleRule(t *testing.T) {
	cases := []struct {
		name, alias string
		noHardware  bool
		want        string // "" = writes
	}{
		{"local slot", "Local:1:I.Data.3", false, ""},
		{"local output", "Local:2:O.Data.0", false, ""},
		{"remote slot", "Remote1:1:I.Data.0", false, ""},
		{"module by name", "Remote1:I.SlotStatusBits0_31.1", false, ""},
		{"alias of a tag", "Other.5", false, ""},
		{"empty local slot", "Local:4:I.Data.0", false, "StartPB is an alias of Local:4:I.Data.0, and hardware.L5X has no module in slot 4 of the controller's chassis (Local); it has Local:1 DI_Slot1 (1756-IB16), Local:2 DO_Slot2 (1756-OB16E), Remote1 (1734-AENT/A), Remote1:1 Remote1_DI (1734-IB8/C). Add the module in Logix Designer"},
		{"empty remote slot", "Remote1:2:I.Data.0", false, "no module in slot 2 of Remote1"},
		{"no such adapter", "Rack9:1:I.Data.0", false, "hardware.L5X has no adapter named Rack9"},
		{"no such module", "Drive1:I.Speed", false, "hardware.L5X has no module named Drive1"},
		{"no hardware", "Local:1:I.Data.3", true, "StartPB is an alias of Local:1:I.Data.3, a point of the module in slot 1 of the controller's chassis (Local), and the project has no hardware configuration to carry it: set target.logix.hardware"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := Options{Aliases: map[string]string{"StartPB": c.alias}, Hardware: iolineHardware(t)}
			if c.noHardware {
				o.Hardware = nil
			}
			src := "PROGRAM P\nVAR_EXTERNAL\n    StartPB : BOOL; Run : BOOL;\nEND_VAR\nLD\n  RUNG r\n    StartPB ( Run )\nEND_LD\nEND_PROGRAM\n"
			_, diags, err := Write(src, o)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if len(diags) > 0 {
					t.Errorf("refused: %v", diags)
				}
				return
			}
			if len(diags) != 1 || diags[0].Rule != ruleIOModule || !strings.Contains(diags[0].Message, c.want) || diags[0].Line != 3 {
				t.Errorf("diags = %v, want one %s at line 3 containing %q", diags, ruleIOModule, c.want)
			}
		})
	}
}

// LGA-052: a hardware configuration made for another controller, or
// another revision, is refused by logix/hardware; and a file with no
// <Modules> or no Local module is not a hardware configuration.
func TestHardwareMustMatchTheTarget(t *testing.T) {
	for _, c := range []struct {
		name string
		o    func(*Options)
		want string
	}{
		{"processor", func(o *Options) { o.ProcessorType = "1756-L83E" }, "hardware.L5X: its Local module is a 1756-L85E, and the target's processor is 1756-L83E"},
		{"revision", func(o *Options) { o.MajorRev = "36" }, `hardware.L5X: its Local module is at revision 38.11, and the target's revision is 36.11; set target.logix.revision to "38.11"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := iolineOpts(t)
			c.o(&o)
			_, diags, err := Write(iolineSrc, o)
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 1 || diags[0].Rule != ruleHardware || !strings.Contains(diags[0].Message, c.want) {
				t.Errorf("diags = %v, want %q", diags, c.want)
			}
		})
	}

	for _, c := range []struct{ name, doc, want string }{
		{"not an L5X", "<x/>", "not an L5X"},
		{"no Modules", `<RSLogix5000Content><Controller Name="C"></Controller></RSLogix5000Content>`, "hw.L5X: no <Modules> section"},
		{"no Local", `<RSLogix5000Content><Controller Name="C"><Modules><Module Name="X" CatalogNumber="1756-IB16"/></Modules></Controller></RSLogix5000Content>`, "hw.L5X: its <Modules> has no Local module"},
	} {
		if _, err := ParseHardware("hw.L5X", []byte(c.doc)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestRackPoint(t *testing.T) {
	for in, want := range map[string][3]string{
		"Local:1:I.Data.3":     {"Local", "1", "y"},
		"Local:12:C.Ch0Config": {"Local", "12", "y"},
		"Remote1:2:O.Data[1]":  {"Remote1", "2", "y"},
		"Drive1:I.OutputFreq":  {"Drive1", "", "y"},
		"Program:Main.Tag":     {"", "", ""},
		"Tank.Level":           {"", "", ""},
		"Arr[3]":               {"", "", ""},
		"Local:x:I.Data":       {"", "", ""},
	} {
		p, s, ok := RackPoint(in)
		if got := [3]string{p, s, map[bool]string{true: "y"}[ok]}; got != want {
			t.Errorf("RackPoint(%q) = %v, want %v", in, got, want)
		}
	}
}
