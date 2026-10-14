package project

import (
	"testing"
	"testing/fstest"

	"github.com/joyautomation/nautilus/runtime"
)

// A PROGRAM-less .ld file and a PROGRAM-less .fbd file are both project
// libraries: their FUNCTION_BLOCKs join the prelude ahead of every task, and
// an ST, a ladder, an FBD and an SFC program each call both blocks. The ladder
// block's seal-in is retained per instance, so each task's call holds its
// own Run after the command drops.

const cLibManifest = `
name: libs-everywhere
tasks:
  - program: main.st
    scan: 100ms
  - name: ld
    program: rungs.ld
    scan: 100ms
  - name: fbd
    program: net.fbd
    scan: 100ms
  - name: sfc
    program: chart.sfc
    scan: 100ms
tags:
  - { name: Cmd,    role: setpoint, init: false }
  - { name: Lvl,    role: setpoint, init: 50.0 }
  - { name: X,      role: setpoint, init: 0.0 }
  - { name: StRun,  role: state, init: false }
  - { name: LdRun,  role: state, init: false }
  - { name: FbdRun, role: state, init: false }
  - { name: StDbl,  role: state, init: 0.0 }
  - { name: LdDbl,  role: state, init: 0.0 }
  - { name: FbdDbl, role: state, init: 0.0 }
  - { name: SfcRun, role: state, init: false }
  - { name: SfcDbl, role: state, init: 0.0 }
driver:
  type: memory
`

// cBlocksLD: a ladder library — one FUNCTION_BLOCK whose body is rungs.
const cBlocksLD = `FUNCTION_BLOCK PumpSeq
VAR_INPUT  Start : BOOL; Stop : BOOL; Level : REAL; END_VAR
VAR_OUTPUT Run : BOOL; END_VAR
LD
  RUNG seal  [ Start | Run ] /Stop GT(Level, 10.0) ( Run )
END_LD
END_FUNCTION_BLOCK`

// cDoublerFBD: an FBD library — one FUNCTION_BLOCK whose body is a netlist.
const cDoublerFBD = `FUNCTION_BLOCK Doubler
VAR_INPUT  IN : REAL; END_VAR
VAR_OUTPUT OUT : REAL; END_VAR
FBD
  OUT := MUL(IN, 2.0)
END_FBD
END_FUNCTION_BLOCK`

const cMainST = `PROGRAM MainSt
VAR_EXTERNAL Cmd : BOOL; Lvl : REAL; X : REAL; StRun : BOOL; StDbl : REAL; END_VAR
VAR p : PumpSeq; d : Doubler; END_VAR
p(Start := Cmd, Stop := FALSE, Level := Lvl);
StRun := p.Run;
d(IN := X);
StDbl := d.OUT;
END_PROGRAM`

const cRungsLD = `PROGRAM MainLd
VAR_EXTERNAL Cmd : BOOL; Lvl : REAL; X : REAL; LdRun : BOOL; LdDbl : REAL; END_VAR
VAR p : PumpSeq; d : Doubler; END_VAR
LD
  RUNG call  Cmd p:PumpSeq(Level := Lvl, Run => LdRun)
  RUNG dbl   d:Doubler(IN := X, OUT => LdDbl)
END_LD
END_PROGRAM`

const cNetFBD = `PROGRAM MainFbd
VAR_EXTERNAL Cmd : BOOL; Lvl : REAL; X : REAL; FbdRun : BOOL; FbdDbl : REAL; END_VAR
FBD
  p : PumpSeq(Start := Cmd, Stop := FALSE, Level := Lvl)
  FbdRun := p.Run
  d : Doubler(IN := X)
  FbdDbl := d.OUT
END_FBD
END_PROGRAM`

const cChartSFC = `PROGRAM MainSfc
VAR_EXTERNAL Cmd : BOOL; Lvl : REAL; X : REAL; SfcRun : BOOL; SfcDbl : REAL; END_VAR
VAR p : PumpSeq; d : Doubler; END_VAR
SFC

  INITIAL_STEP Running:
    N Call;
  END_STEP

  ACTION Call:
    p(Start := Cmd, Stop := FALSE, Level := Lvl);
    SfcRun := p.Run;
    d(IN := X);
    SfcDbl := d.OUT;
  END_ACTION

END_SFC
END_PROGRAM`

func TestLadderAndFBDLibrariesCalledFromEveryLanguage(t *testing.T) {
	fsys := fstest.MapFS{
		"nautilus.yaml": {Data: []byte(cLibManifest)},
		"blocks.ld":     {Data: []byte(cBlocksLD)},
		"doubler.fbd":   {Data: []byte(cDoublerFBD)},
		"main.st":       {Data: []byte(cMainST)},
		"rungs.ld":      {Data: []byte(cRungsLD)},
		"net.fbd":       {Data: []byte(cNetFBD)},
		"chart.sfc":     {Data: []byte(cChartSFC)},
	}
	libs, err := libraries(fsys)
	if err != nil {
		t.Fatalf("libraries: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("want 2 libraries (blocks.ld, doubler.fbd), got %d", len(libs))
	}
	proj, err := Load(fsys, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt, err := runtime.New(proj.Runtime)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	scanAll := func() {
		t.Helper()
		rt.Scan()
		for _, task := range []string{"ld", "fbd", "sfc"} {
			if err := rt.ScanTask(task); err != nil {
				t.Fatalf("scan %s: %v", task, err)
			}
		}
	}
	tags := rt.Tags()
	tags.SetBool("Cmd", true)
	tags.SetReal("X", 21)
	scanAll()
	for _, run := range []string{"StRun", "LdRun", "FbdRun", "SfcRun"} {
		if !tags.Bool(run) {
			t.Errorf("%s: the ladder library's block should have latched Run", run)
		}
	}
	for _, dbl := range []string{"StDbl", "LdDbl", "FbdDbl", "SfcDbl"} {
		if got := tags.Real(dbl); got != 42 {
			t.Errorf("%s = %v, want 42 (the FBD library's block)", dbl, got)
		}
	}
	// The command drops: every caller's instance holds its own seal-in.
	tags.SetBool("Cmd", false)
	scanAll()
	for _, run := range []string{"StRun", "LdRun", "FbdRun", "SfcRun"} {
		if !tags.Bool(run) {
			t.Errorf("%s: the block's seal-in should hold Run with the command released", run)
		}
	}
	// The level permissive inside the block drops every caller's Run.
	tags.SetReal("Lvl", 5)
	scanAll()
	for _, run := range []string{"StRun", "LdRun", "FbdRun", "SfcRun"} {
		if tags.Bool(run) {
			t.Errorf("%s: the block's level permissive should drop Run", run)
		}
	}
}
