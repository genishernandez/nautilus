package st

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// ST-031: the documented precedence, tightest first: unary NOT and -; * / MOD;
// + -; comparisons; AND; XOR; OR. Each case is chosen so that the neighbouring
// (wrong) precedence gives the opposite answer.
func TestOperatorPrecedence(t *testing.T) {
	cases := []struct {
		name, expr string
		wantInt    int64
		wantBool   bool
		isBool     bool
	}{
		{name: "star over plus", expr: "2 + 3 * 4", wantInt: 14},
		{name: "plus after star on the left", expr: "2 * 3 + 4", wantInt: 10},
		{name: "slash over minus", expr: "20 - 8 / 4", wantInt: 18},
		{name: "mod over plus", expr: "2 + 7 MOD 4", wantInt: 5},
		{name: "unary minus over plus", expr: "-3 + 5", wantInt: 2},
		{name: "arithmetic over comparison", expr: "1 + 1 = 2", isBool: true, wantBool: true},
		{name: "comparison over AND", expr: "a = 1 AND b = 2", isBool: true, wantBool: true},
		{name: "NOT over AND", expr: "NOT f AND f", isBool: true, wantBool: false},
		{name: "AND over XOR", expr: "t XOR t AND f", isBool: true, wantBool: true},
		{name: "XOR over OR", expr: "t OR t XOR t", isBool: true, wantBool: true},
		{name: "AND over OR", expr: "f AND f OR t", isBool: true, wantBool: true},
		{name: "AND over OR on the right", expr: "t OR f AND f", isBool: true, wantBool: true},
		{name: "XOR before OR on the left", expr: "t XOR t OR t", isBool: true, wantBool: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var src string
			if tc.isBool {
				src = "PROGRAM p\nVAR a : INT := 1; b : INT := 2; t : BOOL := TRUE; f : BOOL := FALSE; r : BOOL; END_VAR\nr := " + tc.expr + ";\nEND_PROGRAM"
			} else {
				src = "PROGRAM p\nVAR r : INT; END_VAR\nr := " + tc.expr + ";\nEND_PROGRAM"
			}
			_, prog, frame := scanN(t, src, 1, nil)
			got := frame.Slots[prog.SlotIndex["r"]]
			if tc.isBool && got.B != tc.wantBool {
				t.Errorf("%s = %v, want %v", tc.expr, got.B, tc.wantBool)
			}
			if !tc.isBool && got.I != tc.wantInt {
				t.Errorf("%s = %d, want %d", tc.expr, got.I, tc.wantInt)
			}
		})
	}
}

// ST-029: = is equality, <> is inequality, := assigns.
func TestEqualityAndInequalityOperators(t *testing.T) {
	src := `
PROGRAM p
VAR a : INT := 3; b : INT := 3; c : INT := 4; eqSame, eqDiff, neSame, neDiff : BOOL; END_VAR
eqSame := a = b;
eqDiff := a = c;
neSame := a <> b;
neDiff := a <> c;
a := c;
END_PROGRAM`
	_, prog, frame := scanN(t, src, 1, nil)
	for name, want := range map[string]bool{"eqSame": true, "eqDiff": false, "neSame": false, "neDiff": true} {
		if got := frame.Slots[prog.SlotIndex[name]].B; got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	// := is the assignment, and does not compare: the target takes the value.
	if got := frame.Slots[prog.SlotIndex["a"]].I; got != 4 {
		t.Errorf("a = %d, want 4 after a := c", got)
	}
}

// ST-008: VAR_GLOBAL and VAR_EXTERNAL name the same thing, a tag in the store:
// a program written with either reads the seeded tag and writes it back.
func TestVarGlobalAndVarExternalResolveIdentically(t *testing.T) {
	for _, kw := range []string{"VAR_GLOBAL", "VAR_EXTERNAL"} {
		t.Run(kw, func(t *testing.T) {
			src := "PROGRAM p\n" + kw + " Count : INT; Total : INT; END_VAR\nTotal := Count * 2;\nCount := Count + 1;\nEND_PROGRAM"
			h, prog, _ := scanN(t, src, 2, map[string]ir.Value{"Count": ir.IntVal(5), "Total": ir.IntVal(0)})
			if _, local := prog.SlotIndex["Count"]; local {
				t.Errorf("Count became a program-local slot, not a store tag")
			}
			if h.globals["Count"].I != 7 || h.globals["Total"].I != 12 {
				t.Errorf("store Count=%d Total=%d, want 7 and 12", h.globals["Count"].I, h.globals["Total"].I)
			}
		})
	}
}

// ST-015: the page lists WSTRING, CHAR and WCHAR as accepted declarations held
// as STRING, alongside the other elementary types.
func TestWideAndCharTypesAreAcceptedAsString(t *testing.T) {
	src := `
PROGRAM p
VAR
  ws : WSTRING := 'wide';
  ch : CHAR;
  wc : WCHAR;
  s  : STRING;
  sb : SINT; ub : USINT; i : INT; ui : UINT; d : DINT; ud : UDINT; l : LINT; ul : ULINT;
  bt : BYTE; w : WORD; dw : DWORD; lw : LWORD;
  r : REAL; lr : LREAL; tm : TIME; ltm : LTIME; b : BOOL;
END_VAR
ch := 'A';
wc := 'B';
s := ws;
END_PROGRAM`
	_, prog, frame := scanN(t, src, 1, nil)
	if got := frame.Slots[prog.SlotIndex["s"]].S; got != "wide" {
		t.Errorf("s = %q, want wide (a WSTRING copies into a STRING)", got)
	}
	if got := frame.Slots[prog.SlotIndex["ch"]].S; got != "A" {
		t.Errorf("ch = %q, want A", got)
	}
	if got := frame.Slots[prog.SlotIndex["wc"]].S; got != "B" {
		t.Errorf("wc = %q, want B", got)
	}
	for _, n := range []string{"ws", "ch", "wc"} {
		if k := prog.Slots[prog.SlotIndex[n]].Type.Kind; k != ir.TypeString {
			t.Errorf("%s is held as kind %v, want STRING", n, k)
		}
	}
}

// ST-019: arithmetic written in the bound itself, not only in the constant.
func TestArrayBoundArithmeticInTheBound(t *testing.T) {
	src := `
PROGRAM P
VAR_EXTERNAL Out : INT; END_VAR
VAR CONSTANT N_TANKS : INT := 3; END_VAR
VAR a : ARRAY[1..N_TANKS * 2] OF INT; b : ARRAY[0..N_TANKS + 1] OF INT; i : INT; END_VAR
FOR i := 1 TO 6 DO a[i] := i; END_FOR;
b[4] := 7;
Out := a[6] + b[4];
END_PROGRAM`
	h, _, _ := scanN(t, src, 1, map[string]ir.Value{"Out": ir.IntVal(0)})
	if got := h.globals["Out"].I; got != 13 {
		t.Errorf("Out = %d, want 13 (a[6] + b[4])", got)
	}
	// One past the computed upper bound faults the scan, so the bound was 6.
	prog := lowerSource(t, `
PROGRAM P
VAR CONSTANT N : INT := 3; END_VAR
VAR a : ARRAY[1..N * 2] OF INT; END_VAR
a[7] := 1;
END_PROGRAM`)
	if err := ir.Run(prog, ir.NewFrame(prog), newStubHost()); err == nil {
		t.Error("a[7] on ARRAY[1..N*2] did not fault")
	}
}

// ST-020: TYPE declares an alias for another type.
func TestTypeAliasOfAnotherType(t *testing.T) {
	src := `
TYPE
  Speed : REAL;
  Count : INT;
END_TYPE
PROGRAM P
VAR_EXTERNAL Out : REAL; END_VAR
VAR s : Speed; c : Count; END_VAR
s := 1.5;
c := 4;
Out := s * c;
END_PROGRAM`
	h, prog, _ := scanN(t, src, 1, map[string]ir.Value{"Out": ir.RealVal(0)})
	if got := h.globals["Out"].F; got != 6.0 {
		t.Errorf("Out = %v, want 6", got)
	}
	if k := prog.Slots[prog.SlotIndex["s"]].Type.Kind; k != ir.TypeReal {
		t.Errorf("a Speed variable has kind %v, want REAL (the alias target)", k)
	}
	if k := prog.Slots[prog.SlotIndex["c"]].Type.Kind; k != ir.TypeInt {
		t.Errorf("a Count variable has kind %v, want INT", k)
	}
}

// ST-022: the exponent form of a decimal literal.
func TestExponentLiteral(t *testing.T) {
	src := `
PROGRAM P
VAR a, b, c : REAL; END_VAR
a := 1.5e3;
b := 2.0E-2;
c := 1.5e+1;
END_PROGRAM`
	_, prog, frame := scanN(t, src, 1, nil)
	for n, want := range map[string]float64{"a": 1500, "b": 0.02, "c": 15} {
		if got := frame.Slots[prog.SlotIndex[n]].F; got != want {
			t.Errorf("%s = %v, want %v", n, got, want)
		}
	}
}

// ST-024: RETAIN changes nothing at runtime: a RETAIN program variable and a
// RETAIN function block instance behave exactly as their plain twins, each
// keeping its value from scan to scan.
func TestRetainChangesNothingAtRuntime(t *testing.T) {
	const body = `
FUNCTION_BLOCK Acc
VAR_INPUT Step : INT; END_VAR
VAR_OUTPUT Total : INT; END_VAR
VAR n : INT; END_VAR
n := n + Step;
Total := n;
END_FUNCTION_BLOCK
PROGRAM P
VAR_EXTERNAL Out : INT; Fb : INT; END_VAR
VAR%s counter : INT; a : Acc; END_VAR
counter := counter + 1;
a(Step := 2);
Out := counter;
Fb := a.Total;
END_PROGRAM`
	run := func(retain string) (int64, int64) {
		src := strings.Replace(body, "VAR%s", "VAR"+retain, 1)
		h, _, _ := scanN(t, src, 4, map[string]ir.Value{"Out": ir.IntVal(0), "Fb": ir.IntVal(0)})
		return h.globals["Out"].I, h.globals["Fb"].I
	}
	plainOut, plainFb := run("")
	retOut, retFb := run(" RETAIN")
	if plainOut != 4 || plainFb != 8 {
		t.Fatalf("plain twin: Out=%d Fb=%d, want 4 and 8 (state kept scan to scan)", plainOut, plainFb)
	}
	if retOut != plainOut || retFb != plainFb {
		t.Errorf("RETAIN: Out=%d Fb=%d, plain: Out=%d Fb=%d; RETAIN must change nothing", retOut, retFb, plainOut, plainFb)
	}
}

// ST-026: a FOR loop with a BY step (positive and negative), and RETURN from an
// ST program.
func TestForByStepAndReturnFromST(t *testing.T) {
	src := `
PROGRAM P
VAR_EXTERNAL Up : INT; Down : INT; Seen : INT; END_VAR
VAR i : INT; END_VAR
Up := 0;
FOR i := 0 TO 10 BY 3 DO Up := Up + i; END_FOR;
Down := 0;
FOR i := 10 TO 1 BY -4 DO Down := Down + i; END_FOR;
Seen := 1;
IF Up = 18 THEN RETURN; END_IF;
Seen := 2;
END_PROGRAM`
	h, _, _ := scanN(t, src, 1, map[string]ir.Value{"Up": ir.IntVal(0), "Down": ir.IntVal(0), "Seen": ir.IntVal(0)})
	if got := h.globals["Up"].I; got != 18 { // 0+3+6+9
		t.Errorf("Up = %d, want 18 (i = 0,3,6,9)", got)
	}
	if got := h.globals["Down"].I; got != 18 { // 10+6+2
		t.Errorf("Down = %d, want 18 (i = 10,6,2)", got)
	}
	if got := h.globals["Seen"].I; got != 1 {
		t.Errorf("Seen = %d, want 1: RETURN must stop the program before Seen := 2", got)
	}
}

// ST-032: an INT widens to REAL where one is expected: also as an argument to a
// REAL parameter of a function and of a function block.
func TestIntWidensToRealParameter(t *testing.T) {
	src := `
FUNCTION Pass : REAL
VAR_INPUT x : REAL; END_VAR
Pass := x;
END_FUNCTION
FUNCTION_BLOCK Scale
VAR_INPUT x : REAL; END_VAR
VAR_OUTPUT y : REAL; END_VAR
y := x;
END_FUNCTION_BLOCK
PROGRAM P
VAR_EXTERNAL A : REAL; B : REAL; END_VAR
VAR n : INT := 5; s : Scale; END_VAR
A := Pass(n);
s(x := n);
B := s.y;
END_PROGRAM`
	h, _, _ := scanN(t, src, 1, map[string]ir.Value{"A": ir.RealVal(0), "B": ir.RealVal(0)})
	if h.globals["A"].F != 5 {
		t.Errorf("Pass(5) = %v, want 5 (the INT arrives as a REAL)", h.globals["A"].F)
	}
	if h.globals["B"].F != 5 {
		t.Errorf("Scale(5).y = %v, want 5 (the INT arrives as a REAL)", h.globals["B"].F)
	}
}

// ST-033: assigning a REAL expression to an INT variable is a compile error.
func TestRealIntoIntAssignmentIsACompileError(t *testing.T) {
	for _, body := range []string{"i := r;", "i := r * 2.0;", "i := 1.5;"} {
		t.Run(body, func(t *testing.T) {
			_, err := lowerSrc("PROGRAM p\nVAR i : INT; r : REAL; END_VAR\n" + body + "\nEND_PROGRAM")
			if err == nil {
				t.Fatalf("%q compiled; REAL into INT must be an error", body)
			}
			if !strings.Contains(err.Error(), "REAL") || !strings.Contains(err.Error(), "INT") {
				t.Errorf("error %q should name REAL and INT", err)
			}
		})
	}
	// Narrowing through a conversion is the sanctioned way.
	if _, err := lowerSrc("PROGRAM p\nVAR i : INT; r : REAL; END_VAR\ni := REAL_TO_INT(r);\nEND_PROGRAM"); err != nil {
		t.Errorf("REAL_TO_INT(r) refused: %v", err)
	}
}

// ST-035: there are no WSTRING_TO_, CHAR_TO_ or WCHAR_TO_ functions.
func TestNoWideOrCharConversionFunctions(t *testing.T) {
	for _, fn := range []string{"WSTRING_TO_STRING", "CHAR_TO_STRING", "WCHAR_TO_STRING", "STRING_TO_WSTRING", "STRING_TO_CHAR", "STRING_TO_WCHAR", "CHAR_TO_INT", "WSTRING_TO_INT"} {
		t.Run(fn, func(t *testing.T) {
			_, err := lowerSrc("PROGRAM p\nVAR s : STRING; i : INT; END_VAR\ns := " + fn + "(s);\nEND_PROGRAM")
			if err == nil {
				t.Fatalf("%s compiled; it should not exist", fn)
			}
		})
	}
	// The matrix names that do exist still resolve, so the refusal above is
	// about these names and not about conversions in general.
	if _, err := lowerSrc("PROGRAM p\nVAR s : STRING; i : INT; END_VAR\ns := INT_TO_STRING(i);\nEND_PROGRAM"); err != nil {
		t.Errorf("INT_TO_STRING refused: %v", err)
	}
}

// ST-041: each FUNCTION_BLOCK instance owns its slots: two instances of one
// block count independently.
func TestUserFBInstancesOwnTheirSlots(t *testing.T) {
	src := `
FUNCTION_BLOCK Accum
VAR_INPUT Step : INT; END_VAR
VAR_OUTPUT Total : INT; END_VAR
VAR acc : INT; END_VAR
acc := acc + Step;
Total := acc;
END_FUNCTION_BLOCK
PROGRAM P
VAR_EXTERNAL A : INT; B : INT; END_VAR
VAR x : Accum; y : Accum; END_VAR
x(Step := 1);
y(Step := 10);
A := x.Total;
B := y.Total;
END_PROGRAM`
	h, _, _ := scanN(t, src, 3, map[string]ir.Value{"A": ir.IntVal(0), "B": ir.IntVal(0)})
	if h.globals["A"].I != 3 || h.globals["B"].I != 30 {
		t.Errorf("A=%d B=%d, want 3 and 30: two instances must not share state", h.globals["A"].I, h.globals["B"].I)
	}
}

// ST-042: pins may be an ARRAY or a TYPE alias.
func TestFBArrayAndAliasPins(t *testing.T) {
	src := `
TYPE Level : REAL; END_TYPE
FUNCTION_BLOCK Peak
VAR_INPUT lv : Level; END_VAR
VAR_IN_OUT hist : ARRAY[0..2] OF Level; END_VAR
VAR_OUTPUT top : Level; END_VAR
hist[2] := lv;
top := hist[2] + hist[0];
END_FUNCTION_BLOCK
PROGRAM P
VAR_EXTERNAL Out : REAL; END_VAR
VAR p : Peak; h : ARRAY[0..2] OF Level; END_VAR
h[0] := 2.0;
p(lv := 4.5, hist := h);
Out := p.top + h[2];
END_PROGRAM`
	h, _, _ := scanN(t, src, 1, map[string]ir.Value{"Out": ir.RealVal(0)})
	if got := h.globals["Out"].F; got != 11.0 { // top = 4.5+2 = 6.5; h[2] = 4.5
		t.Errorf("Out = %v, want 11", got)
	}
}

// countHost counts the store writes made to each tag.
type countHost struct {
	*fakeHost
	writes map[string]int
}

func (h *countHost) WriteGlobal(name string, v ir.Value) error {
	h.writes[name]++
	return h.fakeHost.WriteGlobal(name, v)
}

// ST-044: a VAR_EXTERNAL UDT bound to a VAR_IN_OUT pin is written back to the
// tag store as one whole-struct write per call, however many fields the block
// assigns, and the whole struct carries every field the block wrote.
func TestVarInOutExternalStructWritesBackAsOneWholeStruct(t *testing.T) {
	src := `
TYPE
  Pump : STRUCT Running : BOOL; Starts : INT; Speed : REAL; END_STRUCT;
END_TYPE
FUNCTION_BLOCK FB_Starter
VAR_IN_OUT P : Pump; END_VAR
VAR_INPUT Cmd : BOOL; END_VAR
  P.Starts := P.Starts + 1;
  P.Running := Cmd;
  P.Speed := 60.0;
END_FUNCTION_BLOCK
PROGRAM main
VAR s : FB_Starter; END_VAR
VAR_EXTERNAL P101 : Pump; Run : BOOL; END_VAR
  s(P := P101, Cmd := Run);
END_PROGRAM
`
	irProg := mustLower(t, src)
	host := &countHost{fakeHost: newFakeHost(), writes: map[string]int{}}
	host.vals["P101"] = ir.Zero(irProg.Types["Pump"])
	host.vals["Run"] = ir.BoolVal(true)
	frame := ir.NewFrame(irProg)
	for scan := 1; scan <= 3; scan++ {
		before := host.writes["P101"]
		if err := ir.Run(irProg, frame, host); err != nil {
			t.Fatalf("scan %d: %v", scan, err)
		}
		if n := host.writes["P101"] - before; n != 1 {
			t.Fatalf("scan %d: %d writes to P101, want exactly 1 whole-struct write", scan, n)
		}
	}
	p := host.vals["P101"]
	if !p.Fld[0].B || p.Fld[1].I != 3 || p.Fld[2].F != 60 {
		t.Errorf("P101 = Running %v Starts %d Speed %v; want true, 3, 60 (one write carrying every field)", p.Fld[0].B, p.Fld[1].I, p.Fld[2].F)
	}
}
