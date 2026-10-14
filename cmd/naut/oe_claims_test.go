package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/internal/project"
	"github.com/joyautomation/nautilus/internal/stproject"
	"github.com/joyautomation/nautilus/runtime"
	"github.com/joyautomation/nautilus/server"
)

const rootTypesST = "(* DemoLine root-level types: a PROGRAM-less .st file *)\n" +
	"TYPE\n  Valve : STRUCT\n    Open : BOOL;\n  END_STRUCT;\nEND_TYPE\n"

const libHelperST = "(* lib/ helper *)\nFUNCTION_BLOCK Doubler\nVAR_INPUT IN : REAL; END_VAR\nVAR_OUTPUT OUT : REAL; END_VAR\nOUT := IN * 2.0;\nEND_FUNCTION_BLOCK\n"

const libScaleFBD = "FUNCTION_BLOCK Scale\nVAR_INPUT IN : REAL; END_VAR\nVAR_OUTPUT OUT : REAL; END_VAR\nFBD\n  OUT := MUL(IN, 3.0)\nEND_FBD\nEND_FUNCTION_BLOCK\n"

const rootTrimFBD = "FUNCTION_BLOCK Trim\nVAR_INPUT IN : REAL; END_VAR\nVAR_OUTPUT OUT : REAL; END_VAR\nFBD\n  OUT := SUB(IN, 1.0)\nEND_FBD\nEND_FUNCTION_BLOCK\n"

const libRungsLD = "FUNCTION_BLOCK Seal\nVAR_INPUT Start : BOOL; Stop : BOOL; END_VAR\nVAR_OUTPUT Run : BOOL; END_VAR\nLD\n  RUNG seal [ Start | Run ] /Stop ( Run )\nEND_LD\nEND_FUNCTION_BLOCK\n"

// Online edits: what a download sends is the library prelude — every
// PROGRAM-less .st file in the project root AND under lib/, verbatim, then
// the PROGRAM-less .ld/.fbd libraries (root and lib/) transpiled to ST —
// followed by the program file as written.
func TestComposeCoversRootLevelLibrariesAndBothGraphicalLibraries(t *testing.T) {
	prog := "PROGRAM Main\nVAR_EXTERNAL x : REAL; y : REAL; END_VAR\nVAR d : Doubler; END_VAR\nd(IN := x);\ny := d.OUT;\nEND_PROGRAM\n"
	dir := writeTree(t, map[string]string{
		"nautilus.yaml":  "tasks:\n  - program: main.st\ntags:\n  - { name: x, role: setpoint, init: 1.0 }\n  - { name: y, role: state, init: 0.0 }\n",
		"types.st":       rootTypesST,
		"lib/helpers.st": libHelperST,
		"lib/scale.fbd":  libScaleFBD,
		"lib/seal.ld":    libRungsLD,
		"trim.fbd":       rootTrimFBD,
		"main.st":        prog,
	})
	out, stderr, code := compose(t, "", "--json", filepath.Join(dir, "main.st"))
	if code != 0 {
		t.Fatalf("naut compose = %d: %s", code, stderr)
	}
	var res composeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	iRoot := strings.Index(res.Prelude, rootTypesST)
	iLib := strings.Index(res.Prelude, libHelperST)
	if iRoot < 0 || iLib < 0 {
		t.Fatalf("the .st libraries (root types.st and lib/helpers.st) must be in the prelude verbatim:\n%s", res.Prelude)
	}
	iScale := strings.Index(res.Prelude, "FUNCTION_BLOCK Scale")
	iSeal := strings.Index(res.Prelude, "FUNCTION_BLOCK Seal")
	iTrim := strings.Index(res.Prelude, "FUNCTION_BLOCK Trim")
	if iScale < 0 || iSeal < 0 || iTrim < 0 {
		t.Fatalf("the .fbd and .ld libraries (lib/scale.fbd, lib/seal.ld, root trim.fbd) are missing from the prelude:\n%s", res.Prelude)
	}
	if min(iScale, iSeal, iTrim) < max(iRoot, iLib) {
		t.Errorf("transpiled graphical libraries must follow every .st library:\n%s", res.Prelude)
	}
	if strings.Contains(res.Prelude, "RUNG") || strings.Contains(res.Prelude, "END_FBD") || strings.Contains(res.Prelude, "END_LD") {
		t.Errorf("graphical libraries should arrive as ST, not as rungs or netlists:\n%s", res.Prelude)
	}
	if res.Program != prog || res.Source != res.Prelude+prog {
		t.Errorf("the program must follow the prelude as written")
	}
	if _, err := runtime.Compile(res.Source); err != nil {
		t.Errorf("the controller would refuse the composed source: %v", err)
	}

	// One definition: the runtime and the language server compose the same
	// prelude that a download sends.
	proj, err := project.Load(os.DirFS(dir), "")
	if err != nil {
		t.Fatal(err)
	}
	if rtPrelude := stproject.Join(proj.Runtime.Libraries, ""); rtPrelude != res.Prelude {
		t.Errorf("the runtime's library prelude differs from a download's:\n--- runtime\n%s\n--- download\n%s", rtPrelude, res.Prelude)
	}
	if lspPrelude, _ := stproject.Prelude(filepath.Join(dir, "main.st"), nil); lspPrelude != res.Prelude {
		t.Errorf("the language server's prelude differs from a download's:\n--- lsp\n%s\n--- download\n%s", lspPrelude, res.Prelude)
	}
}

// controllerFor boots a controller from the project in dir (online edits on)
// and serves its API; it returns the host, port and runtime.
func controllerFor(t *testing.T, dir string) (host, port string, rt *runtime.Runtime) {
	t.Helper()
	proj, err := project.Load(os.DirFS(dir), "")
	if err != nil {
		t.Fatal(err)
	}
	rt, err = runtime.New(proj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(rt, server.Options{OnlineEdits: true}).Handler())
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u.Hostname(), u.Port(), rt
}

// Online edits: "Composition is a single definition shared by the runtime,
// the language server, download and pull, so a program round-trips
// losslessly." The program is edited locally, DOWNLOADED exactly as the
// editor does (naut compose, then PUT /api/program), the workspace copy is
// reverted, and PULL brings the edit back byte for byte — for an ST program
// and a ladder program (which the controller reports as the original .ld).
func TestDownloadThenPullRoundTripsLosslessly(t *testing.T) {
	const manifest = "tasks:\n  - program: %s\ntags:\n  - { name: x, role: setpoint, init: 1.0 }\n  - { name: y, role: state, init: 0.0 }\n  - { name: Hi, role: state, init: false }\n"
	cases := []struct {
		name, file, v1, v2 string
	}{
		{"st", "main.st",
			"PROGRAM Main\nVAR d : Doubler; END_VAR\nd(IN := x);\ny := d.OUT;\nEND_PROGRAM\n",
			"PROGRAM Main\n(* retuned in the field *)\nVAR d : Doubler; END_VAR\nd(IN := x + 1.0);\ny := d.OUT;\nEND_PROGRAM\n"},
		{"ld", "main.ld",
			"PROGRAM Main\nLD\n  RUNG r GT(x, 5.0) ( Hi )\nEND_LD\nEND_PROGRAM\n",
			"PROGRAM Main\nLD\n  RUNG r GT(x, 7.0) ( Hi )\nEND_LD\nEND_PROGRAM\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{
				"nautilus.yaml":  strings.Replace(manifest, "%s", tc.file, 1),
				"types.st":       rootTypesST,
				"lib/helpers.st": libHelperST,
				"lib/seal.ld":    libRungsLD,
				tc.file:          tc.v1,
			})
			host, port, rt := controllerFor(t, dir)

			// Edit locally, then download what the editor would send.
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.v2), 0o644); err != nil {
				t.Fatal(err)
			}
			out, stderr, code := compose(t, "", "--json", filepath.Join(dir, tc.file))
			if code != 0 {
				t.Fatalf("naut compose = %d: %s", code, stderr)
			}
			var res composeResult
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"source": res.Source})
			req, _ := http.NewRequest("PUT", "http://"+host+":"+port+"/api/program", bytes.NewReader(body))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("download (PUT /api/program) = %d", resp.StatusCode)
			}
			// The controller reports the original source it was sent (a .ld
			// program is reported as ladder, not as its transpilation).
			if got := rt.Program().Source(); got != res.Source {
				t.Errorf("the controller reports different source than the download sent:\n%s", got)
			}
			if tc.name == "ld" && !strings.Contains(rt.Program().Source(), "RUNG r GT(x, 7.0)") {
				t.Errorf("the controller does not report the original ladder source:\n%s", rt.Program().Source())
			}

			// The workspace reverts; pull restores the downloaded program exactly.
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.v1), 0o644); err != nil {
				t.Fatal(err)
			}
			if code := runPull([]string{"--host", host, "--port", port, "--dir", dir, "--check"}); code != 1 {
				t.Fatalf("pull --check before pulling = %d, want 1 (the edit is un-pulled)", code)
			}
			if code := runPull([]string{"--host", host, "--port", port, "--dir", dir}); code != 0 {
				t.Fatalf("pull = %d", code)
			}
			got, _ := os.ReadFile(filepath.Join(dir, tc.file))
			if string(got) != tc.v2 {
				t.Errorf("pull did not restore the downloaded program byte for byte:\n--- got\n%s\n--- want\n%s", got, tc.v2)
			}
			if code := runPull([]string{"--host", host, "--port", port, "--dir", dir, "--check"}); code != 0 {
				t.Errorf("pull --check after a lossless round trip = %d, want 0", code)
			}
		})
	}
}

// Online edits: pull writes a controller program with no local file into
// <POU>.fbd when it is an FBD program, and rewrites ONLY program files —
// generated type files and libraries are never touched.
func TestPullCreatesFBDFileAndLeavesTypeAndLibraryFilesAlone(t *testing.T) {
	typesGen := "(* generated by naut eip import — do not edit *)\n" + rootTypesST
	fbdProg := "PROGRAM Interlock\nVAR_EXTERNAL x : REAL; z : REAL; END_VAR\nFBD\n  w = ADD(x, 1.0)\n  z := w\nEND_FBD\nEND_PROGRAM\n"
	mainProg := "PROGRAM Main\nVAR_EXTERNAL x : REAL; y : REAL; END_VAR\ny := x + 1.0;\nEND_PROGRAM\n"
	rt, err := runtime.New(runtime.Options{
		Program:   mainProg,
		Libraries: []string{libHelperST, typesGen},
		Seed:      map[string]any{"x": 1.0, "y": 0.0, "z": 0.0},
		Tasks:     []runtime.Task{{Name: "interlock", Program: fbdProg}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(rt, server.Options{OnlineEdits: true}).Handler())
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	dir := writeProject(t, map[string]string{
		"types.st":   typesGen,
		"helpers.st": libHelperST,
		// A stale main program: pull must rewrite it.
		"program.st": "PROGRAM Main\nVAR_EXTERNAL x : REAL; y : REAL; END_VAR\ny := 0.0;\nEND_PROGRAM\n",
	})
	if code := runPull([]string{"--host", u.Hostname(), "--port", u.Port(), "--dir", dir}); code != 0 {
		t.Fatalf("pull = %d", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Interlock.fbd"))
	if err != nil {
		t.Fatalf("a new FBD program should land in Interlock.fbd: %v", err)
	}
	if string(got) != fbdProg {
		t.Errorf("Interlock.fbd = %q, want the controller's FBD program as written", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Interlock.st")); err == nil {
		t.Error("an FBD program was written as Interlock.st")
	}
	if prog, _ := os.ReadFile(filepath.Join(dir, "program.st")); string(prog) != mainProg {
		t.Errorf("program.st was not reconciled:\n%s", prog)
	}
	for name, want := range map[string]string{"types.st": typesGen, "helpers.st": libHelperST} {
		if raw, _ := os.ReadFile(filepath.Join(dir, name)); string(raw) != want {
			t.Errorf("pull rewrote %s, which is not a program file:\n%s", name, raw)
		}
	}
}
