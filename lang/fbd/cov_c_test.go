package fbd

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// A read that crosses a network boundary — a named wire or an FB output
// from another network — draws as a variable box IN the reading network,
// and no edge of the diagram joins nodes of two different networks.
func TestCrossNetworkReadsDrawAsBoxesInTheReadingNetwork(t *testing.T) {
	src := `PROGRAM Nets
VAR_EXTERNAL A : BOOL; B : BOOL; C : BOOL; X : BOOL; Y : BOOL; Z : BOOL; END_VAR
FBD
  NETWORK 'make'
  w = AND(A, B)
  c1 : CTU(CU := w, R := FALSE, PV := 2)
  X := w
  NETWORK 'read'
  Y := OR(w, C)
  Z := c1.Q
END_FBD
END_PROGRAM`
	m := mustGraph(t, src)
	if len(m.Networks) != 2 {
		t.Fatalf("networks: %s", mustJSON(m.Networks))
	}
	// The wire w and the instance output c1.Q are read in network 2: each
	// is a variable box there.
	for _, id := range []string{"v:w", "v:c1.Q"} {
		n := m.node(t, id)
		if n.Kind != "input" || n.Net != 2 {
			t.Errorf("%s: kind %q in network %d, want a variable box (input) in network 2", id, n.Kind, n.Net)
		}
	}
	if n := m.node(t, "b:w.w"); n.Net != 1 {
		t.Errorf("the wire's own block is in network %d, want 1", n.Net)
	}
	// No line crosses bands: every edge joins two nodes of one network.
	net := map[string]int{}
	for _, n := range m.Nodes {
		net[n.ID] = n.Net
	}
	for _, e := range m.Edges {
		if net[e.From] != net[e.To] {
			t.Errorf("edge %s -> %s crosses from network %d to %d", e.From, e.To, net[e.From], net[e.To])
		}
	}
	// Within one network the same wire is a line, not a box.
	if !hasEdge(m, "b:w.w", "c:X") {
		t.Error("network 1's own read of w should be a wire from its block")
	}
}

func hasEdge(m *Model, from, to string) bool {
	for _, e := range m.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

// A body with no NETWORK line is one network — the program it always was:
// it reports no networks, transpiles to the same ST as the same statements
// under one NETWORK line, and runs the same (the FB call is sorted ahead of
// the statement reading its output, so the reader sees the edge in the scan
// it happens; split into two networks it would see it a scan late).
func TestBodyWithoutNetworkLinesIsOneNetwork(t *testing.T) {
	body := `  Early := r.Q
  r : R_TRIG(CLK := A)
`
	wrap := func(b string) string {
		return "PROGRAM Nets\nVAR_EXTERNAL A : BOOL; Early : BOOL; END_VAR\nFBD\n" + b + "END_FBD\nEND_PROGRAM"
	}
	none := wrap(body)
	one := wrap("  NETWORK 'all'\n" + body)
	two := wrap("  NETWORK 'reader'\n  Early := r.Q\n  NETWORK 'edge'\n  r : R_TRIG(CLK := A)\n")

	if m := mustGraph(t, none); len(m.Networks) != 0 {
		t.Fatalf("a body without NETWORK lines reports %d networks", len(m.Networks))
	}
	stNone, stOne := mustTranspile(none), mustTranspile(one)
	if strings.HasPrefix(stNone, "TRANSPILE ERROR") || stNone != stOne {
		t.Fatalf("no NETWORK lines and one NETWORK line should be the same program:\n--- none ---\n%s\n--- one ---\n%s", stNone, stOne)
	}

	firstScan := func(src string) bool {
		t.Helper()
		sc := newScanner(t, src)
		sc.scan(0, map[string]ir.Value{"A": ir.BoolVal(true)})
		return sc.h.vals["Early"].B
	}
	if !firstScan(none) {
		t.Fatal("no NETWORK lines: the call runs before its reader, so Early is TRUE on the edge's scan")
	}
	if !firstScan(one) {
		t.Fatal("one NETWORK: Early is TRUE on the edge's scan")
	}
	if firstScan(two) {
		t.Fatal("two networks (the control): the reader runs first and sees last scan's Q")
	}
}
