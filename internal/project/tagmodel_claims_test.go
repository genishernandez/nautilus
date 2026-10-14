package project

// Assertions for the tag-model page's scope, typing and seeding rules that
// the older tests left open: the same promise in ladder, FBD and SFC
// programs, init:-implied TIME and STRING, a local read, a FUNCTION, GVL
// entries the manifest also declares, ARRAY OF a project TYPE, and the zero
// of a type checked value by value.

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

const implicitTagsManifest = `tasks:
  - program: main.%s
tags:
  - { name: Sensor,   role: input,    type: REAL, init: 20.0 }
  - { name: Setpoint, role: setpoint, init: 50.0 }
  - { name: Alarm,    role: output,   type: BOOL }
  - { name: Out,      role: state,    type: REAL }
`

// Tag-model: "A manifest tag needs no declaration in a PROGRAM — ST, ladder,
// FBD or SFC." Each program below names the tags with no VAR_EXTERNAL.
func TestManifestTagsAreImplicitInLadderFBDAndSFC(t *testing.T) {
	programs := map[string]string{
		"st":  "PROGRAM Main\nOut := Setpoint - Sensor;\nAlarm := Sensor > Setpoint;\nEND_PROGRAM\n",
		"ld":  "PROGRAM Main\nLD\n  RUNG hi GT(Sensor, Setpoint) ( Alarm )\nEND_LD\nEND_PROGRAM\n",
		"fbd": "PROGRAM Main\nFBD\n  w_err = SUB(Setpoint, Sensor)\n  Out := w_err\nEND_FBD\nEND_PROGRAM\n",
		"sfc": "PROGRAM Main\nSFC\n  INITIAL_STEP Track:\n    N  Update;\n  END_STEP\n  ACTION Update:\n    Out := Setpoint - Sensor;\n  END_ACTION\nEND_SFC\nEND_PROGRAM\n",
	}
	for ext, src := range programs {
		t.Run(ext, func(t *testing.T) {
			rt := loadRuntime(t, map[string]string{
				"nautilus.yaml": strings.Replace(implicitTagsManifest, "%s", ext, 1),
				"main." + ext:   src,
			})
			rt.Scan()
			switch ext {
			case "ld":
				if v, _ := rt.Tags().ReadGlobal("Alarm"); v.Kind != ir.TypeBool || v.B {
					t.Errorf("Alarm = %+v, want FALSE (sensor below setpoint)", v)
				}
				rt.Tags().SetReal("Sensor", 60)
				rt.Scan()
				if v, _ := rt.Tags().ReadGlobal("Alarm"); !v.B {
					t.Errorf("Alarm = %+v, want TRUE once the sensor exceeds the setpoint", v)
				}
			default:
				if got := readReal(t, rt, "Out"); got != 30 {
					t.Errorf("Out = %v, want 30 (Setpoint - Sensor read through the implicit tags)", got)
				}
			}
		})
	}
}

// Tag-model: with no type:, the type init: implies — T#5s a TIME, other
// text a STRING. A program that names them without a declaration only
// compiles if those are the types the runtime bound.
func TestInitImpliesTimeAndStringForImplicitTags(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": `tasks:
  - program: main.st
tags:
  - { name: Delay,    role: setpoint, init: T#5s }
  - { name: Label,    role: setpoint, init: Pump1 }
  - { name: DelayOut, role: state,    type: TIME }
  - { name: LabelOut, role: state,    type: STRING }
`,
		"main.st": "PROGRAM Main\nDelayOut := Delay + T#1s;\nLabelOut := Label;\nEND_PROGRAM\n",
	})
	rt.Scan()
	if v, _ := rt.Tags().ReadGlobal("Delay"); v.Kind != ir.TypeTime || v.I != 5000 {
		t.Errorf("Delay = %+v, want TIME 5000 ms", v)
	}
	if v, _ := rt.Tags().ReadGlobal("Label"); v.Kind != ir.TypeString || v.S != "Pump1" {
		t.Errorf("Label = %+v, want STRING Pump1", v)
	}
	if v, _ := rt.Tags().ReadGlobal("DelayOut"); v.Kind != ir.TypeTime || v.I != 6000 {
		t.Errorf("DelayOut = %+v, want TIME 6000 ms (Delay read as a TIME)", v)
	}
	if v, _ := rt.Tags().ReadGlobal("LabelOut"); v.S != "Pump1" {
		t.Errorf("LabelOut = %+v, want Pump1 (Label read as a STRING)", v)
	}
}

// Tag-model: a local shadows a tag — and a READ in that program sees the
// local, not the tag.
func TestLocalShadowIsWhatTheProgramReads(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Level, role: state, init: 5.0 }\n  - { name: Seen, role: state, init: -1.0 }\n",
		"main.st":       "PROGRAM Main\nVAR\n    Level : REAL := 42.0;\nEND_VAR\nSeen := Level;\nEND_PROGRAM\n",
	})
	rt.Scan()
	if got := readReal(t, rt, "Seen"); got != 42 {
		t.Errorf("Seen = %v, want 42 (the local), not 5 (the tag)", got)
	}
	if got := readReal(t, rt, "Level"); got != 5 {
		t.Errorf("tag Level = %v, want 5 (never read or written there)", got)
	}
}

// Tag-model: functions, like function blocks, do not see tags implicitly.
func TestFunctionsDoNotSeeTagsImplicitly(t *testing.T) {
	files := map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Level, role: state, init: 5.0 }\n  - { name: Out, role: state, init: 0.0 }\n",
		"lib.st":        "FUNCTION Peek : REAL\nVAR_INPUT\n    x : REAL;\nEND_VAR\nPeek := x + Level;\nEND_FUNCTION\n",
		"main.st":       "PROGRAM Main\nOut := Peek(1.0);\nEND_PROGRAM\n",
	}
	err := newRuntimeErr(t, files)
	if err == nil || !strings.Contains(err.Error(), `undeclared identifier "Level"`) {
		t.Fatalf("err = %v, want Level undeclared inside the function", err)
	}
	files["lib.st"] = "FUNCTION Peek : REAL\nVAR_INPUT\n    x : REAL;\nEND_VAR\nVAR_EXTERNAL\n    Level : REAL;\nEND_VAR\nPeek := x + Level;\nEND_FUNCTION\n"
	rt := loadRuntime(t, files)
	rt.Scan()
	if got := readReal(t, rt, "Out"); got != 6 {
		t.Errorf("Out = %v, want 6 (the function reached Level through its own VAR_EXTERNAL)", got)
	}
}

// Tag-model: a GVL global the manifest also declares is THAT tag, with the
// manifest supplying role, init, unit and desc; a global the manifest does
// not declare is a state tag at zero; VAR_GLOBAL CONSTANT entries are
// constants, not tags.
func TestGVLGlobalsManifestSuppliesMetadataAndConstantsAreNotTags(t *testing.T) {
	proj, err := Load(mapProject(map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: TankLevel, role: setpoint, init: 7.5, unit: m, desc: Tank level setpoint }\n",
		"gvl.st": "VAR_GLOBAL\n    TankLevel : REAL;\n    RunHours : DINT;\nEND_VAR\n" +
			"VAR_GLOBAL CONSTANT\n    MaxLevel : REAL := 90.0;\nEND_VAR\n",
		"main.st": "PROGRAM Main\nVAR_EXTERNAL\n    TankLevel : REAL;\n    RunHours : DINT;\nEND_VAR\nVAR\n    lim : REAL;\nEND_VAR\nlim := MaxLevel;\nRunHours := RunHours + 1;\nEND_PROGRAM\n",
	}), "")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(proj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	rt.Scan()
	if got := readReal(t, rt, "TankLevel"); got != 7.5 {
		t.Errorf("TankLevel = %v, want 7.5 (the manifest's init)", got)
	}
	m := rt.Meta()["TankLevel"]
	if m.Unit != "m" || m.Desc != "Tank level setpoint" {
		t.Errorf("TankLevel meta = %+v, want the manifest's unit and desc", m)
	}
	if v, err := rt.Tags().ReadGlobal("RunHours"); err != nil || v.Kind != ir.TypeInt || v.I != 1 {
		t.Errorf("RunHours = %+v, %v; want a state tag that started at zero and counted to 1", v, err)
	}
	if _, ok := rt.Tags().All()["MaxLevel"]; ok {
		t.Error("a VAR_GLOBAL CONSTANT became a tag")
	}
	if _, err := rt.Tags().ReadGlobal("MaxLevel"); err == nil {
		t.Error("a VAR_GLOBAL CONSTANT is readable as a tag")
	}
}

// isZero reports whether v is the zero of its type, member by member.
func isZero(v ir.Value) bool {
	switch v.Kind {
	case ir.TypeBool:
		return !v.B
	case ir.TypeReal:
		return v.F == 0
	case ir.TypeInt, ir.TypeTime:
		return v.I == 0
	case ir.TypeString:
		return v.S == ""
	case ir.TypeArray:
		for _, e := range v.Arr {
			if !isZero(e) {
				return false
			}
		}
		return true
	case ir.TypeStruct:
		for _, f := range v.Fld {
			if !isZero(f) {
				return false
			}
		}
		return true
	}
	return false
}

const claimsTypesST = `TYPE
  Limits : STRUCT
    Hi : REAL;
    On : BOOL;
  END_STRUCT;
  Motor : STRUCT
    Running : BOOL;
    Speed   : REAL;
    Count   : DINT;
    Name    : STRING;
    Hold    : TIME;
    Lim     : Limits;
    Steps   : ARRAY[1..3] OF REAL;
  END_STRUCT;
  E_Mode : (Idle, Run := 10, Fault);
END_TYPE
`

// Tag-model: type: takes an ARRAY[lo..hi] OF a project TYPE (a UDT or an
// enumeration), and a typed tag with no init: starts at the zero of its
// type — every member, element and nested member.
func TestArrayOfProjectTypesAndZeroOfTypeValueByValue(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": `tasks:
  - program: main.st
tags:
  - { name: Pumps,  role: setpoint, type: 'ARRAY[1..3] OF Motor' }
  - { name: Modes,  role: state,    type: 'ARRAY[0..1] OF E_Mode' }
  - { name: P101,   role: state,    type: Motor }
  - { name: Out,    role: state,    type: REAL }
`,
		"types.st": claimsTypesST,
		"main.st":  "PROGRAM Main\nOut := Pumps[2].Speed + P101.Lim.Hi;\nEND_PROGRAM\n",
	})
	rt.Scan()
	pumps, err := rt.Tags().ReadGlobal("Pumps")
	if err != nil {
		t.Fatal(err)
	}
	if pumps.Kind != ir.TypeArray || len(pumps.Arr) != 3 {
		t.Fatalf("Pumps = %+v, want an ARRAY of 3", pumps)
	}
	for i, e := range pumps.Arr {
		if e.Kind != ir.TypeStruct || e.Struct == nil || e.Struct.Name != "Motor" {
			t.Errorf("Pumps[%d] = %+v, want a Motor struct", i+1, e)
		}
	}
	modes, err := rt.Tags().ReadGlobal("Modes")
	if err != nil || modes.Kind != ir.TypeArray || len(modes.Arr) != 2 {
		t.Fatalf("Modes = %+v, %v; want an ARRAY of 2 enumerations", modes, err)
	}
	for _, name := range []string{"Pumps", "Modes", "P101"} {
		v, _ := rt.Tags().ReadGlobal(name)
		if !isZero(v) {
			t.Errorf("%s = %+v, want every stored value zero", name, v)
		}
	}
	p, _ := rt.Tags().ReadGlobal("P101")
	if len(p.Fld) != 7 || len(p.Fld[6].Arr) != 3 || len(p.Fld[5].Fld) != 2 {
		t.Errorf("P101 shape = %d members, steps %d, lim %d; want 7, 3, 2", len(p.Fld), len(p.Fld[6].Arr), len(p.Fld[5].Fld))
	}
}

// Tag-model: a UDT tag with no init: seeds to the zero of the type — every
// member FALSE/0/0.0 — and (companion) an ARRAY member seeds element by
// element from its lower bound, not from 0, when that bound is non-zero.
func TestStructDefaultSeedIsZeroMemberByMemberAndArrayLowerBound(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": `tasks:
  - program: main.st
tags:
  - { name: P101, role: state, type: Motor }
  - { name: P102, role: state, type: Motor, init: { Steps: [1.5, 2.5] } }
`,
		"types.st": claimsTypesST,
		"main.st":  "PROGRAM Main\nEND_PROGRAM\n",
	})
	p, _ := rt.Tags().ReadGlobal("P101")
	idx := func(v ir.Value, n string) ir.Value { i, _ := v.Struct.FieldOf(n); return v.Fld[i] }
	for _, n := range []string{"Running", "Speed", "Count", "Name", "Hold", "Lim", "Steps"} {
		if !isZero(idx(p, n)) {
			t.Errorf("P101.%s = %+v, want zero", n, idx(p, n))
		}
	}
	q, _ := rt.Tags().ReadGlobal("P102")
	steps := idx(q, "Steps")
	if len(steps.Arr) != 3 || steps.Arr[0].F != 1.5 || steps.Arr[1].F != 2.5 || steps.Arr[2].F != 0 {
		t.Errorf("Steps (ARRAY[1..3]) = %+v, want [1.5 2.5 0]", steps.Arr)
	}
	if err := rt.Tags().SetPath("P102.Steps[1]", 9.0); err != nil {
		t.Fatal(err)
	}
	q, _ = rt.Tags().ReadGlobal("P102")
	if s := idx(q, "Steps"); s.Arr[0].F != 9 || s.Arr[1].F != 2.5 {
		t.Errorf("Steps after writing [1] = %+v: the declared lower bound (1) is the first element", s.Arr)
	}
}
