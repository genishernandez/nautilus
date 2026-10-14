package runtime_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/runtime"
)

// The page's driver-facing promises (tag-model: "Adding a field input end to
// end" and "Performance notes"), asserted through a scripted driver.

const doublerProg = `PROGRAM Main
VAR_EXTERNAL In : REAL; Out : REAL; END_VAR
Out := In * 2.0;
END_PROGRAM`

// scriptDriver delivers a fixed input map, counts the calls the runtime
// makes, and can be told to fail its next writes.
type scriptDriver struct {
	mu        sync.Mutex
	in        nio.Values
	reads     int
	writes    int
	failWrite bool
	lastOut   nio.Values
}

func (d *scriptDriver) ReadInputs() (nio.Values, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	out := make(nio.Values, len(d.in))
	for k, v := range d.in {
		out[k] = v
	}
	return out, nil
}

func (d *scriptDriver) WriteOutputs(v nio.Values) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes++
	if d.failWrite {
		return errors.New("bus write failed")
	}
	d.lastOut = nio.Values{}
	for k, x := range v {
		d.lastOut[k] = x
	}
	return nil
}

func (d *scriptDriver) writeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writes
}

func newDoubler(t *testing.T, drv nio.Driver, coord runtime.Coordinator) *runtime.Runtime {
	t.Helper()
	rt, err := runtime.New(runtime.Options{
		Program:     doublerProg,
		Driver:      drv,
		Inputs:      []string{"In"},
		Outputs:     []string{"Out"},
		Seed:        nio.Values{"Out": 0.0},
		Coordinator: coord,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return rt
}

// A driver input must be listed (role: input); a name the driver delivers
// that nothing declared never reaches the store.
func TestDriverCannotCreateUnlistedTags(t *testing.T) {
	drv := &scriptDriver{in: nio.Values{"In": 3.0, "Rogue": 99.0, "ZONE1_FIT_001": 1.5}}
	rt := newDoubler(t, drv, nil)
	rt.Scan()
	tags := rt.Tags()
	if v, err := tags.ReadGlobal("Out"); err != nil || v.F != 6 {
		t.Fatalf("the listed input must be delivered: Out = %v, %v (want 6)", v.F, err)
	}
	all := tags.All()
	for _, name := range []string{"Rogue", "ZONE1_FIT_001"} {
		if _, ok := all[name]; ok {
			t.Errorf("driver-delivered %q is not in the allowlist but landed in the store", name)
		}
		if _, err := tags.ReadGlobal(name); err == nil {
			t.Errorf("ReadGlobal(%q) found a tag the driver was never allowed to create", name)
		}
	}
}

// batchDriver implements io.BatchReader and records which map each call
// was handed; its plain ReadInputs must never be used.
type batchDriver struct {
	*scriptDriver
	t        *testing.T
	maps     []uintptr
	plainUse int
}

func (b *batchDriver) ReadInputs() (nio.Values, error) {
	b.plainUse++
	return b.scriptDriver.ReadInputs()
}

func (b *batchDriver) ReadInputsInto(dst nio.Values) error {
	b.maps = append(b.maps, reflect.ValueOf(dst).Pointer())
	for k, v := range b.in {
		dst[k] = v
	}
	return nil
}

func TestBatchReaderGetsTheSameMapEachScanAndPlainFallsBack(t *testing.T) {
	bd := &batchDriver{scriptDriver: &scriptDriver{in: nio.Values{"In": 1.0}}, t: t}
	rt := newDoubler(t, bd, nil)
	for i := 0; i < 4; i++ {
		rt.Scan()
	}
	if len(bd.maps) != 4 {
		t.Fatalf("ReadInputsInto called %d times over 4 scans, want 4", len(bd.maps))
	}
	for i, p := range bd.maps {
		if p != bd.maps[0] {
			t.Errorf("scan %d was handed a different map than scan 0", i)
		}
	}
	if bd.plainUse != 0 {
		t.Errorf("ReadInputs was called %d times though the driver implements BatchReader", bd.plainUse)
	}
	if v, _ := rt.Tags().ReadGlobal("Out"); v.F != 2 {
		t.Errorf("batch-delivered input did not drive the program: Out = %v", v.F)
	}

	// A driver without ReadInputsInto is read through ReadInputs.
	plain := &scriptDriver{in: nio.Values{"In": 5.0}}
	rt2 := newDoubler(t, plain, nil)
	rt2.Scan()
	rt2.Scan()
	if plain.reads != 2 {
		t.Errorf("plain driver ReadInputs called %d times over 2 scans, want 2", plain.reads)
	}
	if v, _ := rt2.Tags().ReadGlobal("Out"); v.F != 10 {
		t.Errorf("plain driver input did not drive the program: Out = %v", v.F)
	}
}

type leaderFlag struct{ lead bool }

func (l *leaderFlag) IsLeader() bool { return l.lead }

// WriteOutputs fires on the first scan, when an output moves, after a failed
// write and after a redundancy takeover — and is skipped otherwise.
func TestWriteOutputsTriggersFirstScanFailureAndTakeover(t *testing.T) {
	drv := &scriptDriver{in: nio.Values{"In": 1.0}}
	lead := &leaderFlag{lead: true}
	rt := newDoubler(t, drv, lead)

	rt.Scan()
	if got := drv.writeCount(); got != 1 {
		t.Fatalf("first scan: WriteOutputs called %d times, want 1", got)
	}
	rt.Scan()
	rt.Scan()
	if got := drv.writeCount(); got != 1 {
		t.Fatalf("quiet scans: WriteOutputs total %d, want still 1", got)
	}

	// After a failed write: the output moves, the write fails, and the next
	// quiet scan must retry the whole set.
	drv.mu.Lock()
	drv.in["In"] = 2.0
	drv.failWrite = true
	drv.mu.Unlock()
	rt.Scan()
	if got := drv.writeCount(); got != 2 {
		t.Fatalf("moved output: WriteOutputs total %d, want 2", got)
	}
	drv.mu.Lock()
	drv.failWrite = false
	drv.mu.Unlock()
	rt.Scan() // nothing changed since the failed push, but the driver never got it
	if got := drv.writeCount(); got != 3 {
		t.Fatalf("scan after a failed write: WriteOutputs total %d, want 3 (retry)", got)
	}
	if drv.lastOut["Out"] != 4.0 {
		t.Errorf("retry delivered Out = %v, want 4", drv.lastOut["Out"])
	}
	rt.Scan()
	if got := drv.writeCount(); got != 3 {
		t.Fatalf("after the retry succeeded: WriteOutputs total %d, want still 3", got)
	}

	// Redundancy takeover: step down, step up; nothing changed, but this
	// replica has never written to the field.
	lead.lead = false
	rt.Scan()
	lead.lead = true
	rt.Scan()
	if got := drv.writeCount(); got != 4 {
		t.Fatalf("first scan after a takeover: WriteOutputs total %d, want 4", got)
	}
	rt.Scan()
	if got := drv.writeCount(); got != 4 {
		t.Fatalf("quiet scan after takeover: WriteOutputs total %d, want still 4", got)
	}
}

// Every stored value carries the generation at which it last changed: a
// same-value rewrite leaves THAT TAG's generation alone while another tag's
// change moves its own.
func TestPerTagGenerationUnchangedBySameValueRewrite(t *testing.T) {
	tags := runtime.NewTags()
	tags.SetReal("Pump1Speed", 1)
	genA, _ := tags.TagGeneration("Pump1Speed")
	tags.SetReal("TankLevel", 5)
	genB, _ := tags.TagGeneration("TankLevel")
	if genB <= genA {
		t.Fatalf("a later creation must carry a later generation: %d then %d", genA, genB)
	}

	tags.SetReal("Pump1Speed", 1) // same value
	if got, _ := tags.TagGeneration("Pump1Speed"); got != genA {
		t.Errorf("a same-value rewrite moved the tag's generation %d → %d", genA, got)
	}
	tags.SetReal("Pump1Speed", 2)
	got, _ := tags.TagGeneration("Pump1Speed")
	if got <= genB {
		t.Errorf("a changed value must be stamped with the new generation, got %d (store was at %d)", got, genB)
	}
	if other, _ := tags.TagGeneration("TankLevel"); other != genB {
		t.Errorf("an unrelated tag's generation moved %d → %d", genB, other)
	}
}

// A function-block instance is never equal to anything, so rewriting the
// very same instance still advances the generation (it can over-report a
// change, never hide one).
func TestFunctionBlockInstanceNeverEqual(t *testing.T) {
	tags := runtime.NewTags()
	inst := &ir.FBInstance{}
	fb := ir.Value{Kind: ir.TypeFB, FB: inst}
	tags.Set("Timer1", fb)
	g := tags.Generation()
	tags.Set("Timer1", fb) // the identical instance
	if tags.Generation() == g {
		t.Error("re-writing a function-block instance was treated as unchanged")
	}
}

// SnapshotInto fills the caller's map in place: the map comes back as the
// same map, and a steady-state refill allocates nothing.
func TestSnapshotIntoRefillsTheCallersMapWithoutAllocating(t *testing.T) {
	tags := runtime.NewTags()
	for _, n := range []string{"A", "B", "C", "D"} {
		tags.SetReal(n, 1)
	}
	dst := make(map[string]runtime.Sample)
	got := tags.SnapshotInto(dst)
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(dst).Pointer() {
		t.Fatal("SnapshotInto returned a different map than the caller's")
	}
	if len(dst) != 4 {
		t.Fatalf("the caller's own map holds %d tags after the call, want 4", len(dst))
	}
	tags.SetReal("A", 2)
	if n := testing.AllocsPerRun(50, func() { tags.SnapshotInto(dst) }); n != 0 {
		t.Errorf("refilling a warm map allocated %v times per call, want 0", n)
	}
	if dst["A"].Value.F != 2 {
		t.Errorf("refill did not pick up the changed value: %v", dst["A"].Value.F)
	}
}
