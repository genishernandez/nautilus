package alarm

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The log notifier writes one Info record per event carrying what an
// operator searching the log needs: which alarm, what happened, how bad,
// where, and who.
func TestLogNotifier(t *testing.T) {
	var buf bytes.Buffer
	n := NewLogNotifier(slog.New(slog.NewJSONHandler(&buf, nil)))
	e := ev(1000, "FIT_001.HH", "ack")
	e.By = "operator1"
	if err := n.Notify(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("want one JSON log record, got %q: %v", buf.String(), err)
	}
	for k, want := range map[string]any{
		"level": "INFO", "msg": "alarm",
		"id": "FIT_001.HH", "name": "FIT_001.HH", "kind": "ack", "priority": "high",
		"site": "RTU9", "state": "unack-active", "by": "operator1", "ts": 1000.0,
	} {
		if rec[k] != want {
			t.Errorf("log record %s = %v, want %v (record %s)", k, rec[k], want, buf.String())
		}
	}
	if lines := strings.Count(buf.String(), "\n"); lines != 1 {
		t.Errorf("logged %d records for one event, want 1", lines)
	}
}

func TestWebhookPostsTheEventAsJSON(t *testing.T) {
	var got Event
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if k := r.Header.Get("X-Key"); k != "s3cret" {
			t.Errorf("custom header = %q", k)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	n := NewWebhook(srv.URL, WebhookOptions{Headers: map[string]string{"X-Key": "s3cret"}})
	if err := n.Notify(context.Background(), ev(1000, "A.HH", "active")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.ID != "A.HH" || got.Kind != "active" || got.Priority != High {
		t.Fatalf("webhook received %+v", got)
	}
}

func TestWebhookRetriesThenSucceeds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewWebhook(srv.URL, WebhookOptions{Retries: 2, Backoff: time.Millisecond})
	if err := n.Notify(context.Background(), ev(1000, "A", "active")); err != nil {
		t.Fatalf("Notify should have succeeded on the third attempt: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestWebhookGivesUpAndReportsTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	n := NewWebhook(srv.URL, WebhookOptions{Retries: 1, Backoff: time.Millisecond})
	err := n.Notify(context.Background(), ev(1000, "A", "active"))
	if err == nil {
		t.Fatal("want an error after the retries are spent")
	}
}

// countingNotifier records what the engine handed it.
type countingNotifier struct {
	mu   sync.Mutex
	seen []Event
}

func (c *countingNotifier) Notify(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	return nil
}

func (c *countingNotifier) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

// blockingNotifier records each event, says it has one, and then blocks
// until the test releases it: a notifier stuck on a dead endpoint.
type blockingNotifier struct {
	countingNotifier
	arrived chan struct{}
	release chan struct{}
}

func (b *blockingNotifier) Notify(ctx context.Context, e Event) error {
	_ = b.countingNotifier.Notify(ctx, e)
	b.arrived <- struct{}{}
	<-b.release
	return nil
}

// TestEngineNotifiesOffTheScanGoroutine — the engine must hand events over
// and keep scanning, never await a notifier. The notifier here blocks on
// every event until the test lets it go, so an engine that delivered on
// the scan goroutine would hang in Evaluate.
func TestEngineNotifiesOffTheScanGoroutine(t *testing.T) {
	clk, tg := newClock(), tags{}
	n := &blockingNotifier{arrived: make(chan struct{}, 2), release: make(chan struct{})}
	e, err := New(Options{
		Defs: []Def{def("A", "A")}, Read: tg.read, Now: clk.now,
		Journal: NewRing(10), Notify: []Notifier{n},
	})
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			close(n.release)
			released = true
		}
	}
	defer release()
	// evaluate runs one Evaluate and fails if it does not come back while
	// the notifier is blocked. The deadline only bounds how long a
	// synchronous engine is given to show itself.
	evaluate := func(what string) {
		t.Helper()
		done := make(chan struct{})
		go func() { e.Evaluate(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			release()
			<-done
			t.Fatalf("%s: Evaluate waited on a blocked notifier", what)
		}
	}

	tg["A"] = true
	evaluate("active")
	select {
	case <-n.arrived: // the notifier holds the event and is now blocked in it
	case <-time.After(10 * time.Second):
		t.Fatal("the notifier never received the active event")
	}
	tg["A"] = false
	clk.advance(time.Second)
	evaluate("return to normal") // with the notifier still blocked
	release()

	// Close drains the queue, so this is deterministic without a sleep.
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if got := n.len(); got != 2 {
		t.Fatalf("notifier saw %d events, want 2", got)
	}
	if n.seen[0].Kind != KindActive || n.seen[1].Kind != KindRTN {
		t.Fatalf("notifier saw %+v", n.seen)
	}
	if e.Dropped() != 0 {
		t.Fatalf("dropped %d events with an idle notifier", e.Dropped())
	}
}
