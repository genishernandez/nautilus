package l5x

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// The I/O configuration of an export (#275): each module's identity and
// place, and the <Modules> element exactly as the export has it.
func TestParseModules(t *testing.T) {
	f := load(t, "ioline.L5X")
	type row struct{ name, cat, parent, port, slot string }
	want := []row{
		{"Local", "1756-L85E", "Local", "1", ""},
		{"DI_Slot1", "1756-IB16", "Local", "1", "1"},
		{"DO_Slot2", "1756-OB16E", "Local", "1", "2"},
		{"Remote1", "1734-AENT/A", "Local", "2", "192.168.1.20"},
		{"Remote1_DI", "1734-IB8/C", "Remote1", "1", "1"},
	}
	ms := f.Controller.Modules
	if len(ms) != len(want) {
		t.Fatalf("%d modules, want %d", len(ms), len(want))
	}
	for i, w := range want {
		m := ms[i]
		got := row{m.Name, m.CatalogNumber, m.ParentModule, m.ParentModPortID, m.Slot()}
		if got != w {
			t.Errorf("module %d = %+v, want %+v", i, got, w)
		}
		if m.Line == 0 {
			t.Errorf("%s: no line", m.Name)
		}
	}
	if m := ms[1]; m.Vendor != "1" || m.ProductType != "7" || m.ProductCode != "11" || m.Major != "3" || m.Minor != "1" {
		t.Errorf("DI_Slot1 identity = %s/%s/%s %s.%s", m.Vendor, m.ProductType, m.ProductCode, m.Major, m.Minor)
	}

	raw, err := os.ReadFile("testdata/ioline.L5X")
	if err != nil {
		t.Fatal(err)
	}
	x := f.Controller.ModulesXML
	if !strings.HasPrefix(x, "<Modules>") || !strings.HasSuffix(x, "</Modules>") {
		t.Fatalf("ModulesXML is not the element: %.40q … %.40q", x, x[max(0, len(x)-40):])
	}
	// Verbatim: the export's own bytes, CRLF and all.
	if !bytes.Contains(raw, []byte(x)) || !strings.Contains(x, "\r\n") {
		t.Error("ModulesXML is not a verbatim slice of the export")
	}
	if !strings.Contains(x, `<Structure DataType="AB:1756_DI:C:1">`) {
		t.Error("ModulesXML lost the module configuration")
	}

	// An export with only the controller's own module, and one with none.
	if d := load(t, "demoline.L5X").Controller; len(d.Modules) != 1 || d.Modules[0].Name != "Local" {
		t.Errorf("demoline modules = %+v", d.Modules)
	}
	p, err := Parse([]byte(`<RSLogix5000Content><Controller Name="X"><Modules/></Controller></RSLogix5000Content>`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Controller.ModulesXML != "<Modules/>" || len(p.Controller.Modules) != 0 {
		t.Errorf("empty Modules: %q, %d", p.Controller.ModulesXML, len(p.Controller.Modules))
	}
}
