package main

// startAlarms is `naut run`'s alarm wiring: the real engine, registered with
// the runtime so the retain saver persists acknowledgements and a takeover
// restores them. Each half has its own test (the engine's snapshot and
// restore in ./alarm, the runtime's plumbing with a fake engine in
// ./runtime); this one runs them together through the call runProject
// makes.

import (
	"context"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/joyautomation/nautilus/alarm"
	"github.com/joyautomation/nautilus/internal/project"
	"github.com/joyautomation/nautilus/retain"
	"github.com/joyautomation/nautilus/runtime"
)

// memRetain is an in-memory retain.Store, so a "restart" is a second
// runtime loading what the first one saved.
type memRetain struct {
	mu    sync.Mutex
	state retain.State
}

func (m *memRetain) Load() (retain.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}

func (m *memRetain) Save(s retain.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return nil
}

func (m *memRetain) Kind() string { return "memory" }

func (m *memRetain) alarm(id string) retain.AlarmRetain {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Alarms[id]
}

var runAlarmsProject = fstest.MapFS{
	"nautilus.yaml": &fstest.MapFile{Data: []byte(`
tasks:
  - program: program.st
    scan: 100ms
tags:
  - { name: FIT_001, role: state, type: AnalogInput, init: { PV: 50.0 }, desc: "Well flow" }
alarms:
  rules:
    - match: { type: AnalogInput, member: L }
      name: "{desc} Low"
      priority: medium
`)},
	"blocks.st": &fstest.MapFile{Data: []byte(`TYPE
  AnalogInput : STRUCT
    PV : REAL;
    L  : BOOL;
  END_STRUCT;
END_TYPE
`)},
	"program.st": &fstest.MapFile{Data: []byte(`PROGRAM Main
VAR_EXTERNAL FIT_001 : AnalogInput; END_VAR
FIT_001.L := FIT_001.PV < 10.0;
END_PROGRAM`)},
}

// bootAlarms is the part of runProject under test: the project loaded,
// the retain store set, the runtime compiled and startAlarms wiring the
// engine in. The flow is low on the first scan.
func bootAlarms(t *testing.T, store retain.Store) (*runtime.Runtime, *project.Alarms) {
	t.Helper()
	proj, err := project.Load(runAlarmsProject, "")
	if err != nil {
		t.Fatal(err)
	}
	proj.Runtime.Retain = store
	rt, err := runtime.New(proj.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	alarms, err := startAlarms(proj, rt)
	if err != nil || alarms == nil {
		t.Fatalf("startAlarms = %v, %v", alarms, err)
	}
	t.Cleanup(func() { _ = alarms.Close() })
	if err := rt.Tags().SetPath("FIT_001.PV", 5.0); err != nil {
		t.Fatal(err)
	}
	rt.Scan()
	return rt, alarms
}

func alarmRecord(t *testing.T, a *project.Alarms, id string) alarm.Record {
	t.Helper()
	for _, r := range a.Engine.Records() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no alarm %s", id)
	return alarm.Record{}
}

// An acknowledgement made on the real engine is saved by the runtime's
// retain saver and restored into a fresh engine on the next start, so a
// restart does not re-annunciate an alarm an operator acked. Without the
// SetAlarms in startAlarms nothing is saved and the restart comes back
// unacked.
func TestRunWiresAlarmAcksThroughRetain(t *testing.T) {
	store := &memRetain{}
	rt, a := bootAlarms(t, store)
	if got := alarmRecord(t, a, "FIT_001.L").State; got != alarm.UnackActive {
		t.Fatalf("setup: FIT_001.L is %s, want unack-active", got)
	}
	if _, err := a.Engine.Ack([]string{"FIT_001.L"}, "operator1"); err != nil {
		t.Fatal(err)
	}

	// The saver runs under Run, on its own interval; wait for it. The
	// deadline bounds a broken wiring, not a working one.
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{})
	go func() { rt.Run(ctx); close(ran) }()
	deadline := time.Now().Add(15 * time.Second)
	for !store.alarm("FIT_001.L").Acked {
		if time.Now().After(deadline) {
			cancel()
			<-ran
			t.Fatalf("the ack never reached the retain store: %+v", store.alarm("FIT_001.L"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-ran
	if by := store.alarm("FIT_001.L").AckBy; by != "operator1" {
		t.Errorf("retained ackBy = %q, want operator1", by)
	}

	// The restart: a new runtime and engine over the same store, with the
	// flow still low.
	_, a2 := bootAlarms(t, store)
	rec := alarmRecord(t, a2, "FIT_001.L")
	if rec.State != alarm.AckActive || rec.AckBy != "operator1" {
		t.Errorf("after restart FIT_001.L is %s (ackBy %q), want ack-active by operator1", rec.State, rec.AckBy)
	}
	if n := a2.Engine.Summary().Unacked; n != 0 {
		t.Errorf("after restart %d alarms are unacked, want 0", n)
	}
}
