package server

import (
	"net/http"
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/runtime"
)

// ST-048: downloading a program (PUT /api/program) warm-swaps it into the
// running controller with its state carried across, and a download that fails
// to compile leaves the running program, and its state, untouched.
func TestProgramDownloadCarriesStateAndFailedDownloadChangesNothing(t *testing.T) {
	const v1 = `PROGRAM Counter
VAR_EXTERNAL Count : REAL; END_VAR
VAR n : REAL; END_VAR
n := n + 1.0;
Count := n;
END_PROGRAM
`
	const v2 = `PROGRAM Counter
VAR_EXTERNAL Count : REAL; END_VAR
VAR n : REAL; END_VAR
n := n + 10.0;
Count := n;
END_PROGRAM
`
	rt, err := runtime.New(runtime.Options{
		Program: v1,
		Driver:  nio.NewMemory(),
		Seed:    nio.Values{"Count": 0.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := New(rt, Options{OnlineEdits: true}).Handler()
	for i := 0; i < 3; i++ {
		rt.Scan()
	}
	if got := rt.Tags().Real("Count"); got != 3 {
		t.Fatalf("Count after 3 scans = %v, want 3", got)
	}

	// A download that does not compile is refused and the running program,
	// with its state, carries on.
	w, body := doJSON(t, h, "PUT", "/api/program", map[string]string{"source": "PROGRAM Counter\nVAR n : REAL; END_VAR\nn := ;\nEND_PROGRAM"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("broken download = %d %v, want 422", w.Code, body)
	}
	rt.Scan()
	if got := rt.Tags().Real("Count"); got != 4 {
		t.Fatalf("Count after the refused download and one more scan = %v, want 4 (the running program untouched)", got)
	}

	// A good download swaps the logic and carries n: the next scan adds 10 to
	// the 4 already counted. A reset would read 10.
	w, body = doJSON(t, h, "PUT", "/api/program", map[string]string{"source": v2})
	if w.Code != http.StatusOK {
		t.Fatalf("download = %d %v, want 200", w.Code, body)
	}
	rt.Scan()
	if got := rt.Tags().Real("Count"); got != 14 {
		t.Fatalf("Count after the download and one scan = %v, want 14 (new logic, state carried)", got)
	}
}
