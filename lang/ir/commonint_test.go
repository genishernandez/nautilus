package ir

import "testing"

var allIntNames = []string{"SINT", "INT", "DINT", "LINT", "USINT", "UINT", "UDINT", "ULINT", "BYTE", "WORD", "DWORD", "LWORD"}

// #326: the common type of every pair is order-independent, holds both
// operands' widths, is computed (never the declared singleton), and a
// conversion of it reads every bit (as LINT).
func TestCommonIntEveryPair(t *testing.T) {
	for _, l := range allIntNames {
		for _, r := range allIntNames {
			got := CommonInt(IntNamed(l), IntNamed(r))
			if back := CommonInt(IntNamed(r), IntNamed(l)); back.Name != got.Name {
				t.Errorf("%s op %s = %s, but %s op %s = %s", l, r, got, r, l, back)
			}
			if !got.Computed || got == IntNamed(got.Name) {
				t.Errorf("%s op %s = %s is not a computed type", l, r, got)
			}
			if w := got.BitWidth(); w < IntNamed(l).BitWidth() || w < IntNamed(r).BitWidth() {
				t.Errorf("%s op %s = %s (%d bits) is narrower than an operand", l, r, got, w)
			}
			if !got.Equal(IntT) {
				t.Errorf("%s op %s = %s is not Equal to INT; assignability would change", l, r, got)
			}
			if c, ok := sourceConvType(got); !ok || c.Name != "LINT" {
				t.Errorf("a %s expression converts as %s, want LINT", got, c.Name)
			}
		}
		if got := CommonInt(IntNamed(l), IntT); got.Name != l {
			t.Errorf("%s op literal = %s, want %s", l, got, l)
		}
		if got := CommonInt(IntT, IntNamed(l)); got.Name != l {
			t.Errorf("literal op %s = %s, want %s", l, got, l)
		}
	}
	if got := CommonInt(IntT, IntT); got != IntT {
		t.Errorf("literal op literal = %s, want the canonical INT", got)
	}
}
