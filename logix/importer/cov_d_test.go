package importer

import (
	"strings"
	"testing"
)

// docs/functions.md#dialects: "the L5X importer turns an export's alias
// tags ... into tags with alias:". An export whose controller declares an
// alias tag (TagType="Alias" AliasFor=...) imports as a tag carrying that
// binding, and the program keeps naming the tag, not the point.
func TestAliasTagImportsWithAlias(t *testing.T) {
	doc := string(miniL5X([]string{"StartPB:BOOL", "Run:BOOL"},
		"XIC(StartPB)OTE(Run);",
	))
	base := `<Tag Name="StartPB" TagType="Base" DataType="BOOL" Constant="false" ExternalAccess="Read/Write">`
	alias := `<Tag Name="StartPB" TagType="Alias" Radix="Decimal" AliasFor="Local:1:I.Data.3" ExternalAccess="Read/Write">`
	if !strings.Contains(doc, base) {
		t.Fatalf("fixture changed: no %s", base)
	}
	doc = strings.Replace(doc, base, alias, 1)

	p := mustImport(t, []byte(doc))
	tf := string(p.Files["tags/logix.yaml"])
	var startLine, runLine string
	for _, ln := range strings.Split(tf, "\n") {
		switch {
		case strings.Contains(ln, "name: StartPB,"):
			startLine = ln
		case strings.Contains(ln, "name: Run,"):
			runLine = ln
		}
	}
	if !strings.Contains(startLine, `alias: "Local:1:I.Data.3"`) {
		t.Errorf("StartPB should import with alias: \"Local:1:I.Data.3\", got %q in\n%s", startLine, tf)
	}
	if runLine == "" || strings.Contains(runLine, "alias:") {
		t.Errorf("Run is a base tag and must import without alias:, got %q", runLine)
	}
	// The rung naming StartPB is carried: an alias tag has no DataType in
	// L5X, so it is typed through its target, here a rack point's bit
	// (#330; every kind of target is in alias_test.go).
	for _, n := range p.Notes {
		if n.Rung >= 0 {
			t.Errorf("rung not carried: %s", n)
		}
	}
	src := strings.Join(strings.Fields(string(p.Files["Main.ld"])), " ")
	for _, want := range []string{"StartPB : BOOL;", "StartPB ( Run )"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, p.Files["Main.ld"])
		}
	}
}
