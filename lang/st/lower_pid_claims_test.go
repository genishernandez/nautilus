package st

import (
	"math"
	"testing"

	"github.com/joyautomation/nautilus/lang/ir"
)

// These tests pin the PID block's documented pin semantics (docs/functions.md
// "PID: closed-loop control") one pin at a time, against hand-computed
// values of the ISA standard form
//
//	CV = KP·(error + KI·∫error·dt + KD·d(PV)/dt)
//
// with the derivative on PV through a first-order filter of time constant
// KD/10. Every test drives the pidSrc harness (every pin wired to a
// VAR_GLOBAL) with DT bound, so each scan is exactly DT seconds.
//
// One convention matters for the hand computations: the block's first scan
// in AUTO (power-up counts as coming from MANUAL) does not integrate — the
// bumpless-transfer rule uses the integral as it stands on that scan — so
// ∫error·dt starts accumulating on the second scan.

type pidRig struct {
	t     *testing.T
	host  *fakeHost
	prog  *ir.Program
	frame *ir.Frame
	nowMs int64
}

func newPIDRig(t *testing.T) *pidRig {
	t.Helper()
	host := newFakeHost()
	prog, frame := compilePID(t, pidSrc)
	host.vals["auto"] = ir.BoolVal(true)
	host.vals["direct"] = ir.BoolVal(false)
	host.vals["kp"] = ir.RealVal(1)
	host.vals["ki"] = ir.RealVal(0)
	host.vals["kd"] = ir.RealVal(0)
	host.vals["cvMin"] = ir.RealVal(-1000)
	host.vals["cvMax"] = ir.RealVal(1000)
	host.vals["dt"] = ir.RealVal(1)
	host.vals["sp"] = ir.RealVal(0)
	host.vals["pv"] = ir.RealVal(0)
	return &pidRig{t: t, host: host, prog: prog, frame: frame}
}

func (r *pidRig) set(name string, v float64) { r.host.vals[name] = ir.RealVal(v) }
func (r *pidRig) setB(name string, v bool)   { r.host.vals[name] = ir.BoolVal(v) }
func (r *pidRig) get(name string) float64    { return r.host.vals[name].F }
func (r *pidRig) getB(name string) bool      { return r.host.vals[name].B }

// scan runs one call, advancing the virtual clock by one second.
func (r *pidRig) scan() {
	r.t.Helper()
	r.nowMs += 1000
	r.host.now = r.nowMs
	runPID(r.t, r.prog, r.frame, r.host)
}

func (r *pidRig) expect(scan int, name string, want float64) {
	r.t.Helper()
	if got := r.get(name); math.Abs(got-want) > 1e-9 {
		r.t.Fatalf("scan %d: %s = %.9f, want %.9f", scan, name, got, want)
	}
}

// FN-131: KP = 0 disables the whole controller (KI and KD ride on KP);
// KI = 0 disables integral action; KD = 0 disables derivative action.
func TestPIDZeroGainsDisable(t *testing.T) {
	t.Run("KP=0 disables P, I and D", func(t *testing.T) {
		r := newPIDRig(t)
		r.set("kp", 0)
		r.set("ki", 1)
		r.set("kd", 10)
		r.set("sp", 50)
		for i := 1; i <= 10; i++ {
			r.set("pv", float64(7*i)) // PV moves every scan: D would see it
			r.scan()
			if r.get("errOut") == 0 {
				t.Fatalf("scan %d: premise: error should be nonzero", i)
			}
			for _, n := range []string{"cvOut", "pTermOut", "iTermOut", "dTermOut"} {
				r.expect(i, n, 0)
			}
		}
	})

	t.Run("KI=0 disables integral action", func(t *testing.T) {
		r := newPIDRig(t)
		r.set("kp", 2)
		r.set("ki", 0)
		r.set("sp", 50)
		r.set("pv", 40) // error 10, held for 10 s
		for i := 1; i <= 10; i++ {
			r.scan()
			r.expect(i, "iTermOut", 0)
			r.expect(i, "cvOut", 20) // P alone: 2 × 10, never growing
		}
		// Control: the same loop with KI > 0 does integrate, so the zero
		// above is KI's doing, not the harness's.
		r2 := newPIDRig(t)
		r2.set("kp", 2)
		r2.set("ki", 0.5)
		r2.set("sp", 50)
		r2.set("pv", 40)
		for i := 1; i <= 3; i++ {
			r2.scan()
		}
		r2.expect(3, "iTermOut", 2*0.5*20) // two integrating scans of 10·1 s
	})

	t.Run("KD=0 disables derivative action", func(t *testing.T) {
		r := newPIDRig(t)
		r.set("kp", 2)
		r.set("kd", 0)
		r.set("sp", 50)
		for i := 1; i <= 10; i++ {
			r.set("pv", float64(5*i)) // PV ramps: a derivative would see 5/s
			r.scan()
			r.expect(i, "dTermOut", 0)
			r.expect(i, "cvOut", 2*(50-float64(5*i)))
		}
	})
}

// FN-132: the derivative acts on PV, not on error — a setpoint step leaves
// D_TERM at zero — and runs through a first-order filter of time constant
// KD/10: with DT = 1 s the filter weight is alpha = DT/(KD/10 + DT).
func TestPIDDerivativeOnPVFiltered(t *testing.T) {
	t.Run("setpoint step never kicks D_TERM", func(t *testing.T) {
		r := newPIDRig(t)
		r.set("kp", 1)
		r.set("kd", 10)
		r.set("pv", 40)
		r.set("sp", 50)
		for i := 1; i <= 3; i++ {
			r.scan()
		}
		for i, sp := range []float64{80, 0, -50, 50} { // large SP steps, PV still
			r.set("sp", sp)
			r.scan()
			r.expect(4+i, "dTermOut", 0)
			r.expect(4+i, "pTermOut", sp-40)
		}
	})

	// A PV step of +10 at scan 2, then PV held: the raw derivative is 10/s
	// for one scan and 0 after. The filtered derivative is
	// d_k = d_{k-1} + alpha·(raw_k − d_{k-1}), and D_TERM = −KP·KD·d_k
	// (reverse acting: a rising PV pulls CV down).
	for _, tc := range []struct {
		kd    float64
		alpha float64 // DT / (KD/10 + DT) with DT = 1
	}{
		{kd: 10, alpha: 0.5},  // time constant 1 s
		{kd: 30, alpha: 0.25}, // time constant 3 s
	} {
		r := newPIDRig(t)
		r.set("kp", 2)
		r.set("kd", tc.kd)
		r.set("sp", 0)
		r.set("pv", 0)
		r.scan() // scan 1: no previous PV yet
		r.expect(1, "dTermOut", 0)
		r.set("pv", 10)
		d := 0.0
		for i := 2; i <= 6; i++ {
			raw := 0.0
			if i == 2 {
				raw = 10
			}
			d += tc.alpha * (raw - d)
			r.scan()
			r.expect(i, "dTermOut", -2*tc.kd*d)
		}
	}
}

// FN-133: DB is a deadband on error — within ±DB the P and I terms see zero
// error — while the D term still sees every PV move.
func TestPIDDeadband(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 2)
	r.set("ki", 0.5)
	r.set("kd", 10)
	r.set("db", 5)
	r.set("sp", 50)
	// PV wanders inside the band (error 3, 2, 4, -1, -4): P and I see zero.
	for i, pv := range []float64{47, 48, 46, 51, 54} {
		r.set("pv", pv)
		r.scan()
		r.expect(i+1, "errOut", 50-pv) // ERR is the raw error
		r.expect(i+1, "pTermOut", 0)
		r.expect(i+1, "iTermOut", 0)
		if i > 0 && r.get("dTermOut") == 0 {
			t.Fatalf("scan %d: D_TERM = 0 inside the deadband; it must still see the PV move", i+1)
		}
	}
	// Outside the band the full error (not error − DB) reaches P and I.
	r.set("pv", 40) // error 10
	r.scan()
	r.expect(6, "pTermOut", 2*10)
	r.scan()
	r.expect(7, "pTermOut", 2*10)
	r.expect(7, "iTermOut", 2*0.5*(10+10)) // two integrating scans at error 10
}

// FN-134: RESET = TRUE zeroes the integral on the scan it is TRUE; the
// integral builds again from zero once RESET drops.
func TestPIDResetZeroesIntegral(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 1)
	r.set("ki", 1)
	r.set("sp", 10)
	r.set("pv", 0) // error 10
	for i := 1; i <= 5; i++ {
		r.scan()
	}
	r.expect(5, "iTermOut", 40) // four integrating scans of 10·1 s
	r.setB("reset", true)
	r.scan()
	r.expect(6, "iTermOut", 0)
	r.expect(6, "cvOut", 10) // P alone on the reset scan
	r.setB("reset", false)
	r.scan()
	r.expect(7, "iTermOut", 10) // one scan of integration from zero
}

// FN-135: ERR is the error before the deadband, and P_TERM, I_TERM and
// D_TERM are the three contributions to CV:
// P = KP·error, I = KP·KI·∫error·dt, D = KP·KD·(filtered dPV/dt, sign by
// DIRECT), CV = P + I + D (inside the clamp).
func TestPIDDiagnosticTerms(t *testing.T) {
	for _, direct := range []bool{false, true} {
		r := newPIDRig(t)
		r.setB("direct", direct)
		r.set("kp", 2)
		r.set("ki", 0.5)
		r.set("kd", 10) // filter alpha 0.5 at DT = 1 s
		r.set("db", 0.5)
		r.set("sp", 50)
		sign := 1.0 // error = sign·(SP − PV); derivative term = −sign·KP·KD·d
		if direct {
			sign = -1
		}
		type want struct{ pv, err, p, i, d float64 }
		// Hand computation (reverse acting; direct flips every sign):
		//   scan 1  PV 40  error 10  P 20  I 0 (first AUTO scan)        d 0
		//   scan 2  PV 41  error  9  P 18  ∫ 9   I 2·0.5·9  = 9   d 0.5  D −10
		//   scan 3  PV 42  error  8  P 16  ∫ 17  I 2·0.5·17 = 17  d 0.75 D −15
		//   scan 4  PV 50.4 error −0.4 (inside DB: P and I see 0)  ∫ 17
		//           raw 8.4, d 0.75 + 0.5·(8.4 − 0.75) = 4.575  D −91.5
		steps := []want{
			{pv: 40, err: 10, p: 20, i: 0, d: 0},
			{pv: 41, err: 9, p: 18, i: 9, d: -10},
			{pv: 42, err: 8, p: 16, i: 17, d: -15},
			{pv: 50.4, err: -0.4, p: 0, i: 17, d: -91.5},
		}
		for k, s := range steps {
			r.set("pv", s.pv)
			r.scan()
			r.expect(k+1, "errOut", sign*s.err)
			r.expect(k+1, "pTermOut", sign*s.p)
			r.expect(k+1, "iTermOut", sign*s.i)
			r.expect(k+1, "dTermOut", sign*s.d)
			r.expect(k+1, "cvOut", sign*(s.p+s.i+s.d))
		}
	}
}

// FN-125: CV is clamped to [CV_MIN, CV_MAX] at both ends, and SAT_LO /
// SAT_HI say which.
func TestPIDClampsAtCVMin(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 1)
	r.set("cvMin", 10)
	r.set("cvMax", 90)
	r.set("sp", 0)
	r.set("pv", 1000) // asks for CV = -1000
	r.scan()
	r.expect(1, "cvOut", 10)
	if !r.getB("satLoOut") || r.getB("satHiOut") {
		t.Fatalf("SAT_LO=%v SAT_HI=%v, want TRUE/FALSE at the floor", r.getB("satLoOut"), r.getB("satHiOut"))
	}
	r.set("pv", -1000) // asks for CV = +1000
	r.scan()
	r.expect(2, "cvOut", 90)
	r.set("pv", -50) // asks for CV = 50: inside the range, untouched
	r.scan()
	r.expect(3, "cvOut", 50)
	if r.getB("satLoOut") || r.getB("satHiOut") {
		t.Fatal("SAT_LO/SAT_HI set with CV inside [CV_MIN, CV_MAX]")
	}
}

// FN-127: in MANUAL, CV follows a CV_MAN that moves, and the integral is
// back-solved every scan (P_TERM + I_TERM + D_TERM = CV_MAN on each manual
// scan, while PV and SP move too), so the switch to AUTO does not bump CV.
func TestPIDManualTracksMovingCVMan(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 2)
	r.set("ki", 0.25)
	r.set("cvMin", 0)
	r.set("cvMax", 100)
	r.set("sp", 50)
	r.set("pv", 45)
	r.scan() // one scan in AUTO first: the operator then takes the loop to MANUAL
	r.setB("auto", false)
	manual := []struct{ cvMan, sp, pv float64 }{
		{10, 50, 30}, {25, 50, 35}, {40, 60, 35}, {37, 55, 48}, {62, 70, 52},
	}
	for i, m := range manual {
		r.set("cvMan", m.cvMan)
		r.set("sp", m.sp)
		r.set("pv", m.pv)
		r.scan()
		r.expect(i+2, "cvOut", m.cvMan)
		sum := r.get("pTermOut") + r.get("iTermOut") + r.get("dTermOut")
		if math.Abs(sum-m.cvMan) > 1e-9 {
			t.Fatalf("manual scan %d: P+I+D = %.9f, want CV_MAN = %.9f (integral back-solved this scan)", i+2, sum, m.cvMan)
		}
	}
	r.setB("auto", true) // same PV and SP as the last manual scan
	r.scan()
	r.expect(len(manual)+2, "cvOut", 62)
}

// FN-128: conditional integration at the LOW rail — the integral does not
// wind further past CV_MIN, SAT_LO reports the clamp, and CV leaves the
// rail on the scan the error reverses.
func TestPIDAntiWindupLowRail(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 1)
	r.set("ki", 0.5)
	r.set("cvMin", 0)
	r.set("cvMax", 100)
	r.set("sp", 0)
	r.set("pv", 1000) // unreachable: saturates low forever
	var iTerm float64
	for i := 1; i <= 60; i++ {
		r.scan()
		r.expect(i, "cvOut", 0)
		if !r.getB("satLoOut") || r.getB("satHiOut") {
			t.Fatalf("scan %d: SAT_LO=%v SAT_HI=%v, want TRUE/FALSE", i, r.getB("satLoOut"), r.getB("satHiOut"))
		}
		if i > 1 && r.get("iTermOut") != iTerm {
			t.Fatalf("scan %d: I_TERM moved %.6f -> %.6f while clamped low", i, iTerm, r.get("iTermOut"))
		}
		iTerm = r.get("iTermOut")
	}
	// The error reverses (PV now 20 below SP): CV comes straight off the
	// floor. A wound-up integral (60 s × −1000 × 0.5) would hold it at 0.
	r.set("pv", -20)
	r.scan()
	if cv := r.get("cvOut"); cv < 19 {
		t.Fatalf("CV = %.3f after the error reversed, want it off the floor at once (~20)", cv)
	}
	if r.getB("satLoOut") {
		t.Fatal("SAT_LO still TRUE after CV left the floor")
	}
}

// FN-129: a bound DT is the seconds since the last call, whatever the
// scan clock says: here the clock advances 1 s per scan (and once 3 s),
// DT says 0.5 s, and the integral grows by error × 0.5 per scan.
func TestPIDBoundDTOverridesClock(t *testing.T) {
	r := newPIDRig(t)
	r.set("kp", 1)
	r.set("ki", 1)
	r.set("dt", 0.5)
	r.set("sp", 10)
	r.set("pv", 0) // error 10
	r.scan()
	r.scan()
	r.nowMs += 2000 // an irregular gap on the clock: DT still rules
	r.scan()
	r.scan()
	r.expect(4, "iTermOut", 10*0.5*3) // three integrating scans of 0.5 s
}
