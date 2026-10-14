package ir

import (
	"math"
	"strings"
	"testing"
)

// Coverage tests for docs/functions.md "Type conversions" claims.

// "A pattern that is NaN or infinite faults" — the infinite half, both
// widths and both signs (the NaN half is TestConversionFaults).
func TestConversionInfinitePatternFaults(t *testing.T) {
	for _, c := range []struct {
		fn string
		in Value
	}{
		{"DWORD_TO_REAL", IntVal(0x7F800000)},          // +Inf
		{"DWORD_TO_REAL", IntVal(0xFF800000)},          // -Inf
		{"LWORD_TO_LREAL", IntVal(0x7FF0000000000000)}, // +Inf
		{"LWORD_TO_LREAL", IntVal(-0x10000000000000)},  // 16#FFF0..., -Inf
	} {
		_, err := call(t, c.fn, c.in)
		if err == nil || !strings.Contains(err.Error(), "not a finite") {
			t.Errorf("%s(16#%X): err = %v, want the not-a-finite fault", c.fn, uint64(c.in.I), err)
		}
	}
	// The largest finite pattern still converts.
	got, err := call(t, "DWORD_TO_REAL", IntVal(0x7F7FFFFF))
	if err != nil || got.F != float64(math.MaxFloat32) {
		t.Errorf("DWORD_TO_REAL(16#7F7FFFFF) = %v, %v; want MaxFloat32", got.F, err)
	}
}

// "Integers to REAL are exact up to 2^53 ... REAL_TO_LREAL and
// LREAL_TO_REAL change only the type. ULINT and LWORD convert unsigned."
func TestConversionExactUnsignedAndRealLreal(t *testing.T) {
	const p53 = int64(1) << 53
	for _, c := range []struct {
		fn   string
		in   Value
		want float64
	}{
		{"DINT_TO_REAL", IntVal(16777217), 16777217}, // a float32 would give 16777216
		{"LINT_TO_LREAL", IntVal(p53), float64(p53)},
		{"LINT_TO_REAL", IntVal(p53 - 1), float64(p53 - 1)},
		{"LINT_TO_LREAL", IntVal(-p53), -float64(p53)},
		{"UDINT_TO_REAL", IntVal(-1), 4294967295}, // read as its own type first
		{"REAL_TO_LREAL", RealVal(0.1), 0.1},      // 0.1 is not a float32 value
		{"REAL_TO_LREAL", RealVal(16777217), 16777217},
		{"LREAL_TO_REAL", RealVal(16777217), 16777217},
	} {
		got, err := call(t, c.fn, c.in)
		if err != nil || got.Kind != TypeReal || got.F != c.want {
			t.Errorf("%s(%+v) = %+v, %v; want REAL %v", c.fn, c.in, got, err, c.want)
		}
	}
	for _, name := range []string{"REAL_TO_LREAL", "LREAL_TO_REAL"} {
		if r := Builtins[name].Result; r == nil || r.String() != strings.SplitN(name, "_TO_", 2)[1] {
			t.Errorf("%s result type = %v, want %s", name, r, strings.SplitN(name, "_TO_", 2)[1])
		}
	}
	// LWORD (and ULINT) read unsigned: all 64 bits set is 2^64-1, never -1.
	for _, c := range []struct {
		fn   string
		in   Value
		want Value
	}{
		{"LWORD_TO_STRING", IntVal(-1), StringVal("18446744073709551615")},
		{"LWORD_TO_ULINT", IntVal(-1), IntVal(-1)},
		{"LWORD_TO_DWORD", IntVal(-1), IntVal(4294967295)},
		{"ULINT_TO_REAL", IntVal(-1), RealVal(18446744073709551615)},
		{"ULINT_TO_STRING", IntVal(-2), StringVal("18446744073709551614")},
	} {
		got, err := call(t, c.fn, c.in)
		if err != nil || got.Kind != c.want.Kind || got.I != c.want.I || got.S != c.want.S || got.F != c.want.F {
			t.Errorf("%s(%+v) = %+v, %v; want %+v", c.fn, c.in, got, err, c.want)
		}
	}
	// TO_LREAL of an LWORD is its bit pattern (a binary transfer), so its
	// unsigned reading shows in TO_STRING instead.
	_, fn, err := Builtins["TO_STRING"].Resolve([]*Type{IntNamed("LWORD")})
	if err != nil {
		t.Fatalf("TO_STRING(LWORD): %v", err)
	}
	if got, _ := fn([]Value{IntVal(-1)}); got.S != "18446744073709551615" {
		t.Errorf("TO_STRING(LWORD all ones) = %q, want 18446744073709551615", got.S)
	}
}

// STRING parses "an integer in decimal or base-prefixed (16#FF, 2#1010,
// 8#17) with _ separators, a REAL in ordinary decimal or exponent notation,
// a TIME as a duration (T#1m30s, 90s, 500ms), a BOOL as TRUE/FALSE/1/0 in
// any case", after trimming blanks.
func TestStringParsesEveryDocumentedForm(t *testing.T) {
	for _, c := range []struct {
		fn   string
		in   string
		want Value
	}{
		{"STRING_TO_DINT", "8#17", IntVal(15)},
		{"STRING_TO_DINT", " 8#1_7 ", IntVal(15)},
		{"STRING_TO_WORD", "16#FF", IntVal(255)},
		{"STRING_TO_INT", "2#1010", IntVal(10)},
		{"STRING_TO_DINT", "1_000_000", IntVal(1000000)},
		{"STRING_TO_REAL", "1.5e3", RealVal(1500)},
		{"STRING_TO_REAL", "2E-2", RealVal(0.02)},
		{"STRING_TO_LREAL", " -0.25 ", RealVal(-0.25)},
		{"STRING_TO_TIME", "90s", TimeVal(90000)},
		{"STRING_TO_TIME", "500ms", TimeVal(500)},
		{"STRING_TO_TIME", " 1m30s ", TimeVal(90000)},
		{"STRING_TO_TIME", "T#1m30s", TimeVal(90000)},
		{"STRING_TO_LTIME", "90s", TimeVal(90000)},
		{"STRING_TO_BOOL", "TRUE", BoolVal(true)},
		{"STRING_TO_BOOL", "True", BoolVal(true)},
		{"STRING_TO_BOOL", "tRuE", BoolVal(true)},
		{"STRING_TO_BOOL", "1", BoolVal(true)},
		{"STRING_TO_BOOL", "FALSE", BoolVal(false)},
		{"STRING_TO_BOOL", "False", BoolVal(false)},
		{"STRING_TO_BOOL", " fAlSe ", BoolVal(false)},
		{"STRING_TO_BOOL", "0", BoolVal(false)},
	} {
		got, err := call(t, c.fn, StringVal(c.in))
		if err != nil || got.Kind != c.want.Kind || got.I != c.want.I || got.B != c.want.B || got.F != c.want.F {
			t.Errorf("%s(%q) = %+v, %v; want %+v", c.fn, c.in, got, err, c.want)
		}
	}
}
