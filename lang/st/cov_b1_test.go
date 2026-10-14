package st

import (
	"strings"
	"testing"
)

// TestB1ABSKeepsArgumentType: ABS's result is the argument's own type. The
// compiler names an expression's type when it cannot be assigned, so
// assigning ABS(x) to a STRING reports exactly what ABS returned.
func TestB1ABSKeepsArgumentType(t *testing.T) {
	for _, typ := range []string{"SINT", "INT", "DINT", "LINT", "UINT", "UDINT", "REAL"} {
		src := "PROGRAM p\nVAR x : " + typ + "; s : STRING; END_VAR\ns := ABS(x);\nEND_PROGRAM"
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("%s: parse: %v", typ, err)
		}
		_, err = Lower(prog)
		want := "cannot assign " + typ + " to STRING"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ABS of a %s: err = %v, want %q", typ, err, want)
		}
	}
}
