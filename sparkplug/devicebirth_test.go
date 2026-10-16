// devicebirth_test.go: a device's DBIRTH waits for its connection's first
// read to reach the tag store. A field driver connects before its first poll
// completes; a DBIRTH sent in between carried no metrics ("device birth
// device=RTU53 metrics=0"), and the tags landing a moment later rebirthed
// the whole node.

package sparkplug

import (
	"strings"
	"testing"
	"time"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

func TestDeviceBirthWaitsForFirstRead(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: "PROGRAM P\nVAR_EXTERNAL\n  Level : REAL;\nEND_VAR\nEND_PROGRAM",
		Driver:  nio.NewMemory(),
		Scan:    50 * time.Millisecond,
		Tags:    []runtime.TagDef{runtime.Input("Level")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.Tags().Snapshot()["Level"]; ok {
		t.Skip("the runtime seeds inputs before a read; nothing to wait for")
	}
	connected := true
	n, err := New(rt, Config{GroupID: "g", EdgeNode: "e"},
		WithDevice(Device{ID: "RTU53", Tags: []string{"Level"}, Health: func() bool { return connected }}))
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{open: true}
	n.cli = fc
	if err := n.birth(); err != nil {
		t.Fatal(err)
	}
	if got := len(fc.published("DBIRTH")); got != 0 {
		t.Fatalf("%d DBIRTH before the first read, want 0", got)
	}
	tickWithin(t, n, 2*time.Second)
	if got := len(fc.published("DBIRTH")); got != 0 {
		t.Fatalf("%d DBIRTH on a tick before the first read, want 0", got)
	}

	// The first read lands.
	if err := rt.Tags().WriteGlobal("Level", ir.RealVal(4.5)); err != nil {
		t.Fatal(err)
	}
	tickWithin(t, n, 2*time.Second)
	db := fc.published("DBIRTH")
	if len(db) != 1 {
		t.Fatalf("%d DBIRTH after the first read, want 1", len(db))
	}
	p, err := DecodePayload(db[0].payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Metrics) != 1 || p.Metrics[0].Name != "Level" || p.Metrics[0].Value != 4.5 {
		t.Errorf("DBIRTH metrics = %+v, want Level 4.5", p.Metrics)
	}
	// …and the node did not rebirth to make room for it.
	time.Sleep(700 * time.Millisecond) // past the 500 ms rebirth debounce
	tickWithin(t, n, 2*time.Second)
	if got := len(fc.published("NBIRTH")); got != 1 {
		t.Errorf("%d NBIRTH, want 1 — the device's tags arriving rebirthed the node", got)
	}

	// A dead connection still kills the device, data or no data.
	connected = false
	tickWithin(t, n, 2*time.Second)
	if got := len(fc.published("DDEATH")); got != 1 {
		t.Errorf("%d DDEATH after the connection dropped, want 1", got)
	}
}

func TestDeviceReady(t *testing.T) {
	has := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	up := func() bool { return true }
	down := func() bool { return false }
	cases := []struct {
		name string
		d    Device
		has  func(string) bool
		want bool
	}{
		{"no tags, healthy", Device{ID: "a", Health: up}, has(), true},
		{"no tags, nil health", Device{ID: "a"}, has(), true},
		{"down", Device{ID: "a", Tags: []string{"X"}, Health: down}, has("X"), false},
		{"up, nothing read", Device{ID: "a", Tags: []string{"X", "Y"}, Health: up}, has(), false},
		{"up, partly read", Device{ID: "a", Tags: []string{"X", "Y"}, Health: up}, has("Y"), false},
		{"up, partly read, waited", Device{ID: "a", Tags: []string{"X", "Y"}, Health: up}, has("Y"), true},
		{"up, nothing read, waited", Device{ID: "a", Tags: []string{"X", "Y"}, Health: up}, has(), false},
		{"up, all read", Device{ID: "a", Tags: []string{"X", "Y"}, Health: up}, has("X", "Y"), true},
	}
	for _, c := range cases {
		if got := deviceReady(c.d, c.has, strings.HasSuffix(c.name, "waited")); got != c.want {
			t.Errorf("%s: deviceReady = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDeviceBirthWaitsForEveryTag — the field case: the driver is primed but
// the tag store, a runtime scan behind it, has only some of the device's
// tags (the slow struct tags, read member by member, land last). The DBIRTH
// waits for the rest and carries all of them; a tag that never arrives
// holds the device back only for the wait, then it is born without it.
func TestDeviceBirthWaitsForEveryTag(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: "PROGRAM P\nVAR_EXTERNAL\n  Level : REAL;\n  Flow : REAL;\n  Spare : REAL;\nEND_VAR\nEND_PROGRAM",
		Driver:  nio.NewMemory(),
		Scan:    50 * time.Millisecond,
		Tags:    []runtime.TagDef{runtime.Input("Level"), runtime.Input("Flow"), runtime.Input("Spare")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.Tags().Snapshot()["Level"]; ok {
		t.Skip("the runtime seeds inputs before a read; nothing to wait for")
	}
	n, err := New(rt, Config{GroupID: "g", EdgeNode: "e"},
		WithDevice(Device{ID: "RTU53", Tags: []string{"Level", "Flow"}, Health: func() bool { return true }}),
		WithDevice(Device{ID: "RTU54", Tags: []string{"Spare", "Never"}, Health: func() bool { return true }}))
	if err != nil {
		t.Fatal(err)
	}
	n.birthWait = 300 * time.Millisecond
	fc := &fakeClient{open: true}
	n.cli = fc
	if err := n.birth(); err != nil {
		t.Fatal(err)
	}
	dbirths := func() map[string][]string {
		out := map[string][]string{}
		for _, p := range fc.published("DBIRTH") {
			pl, err := DecodePayload(p.payload)
			if err != nil {
				t.Fatal(err)
			}
			out[p.topic[strings.LastIndex(p.topic, "/")+1:]] = metricNames(pl.Metrics)
		}
		return out
	}

	// Part of each device's tags is in.
	rt.Tags().WriteGlobal("Level", ir.RealVal(4.5))
	rt.Tags().WriteGlobal("Spare", ir.RealVal(1))
	tickWithin(t, n, 2*time.Second)
	if got := dbirths(); len(got) != 0 {
		t.Fatalf("DBIRTH with only some tags in the store: %v", got)
	}
	// The rest of RTU53 lands: born whole, at once.
	rt.Tags().WriteGlobal("Flow", ir.RealVal(7))
	tickWithin(t, n, 2*time.Second)
	if got := dbirths()["RTU53"]; strings.Join(got, ",") != "Flow,Level" {
		t.Fatalf("RTU53 DBIRTH = %v, want Flow and Level", got)
	}
	if _, ok := dbirths()["RTU54"]; ok {
		t.Fatal("RTU54 born before its wait ran out")
	}
	// RTU54's second tag never reads: after the wait, born without it.
	time.Sleep(350 * time.Millisecond)
	tickWithin(t, n, 2*time.Second)
	if got := dbirths()["RTU54"]; strings.Join(got, ",") != "Spare" {
		t.Errorf("RTU54 DBIRTH after the wait = %v, want Spare", got)
	}
	if got := len(fc.published("NBIRTH")); got != 1 {
		t.Errorf("%d NBIRTH, want 1", got)
	}
}
