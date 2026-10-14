package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/joyautomation/nautilus/internal/project"
)

// scaffoldInto writes sc under a fresh temp dir and returns the project dir.
func scaffoldInto(t *testing.T, sc scaffold) string {
	t.Helper()
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := write(&sc); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, sc.Name)
}

// Getting started: the tour is three tasks in three IEC languages and a
// simulated plant — read off the scaffolded manifest.
func TestScaffoldTourDeclaresThreeTasksInThreeLanguages(t *testing.T) {
	dir := scaffoldInto(t, scaffold{Name: "tour", Program: "Tour", Template: tmplDemo})
	m, err := project.ReadManifest(os.DirFS(dir), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tasks) != 3 {
		t.Fatalf("the tour declares %d tasks, want 3: %+v", len(m.Tasks), m.Tasks)
	}
	langs := map[string]bool{}
	var sim bool
	for _, task := range m.Tasks {
		langs[filepath.Ext(task.Program)] = true
		if task.Name == "sim" && task.Program == "sim.st" {
			sim = true
		}
	}
	for _, ext := range []string{".fbd", ".st", ".ld"} {
		if !langs[ext] {
			t.Errorf("no task is written in %s: %v", ext, langs)
		}
	}
	if !sim {
		t.Error("the plant is not simulated by a sim task (sim.st)")
	}
}

// Getting started: minimal is one task and one program, with its acceptance
// tests in one *_test.yaml.
func TestScaffoldMinimalIsOneTaskOneProgramOneTestFile(t *testing.T) {
	for _, lang := range []string{"st", "fbd", "ld"} {
		t.Run(lang, func(t *testing.T) {
			dir := scaffoldInto(t, scaffold{Name: "min", Program: "Min", Template: tmplMinimal, Language: lang})
			m, err := project.ReadManifest(os.DirFS(dir), "")
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Tasks) != 1 || m.Tasks[0].Program != "program."+lang {
				t.Fatalf("minimal tasks = %+v, want one task running program.%s", m.Tasks, lang)
			}
			var progs, suites []string
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				switch ext := filepath.Ext(e.Name()); {
				case strings.HasSuffix(e.Name(), "_test.yaml"):
					suites = append(suites, e.Name())
				case ext == ".st" || ext == ".fbd" || ext == ".ld" || ext == ".sfc":
					progs = append(progs, e.Name())
				}
			}
			if len(progs) != 1 || len(suites) != 1 {
				t.Errorf("minimal holds programs %v and test files %v, want one of each", progs, suites)
			}
		})
	}
}

// Getting started: the scaffolded CI gates on naut check, naut test and
// naut build (the manifest templates).
func TestScaffoldedManifestCIGatesOnCheckTestBuild(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
	}{{"tour", tmplDemo}, {"minimal", tmplMinimal}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := scaffoldInto(t, scaffold{Name: tc.name, Program: "P", Template: tc.template, CI: true})
			raw, err := os.ReadFile(filepath.Join(dir, ".github/workflows/ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			var wf struct {
				Jobs map[string]struct {
					Steps []struct {
						Run string `yaml:"run"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &wf); err != nil {
				t.Fatalf("ci.yml is not valid YAML: %v", err)
			}
			var runs []string
			for _, j := range wf.Jobs {
				for _, s := range j.Steps {
					if s.Run != "" {
						runs = append(runs, s.Run)
					}
				}
			}
			for _, want := range []string{"naut check", "naut test", "naut build"} {
				found := false
				for _, r := range runs {
					if strings.HasPrefix(strings.TrimSpace(r), want) {
						found = true
					}
				}
				if !found {
					t.Errorf("the scaffolded CI never runs %q; its run steps are %q", want, runs)
				}
			}
		})
	}
}

// Getting started: a freshly scaffolded project can be checked and built with
// the CLI alone — naut check and naut build succeed on every manifest
// template, and the emitted controller is a real file.
func TestScaffoldedManifestProjectsCheckAndBuild(t *testing.T) {
	for _, tc := range []struct{ name, template, language string }{
		{"minimal-st", tmplMinimal, "st"},
		{"minimal-fbd", tmplMinimal, "fbd"},
		{"minimal-ld", tmplMinimal, "ld"},
		{"tour", tmplDemo, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := scaffoldInto(t, scaffold{Name: tc.name, Program: pascalCase(tc.name), Template: tc.template, Language: tc.language})
			oldOut := os.Stdout
			devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			os.Stdout = devnull
			code := runCheck([]string{dir})
			os.Stdout = oldOut
			devnull.Close()
			if code != 0 {
				t.Fatalf("naut check on the scaffold exited %d", code)
			}
			out := filepath.Join(t.TempDir(), "controller")
			if output, code := buildIn(t, dir, "-o", out); code != 0 {
				t.Fatalf("naut build on the scaffold exited %d:\n%s", code, output)
			}
			if st, err := os.Stat(out); err != nil || st.Size() == 0 {
				t.Fatalf("naut build produced no controller binary: %v", err)
			}
		})
	}
}

// Getting started: an SDK project's go test runs its acceptance suite — here
// the scaffolded sdk and sdk-demo projects are generated against this
// checkout (--replace) and `go test ./...` is run inside them, offline.
func TestScaffoldedSDKProjectsGoTest(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and tests two generated Go projects")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		template string
	}{{"sdkplain", tmplSDK}, {"sdkdemo", tmplSDKDemo}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := scaffoldInto(t, scaffold{Name: tc.name, Module: "example.com/" + tc.name, Program: pascalCase(tc.name), Template: tc.template, Replace: repo})
			if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(goBin, "test", "-count=1", "-v", "./...")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go test in the scaffolded %s project: %v\n%s", tc.name, err, out)
			}
			if !strings.Contains(string(out), "--- PASS") {
				t.Errorf("the scaffolded project ran no passing tests:\n%s", out)
			}
		})
	}
}
