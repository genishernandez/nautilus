package server

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// dashboardHarness runs the dashboard's REAL inline script under node with a
// DOM that absorbs every call, feeds it the controller's real /api/meta and a
// tag snapshot, and prints how the tag table would render each row's value
// cell. Nothing about the page's logic is re-implemented here.
const dashboardHarness = `
const vm = require("vm");
const fs = require("fs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
process.on("unhandledRejection", () => {});
const absorb = new Proxy(function () {}, {
  get: (_t, p) => (p === Symbol.toPrimitive ? () => "" : p === "then" ? undefined : absorb),
  apply: () => absorb,
  construct: () => absorb,
  set: () => true,
});
const never = new Promise(() => {});
const ctx = {
  document: absorb, window: absorb, localStorage: absorb, navigator: absorb,
  EventSource: function () { return absorb; },
  setTimeout: () => 0, setInterval: () => 0, clearTimeout: () => {}, requestAnimationFrame: () => 0,
  matchMedia: () => absorb, console, Math, JSON, Object, Array, Number, String, Set, Map, Promise, Date,
  fetch: (url) => url.startsWith("/api/meta")
    ? Promise.resolve({ ok: true, json: () => Promise.resolve(input.meta), text: () => Promise.resolve("") })
    : never,
};
vm.createContext(ctx);
vm.runInContext(input.script, ctx);
setImmediate(() => setImmediate(() => {
  const out = vm.runInContext(
    "lastTags = " + JSON.stringify(input.tags) + "; " +
    JSON.stringify(input.expand || []) + ".forEach((p) => expanded.add(p)); " +
    "tableModel().map((m) => ({ key: m.key, kind: m.kind, write: m.write, cell: valueCell(m) }))",
    ctx);
  process.stdout.write(JSON.stringify(out));
}));
`

type dashRow struct {
	Key   string  `json:"key"`
	Kind  string  `json:"kind"`
	Write *string `json:"write"`
	Cell  string  `json:"cell"`
}

// dashboardRows renders tags through the real dashboard script against a
// runtime's real /api/meta, with the given struct paths expanded, and
// returns the table rows keyed by their path.
func dashboardRows(t *testing.T, rt *runtime.Runtime, tags map[string]any, expand ...string) map[string]dashRow {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed — cannot run the dashboard script")
	}
	rec := httptest.NewRecorder()
	New(rt).Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/meta", nil))
	var meta any
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	var js strings.Builder
	for _, m := range scriptRe.FindAllSubmatch(indexHTML, -1) {
		js.Write(m[1])
		js.WriteByte('\n')
	}
	in, _ := json.Marshal(map[string]any{"script": js.String(), "meta": meta, "tags": tags, "expand": expand})
	cmd := exec.Command(node, "-e", dashboardHarness)
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard harness: %v\n%s", err, out)
	}
	var rows []dashRow
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("harness output: %v\n%s", err, out)
	}
	byKey := map[string]dashRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	return byKey
}

// Getting started: in the dashboard setpoints are click-to-set (BOOLs by
// toggle) while inputs and outputs stay read-only, because the driver
// rewrites an input and the logic an output within a scan.
func TestDashboardSetpointsEditableInputsAndOutputsReadOnly(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program: `PROGRAM Main
VAR_EXTERNAL Level : REAL; Pump1Run : BOOL; Out : REAL; TankSP : REAL; AutoMode : BOOL; Latch : BOOL; END_VAR
Out := Level;
END_PROGRAM`,
		Driver: nio.NewMemory(),
		Tags: []runtime.TagDef{
			runtime.Input("Level"),
			runtime.Output("Pump1Run", runtime.Init(false)),
			runtime.Output("Out", runtime.Init(0.0)),
			runtime.Setpoint("TankSP", 50.0),
			runtime.Setpoint("AutoMode", true),
			runtime.State("Latch", false),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := dashboardRows(t, rt, map[string]any{
		"Level": 12.5, "Pump1Run": true, "Out": 12.5,
		"TankSP": 50.0, "AutoMode": true, "Latch": false,
	})

	// Setpoints and state: a number is click-to-set, a BOOL is a toggle.
	if r := rows["TankSP"]; r.Kind != "num" || !strings.Contains(r.Cell, `title="click to set"`) || !strings.Contains(r.Cell, `data-write="TankSP"`) {
		t.Errorf("setpoint TankSP row = %+v, want a click-to-set number", r)
	}
	if r := rows["AutoMode"]; r.Kind != "toggle" || !strings.Contains(r.Cell, `class="toggle on"`) || !strings.Contains(r.Cell, `data-write="AutoMode"`) {
		t.Errorf("BOOL setpoint AutoMode row = %+v, want a toggle", r)
	}
	if r := rows["Latch"]; r.Kind != "toggle" {
		t.Errorf("BOOL state tag Latch row = %+v, want a toggle", r)
	}
	// Driver-owned inputs and logic-owned outputs: read-only, with the owner named.
	for name, owner := range map[string]string{"Level": "driver-owned input", "Pump1Run": "logic-owned output", "Out": "logic-owned output"} {
		r := rows[name]
		if r.Kind != "plain" {
			t.Errorf("%s row kind = %q, want plain (read-only)", name, r.Kind)
		}
		if strings.Contains(r.Cell, "data-write") || strings.Contains(r.Cell, "click to") {
			t.Errorf("%s has an edit affordance: %s", name, r.Cell)
		}
		if !strings.Contains(r.Cell, owner) {
			t.Errorf("%s cell does not say who owns it (%q): %s", name, owner, r.Cell)
		}
	}
}

// Tag-model: the built-in dashboard's tag table makes each leaf of a writable
// struct tag editable, addressed by its dotted path; a driver- or
// logic-owned struct's leaves stay read-only.
func TestDashboardWritableStructLeavesAreEditableByDottedPath(t *testing.T) {
	rt, err := runtime.New(runtime.Options{
		Program:   udtProgram,
		Libraries: []string{udtLib},
		Driver:    nio.NewMemory(),
		Tags: []runtime.TagDef{
			runtime.Typed("Feed", runtime.RoleInput, "Motor"),
			runtime.Typed("P101", runtime.RoleState, "Motor"),
			runtime.Typed("Cmd", runtime.RoleOutput, "Motor", runtime.Init(map[string]any{})),
			runtime.Setpoint("SP", 65.0),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	motor := func() map[string]any {
		return map[string]any{"START": false, "Speed": 1.5, "LVL": map[string]any{"HSP": 80.0, "LSP": 20.0}}
	}
	rows := dashboardRows(t, rt,
		map[string]any{"Feed": motor(), "P101": motor(), "Cmd": motor(), "SP": 65.0},
		"P101", "P101.LVL", "Feed", "Cmd")

	for path, kind := range map[string]string{
		"P101.START": "toggle", "P101.Speed": "num", "P101.LVL.HSP": "num", "P101.LVL.LSP": "num",
	} {
		r, ok := rows[path]
		if !ok {
			t.Errorf("no row for %s; the struct did not expand", path)
			continue
		}
		if r.Kind != kind || r.Write == nil || *r.Write != path {
			t.Errorf("%s = %+v, want an editable %s addressed by %q", path, r, kind, path)
		}
	}
	for _, path := range []string{"Feed.START", "Feed.Speed", "Cmd.START", "Cmd.Speed"} {
		r, ok := rows[path]
		if !ok {
			t.Errorf("no row for %s; the struct did not expand", path)
			continue
		}
		if r.Kind != "plain" || r.Write != nil {
			t.Errorf("%s = %+v, want a read-only leaf (its root is driver/logic-owned)", path, r)
		}
	}
}
