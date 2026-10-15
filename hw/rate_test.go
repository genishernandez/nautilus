package hw

import (
	"github.com/joyautomation/nautilus/lang/ir"
	"testing"
	"time"
)

func TestCounterRate(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	if _, ok := c.Observe(1000, 64, t0); ok {
		t.Fatal("first sample must not yield a rate")
	}
	r, ok := c.Observe(3000, 64, t0.Add(10*time.Second))
	if !ok || r != 200 {
		t.Fatalf("rate = %v, %v; want 200", r, ok)
	}
	// 64-bit going backwards is a reset: no rate, but the sample is kept
	// so the next delta is computed from it.
	if _, ok := c.Observe(100, 64, t0.Add(20*time.Second)); ok {
		t.Fatal("a 64-bit step backwards must read as a reset")
	}
	r, ok = c.Observe(600, 64, t0.Add(30*time.Second))
	if !ok || r != 50 {
		t.Fatalf("after reset: rate = %v, %v; want 50", r, ok)
	}
	// Same instant: no rate (no division by zero).
	if _, ok := c.Observe(700, 64, t0.Add(30*time.Second)); ok {
		t.Fatal("zero elapsed must not yield a rate")
	}
}

func TestCounterWrap32(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	c.Observe(1<<32-100, 32, t0)
	r, ok := c.Observe(100, 32, t0.Add(time.Second))
	if !ok || r != 200 {
		t.Fatalf("32-bit wrap: rate = %v, %v; want 200", r, ok)
	}
	c.Reset()
	if _, ok := c.Observe(5, 32, t0.Add(2*time.Second)); ok {
		t.Fatal("after Reset the next sample is a first sample")
	}
}

// A counter whose wire form flips between int and float must not compute a
// delta against the other path's zero: 1e6 as int, then 1000050.0 as a
// float, read as a million per second before the paths kept their own.
func TestCounterSwitchingPathsStartsOver(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	c.Observe(1000000, 64, t0)
	if r, ok := c.ObserveFloat(1000050, t0.Add(time.Second)); ok {
		t.Fatalf("int then float yielded rate %v; the switch is a first sample", r)
	}
	r, ok := c.ObserveFloat(1000150, t0.Add(2*time.Second))
	if !ok || r != 100 {
		t.Fatalf("float after float: rate = %v, %v; want 100", r, ok)
	}
	if r, ok := c.Observe(1000200, 64, t0.Add(3*time.Second)); ok {
		t.Fatalf("float then int yielded rate %v; the switch is a first sample", r)
	}
	r, ok = c.Observe(1000300, 64, t0.Add(4*time.Second))
	if !ok || r != 100 {
		t.Fatalf("int after int: rate = %v, %v; want 100", r, ok)
	}
}

func TestCounterFloat(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	if _, ok := c.ObserveFloat(10.25, t0); ok {
		t.Fatal("first sample")
	}
	r, ok := c.ObserveFloat(12.75, t0.Add(10*time.Second))
	if !ok || r != 0.25 {
		t.Fatalf("rate = %v %v; the fraction must survive", r, ok)
	}
	if _, ok := c.ObserveFloat(1, t0.Add(20*time.Second)); ok {
		t.Fatal("any decrease is a reset")
	}
	// Through a binding: a RawFloat counter uses the float path, a RawUint
	// the wire-width one.
	b := Binding{Rate: true}
	f := field("X", ir.TypeReal)
	c.Reset()
	b.Apply(f, RawFloatVal(100.5), &c, t0)
	v, ok, _ := b.Apply(f, RawFloatVal(101.5), &c, t0.Add(time.Second))
	if !ok || v.F != 1 {
		t.Fatalf("float rate = %+v %v", v, ok)
	}
}

// A 32-bit counter that resets from the lower half of its range steps
// backwards by more than a wrap ever could in one interval: no rate, not a
// spike of ~4 G per interval. A genuine wrap still rates.
func TestCounterReset32(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	c.Observe(1_000_000, 32, t0)
	if r, ok := c.Observe(500, 32, t0.Add(10*time.Second)); ok {
		t.Fatalf("a 32-bit reset from 1e6 to 500 rated %v (read as a wrap of %v)", r, float64(500+(1<<32-1_000_000)))
	}
	// The reset sample is the new baseline.
	if r, ok := c.Observe(1500, 32, t0.Add(20*time.Second)); !ok || r != 100 {
		t.Fatalf("after the reset: rate = %v, %v; want 100", r, ok)
	}
	// Exactly half the range is still a wrap; one more is not.
	c.Reset()
	c.Observe(1<<32-1<<30, 32, t0)
	if _, ok := c.Observe(1<<30, 32, t0.Add(time.Second)); !ok {
		t.Fatal("a wrap of exactly 2^31 must still rate")
	}
	c.Reset()
	c.Observe(1<<32-1<<30, 32, t0)
	if _, ok := c.Observe(1<<30+1, 32, t0.Add(time.Second)); ok {
		t.Fatal("more than 2^31 in one interval is a reset")
	}
}

// Discontinuity drops a sample from before the source restarted — the
// reset a 32-bit counter cannot show by itself (from the upper half of its
// range, a reset reads as a plausible wrap) — and keeps one from after.
func TestCounterDiscontinuity(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var c Counter
	c.Observe(1<<32-1000, 32, t0)
	// The agent restarted 5 s into the interval; the counter is back to 400.
	// As a wrap that is 1400 octets — plausible, and wrong.
	c.Discontinuity(t0.Add(5 * time.Second))
	if r, ok := c.Observe(400, 32, t0.Add(10*time.Second)); ok {
		t.Fatalf("a rate across a restart: %v", r)
	}
	// A restart long before the last sample changes nothing.
	c.Discontinuity(t0.Add(-time.Hour))
	if r, ok := c.Observe(1400, 32, t0.Add(20*time.Second)); !ok || r != 100 {
		t.Fatalf("rate = %v, %v; want 100", r, ok)
	}
	// Before any sample it is a no-op.
	var fresh Counter
	fresh.Discontinuity(t0)
	if _, ok := fresh.Observe(1, 64, t0.Add(time.Second)); ok {
		t.Fatal("first sample")
	}
}
