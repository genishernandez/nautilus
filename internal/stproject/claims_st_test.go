package stproject

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/joyautomation/nautilus/lang/st"
)

// ST-046: a project's PROGRAM-less .st files are libraries of TYPE and
// FUNCTION_BLOCK declarations in scope for every program; PROGRAM-less .ld and
// .fbd files are libraries too, composed after the ST ones. The graphical
// libraries here sort alphabetically before the ST one, so only the
// composition rule, not file-name order, can put the ST library first.
func TestLibrariesComposeSTFirstThenGraphical(t *testing.T) {
	fsys := fstest.MapFS{
		"lib/a_pump.ld": {Data: []byte(ldLibSrc)},
		"lib/b_scale.fbd": {Data: []byte(`FUNCTION_BLOCK Scale
VAR_INPUT  IN : REAL; END_VAR
VAR_OUTPUT OUT : REAL; END_VAR
VAR hdr : Header_Type; END_VAR
FBD
  OUT := MUL(IN, 2.0)
END_FBD
END_FUNCTION_BLOCK
`)},
		"lib/z_types.st": {Data: []byte(typesSrc)},
		"one.st": {Data: []byte(`PROGRAM One
VAR_EXTERNAL Raw : REAL; Eng : REAL; END_VAR
VAR s : Scale; END_VAR
s(IN := Raw);
Eng := s.OUT;
END_PROGRAM
`)},
		"two.st": {Data: []byte(`PROGRAM Two
VAR_EXTERNAL Cmd : BOOL; Motor : BOOL; END_VAR
VAR p : PumpSeq; h : Header_Type; END_VAR
p(Start := Cmd, Stop := FALSE);
Motor := p.Run AND NOT h.Valid;
END_PROGRAM
`)},
	}
	libs, err := Libraries(fsys, nil)
	if err != nil {
		t.Fatalf("libraries: %v", err)
	}
	var order []string
	for _, l := range libs {
		order = append(order, l.Path)
	}
	want := []string{"lib/z_types.st", "lib/a_pump.ld", "lib/b_scale.fbd"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("library order = %v, want the ST library first, then the graphical ones: %v", order, want)
	}
	if !strings.Contains(libs[0].ST, "Header_Type") {
		t.Errorf("library 0 should be the ST types file:\n%s", libs[0].ST)
	}
	if !strings.Contains(libs[1].ST, "FUNCTION_BLOCK PumpSeq") || strings.Contains(libs[1].ST, "RUNG") {
		t.Errorf("library 1 should be the ladder block as ST:\n%s", libs[1].ST)
	}
	if !strings.Contains(libs[2].ST, "FUNCTION_BLOCK Scale") || strings.Contains(libs[2].ST, "FBD") {
		t.Errorf("library 2 should be the FBD block as ST:\n%s", libs[2].ST)
	}
	// Every program in the directory compiles against the one prelude.
	var sts []string
	for _, l := range libs {
		sts = append(sts, l.ST)
	}
	for _, name := range []string{"one.st", "two.st"} {
		prog := Join(sts, string(fsys[name].Data))
		ast, err := st.Parse(prog)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if _, err := st.Lower(ast); err != nil {
			t.Errorf("%s does not compile against the libraries: %v", name, err)
		}
	}
}
