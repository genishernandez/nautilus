package project

import (
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// Getting started, "Make it yours": swap the simulated plant for real field
// I/O — delete the sim task and point driver: at the bus — and the control
// logic, byte for byte, reads the same tags either way.
const swapControlST = `PROGRAM Control
IF LevelPct <= PumpStartLevel THEN PumpRun := TRUE;
ELSIF LevelPct >= PumpStopLevel THEN PumpRun := FALSE; END_IF;
Heater := LIMIT(0.0, Kp * (TempSP - TempC), 100.0);
END_PROGRAM
`

const swapSimST = `PROGRAM Plant
LevelPct := 30.0;
TempC := 55.0;
END_PROGRAM
`

const swapControlTags = `  - { name: TempSP,         role: setpoint, init: 65.0 }
  - { name: Kp,             role: setpoint, init: 4.0 }
  - { name: PumpStartLevel, role: setpoint, init: 40.0 }
  - { name: PumpStopLevel,  role: setpoint, init: 75.0 }
  - { name: PumpRun,        role: output,   init: false }
  - { name: Heater,         role: output,   init: 0.0 }
`

func TestControlLogicUnchangedWhenSimIsSwappedForDriverInputs(t *testing.T) {
	// Before: a sim task owns the process values.
	simProj, err := Load(mapProject(map[string]string{
		"nautilus.yaml": "tasks:\n  - program: control.st\n  - name: sim\n    program: sim.st\ntags:\n" +
			"  - { name: LevelPct, role: state, init: 60.0 }\n  - { name: TempC, role: state, init: 60.0 }\n" +
			swapControlTags + "driver:\n  type: memory\n",
		"control.st": swapControlST,
		"sim.st":     swapSimST,
	}), "")
	if err != nil {
		t.Fatal(err)
	}
	simRT, err := runtime.New(simProj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	simRT.Scan()
	if err := simRT.ScanTask("sim"); err != nil {
		t.Fatal(err)
	}
	simRT.Scan() // control now sees the plant's values

	// After: the sim task is deleted, the process values are field inputs, and
	// the driver delivers the same numbers. control.st is the identical file.
	fieldProj, err := Load(mapProject(map[string]string{
		"nautilus.yaml": "tasks:\n  - program: control.st\ntags:\n" +
			"  - { name: LevelPct, role: input, type: REAL }\n  - { name: TempC, role: input, type: REAL }\n" +
			swapControlTags + "driver:\n  type: memory\n",
		"control.st": swapControlST,
	}), "")
	if err != nil {
		t.Fatal(err)
	}
	mem, ok := fieldProj.Runtime.Driver.(*nio.Memory)
	if !ok {
		t.Fatalf("memory driver is %T, want *io.Memory", fieldProj.Runtime.Driver)
	}
	if err := mem.WriteOutputs(nio.Values{"LevelPct": 30.0, "TempC": 55.0}); err != nil {
		t.Fatal(err)
	}
	fieldRT, err := runtime.New(fieldProj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	fieldRT.Scan()

	for _, name := range []string{"PumpRun", "Heater", "LevelPct", "TempC"} {
		a, err := simRT.Tags().ReadGlobal(name)
		if err != nil {
			t.Fatalf("sim project: %v", err)
		}
		b, err := fieldRT.Tags().ReadGlobal(name)
		if err != nil {
			t.Fatalf("driver project: %v", err)
		}
		if a.Kind != b.Kind || a.B != b.B || a.F != b.F {
			t.Errorf("%s: with the sim = %+v, with driver inputs = %+v; the same logic must read the same tags", name, a, b)
		}
	}
	if h, _ := fieldRT.Tags().ReadGlobal("Heater"); h.Kind != ir.TypeReal || h.F != 40 {
		t.Errorf("Heater = %+v, want 40 (Kp 4 x (65 - 55)): the control logic actually ran on the driver's inputs", h)
	}
	if p, _ := fieldRT.Tags().ReadGlobal("PumpRun"); !p.B {
		t.Errorf("PumpRun = %+v, want true (level 30 below the 40 start level)", p)
	}
}
