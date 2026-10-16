package project

import (
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// ST-003: a manifest tag with no type: is typed by its init:, and every program
// sees it without a VAR_EXTERNAL. A number init is a REAL, true/false a BOOL, a
// string a STRING. The programs below only compile, and only compute these
// values, if the init-derived types are those: REAL division does not
// truncate (50 / 8 is 6.25, not 6), NOT needs a BOOL, CONCAT needs a STRING.
func TestInitTypesAnImplicitTag(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": `tasks:
  - program: main.st
  - name: second
    program: second.st
tags:
  - { name: Echo,   role: state, type: REAL }
  - { name: Seed,   role: state, init: 50 }
  - { name: Armed,  role: state, init: false }
  - { name: Label,  role: state, init: 'PV' }
  - { name: Ratio,  role: state, type: REAL }
  - { name: Idle,   role: state, type: BOOL }
  - { name: Tagged, role: state, type: STRING }
`,
		"main.st": `PROGRAM Main
Ratio := Seed / 8;
Idle := NOT Armed;
Tagged := CONCAT(Label, '-1');
END_PROGRAM
`,
		// A second program names the same tags with no declaration either.
		"second.st": "PROGRAM Second\nEcho := Seed + 1.0;\nEND_PROGRAM\n",
	})
	rt.Scan()
	if err := rt.ScanTask("second"); err != nil {
		t.Fatal(err)
	}
	if got := readReal(t, rt, "Echo"); got != 51 {
		t.Errorf("Echo = %v, want 51: the second program sees Seed too", got)
	}
	for name, want := range map[string]ir.TypeKind{"Seed": ir.TypeReal, "Armed": ir.TypeBool, "Label": ir.TypeString} {
		v, err := rt.Tags().ReadGlobal(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if v.Kind != want {
			t.Errorf("%s holds %v, want %v (typed by its init:)", name, v.Kind, want)
		}
	}
	if got := readReal(t, rt, "Ratio"); got != 6.25 {
		t.Errorf("Ratio = %v, want 6.25: Seed (init: 50) is a REAL, so Seed / 8 does not truncate", got)
	}
	if v, _ := rt.Tags().ReadGlobal("Idle"); !v.B {
		t.Error("Idle = FALSE, want TRUE: Armed (init: false) is a BOOL, so NOT Armed is TRUE")
	}
	if v, _ := rt.Tags().ReadGlobal("Tagged"); v.S != "PV-1" {
		t.Errorf("Tagged = %q, want PV-1 (Label, init: 'PV', is a STRING)", v.S)
	}
}

// ST-014: a GVL global the manifest does not declare becomes a tag in the
// state role (not an input, output or setpoint), and a manifest-declared one
// keeps the role the manifest gave it.
func TestGVLUndeclaredGlobalBecomesAStateTag(t *testing.T) {
	proj, err := Load(mapProject(map[string]string{
		"nautilus.yaml": "tasks:\n  - program: a.st\ntags:\n  - { name: Level, role: input, type: REAL }\n",
		"gvl.st":        "VAR_GLOBAL\n    Count : DINT;\n    Level : REAL;\nEND_VAR\n",
		"a.st":          "PROGRAM A\nCount := Count + 1;\nEND_PROGRAM\n",
	}), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	roles := map[string]runtime.TagRole{}
	n := map[string]int{}
	for _, d := range proj.Runtime.Tags {
		roles[d.Name] = d.Role
		n[d.Name]++
	}
	if _, ok := roles["Count"]; !ok {
		t.Fatalf("the undeclared GVL global Count is not a tag: %v", roles)
	}
	if roles["Count"] != runtime.RoleState {
		t.Errorf("Count has role %v, want state", roles["Count"])
	}
	if roles["Level"] != runtime.RoleInput || n["Level"] != 1 {
		t.Errorf("Level (declared in the manifest) has role %v and %d definitions, want its manifest role input and one", roles["Level"], n["Level"])
	}
}

// ST-004: a VAR_EXTERNAL naming a tag typed only by its init: sets the tag's
// type (the init's implied type does not override it), and the init: must
// then be a value of the declared type. A number init fits an integer
// declaration; a boolean, a fraction or text that contradicts the declaration
// is an error naming the tag and both kinds, not silently accepted.
func TestDeclarationTypesAnInitOnlyTag(t *testing.T) {
	files := func(init, decl string) map[string]string {
		return map[string]string{
			"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Speed, role: state, init: " + init + " }\n",
			"main.st":       "PROGRAM Main\nVAR_EXTERNAL\n    Speed : " + decl + ";\nEND_VAR\nEND_PROGRAM\n",
		}
	}
	rt := loadRuntime(t, files("0", "DINT"))
	if v, _ := rt.Tags().ReadGlobal("Speed"); v.Kind != ir.TypeInt {
		t.Errorf("init: 0 with VAR_EXTERNAL DINT seeded %v, want an integer (the declaration)", v.Kind)
	}
	for _, tc := range []struct{ init, decl, want string }{
		{"true", "REAL", "tag Speed (declared REAL by a program): init: want REAL, got a boolean"},
		{"2.5", "DINT", "tag Speed (declared DINT by a program)"},
		{"0", "BOOL", "init: want BOOL, got a number"},
		{"'fast'", "REAL", "init: want REAL, got a string"},
	} {
		err := newRuntimeErr(t, files(tc.init, tc.decl))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("init: %s with VAR_EXTERNAL %s: err = %v, want it to contain %q", tc.init, tc.decl, err, tc.want)
		}
	}
}
