package st

import (
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// "TO_<type>(x) is the overloaded form: <A>_TO_<type> for whatever x is
// declared as (an expression's integer converts as LINT ...)". An INT
// variable converts as an INT (y holding 60000 is read as -5536), but the
// same arithmetic as an expression is a LINT: x + x with x = 30000 is
// 60000, not the -5536 an INT would wrap to, and 3_000_000_000 * 2 keeps
// all its bits until TO_<type> fits it.
func TestToTypeOfIntegerExpressionIsLINT(t *testing.T) {
	h := b2Run(t, `
PROGRAM P
VAR_EXTERNAL
  sum : DINT; sumStr : STRING; viaInt : DINT; big : LINT; bigU : UDINT; lit : LINT;
END_VAR
VAR x : INT := 30000; y : INT := 0; z : DINT := 1500000000; END_VAR
y := x + x;   (* an INT variable holding 60000 *)
sum := TO_DINT(x + x);
sumStr := TO_STRING(x + x);
viaInt := TO_DINT(y);
big := TO_LINT(z * 4);
bigU := TO_UDINT(z * 4);
lit := TO_LINT(3000000000 * 2);
END_PROGRAM`, 1, map[string]ir.Value{
		"sum": ir.IntVal(0), "sumStr": ir.StringVal(""), "viaInt": ir.IntVal(0),
		"big": ir.IntVal(0), "bigU": ir.IntVal(0), "lit": ir.IntVal(0),
	})
	for k, w := range map[string]int64{
		"sum":    60000,      // read as LINT; an INT would wrap to -5536
		"viaInt": -5536,      // the same value in an INT variable is read as INT
		"big":    6000000000, // DINT * 4 as an expression: LINT, all bits kept
		"bigU":   1705032704, // 6000000000 fitted into UDINT's low 32 bits
		"lit":    6000000000,
	} {
		if got := h.globals[k].I; got != w {
			t.Errorf("%s = %d, want %d", k, got, w)
		}
	}
	if got := h.globals["sumStr"].S; got != "60000" {
		t.Errorf("TO_STRING(x + x) = %q, want \"60000\" (LINT, not a wrapped INT)", got)
	}
}
