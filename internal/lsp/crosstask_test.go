package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #327: the editor explains a reference to another task's instance as
// `naut check` and the runtime do.
func TestCrossTaskInstanceDiagnostic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nautilus.yaml", "tasks:\n  - program: owner.st\n  - name: alarms\n    program: reader.st\ntags:\n  - { name: Run, role: state, init: false }\n  - { name: Done, role: state, init: false }\n")
	write("owner.st", "PROGRAM Owner\nVAR t1 : TON; END_VAR\nt1(IN := Run, PT := T#5S);\nEND_PROGRAM\n")
	write("reader.st", "PROGRAM Reader\nDone := t1.Q;\nEND_PROGRAM\n")

	s := startSession(t)
	s.recvResponse(s.send("initialize", map[string]any{}, true))
	s.send("initialized", map[string]any{}, false)

	_, diags := s.openAndDiagnose(filepath.Join(dir, "reader.st"), "iec-st")
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v, want one", diags)
	}
	const want = `undeclared identifier "t1": t1 is a local of program Owner (task main), and one program cannot read another's variables. Tasks share only tags: in Owner, write the value to a tag (an assignment or a coil) and read that tag here`
	if d := diags[0]; !strings.Contains(d.Message, want) || d.Range.Start.Line != 1 || d.Range.Start.Character != 8 {
		t.Fatalf("diagnostic = %+v, want %q at 2:9", d, want)
	}
}
