package main

// The lang/l5x tests prove the reader reads. These prove the two files it
// writes are a nautilus project: `naut logix import` then `nautilus
// check`, with a program that actually binds the imported tags and their
// imported types. That is the same posture as lang/stgen — generate, then
// let the compiler have the last word — carried out to the CLI.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/lang/ld"
)

const varietyL5X = "../../lang/l5x/testdata/variety.L5X"

func TestLogixImportProducesACheckableProject(t *testing.T) {
	dir := t.TempDir()
	if code := runLogixImport([]string{"--out", dir, "--scope", "*", varietyL5X}); code != 0 {
		t.Fatalf("import exited %d", code)
	}
	types, err := os.ReadFile(filepath.Join(dir, "logix_types.st"))
	if err != nil {
		t.Fatal(err)
	}
	tags, err := os.ReadFile(filepath.Join(dir, "tags", "logix.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// A program that binds the controller's tags at the controller's own
	// types — the UDT nested three deep, and a predefined TIMER.
	out, code := checkIn(t, map[string]string{
		"nautilus.yaml": `
tasks:
  - program: program.st
tag-files:
  - tags/logix.yaml
tags:
  - { name: HiAlarm, role: state, init: false, desc: "Level above the high setpoint" }
`,
		"tags/logix.yaml": string(tags),
		"logix_types.st":  string(types),
		"program.st": `PROGRAM Main
VAR_EXTERNAL
    P101_Level : Analog_Input;
    DwellTmr   : TIMER;
    StartPB    : BOOL;
    HiAlarm    : BOOL;
END_VAR
HiAlarm := StartPB AND (P101_Level.EU >= P101_Level.Alarms.Hi) AND DwellTmr.DN;
END_PROGRAM`,
	})
	if code != 0 {
		t.Fatalf("an imported project did not check out (%d):\n%s", code, out)
	}
	if strings.Contains(out, ": error:") {
		t.Errorf("imported project produced errors:\n%s", out)
	}
}

// The descriptions are the reason to read an L5X at all: Logix keeps tag
// documentation in the offline project, so `naut eip import` from a
// live controller has to leave desc: empty (eip/codegen/tags.go).
func TestLogixImportRecoversDescriptions(t *testing.T) {
	dir := t.TempDir()
	if code := runLogixImport([]string{"--out", dir, varietyL5X}); code != 0 {
		t.Fatalf("import exited %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tags", "logix.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `desc: "HS-101 start pushbutton"`) {
		t.Errorf("tag file lost the controller's documentation:\n%s", raw)
	}
	// Controller scope by default, so a program's tags stay out.
	if strings.Contains(string(raw), "MainProgram_Scratch") {
		t.Errorf("default scope reached into a program:\n%s", raw)
	}
}

// graph emits exactly what `naut ld graph` emits for a .ld file, so
// the ladder preview and the revision diff consume a Logix routine without
// knowing it is one.
func TestLogixGraphEmitsTheLadderModel(t *testing.T) {
	out := captureStdout(t, func() int {
		return runLogixGraph([]string{varietyL5X, "MainRoutine"})
	})
	var m ld.Model
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not a ladder model: %v\n%s", err, out)
	}
	if len(m.Rungs) != 6 {
		t.Fatalf("rungs = %d, want 6", len(m.Rungs))
	}
	first := m.Rungs[0]
	if first.Line != 147 || first.Coils[0].Ref != "RunCmd" {
		t.Errorf("first rung = %+v", first)
	}
}

func TestLogixGraphReportsErrorsAsJSON(t *testing.T) {
	out := captureStdout(t, func() int {
		return runLogixGraph([]string{varietyL5X, "NoSuchRoutine"})
	})
	var msg struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &msg); err != nil || msg.Error == "" {
		t.Fatalf("want {\"error\": ...} like `naut ld graph`, got: %s", out)
	}
}

// Drift detection, in one command: two exports of the same project are the
// same project even though every export stamps a new ExportDate.
func TestLogixNormalizeChecksDrift(t *testing.T) {
	const a = "../../lang/l5x/testdata/demoline.L5X"
	const b = "../../lang/l5x/testdata/demoline.v80.L5X"
	if code := runLogixNormalize([]string{"--check", a, a}); code != 0 {
		t.Errorf("an export should match itself, exited %d", code)
	}
	if code := runLogixNormalize([]string{"--check", b, a}); code != 1 {
		t.Errorf("a changed setpoint should be reported as drift, exited %d", code)
	}
}

func TestLogixNormalizeWritesAPinnedExport(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "pinned.L5X")
	if code := runLogixNormalize([]string{"-o", dst, varietyL5X}); code != 0 {
		t.Fatalf("normalize exited %d", code)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Thu Sep 17 08:14:03 2026") {
		t.Errorf("ExportDate survived normalization:\n%s", firstLine(string(raw)))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// captureStdout runs fn with stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func() int) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	r.Close()
	return sb.String()
}

// --project writes the export as a whole nautilus project: manifest,
// tags, the program as ladder source — and the project checks clean for
// the Logix target, which is the point of a brownfield import.
func TestLogixImportProject(t *testing.T) {
	dir := t.TempDir()
	var code int
	out := captureStdout(t, func() int {
		code = runLogixImport([]string{"--project", dir, filepath.Join("..", "..", "lang", "l5x", "testdata", "demoline.L5X")})
		return code
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	// The summary counts in words: one routine is not "1 ladder routines".
	if want := "1 ladder routine, 1 complete; 2 rungs, 2 carried\n"; !strings.Contains(out, want) {
		t.Errorf("summary: want %q in\n%s", want, out)
	}
	for _, name := range []string{"nautilus.yaml", "tags/logix.yaml", "MainProgram.ld"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	src, _ := os.ReadFile(filepath.Join(dir, "MainProgram.ld"))
	if !strings.Contains(string(src), "[ StartPB | RunCmd ] /StopPB ( RunCmd )") {
		t.Errorf("program:\n%s", src)
	}
	if code := runCheck([]string{"--target", "logix", dir}); code != 0 {
		t.Errorf("naut check --target logix exit %d", code)
	}
}

// LGA-053: the round trip with the rack. `naut logix import --project`
// of an export with I/O modules writes hardware.L5X and names it in the
// manifest; `naut logix write` of the project carries the export's
// <Modules> back unchanged with the alias tags on them; a module the
// hardware lacks stops the write naming it, and a hardware file that
// cannot be read stops it too.
func TestLogixImportHardwareRoundTrip(t *testing.T) {
	export := filepath.Join("..", "..", "lang", "l5x", "testdata", "ioline.L5X")
	dir := t.TempDir()
	var code int
	captureStdout(t, func() int {
		code = runLogixImport([]string{"--project", dir, export})
		return code
	})
	if code != 0 {
		t.Fatalf("import: exit %d", code)
	}
	man, _ := os.ReadFile(filepath.Join(dir, "nautilus.yaml"))
	if !strings.Contains(string(man), "hardware: hardware.L5X") {
		t.Fatalf("manifest:\n%s", man)
	}
	out := filepath.Join(t.TempDir(), "back.L5X")
	prog := filepath.Join(dir, "MainProgram.ld")
	msg, code := captureStderr(t, func() int { return runLogixWrite([]string{"-o", out, prog}) })
	if code != 0 {
		t.Fatalf("write: exit %d\n%s", code, msg)
	}
	if strings.Contains(msg, "warning") {
		t.Errorf("a project with hardware should not warn about its Local module:\n%s", msg)
	}
	orig, err := l5x.ParseFile(export)
	if err != nil {
		t.Fatal(err)
	}
	back, err := l5x.ParseFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Controller.ModulesXML != strings.ReplaceAll(orig.Controller.ModulesXML, "\r\n", "\n") {
		t.Errorf("<Modules> did not survive the round trip:\n%s", back.Controller.ModulesXML)
	}
	alias := map[string]string{}
	for _, tg := range back.Controller.Tags {
		alias[tg.Name] = tg.AliasFor
	}
	for _, tg := range orig.Controller.Tags {
		if tg.AliasFor != "" && alias[tg.Name] != tg.AliasFor {
			t.Errorf("%s aliases %q, want %q", tg.Name, alias[tg.Name], tg.AliasFor)
		}
	}

	// An alias moved to a slot the rack does not have.
	tags := filepath.Join(dir, "tags", "logix.yaml")
	tf, _ := os.ReadFile(tags)
	if err := os.WriteFile(tags, []byte(strings.Replace(string(tf), `"Local:2:O.Data.0"`, `"Local:5:O.Data.0"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := captureStdout(t, func() int {
		_, code = captureStderr(t, func() int { return runLogixWrite([]string{"-o", out, prog}) })
		return code
	})
	if code != 1 || !strings.Contains(stdout, "RunCmd is an alias of Local:5:O.Data.0, and hardware.L5X has no module in slot 5 of the controller's chassis (Local)") || !strings.Contains(stdout, "[logix/io-module]") {
		t.Errorf("exit %d\n%s", code, stdout)
	}

	// A hardware file that is not one.
	if err := os.WriteFile(filepath.Join(dir, "hardware.L5X"), []byte("<RSLogix5000Content><Controller Name=\"X\"/></RSLogix5000Content>"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, code = captureStderr(t, func() int { return runLogixWrite([]string{"-o", out, prog}) })
	if code != 2 || !strings.Contains(msg, "target.logix.hardware: hardware.L5X: no <Modules> section") {
		t.Errorf("exit %d\n%s", code, msg)
	}
}
