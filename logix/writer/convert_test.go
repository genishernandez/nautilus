package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/lang/st"
)

// The conversions fixture compiles in nautilus and comes out as a golden
// L5X: every conversion the writer writes, in one routine (#255).
func TestGoldenConversions(t *testing.T) {
	src := fixture(t, "conversions.st")
	prog, err := st.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Lower(prog); err != nil {
		t.Fatalf("the fixture does not compile in nautilus: %v", err)
	}
	doc, diags, err := WriteST(src, Options{Controller: "C"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) > 0 {
		t.Fatal(joinDiags(diags))
	}
	got := l5x.Normalize(doc, l5x.NormalizeOptions{})
	path := filepath.Join("testdata", "conversions.golden.L5X")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("emitted L5X differs from %s (run with -update if the change is intended)\n%s", path, firstDiff(string(want), string(got)))
	}
}

// What each conversion class is written as, line by line.
func TestConversionsWritten(t *testing.T) {
	doc, diags, err := WriteST(fixture(t, "conversions.st"), Options{Controller: "C"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) > 0 {
		t.Fatal(joinDiags(diags))
	}
	f, err := l5x.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := f.Controller.Programs[0].Routines[0].Text
	for _, want := range []string{
		// narrowing: the wrap, or the literal it folds to
		"i := ((((d MOD 65536) + 98304) MOD 65536) - 32768);",
		"s := ((((d MOD 256) + 384) MOD 256) - 128);",
		"s := ((((i MOD 256) + 384) MOD 256) - 128);",
		"i := 4464;",
		"d := ((((((a + d) MOD 65536) + 98304) MOD 65536) - 32768) * 2);",
		// INT_TO_DINT reads a DINT as an INT first
		"d := ((((d MOD 65536) + 98304) MOD 65536) - 32768);",
		"d := (((((a + d) MOD 65536) + 98304) MOD 65536) - 32768);",
		// widening
		"d := i;",
		"d := (s + 1);",
		"i := s;",
		"i := ((((d MOD 65536) + 98304) MOD 65536) - 32768);",
		// TIME
		"d := t;",
		"i := ((((t MOD 65536) + 98304) MOD 65536) - 32768);",
		"t := d;",
		// integer → REAL
		"r := d;",
		"r := (r + i);",
		"r := ((a * 1.0) / 2);",
		"r := (a / 2.0);",
		"lr := d;",
		"lr := r;",
		"r := lr;",
		"IF (i > r) THEN",
		// → BOOL
		"b := (d <> 0);",
		"b := ((d MOD 65536) <> 0);",
		"b := (r <> 0.0);",
		// BOOL → number
		"IF b THEN", "d := 1;", "d := 0;", "r := 1.0;", "r := 0.0;",
		// truncation
		"d := TRUNC(r);",
		// REAL → integer, TIME: the store rounds
		"d := r;", "i := r;", "s := r;", "t := r;",
		// integer division
		"d := (a / 2);",
		// a comparison and an index of their own beside a REAL
		"IF ((((((d MOD 65536) + 98304) MOD 65536) - 32768) > 5) AND (r > 1.5)) THEN",
		"r := (r + arr[((((d MOD 256) + 384) MOD 256) - 128)]);",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

// What the writer refuses, each with logix/conversion and what to write
// instead.
func TestConversionsRefused(t *testing.T) {
	wrap := func(body string) string {
		return "PROGRAM P\nVAR\n  d : DINT; a : DINT; i : INT; r : REAL; lr : LREAL; b : BOOL; t : TIME;\nEND_VAR\n" + body + "\nEND_PROGRAM\n"
	}
	cases := []struct{ name, body, mention string }{
		{"REAL to DINT in an expression", "d := REAL_TO_DINT(r) + 1;", "DINT tag of its own"},
		{"REAL to DINT in a comparison", "b := REAL_TO_DINT(r) > 2;", "DINT tag of its own"},
		{"REAL to TIME in an expression", "t := REAL_TO_TIME(r) + t;", "TIME tag of its own"},
		{"REAL to DINT stored into an INT", "i := REAL_TO_DINT(r);", "destination is INT, not DINT"},
		{"REAL to INT stored into a DINT", "d := REAL_TO_INT(r);", "destination is DINT, not INT"},
		{"REAL to DINT stored into a REAL", "r := REAL_TO_DINT(r);", "destination is REAL, not DINT"},
		{"LREAL to DINT", "d := LREAL_TO_DINT(lr);", "LREAL"},
		{"overloaded TO_DINT of an LREAL", "d := TO_DINT(lr);", "LREAL"},
		{"REAL to DINT of an LREAL", "d := REAL_TO_DINT(lr);", "LREAL"},
		{"TRUNC in an expression", "d := TRUNC_DINT(r) + 1;", "tag of its own"},
		{"TRUNC of an LREAL", "d := LREAL_TRUNC_DINT(lr);", "LREAL"},
		{"bit transfer", "r := DWORD_TO_REAL(REAL_TO_DWORD(r));", "COP"},
		{"bit string", "d := WORD_TO_DINT(DINT_TO_WORD(d));", "WORD"},
		{"unsigned", "d := UINT_TO_DINT(DINT_TO_UINT(d));", "unsigned"},
		{"LINT", "d := LINT_TO_DINT(DINT_TO_LINT(d));", "LINT"},
		{"STRING", "d := STRING_TO_DINT('5');", "STRING"},
		{"BOOL in an expression", "d := BOOL_TO_DINT(b) + 1;", "IF"},
		{"integer division to REAL", "r := DINT_TO_REAL(a / d);", "divide as integers"},
		{"wider argument", "r := INT_TO_REAL(d);", "DINT_TO_REAL"},
		{"int to LREAL in an expression", "lr := lr + DINT_TO_LREAL(d);", "LREAL tag of its own"},
		{"REAL to LREAL in an expression", "lr := lr * REAL_TO_LREAL(r);", "LREAL tag of its own"},
		{"narrowing beside a REAL", "r := r + DINT_TO_INT(d);", "integer tag of its own"},
		{"narrowing stored into a REAL", "r := DINT_TO_INT(d);", "integer tag of its own"},
		// #361: an integer / that meets a REAL is divided in REAL on Logix.
		{"integer division stored into a REAL", "r := a / 2;", "integer division"},
		{"integer division of tags stored into a REAL", "r := a / d;", "integer division"},
		{"integer division stored into an LREAL", "lr := a / 2;", "integer division"},
		{"integer division compared with a REAL", "b := a / 2 > r;", "integer division"},
		{"integer division compared in an IF", "IF a / d >= r THEN b := TRUE; END_IF;", "integer division"},
		{"integer division beside a REAL", "r := (a / 2) + r;", "integer division"},
		{"integer division divided by a REAL", "r := (a / 2) / 2.0;", "integer division"},
		{"integer division nested in REAL arithmetic", "r := r * ((a + 1) / d - 1);", "integer division"},
		{"integer division in a call stored into a REAL", "r := ABS(a / 2);", "integer division"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags, err := CheckST(wrap(c.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			found := false
			for _, d := range diags {
				if d.Rule == ruleConversion && strings.Contains(d.Message, c.mention) {
					found = true
				}
			}
			if !found {
				t.Errorf("want %s mentioning %q, got %s", ruleConversion, c.mention, joinDiags(diags))
			}
		})
	}
}

// The narrowing wrap computes lang/ir's conversion for every value a DINT
// holds near the edges, whichever sign rule Logix's MOD follows: truncated
// (Go's %, and Rockwell's documented A - TRN(A / B) * B) or floored.
func TestNarrowingWrapMatchesNautilus(t *testing.T) {
	truncMod := func(a, b int64) int64 { return a % b }
	flooredMod := func(a, b int64) int64 { return ((a % b) + b) % b }
	var values []int64
	for _, edge := range []int64{0, 127, 128, 255, 256, 32767, 32768, 65535, 65536, 70000, 1 << 30, 2147483647} {
		for d := int64(-3); d <= 3; d++ {
			values = append(values, edge+d, -edge+d)
		}
	}
	values = append(values, -2147483648)
	for _, conv := range []struct {
		name  string
		width int
	}{{"DINT_TO_INT", 16}, {"DINT_TO_SINT", 8}, {"INT_TO_SINT", 8}} {
		fn := ir.Builtins[conv.name].Fn
		m := int64(1) << conv.width
		for _, v := range values {
			if v < -2147483648 || v > 2147483647 {
				continue
			}
			arg := v
			if conv.name == "INT_TO_SINT" {
				arg = wrapTo(v, 16) // an INT tag holds an INT
			}
			want, err := fn([]ir.Value{ir.IntVal(arg)})
			if err != nil {
				t.Fatal(err)
			}
			for name, mod := range map[string]func(a, b int64) int64{"truncated": truncMod, "floored": flooredMod} {
				got := mod(mod(arg, m)+m+m/2, m) - m/2
				if got != want.I {
					t.Errorf("%s(%d) with %s MOD: wrap gives %d, nautilus %d", conv.name, arg, name, got, want.I)
				}
			}
		}
	}
	// X_TO_BOOL of an argument wider than X: (v MOD 2^width) <> 0.
	for _, conv := range []struct {
		name  string
		width int
	}{{"INT_TO_BOOL", 16}, {"SINT_TO_BOOL", 8}} {
		fn := ir.Builtins[conv.name].Fn
		for _, v := range values {
			want, err := fn([]ir.Value{ir.IntVal(v)})
			if err != nil {
				t.Fatal(err)
			}
			if got := truncMod(v, int64(1)<<conv.width) != 0; got != want.B {
				t.Errorf("%s(%d): the writer's MOD gives %v, nautilus %v", conv.name, v, got, want.B)
			}
		}
	}
}

// REAL → integer and TIME as the whole right-hand side of an assignment to
// a tag of the conversion's own type is a plain store: Logix rounds it as
// it stores it, halves to even, as nautilus does (verified on a v38
// controller, #297).
func TestRealToIntegerStores(t *testing.T) {
	cases := []struct{ body, want string }{
		{"d := REAL_TO_DINT(r);", "d := r;"},
		{"i := REAL_TO_INT(r);", "i := r;"},
		{"s := REAL_TO_SINT(r);", "s := r;"},
		{"d := TO_DINT(r);", "d := r;"},
		{"i := TO_INT(r);", "i := r;"},
		{"t := REAL_TO_TIME(r);", "t := r;"},
		{"d := REAL_TO_DINT(r * 2.0 + 0.5);", "d := ((r * 2.0) + 0.5);"},
		{"d := REAL_TO_DINT(2.5);", "d := 2.5;"},
		{"arr[1] := REAL_TO_INT(r);", "arr[1] := r;"},
	}
	for _, c := range cases {
		t.Run(c.body, func(t *testing.T) {
			src := "PROGRAM P\nVAR\n  d : DINT; i : INT; s : SINT; r : REAL; t : TIME; arr : ARRAY [0..3] OF INT;\nEND_VAR\n" + c.body + "\nEND_PROGRAM\n"
			text := writeRoutine(t, src)
			if !strings.Contains(text, c.want) {
				t.Errorf("want %q in:\n%s", c.want, text)
			}
		})
	}
}

// An integer / that never meets a REAL is written unchanged: Logix
// truncates it as nautilus does (#361).
func TestIntegerDivisionUnchanged(t *testing.T) {
	cases := []struct{ body, want string }{
		{"d := a / 2;", "d := (a / 2);"},
		{"i := a / d;", "i := (a / d);"},
		{"b := a / 2 > d;", "b := ((a / 2) > d);"},
		{"b := a / 2 = 3;", "b := ((a / 2) = 3);"},
		{"IF a / 2 > 5 AND r > 1.5 THEN b := TRUE; END_IF;", "IF (((a / 2) > 5) AND (r > 1.5)) THEN"},
		{"b := DINT_TO_BOOL(a / 2);", "b := ((a / 2) <> 0);"},
		{"r := DINT_TO_REAL(a) / 2;", "r := ((a * 1.0) / 2);"},
		{"r := a / 2.0;", "r := (a / 2.0);"},
		{"r := a / r;", "r := (a / r);"},
	}
	for _, c := range cases {
		t.Run(c.body, func(t *testing.T) {
			src := "PROGRAM P\nVAR\n  d : DINT; a : DINT; i : INT; r : REAL; b : BOOL;\nEND_VAR\n" + c.body + "\nEND_PROGRAM\n"
			text := writeRoutine(t, src)
			if !strings.Contains(text, c.want) {
				t.Errorf("want %q in:\n%s", c.want, text)
			}
		})
	}
}

// writeRoutine writes src and returns its one ST routine's text, failing
// on any diagnostic.
func writeRoutine(t *testing.T, src string) string {
	t.Helper()
	doc, diags, err := WriteST(src, Options{Controller: "C"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) > 0 {
		t.Fatal(joinDiags(diags))
	}
	f, err := l5x.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	return f.Controller.Programs[0].Routines[0].Text
}
