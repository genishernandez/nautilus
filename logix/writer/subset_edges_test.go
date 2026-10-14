package writer

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// LGA-016: the ST loops and EXIT are written as Logix ST, as written.
func TestStructuredTextLoopsAndExit(t *testing.T) {
	const src = `PROGRAM P
VAR
  i : DINT;
  n : DINT;
  Done : BOOL;
END_VAR
WHILE i < 10 DO
  i := i + 1;
  IF i = 5 THEN
    EXIT;
  END_IF;
END_WHILE;
REPEAT
  n := n + 2;
UNTIL n >= 6
END_REPEAT;
Done := n >= 6;
END_PROGRAM
`
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
	r := f.Controller.Programs[0].Routines[0]
	if r.Type != "ST" {
		t.Fatalf("routine type %s", r.Type)
	}
	// In order: the loop, its EXIT inside the IF, the REPEAT and its UNTIL.
	at := 0
	for _, want := range []string{
		"WHILE (i < 10) DO",
		"i := (i + 1);",
		"IF (i = 5) THEN",
		"EXIT;",
		"END_IF;",
		"END_WHILE;",
		"REPEAT",
		"n := (n + 2);",
		"UNTIL (n >= 6)",
		"END_REPEAT;",
	} {
		i := strings.Index(r.Text[at:], want)
		if i < 0 {
			t.Fatalf("missing %q (in order) in:\n%s", want, r.Text)
		}
		at += i + len(want)
	}
}

// LGA-017 / LGA-036: the refusals the rule table names that had no case. A
// trailing underscore (logix/name) and an Add-On Instruction taking an
// array by value (logix/type) are diagnostics with a line, and no document
// is written.
func TestRefusalsTheTableNames(t *testing.T) {
	cases := []struct {
		name, src, rule, mention string
	}{
		{"name trailing underscore",
			"PROGRAM P\nVAR\n  Bad_ : BOOL;\n  Y : BOOL;\nEND_VAR\nLD\n  RUNG r Bad_ ( Y )\nEND_LD\nEND_PROGRAM\n",
			ruleName, "trailing"},
		{"AOI with an array input",
			"FUNCTION_BLOCK Avg\nVAR_INPUT\n  Samples : ARRAY [0..3] OF REAL;\nEND_VAR\nVAR_OUTPUT\n  Q : BOOL;\nEND_VAR\nLD\n  RUNG r ( Q )\nEND_LD\nEND_FUNCTION_BLOCK\n" +
				"PROGRAM P\nVAR\n  a : Avg;\n  X : BOOL;\n  Y : BOOL;\nEND_VAR\nLD\n  RUNG r X a:Avg(Q => Y)\nEND_LD\nEND_PROGRAM\n",
			ruleType, "array parameter must be VAR_IN_OUT"},
		{"AOI with an array output",
			"FUNCTION_BLOCK Fill\nVAR_INPUT\n  Go : BOOL;\nEND_VAR\nVAR_OUTPUT\n  Buf : ARRAY [0..3] OF DINT;\nEND_VAR\nLD\n  RUNG r Go ( Go )\nEND_LD\nEND_FUNCTION_BLOCK\n" +
				"PROGRAM P\nVAR\n  f : Fill;\n  X : BOOL;\nEND_VAR\nLD\n  RUNG r X f:Fill(Go := X)\nEND_LD\nEND_PROGRAM\n",
			ruleType, "array parameter must be VAR_IN_OUT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags, err := Check(c.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			found := false
			for _, d := range diags {
				if d.Rule == c.rule && strings.Contains(d.Message, c.mention) {
					found = true
					if d.Line == 0 {
						t.Errorf("%s has no line", d)
					}
				}
			}
			if !found {
				t.Errorf("want rule %s mentioning %q, got:\n%s", c.rule, c.mention, joinDiags(diags))
			}
			if doc, _, _ := Write(c.src, Options{}); doc != nil {
				t.Error("Write emitted a document despite the diagnostic")
			}
		})
	}
}

// LGA-017: the structured-text and user-type refusals carry a line and stop
// the writer too, not only the ladder ones.
func TestSTAndTypeRefusalsHaveALineAndWriteNothing(t *testing.T) {
	t.Run("structured text", func(t *testing.T) {
		src := "PROGRAM P\nVAR\n  X : REAL;\n  Y : REAL;\nEND_VAR\nY := 1.0;\nX := MIN(X, Y);\nEND_PROGRAM\n"
		doc, diags, err := WriteST(src, Options{Controller: "C"})
		if err != nil {
			t.Fatal(err)
		}
		if doc != nil {
			t.Error("WriteST emitted a document despite the diagnostic")
		}
		if len(diags) != 1 || diags[0].Rule != ruleST || diags[0].Line != 7 {
			t.Errorf("want one %s on line 7, got:\n%s", ruleST, joinDiags(diags))
		}
	})
	t.Run("user type", func(t *testing.T) {
		types := "TYPE\n  T : STRUCT\n    D : TIME;\n  END_STRUCT;\nEND_TYPE\n"
		src := "PROGRAM P\nVAR_EXTERNAL\n  X : T;\n  Y : BOOL;\nEND_VAR\nLD\n  RUNG r Y ( Y )\nEND_LD\nEND_PROGRAM\n"
		doc, diags, err := Write(src, Options{Libs: []string{types}})
		if err != nil {
			t.Fatal(err)
		}
		if doc != nil {
			t.Error("Write emitted a document despite the diagnostic")
		}
		found := false
		for _, d := range diags {
			if d.Rule == ruleType && strings.Contains(d.Message, "TIME") {
				found = true
				if d.Line == 0 {
					t.Errorf("%s has no line", d)
				}
			}
		}
		if !found {
			t.Errorf("want %s mentioning TIME, got:\n%s", ruleType, joinDiags(diags))
		}
	})
}

// LGA-008: LE is written as the plain compare instruction, like the other
// five.
func TestLEIsAPlainCompare(t *testing.T) {
	src := "PROGRAM P\nVAR A : DINT; B : DINT; Q : BOOL; END_VAR\nLD\n  RUNG r LE(A, B) ( Q )\nEND_LD\nEND_PROGRAM\n"
	f := mustWrite(t, src, Options{})
	if got := f.Controller.Programs[0].Routines[0].Rungs[0].Text; got != "LE(A,B)OTE(Q);" {
		t.Errorf("rung = %q, want LE(A,B)OTE(Q);", got)
	}
}
