package st

import (
	"strings"
	"testing"
)

// "DATE, TIME_OF_DAY and DATE_AND_TIME are not run-time types yet, so they
// have no conversions." (Type conversions) Every spelling of the three is
// refused as a variable type, and no conversion to or from them exists,
// neither typed (DATE_TO_DINT, TOD_TO_TIME, STRING_TO_DT) nor overloaded
// (TO_DATE, TO_TOD, TO_DT).
func TestDateTypesAreNotRuntimeTypes(t *testing.T) {
	for _, typ := range []string{"DATE", "TIME_OF_DAY", "TOD", "DATE_AND_TIME", "DT"} {
		t.Run("type "+typ, func(t *testing.T) {
			got := b2LowerErr(t, "PROGRAM P\nVAR d : "+typ+"; END_VAR\nEND_PROGRAM")
			if !strings.Contains(got, `unknown scalar type "`+typ+`"`) {
				t.Fatalf("VAR d : %s: err = %q, want it refused as an unknown type", typ, got)
			}
		})
	}
	for _, fn := range []string{
		"DATE_TO_DINT", "DINT_TO_DATE", "TOD_TO_TIME", "TIME_OF_DAY_TO_TIME", "TIME_TO_TOD",
		"DT_TO_DINT", "DATE_AND_TIME_TO_STRING", "STRING_TO_DT", "STRING_TO_DATE",
		"TO_DATE", "TO_TOD", "TO_TIME_OF_DAY", "TO_DT", "TO_DATE_AND_TIME",
	} {
		t.Run("conversion "+fn, func(t *testing.T) {
			got := b2LowerErr(t, "PROGRAM P\nVAR x : DINT; END_VAR\nx := "+fn+"(0);\nEND_PROGRAM")
			if !strings.Contains(got, `unknown function "`+fn+`"`) {
				t.Fatalf("%s(0): err = %q, want an unknown function", fn, got)
			}
		})
	}
}
