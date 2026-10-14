package runtime_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// Tag-model: "there are exactly four writers" — a driver input exists once
// the driver has delivered it (and not before), and the program assigning a
// tag (a coil write) creates it.
func TestDriverInputAndProgramCoilCreateTags(t *testing.T) {
	drv := &scriptDriver{in: nio.Values{"In": 3.0}}
	rt, err := runtime.New(runtime.Options{
		Program: `PROGRAM Main
VAR_EXTERNAL In : REAL; Out : REAL; Coil : BOOL; END_VAR
Out := In * 2.0;
Coil := TRUE;
END_PROGRAM`,
		Driver:  drv,
		Inputs:  []string{"In"},
		Outputs: []string{"Out"},
		Seed:    nio.Values{"Out": 0.0},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, name := range []string{"In", "Coil"} {
		if _, ok := rt.Tags().All()[name]; ok {
			t.Errorf("%s exists before any writer has written it", name)
		}
	}
	rt.Scan()
	if v, ok := rt.Tags().All()["In"]; !ok || v != 3.0 {
		t.Errorf("In = %v, %v after the driver delivered it, want 3", v, ok)
	}
	if v, ok := rt.Tags().All()["Coil"]; !ok || v != true {
		t.Errorf("Coil = %v, %v after the program assigned it, want true", v, ok)
	}
}

// Tag-model: Options.Tags composes with the flat Inputs/Outputs/Seed/Meta
// fields — both spellings land in one controller.
func TestTagDefsComposeWithFlatFields(t *testing.T) {
	drv := nio.NewMemory()
	_ = drv.WriteOutputs(nio.Values{"LevelPct": 30.0, "TempC": 55.0})
	rt, err := runtime.New(runtime.Options{
		Program: prog, Driver: drv, DtTag: "ScanDtS",
		// flat spelling
		Inputs:  []string{"LevelPct"},
		Outputs: []string{"PumpRun"},
		Seed:    nio.Values{"TempSP": 65.0, "Kp": 12.0},
		Meta:    map[string]runtime.TagMeta{"LevelPct": {Desc: "Tank level", Unit: "%"}},
		// role-first spelling, alongside it
		Tags: []runtime.TagDef{
			runtime.Input("TempC", runtime.Desc("Tank temperature"), runtime.Unit("C")),
			runtime.Output("Heater", runtime.Unit("%")),
			runtime.Setpoint("Ki", 0.15),
			runtime.Setpoint("PumpStartLevel", 40.0),
			runtime.Setpoint("PumpStopLevel", 75.0),
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	has := func(xs []string, s string) bool {
		for _, x := range xs {
			if x == s {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"LevelPct", "TempC"} {
		if !has(rt.Inputs(), n) {
			t.Errorf("Inputs() = %v, missing %s", rt.Inputs(), n)
		}
	}
	for _, n := range []string{"PumpRun", "Heater"} {
		if !has(rt.Outputs(), n) {
			t.Errorf("Outputs() = %v, missing %s", rt.Outputs(), n)
		}
	}
	if m := rt.Meta()["LevelPct"]; m.Desc != "Tank level" || m.Unit != "%" {
		t.Errorf("flat Meta lost: %+v", m)
	}
	if m := rt.Meta()["TempC"]; m.Desc != "Tank temperature" || m.Unit != "C" {
		t.Errorf("TagDef meta lost: %+v", m)
	}
	all := rt.Tags().All()
	for name, want := range map[string]any{"TempSP": 65.0, "Kp": 12.0, "Ki": 0.15, "PumpStartLevel": 40.0} {
		if all[name] != want {
			t.Errorf("seed %s = %v, want %v (flat Seed and Setpoint() both seed)", name, all[name], want)
		}
	}
	for i := 0; i < 20; i++ {
		rt.Scan()
	}
	out, _ := drv.ReadInputs()
	if out["PumpRun"] != true {
		t.Errorf("PumpRun = %v: the flat-bound output did not reach the driver", out["PumpRun"])
	}
	if h, _ := out["Heater"].(float64); h <= 0 {
		t.Errorf("Heater = %v: the TagDef-bound output did not reach the driver", out["Heater"])
	}
}

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Tag-model: a panic in an OnScan observer is recovered AND logged.
func TestOnScanPanicIsLogged(t *testing.T) {
	var sink syncBuf
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&sink, nil)))
	defer slog.SetDefault(prev)

	drv := nio.NewMemory()
	_ = drv.WriteOutputs(nio.Values{"TempC": 5.0})
	rt := onScanRuntime(t, drv, nil)
	rt.OnScan(func(*runtime.Tags) { panic("observer-boom-Pump1") })
	rt.Scan()

	got := sink.String()
	if !strings.Contains(got, "OnScan observer panicked") || !strings.Contains(got, "observer-boom-Pump1") {
		t.Errorf("the panic was not logged with its value; log output: %q", got)
	}
}

// Tag-model: Tags.ReadPath reads a dotted member path — scalar leaf or
// struct sub-tree — from inside an OnScan callback without deadlocking.
func TestOnScanObserverCanReadDottedPathsWithoutDeadlock(t *testing.T) {
	rt := setPathRuntime(t)
	type result struct {
		leaf    any
		leafOK  bool
		sub     any
		subOK   bool
		nested  any
		nestedO bool
	}
	done := make(chan result, 1)
	rt.OnScan(func(tg *runtime.Tags) {
		var r result
		r.leaf, r.leafOK = tg.ReadPath("P101.Speed")
		r.nested, r.nestedO = tg.ReadPath("P101.LVL.HSP")
		r.sub, r.subOK = tg.ReadPath("P101.LVL")
		done <- r
	})
	go rt.Scan()
	select {
	case r := <-done:
		if !r.leafOK || r.leaf != 42.0 {
			t.Errorf("ReadPath(P101.Speed) in OnScan = %v, %v; want 42", r.leaf, r.leafOK)
		}
		if !r.nestedO || r.nested != 80.0 {
			t.Errorf("ReadPath(P101.LVL.HSP) in OnScan = %v, %v; want 80", r.nested, r.nestedO)
		}
		sub, ok := r.sub.(ir.Value)
		if !r.subOK || !ok || sub.Kind != ir.TypeStruct || len(sub.Fld) != 2 || sub.Fld[0].F != 80 || sub.Fld[1].F != 20 {
			t.Errorf("ReadPath(P101.LVL) in OnScan = %+v, %v; want the Limits sub-tree {80 20}", r.sub, r.subOK)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an OnScan observer calling ReadPath on a dotted path deadlocked")
	}
}

// Tag-model: a member write keeps the leaf's type — a whole number written
// to a REAL member stays a REAL (the JSON number 50 is not an integer here).
func TestSetPathWholeNumberOnRealMemberStaysReal(t *testing.T) {
	rt := setPathRuntime(t)
	if err := rt.Tags().SetPath("P101.Speed", 50.0); err != nil {
		t.Fatal(err)
	}
	if err := rt.Tags().SetPath("P101.LVL.HSP", 90.0); err != nil {
		t.Fatal(err)
	}
	v, _ := rt.Tags().ReadGlobal("P101")
	speed := v.Fld[v.Struct.FieldIndex["Speed"]]
	if speed.Kind != ir.TypeReal || speed.F != 50 {
		t.Errorf("Speed = %+v, want REAL 50", speed)
	}
	hsp := v.Fld[v.Struct.FieldIndex["LVL"]].Fld[0]
	if hsp.Kind != ir.TypeReal || hsp.F != 90 {
		t.Errorf("LVL.HSP = %+v, want REAL 90", hsp)
	}
}
