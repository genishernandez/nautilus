package importer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/logix/writer"
)

var aliasLine = regexp.MustCompile(`name: (\w+),.*alias: "([^"]+)"`)

// importedAliases reads the alias: bindings out of the imported tag file.
func importedAliases(p *Project) map[string]string {
	out := map[string]string{}
	for _, m := range aliasLine.FindAllStringSubmatch(string(p.Files["tags/logix.yaml"]), -1) {
		out[m[1]] = m[2]
	}
	return out
}

// An export whose rungs name alias tags of every kind (#330): an alias of
// a base tag, of a UDT member, of a UDT BOOL (a BIT member), of an Add-On
// Instruction instance's parameter, of a bit of a DINT, of an array
// element, and of a rack point. Logix exports an alias tag with no
// DataType; the import types each through its AliasFor target, carries
// every rung, and keeps every binding as alias:. Written back, the rungs
// are the export's and each alias tag is an alias tag of the same target.
func TestAliasTagsInRungsImportAndRoundTrip(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("testdata", "aliases.L5X"))
	if err != nil {
		t.Fatal(err)
	}
	orig, err := l5x.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Import(orig, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range p.Notes {
		if n.Rung >= 0 {
			t.Errorf("rung not carried: %s", n)
		}
	}
	// The program's four rungs and the Add-On Instruction's one.
	if p.Rungs != 5 || p.Imported != 5 {
		t.Errorf("imported %d of %d rungs, want 5 of 5", p.Imported, p.Rungs)
	}
	src := string(p.Files["Main.ld"])
	flat := strings.Join(strings.Fields(src), " ") // declarations are column-aligned
	for _, want := range []string{
		"StartCmd : BOOL;",     // Pump1Run, a base BOOL
		"Pump1Speed : REAL;",   // Pump1.Speed, a UDT member
		"Pump1Running : BOOL;", // Pump1.Running, a UDT BIT member
		"Valve1Open : BOOL;",   // Valve1Ctl.Open, an AOI output parameter
		"TankLevelHi : BOOL;",  // TankStatus.5, a bit of a DINT
		"TankLevel : REAL;",    // TankLevels[2], an array element
		"StartPB : BOOL;",      // Local:1:I.Data.3, a rack point's bit
		"StartPB /TankLevelHi ( StartCmd )",
		"StartCmd ( Run )",
		"GT(Pump1Speed, 50.0) ( Alarm )",
		"Pump1Running Valve1Open GT(TankLevel, 2.5) ( Ready )",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	want := map[string]string{
		"StartCmd": "Pump1Run", "Pump1Speed": "Pump1.Speed", "Pump1Running": "Pump1.Running",
		"Valve1Open": "Valve1Ctl.Open", "TankLevelHi": "TankStatus.5", "TankLevel": "TankLevels[2]",
		"StartPB": "Local:1:I.Data.3",
	}
	aliases := importedAliases(p)
	for name, target := range want {
		if aliases[name] != target {
			t.Errorf("tag file: %s alias %q, want %q\n%s", name, aliases[name], target, p.Files["tags/logix.yaml"])
		}
	}
	if !strings.Contains(string(p.Files["tags/logix.yaml"]), "name: StartCmd, role: output") {
		t.Errorf("StartCmd is written by the logic; its tag should be an output:\n%s", p.Files["tags/logix.yaml"])
	}

	// Written back with the tag file's bindings, as `naut logix deploy`
	// passes them.
	out, diags, err := writer.Write(src, writer.Options{Controller: "AliasLine", Program: "Main", Libs: libsOf(p), Aliases: aliases})
	if err != nil || len(diags) > 0 {
		t.Fatalf("writer: %v %v\n%s", err, diags, src)
	}
	back, err := l5x.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	a, b := rungTexts(orig, "Main", "MainRoutine"), rungTexts(back, "Main", "MainRoutine")
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("written back:\n  %s\nwant:\n  %s", strings.Join(b, "\n  "), strings.Join(a, "\n  "))
	}
	got := map[string]*l5x.Tag{}
	for _, tg := range back.Controller.Tags {
		got[tg.Name] = tg
	}
	for name, target := range want {
		tg := got[name]
		if tg == nil || tg.TagType != "Alias" || tg.AliasFor != target {
			t.Errorf("written back: %s = %+v, want an alias tag of %s", name, tg, target)
		}
	}
}

// An alias the export cannot type is refused by name, never generically:
// the rung stays a comment and the note says which alias and why.
func TestAliasTagsTheExportCannotTypeAreRefusedByName(t *testing.T) {
	base := miniL5X([]string{"Run:BOOL", "Delay1:TIMER=1000", "Level:REAL"}, "XIC(Run)OTE(Run);")
	cases := []struct {
		tag, rung, why string
	}{
		{`<Tag Name="TankLevel" TagType="Alias" Radix="Float" AliasFor="Local:2:I.Ch0Data" ExternalAccess="Read/Write"/>`,
			"GRT(TankLevel,2.5)OTE(Run);",
			"TankLevel is an alias of the module I/O point Local:2:I.Ch0Data, and the export has no module definition to type it"},
		{`<Tag Name="Pump1Run" TagType="Alias" Radix="Decimal" AliasFor="Pump9Run" ExternalAccess="Read/Write"/>`,
			"XIC(Pump1Run)OTE(Run);",
			"Pump1Run is an alias of Pump9Run, which is not in the export"},
		{`<Tag Name="Pump1Delay" TagType="Alias" Radix="NullType" AliasFor="Delay1" ExternalAccess="Read/Write"/>`,
			"XIC(Pump1Delay.DN)OTE(Run);",
			"Pump1Delay is an alias of the TIMER Delay1; nautilus has no alias of a block instance"},
		{`<Tag Name="LevelLo" TagType="Alias" Radix="Decimal" AliasFor="Level.Lo" ExternalAccess="Read/Write"/>`,
			"XIC(LevelLo)OTE(Run);",
			"LevelLo is an alias of Level.Lo: a REAL has no member Lo"},
	}
	for _, c := range cases {
		doc := strings.Replace(string(base), "</Tags>", c.tag+"\n</Tags>", 1)
		doc = strings.Replace(doc, "XIC(Run)OTE(Run);", c.rung, 1)
		p := mustImport(t, []byte(doc))
		var got []string
		for _, n := range p.Notes {
			if n.Rung >= 0 {
				got = append(got, n.Reason+": "+n.Why)
			}
		}
		if len(got) != 1 || got[0] != "alias: "+c.why {
			t.Errorf("%s: notes %q, want one \"alias: %s\"", c.rung, got, c.why)
		}
	}

	// A program-scoped alias tag: the binding has nowhere to live in
	// nautilus (alias: is a manifest tag's, and a manifest tag is a
	// controller tag), so it is refused rather than imported as a plain
	// local that aliases nothing.
	doc := strings.Replace(string(base), "<Tags/>", `<Tags><Tag Name="LocalRun" TagType="Alias" Radix="Decimal" AliasFor="Run" ExternalAccess="Read/Write"/></Tags>`, 1)
	doc = strings.Replace(doc, "XIC(Run)OTE(Run);", "XIC(LocalRun)OTE(Run);", 1)
	p := mustImport(t, []byte(doc))
	var got []string
	for _, n := range p.Notes {
		if n.Rung >= 0 {
			got = append(got, n.Reason+": "+n.Why)
		}
	}
	want := "alias: LocalRun is a program-scoped alias tag (of Run); nautilus binds an alias on a controller tag"
	if len(got) != 1 || got[0] != want {
		t.Errorf("program-scoped alias: notes %q, want %q", got, want)
	}
}
