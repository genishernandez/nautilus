package sparkplug

import (
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// Tag-model, "A scalar seed takes the program's type": a seeded integer tag
// is born to a Sparkplug host as an Int64 and a REAL as a Double — read off
// the NBIRTH the node actually publishes, not off a bare value conversion.
// The seeds are the plain `init: 0` / `init: 65` a manifest hands the runtime.
func TestNBIRTHBornTypesOfSeededIntegerAndRealTags(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: `PROGRAM Main
VAR_EXTERNAL
    Counter : DINT;
    TempSP  : REAL;
END_VAR
Counter := Counter + 1;
END_PROGRAM`,
		Driver: nio.NewMemory(),
		Tags: []runtime.TagDef{
			runtime.State("Counter", 0),
			runtime.Setpoint("TempSP", 65),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(rt, Config{GroupID: "DemoLine", EdgeNode: "seed-types"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{open: true}
	n.cli = fc
	if err := n.birth(); err != nil {
		t.Fatal(err)
	}
	births := fc.published("NBIRTH")
	if len(births) != 1 {
		t.Fatalf("published %d NBIRTH, want 1", len(births))
	}
	pl, err := DecodePayload(births[0].payload)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]spb.DataType{}
	for _, m := range pl.Metrics {
		got[m.Name] = m.Datatype
	}
	if got["Counter"] != spb.DataType_Int64 {
		t.Errorf("Counter (seeded 0, declared DINT) born as %v, want Int64", got["Counter"])
	}
	if got["TempSP"] != spb.DataType_Double {
		t.Errorf("TempSP (seeded 65, declared REAL) born as %v, want Double", got["TempSP"])
	}
}
