package hw

import "time"

// Counter turns a monotonically increasing wire counter (ifHCInOctets, a
// node_exporter *_total) into a per-second rate across polls.
//
// Two facts about counters decide the rules. A 32-bit counter WRAPS: a
// gigabit port rolls ifInOctets every ~34 s, so a negative delta on a
// counter declared width 32 means one wrap and the rate is computed across
// it (a second wrap inside one interval is undetectable, which is why the
// generator prefers the 64-bit ifHC columns). A 64-bit counter does not wrap
// in any lifetime, so a negative delta there is a RESET — the device
// rebooted or the exporter restarted — and no honest rate exists for that
// interval: Observe reports ok=false and the caller keeps the last rate.
//
// A 32-bit counter can reset too, and from the counter alone a reset and a
// wrap look the same: both step backwards. Two rules tell them apart:
//
//   - A step backwards that, read as a wrap, would mean the counter moved
//     more than HALF its range (2³¹) in one interval is a reset, not a
//     wrap. A counter that really moves that fast is already one interval
//     from a double wrap nobody can see (at the default 10 s that is
//     1.7 Gb/s on an octet counter — RFC 2863 has every port over 20 Mb/s
//     on the 64-bit columns), so nothing honest is lost; a reset from the
//     lower half of the range, the common case, is no longer a spike.
//   - A reset from the upper half still reads as a plausible wrap. Only the
//     device can say it restarted: Discontinuity takes that word (an SNMP
//     source's sysUpTime) and drops the sample from before it.
//
// A counter whose wire value changes between integer and float (an exporter
// that formats 1e+06 one scrape and 1000001 the next) starts over on the
// switch: the two paths keep separate last samples and different reset
// rules, so a delta across them would be against the wrong baseline.
type Counter struct {
	seen  counterPath // which path holds the last sample; none before the first
	last  uint64
	lastF float64
	at    time.Time
}

type counterPath uint8

const (
	pathNone counterPath = iota
	pathInt
	pathFloat
)

// Observe records one sample and returns the rate since the previous one.
// ok is false on the first sample (or the first after an ObserveFloat), on
// a reset, and when no time has passed.
func (c *Counter) Observe(v uint64, width int, now time.Time) (rate float64, ok bool) {
	defer func() { c.seen, c.last, c.at = pathInt, v, now }()
	if c.seen != pathInt {
		return 0, false
	}
	dt := now.Sub(c.at).Seconds()
	if dt <= 0 {
		return 0, false
	}
	var delta float64
	switch {
	case v >= c.last:
		delta = float64(v - c.last)
	case width == 32:
		wrapped := v + (1<<32 - c.last)
		if wrapped > 1<<31 {
			return 0, false // half the range in one interval: a reset, not a wrap
		}
		delta = float64(wrapped)
	default:
		return 0, false // 64-bit went backwards: a reset, not a wrap
	}
	return delta / dt, true
}

// ObserveFloat is Observe for a counter that is already a float with no
// wire width — a Prometheus *_total, an accumulator of fractional seconds.
// Such a counter never wraps; the ecosystem rule is that ANY decrease means
// the process or exporter restarted, so a step backwards is a reset.
func (c *Counter) ObserveFloat(v float64, now time.Time) (rate float64, ok bool) {
	defer func() { c.seen, c.lastF, c.at = pathFloat, v, now }()
	if c.seen != pathFloat {
		return 0, false
	}
	dt := now.Sub(c.at).Seconds()
	if dt <= 0 || v < c.lastF {
		return 0, false
	}
	return (v - c.lastF) / dt, true
}

// Discontinuity says the source's counters started over at restart (an SNMP
// agent's sysUpTime, read alongside them, puts its last start at poll time
// minus the uptime). A previous sample taken before then belongs to the old
// run, so it is forgotten and the next sample is a first sample: no rate is
// computed across the restart, whatever the counter's width or how far it
// has climbed since. A sample taken after the restart is kept.
func (c *Counter) Discontinuity(restart time.Time) {
	if c.seen != pathNone && c.at.Before(restart) {
		c.Reset()
	}
}

// Reset forgets the previous sample — a source that reconnected after an
// outage must not compute a rate across the gap it never observed.
func (c *Counter) Reset() { *c = Counter{} }
