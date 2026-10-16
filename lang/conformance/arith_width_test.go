package main

// #326: integer arithmetic is typed to the operands' common width
// (docs/functions.md "Operators": ADD, SUB, MUL, DIV give the common type).
// The type is what the checker reports, so each case assigns the result to
// a STRING and reads the type out of the diagnostic: DINT + DINT used to be
// the generic INT ("cannot assign INT to STRING"). The values are pinned in
// fn-ops ("integer arithmetic keeps its value past the narrower width").

import (
	"fmt"
	"testing"
)

// arithWidthCases is one row per width pair: the two declared types, the
// operator, and the common type the result must have.
var arithWidthCases = []struct{ l, op, r, want string }{
	// Same type: that type.
	{"SINT", "+", "SINT", "SINT"},
	{"INT", "+", "INT", "INT"},
	{"DINT", "+", "DINT", "DINT"},
	{"LINT", "+", "LINT", "LINT"},
	{"UDINT", "+", "UDINT", "UDINT"},
	{"WORD", "+", "WORD", "WORD"},
	// Every operator, not just +.
	{"DINT", "-", "DINT", "DINT"},
	{"DINT", "*", "DINT", "DINT"},
	{"DINT", "/", "DINT", "DINT"},
	{"DINT", "MOD", "DINT", "DINT"},
	// Signed with signed, unsigned with unsigned, bits with bits: the wider.
	{"SINT", "+", "INT", "INT"},
	{"INT", "*", "DINT", "DINT"},
	{"LINT", "-", "DINT", "LINT"},
	{"USINT", "+", "UINT", "UINT"},
	{"UDINT", "+", "ULINT", "ULINT"},
	{"BYTE", "+", "DWORD", "DWORD"},
	// Signed with unsigned: the narrowest signed type that holds both.
	{"INT", "+", "USINT", "INT"},
	{"INT", "+", "UINT", "DINT"},
	{"UINT", "-", "SINT", "DINT"},
	{"DINT", "+", "UDINT", "LINT"},
	{"LINT", "+", "ULINT", "LINT"},
	// A bit string with a number counts as the unsigned type of its width.
	{"WORD", "+", "INT", "DINT"},
	{"DWORD", "+", "UDINT", "UDINT"},
}

func TestArithmeticTakesTheCommonWidth(t *testing.T) {
	for _, c := range arithWidthCases {
		want := fmt.Sprintf("cannot assign %s to STRING", c.want)
		t.Run(fmt.Sprintf("ST %s %s %s is %s", c.l, c.op, c.r, c.want), func(t *testing.T) {
			src := fmt.Sprintf("PROGRAM P\nVAR a : %s; b : %s; s : STRING; END_VAR\ns := a %s b;\nEND_PROGRAM\n", c.l, c.r, c.op)
			wantCompileErr(t, compileErr(t, "p.st", src, ""), want)
		})
	}
}

// A literal has no width of its own: it takes the other operand's, and a
// chain keeps the width it has.
func TestArithmeticLiteralTakesTheOperandWidth(t *testing.T) {
	for expr, want := range map[string]string{
		"d + 1":       "DINT",
		"1 + d":       "DINT",
		"(d + 1) * 2": "DINT",
		"d * i + 1":   "DINT",
		"i - 1":       "INT",
		"40000 + 1":   "INT", // two literals: the generic integer
	} {
		t.Run(expr, func(t *testing.T) {
			src := "PROGRAM P\nVAR d : DINT; i : INT; s : STRING; END_VAR\ns := " + expr + ";\nEND_PROGRAM\n"
			wantCompileErr(t, compileErr(t, "p.st", src, ""), "cannot assign "+want+" to STRING")
		})
	}
}

// The FBD blocks lower to the ST operators, so they type the same way.
func TestArithmeticBlocksTakeTheCommonWidth(t *testing.T) {
	for _, c := range []struct{ call, want string }{
		{"ADD(a, b)", "DINT"},
		{"ADD(a, b, i)", "DINT"},
		{"SUB(a, b)", "DINT"},
		{"MUL(i, a)", "DINT"},
		{"DIV(a, b)", "DINT"},
		{"MOD(a, b)", "DINT"},
		{"ADD(i, u)", "DINT"},
		{"ADD(i, i)", "INT"},
	} {
		t.Run(c.call, func(t *testing.T) {
			src := "PROGRAM P\nVAR a, b : DINT; i : INT; u : UINT; s : STRING; END_VAR\nFBD\n  s := " + c.call + "\nEND_FBD\nEND_PROGRAM\n"
			wantCompileErr(t, compileErr(t, "p.fbd", src, ""), "cannot assign "+c.want+" to STRING")
		})
	}
}
