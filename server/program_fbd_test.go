package server

import (
	"net/http"
	"testing"
	"time"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// An FBD controller serves its .fbd text, and an FBD edit routes to a task by
// PROGRAM name: the task's own .fbd is swapped, the other program's text is
// left exactly as it was.
func TestProgramFBDServedAndRoutedByPOU(t *testing.T) {
	const mainFBD = `PROGRAM Pump
VAR_EXTERNAL
  Level : REAL;
  Out : REAL;
END_VAR
FBD
  Out := MUL(Level, 2.0)
END_FBD
END_PROGRAM`
	const taskFBD = `PROGRAM Totals
VAR_EXTERNAL
  Out : REAL;
  TotalL : REAL;
END_VAR
FBD
  TotalL := ADD(TotalL, Out)
END_FBD
END_PROGRAM`
	const taskEdited = `PROGRAM Totals
VAR_EXTERNAL
  Out : REAL;
  TotalL : REAL;
END_VAR
FBD
  TotalL := ADD(TotalL, MUL(Out, 3.0))
END_FBD
END_PROGRAM`
	rt, err := runtime.New(runtime.Options{
		Program: mainFBD,
		Driver:  nio.NewMemory(),
		Seed:    nio.Values{"Level": 4.0, "Out": 0.0, "TotalL": 0.0},
		Tasks:   []runtime.Task{{Name: "totals", Program: taskFBD, Scan: time.Second}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := New(rt, Options{OnlineEdits: true}).Handler()

	// Served: GET returns the .fbd text, not transpiled ST.
	w, body := doJSON(t, h, "GET", "/api/program?pou=Totals", nil)
	if w.Code != http.StatusOK || body["source"] != taskFBD || body["language"] != "fbd" {
		t.Fatalf("GET ?pou=Totals = %d %v, want the task's .fbd text", w.Code, body)
	}
	w, body = doJSON(t, h, "GET", "/api/program?pou=Pump", nil)
	if w.Code != http.StatusOK || body["source"] != mainFBD {
		t.Fatalf("GET ?pou=Pump = %d %v, want the main .fbd text", w.Code, body)
	}

	// Accepted and routed: the Totals .fbd lands on the task, Pump untouched.
	w, body = doJSON(t, h, "PUT", "/api/program", map[string]string{"source": taskEdited})
	if w.Code != http.StatusOK || body["task"] != "totals" {
		t.Fatalf("PUT .fbd = %d %v, want routed to task totals", w.Code, body)
	}
	if got := rt.TaskProgram("totals").Source(); got != taskEdited {
		t.Fatalf("task source after PUT:\n%s", got)
	}
	if got := rt.Program().Source(); got != mainFBD {
		t.Fatalf("main program must be untouched, source now:\n%s", got)
	}
}
