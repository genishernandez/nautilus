package project

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/server"
)

// TestProjectConstantIsNotATag: a VAR_GLOBAL CONSTANT in a library file is
// folded into the programs that use it, but it is no tag — the running
// project's tag store has no such name, and the HTTP API's live values
// (/api/state) never list it — while a plain GVL global beside it is a tag
// (the control that shows the API would list a library global if it were
// one).
func TestProjectConstantIsNotATag(t *testing.T) {
	rt := loadRuntime(t, map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Out, role: state, type: REAL }\n",
		"consts.st":     "VAR_GLOBAL CONSTANT\n    MaxLevel : REAL := 90.0;\nEND_VAR\n",
		"gvl.st":        "VAR_GLOBAL\n    Count : DINT;\nEND_VAR\n",
		"main.st":       "PROGRAM Main\nOut := MaxLevel;\nCount := Count + 1;\nEND_PROGRAM\n",
	})
	rt.Scan()
	if got := readReal(t, rt, "Out"); got != 90 {
		t.Fatalf("Out = %v, want the constant's 90", got)
	}
	if _, err := rt.Tags().ReadGlobal("MaxLevel"); err == nil {
		t.Error("the tag store has a MaxLevel: a project constant must not be a tag")
	}

	rec := httptest.NewRecorder()
	server.New(rt).Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/state = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"Out"`) || !strings.Contains(body, `"Count"`) {
		t.Fatalf("premise: /api/state should list the manifest tag and the GVL global:\n%s", body)
	}
	if strings.Contains(body, "MaxLevel") {
		t.Errorf("/api/state lists the project constant MaxLevel:\n%s", body)
	}

	// A tag write by the constant's name never reaches the constant: the
	// program still reads 90. (POST /api/tags with an undeclared name and a
	// scalar value currently answers 204 and creates a stray store entry of
	// that name; see the FN-055 note in docs/claims/functions.yaml.)
	rec = httptest.NewRecorder()
	server.New(rt).Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/tags", strings.NewReader(`{"name": "MaxLevel", "value": 5}`)))
	rt.Scan()
	if got := readReal(t, rt, "Out"); got != 90 {
		t.Errorf("Out = %v after a write to the name MaxLevel, want the constant's 90", got)
	}
}
