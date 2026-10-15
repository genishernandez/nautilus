package writer

import (
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

// The Local module is the 1756-L85E's, the only one taken from a real
// export; any other processor gets the same module with its own catalog
// number, and a warning that says so (#373) — nothing nautilus has no
// data for is guessed.
func TestLocalModule(t *testing.T) {
	const l85 = `<Module Name="Local" CatalogNumber="%s" Vendor="1" ProductType="14" ProductCode="168" Major="38" Minor="11" ParentModule="Local" ParentModPortId="1" Inhibited="false" MajorFault="true">`
	for _, proc := range []string{"", "1756-L85E", "1769-L33ER"} {
		doc, diags, err := Write(udtLadder, Options{Libs: []string{udtTypes}, ProcessorType: proc})
		if err != nil || len(diags) > 0 {
			t.Fatalf("write: %v %v", err, diags)
		}
		cat := proc
		if cat == "" {
			cat = "1756-L85E"
		}
		if !strings.Contains(string(doc), strings.Replace(l85, "%s", cat, 1)) || !strings.Contains(string(doc), `<Bus Size="17"/>`) {
			t.Errorf("%s: Local module not the 1756-L85E's", cat)
		}
	}
	for _, proc := range []string{"", "1756-L85E", " 1756-l85e "} {
		if w := LocalModuleWarning(proc); w != "" {
			t.Errorf("%q: unexpected warning %s", proc, w)
		}
	}
	for _, proc := range []string{"1769-L33ER", "5069-L320ER", "1756-L83E"} {
		w := LocalModuleWarning(proc)
		if !strings.Contains(w, proc) || !strings.Contains(w, "1756-L85E") || !strings.Contains(w, "#373") {
			t.Errorf("%s: warning %q", proc, w)
		}
	}
}
