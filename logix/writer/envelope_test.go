package writer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// A UDT's and its members' descriptions are their declarations' trailing
// comments, written as the <DataType>'s and <Member>'s <Description>
// (#373). A comment on a line of its own, a pragma, and a member with no
// comment carry nothing.
func TestUDTDescriptions(t *testing.T) {
	types := `(* A file header: documents no type. *)
TYPE
  (* A comment above a type is not its description. *)
  LineStatus : STRUCT (* Mode/alarm summary the skid polls *)
    Mode  : DINT; (* 0 idle, 1 run *)
    Alarm : BOOL;
  END_STRUCT;
  Pump : STRUCT (* A pump: the first line
                   and a second one *)
    Run    : BOOL; // a line comment
    Speed  : REAL; (* @layout x=1 *)
    Status : LineStatus;
    Hist   : ARRAY [0..3] OF REAL; (* last four (* nested *) speeds *)
  END_STRUCT;
END_TYPE
`
	doc, diags, err := Write(udtLadder, Options{Libs: []string{types}})
	if err != nil || len(diags) > 0 {
		t.Fatalf("write: %v %v", err, diags)
	}
	f, err := l5x.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, dt := range f.Controller.DataTypes {
		got[dt.Name] = dt.Description
		for _, m := range dt.Members {
			got[dt.Name+"."+m.Name] = m.Description
		}
	}
	want := map[string]string{
		"LineStatus":       "Mode/alarm summary the skid polls",
		"LineStatus.Mode":  "0 idle, 1 run",
		"LineStatus.Alarm": "",
		"Pump":             "A pump: the first line\nand a second one",
		"Pump.Run":         "a line comment",
		"Pump.Speed":       "",
		"Pump.Status":      "",
		"Pump.Hist":        "last four (* nested *) speeds",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: description %q, want %q", k, got[k], w)
		}
	}
	// The element form a Logix export uses for a member with a
	// description; a member without one stays self-closing.
	for _, frag := range []string{
		"<DataType Name=\"LineStatus\" Family=\"NoFamily\" Class=\"User\">\n<Description>\n<![CDATA[Mode/alarm summary the skid polls]]>\n</Description>\n<Members>\n",
		"<Member Name=\"Mode\" DataType=\"DINT\" Dimension=\"0\" Radix=\"Decimal\" Hidden=\"false\" ExternalAccess=\"Read/Write\">\n<Description>\n<![CDATA[0 idle, 1 run]]>\n</Description>\n</Member>\n",
		"<Member Name=\"Alarm\" DataType=\"BOOL\" Dimension=\"0\" Radix=\"Decimal\" Hidden=\"false\" ExternalAccess=\"Read/Write\"/>\n",
	} {
		if !strings.Contains(string(doc), frag) {
			t.Errorf("written L5X lacks\n%s", frag)
		}
	}
}

// Import → write: l5x.Types renders an export's UDT descriptions as the
// trailing comments the writer reads back, so a description survives the
// round trip — the type's and each member's, multi-line and with comment
// delimiters in it.
func TestUDTDescriptionsRoundTrip(t *testing.T) {
	first, diags, err := Write(udtLadder, Options{Libs: []string{udtTypes}})
	if err != nil || len(diags) > 0 {
		t.Fatalf("write: %v %v", err, diags)
	}
	f, err := l5x.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	descs := map[string]string{
		"LineStatus":       "Mode/alarm summary the skid polls alongside the handshake tags",
		"LineStatus.Mode":  "0 idle\n1 run",
		"Pump":             "Transfer pump (see P&ID)",
		"Pump.Hist":        "speeds; a *) in the text",
		"Pump.Status":      "  padded  ",
		"Pump.Speed":       "",
		"LineStatus.Alarm": "",
		"Pump.Run":         "",
	}
	for _, dt := range f.Controller.DataTypes {
		dt.Description = descs[dt.Name]
		for i := range dt.Members {
			dt.Members[i].Description = descs[dt.Name+"."+dt.Members[i].Name]
		}
	}
	src, unresolved, err := l5x.Types(f, l5x.TypesOptions{})
	if err != nil || len(unresolved) > 0 {
		t.Fatalf("types: %v %v", err, unresolved)
	}
	back := mustWrite(t, udtLadder, Options{Libs: []string{src}})
	want := map[string]string{}
	for k, v := range descs {
		want[k] = strings.TrimSpace(v)
	}
	// A comment delimiter in the text is broken up, never written raw.
	want["Pump.Hist"] = "speeds; a * ) in the text"
	for _, dt := range back.Controller.DataTypes {
		if dt.Description != want[dt.Name] {
			t.Errorf("%s: %q, want %q", dt.Name, dt.Description, want[dt.Name])
		}
		for _, m := range dt.Members {
			if k := dt.Name + "." + m.Name; m.Description != want[k] {
				t.Errorf("%s: %q, want %q", k, m.Description, want[k])
			}
		}
	}
}

func TestTrailingComment(t *testing.T) {
	src := "A : INT; (* one *)\n" +
		"(* alone *)\n" +
		"B : INT; // two\n" +
		"C : INT;\n" +
		"D : STRUCT (* three\n   four *)\n" +
		"E : STRING := '(* not a comment *)';\n" +
		"F : INT; (* @pragma *)\n"
	for line, want := range map[int]string{1: "one", 2: "", 3: "two", 4: "", 5: "three\nfour", 6: "", 7: "", 8: "", 99: ""} {
		if got := newCommentSrc(src).trailingComment(line); got != want {
			t.Errorf("line %d: %q, want %q", line, got, want)
		}
	}
}

// The export claims the Logix Designer release of the target's own major
// revision (#373): 38.01 for v38, what ECHO1's exports carry, else the
// major's first release (36.00 for v36, as the yardstick says). A given
// SoftwareRevision wins.
func TestSoftwareRevisionFollowsTheTarget(t *testing.T) {
	for _, c := range []struct{ major, software, want string }{
		{"", "", "38.01"},
		{"38", "", "38.01"},
		{"36", "", "36.00"},
		{"35", "", "35.00"},
		{"36", "36.02", "36.02"},
	} {
		doc, diags, err := Write(udtLadder, Options{Libs: []string{udtTypes}, MajorRev: c.major, SoftwareRevision: c.software})
		if err != nil || len(diags) > 0 {
			t.Fatalf("write: %v %v", err, diags)
		}
		f, err := l5x.Parse(doc)
		if err != nil {
			t.Fatal(err)
		}
		if f.SoftwareRevision != c.want {
			t.Errorf("MajorRev %q SoftwareRevision %q: wrote %q, want %q", c.major, c.software, f.SoftwareRevision, c.want)
		}
	}
	if got := SoftwareRevisionFor("36"); got != "36.00" {
		t.Errorf("SoftwareRevisionFor(36) = %q", got)
	}
}

// A processor in the table gets its own Local module; the default is the
// 1756-L85E's, unchanged since before #373; a processor with no verified
// module gets the 1756-L85E's with its own catalog number, and a warning
// that says so — nothing nautilus has no data for is guessed.
func TestLocalModule(t *testing.T) {
	for _, c := range []struct {
		proc, module string
		ports        []string
		eth          []string
	}{
		{"", `<Module Name="Local" CatalogNumber="1756-L85E" Vendor="1" ProductType="14" ProductCode="168" Major="38" Minor="11" ParentModule="Local" ParentModPortId="1" Inhibited="false" MajorFault="true">`,
			[]string{`<Port Id="1" Address="0" Type="ICP" Upstream="false">`, `<Bus Size="17"/>`, `<Port Id="2" Type="Ethernet" Upstream="false">`},
			[]string{`<EthernetPort Port="1" Label="1" PortEnabled="true"/>`}},
		{"1769-L33ER", `<Module Name="Local" CatalogNumber="1769-L33ER" Vendor="1" ProductType="14" ProductCode="107" Major="38" Minor="11" ParentModule="Local" ParentModPortId="1" Inhibited="false" MajorFault="true">`,
			[]string{`<Port Id="1" Address="0" Type="Compact" Upstream="false">`, `<Bus Size="17"/>`, `<Port Id="2" Type="Ethernet" Upstream="false">`},
			[]string{`<EthernetPort Port="1" Label="1" PortEnabled="true"/>`, `<EthernetPort Port="2" Label="2" PortEnabled="true"/>`}},
		{"5069-L320ER", `<Module Name="Local" CatalogNumber="5069-L320ER" Vendor="1" ProductType="14" ProductCode="217" Major="38" Minor="11" ParentModule="Local" ParentModPortId="1" Inhibited="false" MajorFault="true">`,
			[]string{`<Port Id="1" Address="0" Type="5069" Upstream="false">`, `<Bus Size="17"/>`, `<Port Id="3" Type="Ethernet" Upstream="false">`, `<Port Id="4" Type="Ethernet" Upstream="false">`},
			[]string{`<EthernetPort Port="1" Label="A1" PortEnabled="true"/>`, `<EthernetPort Port="2" Label="A2" PortEnabled="true"/>`}},
		// No verified module: the L85E's, catalog number changed.
		{"1769-L24ER-QB1B", `<Module Name="Local" CatalogNumber="1769-L24ER-QB1B" Vendor="1" ProductType="14" ProductCode="168" Major="38" Minor="11" ParentModule="Local" ParentModPortId="1" Inhibited="false" MajorFault="true">`,
			[]string{`<Port Id="1" Address="0" Type="ICP" Upstream="false">`, `<Bus Size="17"/>`, `<Port Id="2" Type="Ethernet" Upstream="false">`},
			[]string{`<EthernetPort Port="1" Label="1" PortEnabled="true"/>`}},
	} {
		doc, diags, err := Write(udtLadder, Options{Libs: []string{udtTypes}, ProcessorType: c.proc})
		if err != nil || len(diags) > 0 {
			t.Fatalf("write: %v %v", err, diags)
		}
		for _, want := range append(append([]string{c.module}, c.ports...), c.eth...) {
			if !strings.Contains(string(doc), want+"\n") {
				t.Errorf("%q: no %s", c.proc, want)
			}
		}
		if n := strings.Count(string(doc), `<EthernetPort `); n != len(c.eth) {
			t.Errorf("%q: %d EthernetPorts, want %d", c.proc, n, len(c.eth))
		}
	}
	for _, proc := range []string{"", "1756-L85E", " 1756-l85e ", "1769-L33ER", "5069-L320ER", "1756-L83E"} {
		if w := LocalModuleWarning(proc); w != "" {
			t.Errorf("%q: unexpected warning %s", proc, w)
		}
	}
	// A 5370 L1/L2 (embedded I/O), a safety controller, a 5480 (v38
	// cannot create one), a 1756-L7x.
	for _, proc := range []string{"1769-L24ER-QB1B", "1769-L16ER-BB1B", "1756-L81ES", "5069-L46ERMW", "1756-L75"} {
		w := LocalModuleWarning(proc)
		if !strings.Contains(w, proc) || !strings.Contains(w, "1756-L85E") || !strings.Contains(w, "#373") {
			t.Errorf("%s: warning %q", proc, w)
		}
	}
}

// xnode is any XML element, kept whole: its attributes in order and its
// children.
type xnode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []xnode    `xml:",any"`
}

func (n xnode) child(name string) *xnode {
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == name {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n xnode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// render writes an element and its descendants with their attributes
// in order, without the ones in skip.
func (n xnode) render(skip map[string]bool) string {
	var b strings.Builder
	b.WriteString("<" + n.XMLName.Local)
	for _, a := range n.Attrs {
		if !skip[a.Name.Local] {
			fmt.Fprintf(&b, " %s=%q", a.Name.Local, a.Value)
		}
	}
	b.WriteString(">")
	for _, c := range n.Nodes {
		b.WriteString(c.render(skip))
	}
	b.WriteString("</" + n.XMLName.Local + ">")
	return b.String()
}

func parseNode(t *testing.T, doc []byte) xnode {
	t.Helper()
	var n xnode
	if err := xml.Unmarshal(bytes.TrimPrefix(doc, []byte("\uFEFF")), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Every row of the verified table is what Logix Designer v38 itself
// writes for that processor: testdata/local holds, verbatim, the export
// of a project the SDK created for each one (#373). A project written for
// the processor has the export's Local module element for element and
// attribute for attribute, its <EthernetPorts>, its <RedundancyInfo>, and
// its <Controller> attributes in the export's order (all but the name,
// the dates and the DataExchangeId, which are the project's own). And
// the table has a row for exactly the processors there are exports of.
func TestLocalModulesMatchExports(t *testing.T) {
	files, err := filepath.Glob("testdata/local/*.L5X")
	if err != nil || len(files) == 0 {
		t.Fatalf("no exports: %v", err)
	}
	seen := map[string]bool{}
	ctrlSkip := map[string]bool{"Name": true, "ProjectCreationDate": true, "LastModifiedDate": true, "DataExchangeId": true}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		want := parseNode(t, raw).child("Controller")
		cat := want.attr("ProcessorType")
		if cat+".L5X" != filepath.Base(f) {
			t.Errorf("%s: export is a %s", f, cat)
		}
		seen[cat] = true
		if w := LocalModuleWarning(cat); w != "" {
			t.Errorf("%s: an export but no row: %s", cat, w)
			continue
		}
		doc, diags, err := Write(udtLadder, Options{Libs: []string{udtTypes}, ProcessorType: cat, MajorRev: want.attr("MajorRev"), MinorRev: want.attr("MinorRev")})
		if err != nil || len(diags) > 0 {
			t.Fatalf("%s: write: %v %v", cat, err, diags)
		}
		got := parseNode(t, doc).child("Controller")
		for _, el := range []string{"Modules", "EthernetPorts", "RedundancyInfo"} {
			if g, w := got.child(el).render(nil), want.child(el).render(nil); g != w {
				t.Errorf("%s: %s\n got %s\nwant %s", cat, el, g, w)
			}
		}
		if g, w := (xnode{XMLName: got.XMLName, Attrs: got.Attrs}).render(ctrlSkip), (xnode{XMLName: want.XMLName, Attrs: want.Attrs}).render(ctrlSkip); g != w {
			t.Errorf("%s: Controller\n got %s\nwant %s", cat, g, w)
		}
	}
	for cat := range localModules {
		if !seen[cat] {
			t.Errorf("%s: a row but no export in testdata/local", cat)
		}
	}
}
