package facade

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/eip/logixserver"
)

// startFacadeOn runs a facade at the given poll rate over an emulator
// serving s, seeded with values.
func startFacadeOn(t *testing.T, s *logixserver.TagSurfaceSpec, poll time.Duration, values map[string]any) (string, *logixserver.TagStore, *Facade) {
	t.Helper()
	schema, tags, name, err := logixserver.CompileSurface(s)
	if err != nil {
		t.Fatal(err)
	}
	store := logixserver.NewTagStore()
	for _, tc := range tags {
		store.Set(tc.Path, tc.LeafType, tc.Default)
	}
	for p, v := range values {
		store.UpdateValue(p, v)
	}
	// Bind :0 inside the server (no pick-then-bind window) and read the port back.
	srv := logixserver.NewServer(store, schema, name, "127.0.0.1:0", nil)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	_, ps, _ := net.SplitHostPort(srv.Addr())
	port, _ := strconv.Atoi(ps)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Run(ctx) }()
	f, err := New(ctx, Options{Host: "127.0.0.1", Port: port, Poll: poll})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	api := httptest.NewServer(f.Handler())
	t.Cleanup(func() { api.Close(); cancel(); <-done })
	return api.URL, store, f
}

// LGX-012: what the frame shows after a write is what a poll read back
// from the controller, never the value the request carried.
func TestTheFrameShowsTheReadBackNotTheRequest(t *testing.T) {
	t.Run("until the next poll the frame keeps the controller's last value", func(t *testing.T) {
		// One poll at startup and none for an hour: a value in the frame
		// after the write could only have come from the request. (A
		// reconnect polls at once; the poll counter says if one did, and
		// then the frame may rightly show the read-back.)
		base, store, f := startFacadeOn(t, spec(), time.Hour, map[string]any{"Speed": 10.0})
		waitFor(t, "the startup poll", func() bool { return state(t, base)["Speed"] == 10.0 })
		polls := f.drv.Health().Polls
		if code, body := post(t, base, `{"name": "Speed", "value": 55.5}`); code != http.StatusNoContent {
			t.Fatalf("write = %d %s", code, body)
		}
		if _, got, _ := store.Resolve("Speed"); fmt.Sprint(got) != "55.5" {
			t.Fatalf("controller holds %v, want 55.5: the write did not land", got)
		}
		for i := 0; i < 5; i++ {
			got := state(t, base)["Speed"]
			if f.drv.Health().Polls != polls {
				t.Logf("a poll ran after %d checks; the frame may now hold the read-back", i)
				break
			}
			if got != 10.0 {
				t.Fatalf("frame shows Speed = %v with no poll since the write, want the polled 10", got)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	t.Run("the frame carries what the controller holds, not what was sent", func(t *testing.T) {
		// A REAL holds 0.1 as the nearest float32, which is not 0.1: the
		// read-back and the request differ, and the frame must show the
		// read-back.
		held := float64(float32(0.1))
		if held == 0.1 {
			t.Fatal("the case cannot tell the read-back from the request")
		}
		base, _, _ := startFacadeOn(t, spec(), 30*time.Millisecond, map[string]any{"Speed": 10.0})
		waitFor(t, "first poll", func() bool { return state(t, base)["Speed"] == 10.0 })
		if code, body := post(t, base, `{"name": "Speed", "value": 0.1}`); code != http.StatusNoContent {
			t.Fatalf("write = %d %s", code, body)
		}
		waitFor(t, "the read-back in the frame", func() bool { return state(t, base)["Speed"] == held })
		for i := 0; i < 5; i++ {
			if got := state(t, base)["Speed"]; got != held {
				t.Fatalf("frame shows Speed = %v, want the controller's %v", got, held)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

// LGX-013: a write the controller itself refuses comes back as that
// refusal, and the controller's value is untouched. The facade cannot see
// this one coming: it does not know the array's length, so the index goes
// to the controller, which refuses it.
func TestAWriteTheControllerRefusesReturnsItsRefusal(t *testing.T) {
	s := spec()
	s.Symbols = append(s.Symbols, logixserver.SymbolSpec{Name: "Setpoints", Datatype: "DINT", Dims: []uint32{4}})
	for i := 0; i < 4; i++ {
		s.Tags = append(s.Tags, logixserver.TagSpec{Path: fmt.Sprintf("Setpoints[%d]", i), Datatype: "DINT"})
	}
	base, store, _ := startFacadeOn(t, s, 30*time.Millisecond, map[string]any{"Speed": 10.0})
	waitFor(t, "first poll", func() bool { return state(t, base)["Speed"] == 10.0 })

	// In range, the controller takes it.
	if code, body := post(t, base, `{"name": "Setpoints[2]", "value": 7}`); code != http.StatusNoContent {
		t.Fatalf("Setpoints[2] = %d %s", code, body)
	}
	if _, got, _ := store.Resolve("Setpoints[2]"); fmt.Sprint(got) != "7" {
		t.Fatalf("controller holds Setpoints[2] = %v, want 7", got)
	}
	// Out of range, the controller refuses, and the reply is that refusal.
	code, body := post(t, base, `{"name": "Setpoints[9]", "value": 7}`)
	if code == http.StatusNoContent || !strings.Contains(body, "the controller refused Setpoints[9]") ||
		!strings.Contains(body, "CIP status 0x05") {
		t.Errorf("Setpoints[9] = %d %q, want the controller's refusal (CIP status 0x05)", code, body)
	}
}
