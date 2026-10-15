package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/logix/writer"
)

func iolineExport(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "lang", "l5x", "testdata", "ioline.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// iolineHardware has a module in slots 1 and 2 of the controller's
// chassis and the adapter Remote1 (lang/l5x/testdata/README.md).
func iolineHardware(t *testing.T) *writer.Hardware {
	t.Helper()
	h, err := writer.ParseHardware("hardware.L5X", iolineExport(t))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// LGA-053: an export with I/O modules imports with its hardware
// configuration — hardware.L5X, the export's <Modules> unchanged, named
// by target.logix.hardware — and writes back with the same modules, the
// alias tags on them and the same rungs. An export whose only module is
// the controller's own gets no hardware file.
func TestImportCarriesHardware(t *testing.T) {
	raw := iolineExport(t)
	f, err := l5x.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Import(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	hw := p.Files[HardwareFile]
	if hw == nil {
		t.Fatalf("no %s", HardwareFile)
	}
	if !strings.Contains(string(p.Files["nautilus.yaml"]), "\n    hardware: hardware.L5X\n") {
		t.Errorf("manifest does not name the hardware:\n%s", p.Files["nautilus.yaml"])
	}
	h, err := writer.ParseHardware(HardwareFile, hw)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(f.Controller.ModulesXML, "\r\n", "\n")
	if h.XML != want {
		t.Errorf("hardware.L5X <Modules> differs from the export's")
	}
	hf, _ := l5x.Parse(hw)
	if c := hf.Controller; c.Name != "IOLine" || c.ProcessorType != "1756-L85E" || c.MajorRev != "38" || c.MinorRev != "11" {
		t.Errorf("hardware.L5X controller = %+v", c)
	}

	// Written back as `naut logix write` does for the project.
	aliases := importedAliases(p)
	for name, target := range map[string]string{"StartPB": "Local:1:I.Data.0", "StopPB": "Local:1:I.Data.1", "FlowSw": "Remote1:1:I.Data.0", "RunCmd": "Local:2:O.Data.0"} {
		if aliases[name] != target {
			t.Errorf("tag file: %s alias %q, want %q", name, aliases[name], target)
		}
	}
	src := string(p.Files["MainProgram.ld"])
	out, diags, err := writer.Write(src, writer.Options{Controller: "IOLine", Aliases: aliases, Hardware: h})
	if err != nil || len(diags) > 0 {
		t.Fatalf("writer: %v %v\n%s", err, diags, src)
	}
	back, err := l5x.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Controller.ModulesXML != want {
		t.Errorf("written back, <Modules> differs from the export's")
	}
	a, b := rungTexts(f, "MainProgram", "MainRoutine"), rungTexts(back, "MainProgram", "MainRoutine")
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("written back:\n  %s\nwant:\n  %s", strings.Join(b, "\n  "), strings.Join(a, "\n  "))
	}

	// DemoLine has only its Local module: nothing to carry.
	demo, err := l5x.ParseFile(filepath.Join("..", "..", "lang", "l5x", "testdata", "demoline.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	dp, err := Import(demo, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if dp.Files[HardwareFile] != nil || strings.Contains(string(dp.Files["nautilus.yaml"]), "hardware:") {
		t.Error("an export with only the Local module got a hardware configuration")
	}
}
