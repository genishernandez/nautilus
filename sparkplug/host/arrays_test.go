// arrays_test.go: Sparkplug array metrics on the host side — array bindings
// and array template members decode into ir arrays, validated against the
// manifest's arraylen:, and array writes are refused (counted, logged), never
// sent mangled.

package host

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/sparkplug"
	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// arrayManifest is RTU53's shape in miniature: Get_TOD's gettime is an
// ARRAY [0..6] OF DINT member, and two metrics are arrays of their own.
func arrayManifest() Manifest {
	return Manifest{
		Group: "G",
		Types: []TypeDef{{Name: "Get_TOD", Fields: []FieldDef{
			{Name: "EnableIn", Type: "Boolean"},
			{Name: "gettime", Type: "Int32", ArrayLen: 7},
		}}},
		Nodes: []Node{{EdgeNode: "W6", Prefix: "W6", Devices: []Device{{Device: "RTU53"}}}},
		Tags: []Binding{
			{Name: "W6_RTC", Node: "W6", Device: "RTU53", Metric: "RTC", Type: "Get_TOD"},
			{Name: "W6_Levels", Node: "W6", Device: "RTU53", Metric: "Levels", Type: "Double", ArrayLen: 3},
			{Name: "W6_Flags", Node: "W6", Device: "RTU53", Metric: "Flags", Type: "Boolean", ArrayLen: 10},
		},
	}
}

func todInstance(name string, ms ...sparkplug.Metric) sparkplug.Metric {
	return sparkplug.Metric{Name: name, Datatype: spb.DataType_Template,
		Value: &sparkplug.Template{TemplateRef: "Get_TOD", Metrics: ms}}
}

func int32s(name string, xs ...int64) sparkplug.Metric {
	return sparkplug.Metric{Name: name, Datatype: spb.DataType_Int32Array, Value: xs}
}

func irInts(xs ...int64) ir.Value {
	v := ir.Value{Kind: ir.TypeArray, Arr: make([]ir.Value, len(xs))}
	for i, x := range xs {
		v.Arr[i] = ir.IntVal(x)
	}
	return v
}

// TestArrayBirthDecodesIntoTagStore — an NBIRTH carrying the Get_TOD
// definition (gettime an Int32Array, no value) and a DBIRTH carrying an
// instance plus two array metrics land as ir arrays of the manifest's length.
func TestArrayBirthDecodesIntoTagStore(t *testing.T) {
	d, _ := newTestDriver(t, arrayManifest(), Config{})
	def := sparkplug.Metric{Name: "Get_TOD", Datatype: spb.DataType_Template, Value: &sparkplug.Template{
		IsDefinition: true,
		Metrics: []sparkplug.Metric{
			{Name: "EnableIn", Datatype: spb.DataType_Boolean, IsNull: true},
			{Name: "gettime", Datatype: spb.DataType_Int32Array, IsNull: true},
		},
	}}
	d.handleMessage("spBv1.0/G/NBIRTH/W6", enc(t, nbirth(1, def)))
	d.handleMessage("spBv1.0/G/DBIRTH/W6/RTU53", enc(t, sparkplug.Payload{Timestamp: 1_000, Seq: 1, Metrics: []sparkplug.Metric{
		todInstance("RTC", boolean("EnableIn", true), int32s("gettime", 2026, 10, 7, 23, 59, 1, 0)),
		{Name: "Levels", Datatype: spb.DataType_FloatArray, Value: []float64{1.5, 2.5, -3}},
		{Name: "Flags", Datatype: spb.DataType_BooleanArray,
			Value: []bool{true, false, false, false, false, false, false, false, true, true}},
	}}))

	snap := d.snapshot()
	rtc, ok := mustValue(t, snap, "W6_RTC").(ir.Value)
	if !ok || rtc.Kind != ir.TypeStruct {
		t.Fatalf("W6_RTC = %#v", snap["W6_RTC"])
	}
	if !rtc.Fld[0].B || !reflect.DeepEqual(rtc.Fld[1], irInts(2026, 10, 7, 23, 59, 1, 0)) {
		t.Errorf("W6_RTC = %+v", rtc.Fld)
	}
	levels, ok := mustValue(t, snap, "W6_Levels").(ir.Value)
	if !ok || levels.Kind != ir.TypeArray || len(levels.Arr) != 3 || levels.Arr[2].F != -3 {
		t.Errorf("W6_Levels = %#v", snap["W6_Levels"])
	}
	flags, ok := mustValue(t, snap, "W6_Flags").(ir.Value)
	if !ok || len(flags.Arr) != 10 || !flags.Arr[0].B || flags.Arr[1].B || !flags.Arr[9].B {
		t.Errorf("W6_Flags = %#v", snap["W6_Flags"])
	}

	// A partial template carrying only gettime keeps EnableIn.
	d.handleMessage("spBv1.0/G/DDATA/W6/RTU53", enc(t, data(2, todInstance("RTC", int32s("gettime", 1, 2, 3, 4, 5, 6, 7)))))
	rtc = mustValue(t, d.snapshot(), "W6_RTC").(ir.Value)
	if !rtc.Fld[0].B || rtc.Fld[1].Arr[6].I != 7 {
		t.Errorf("after partial DDATA: %+v", rtc.Fld)
	}
}

// TestArrayLengthIsTheContract — an array of another length than the
// manifest's arraylen: is refused (and the previous value kept), whether it
// is a metric of its own or a template member.
func TestArrayLengthIsTheContract(t *testing.T) {
	d, _ := newTestDriver(t, arrayManifest(), Config{})
	d.handleMessage("spBv1.0/G/NBIRTH/W6", enc(t, nbirth(1)))
	d.handleMessage("spBv1.0/G/DBIRTH/W6/RTU53", enc(t, sparkplug.Payload{Timestamp: 1_000, Seq: 1, Metrics: []sparkplug.Metric{
		todInstance("RTC", boolean("EnableIn", true), int32s("gettime", 1, 2, 3, 4, 5, 6, 7)),
		{Name: "Levels", Datatype: spb.DataType_DoubleArray, Value: []float64{1, 2, 3}},
	}}))
	d.handleMessage("spBv1.0/G/DDATA/W6/RTU53", enc(t, data(2,
		todInstance("RTC", int32s("gettime", 9, 9, 9)),
		sparkplug.Metric{Name: "Levels", Datatype: spb.DataType_DoubleArray, Value: []float64{9, 9, 9, 9}},
	)))
	snap := d.snapshot()
	if rtc := snap["W6_RTC"].(ir.Value); rtc.Fld[1].Arr[0].I != 1 {
		t.Errorf("a 3-element gettime replaced the 7-element one: %+v", rtc.Fld[1])
	}
	if lv := snap["W6_Levels"].(ir.Value); len(lv.Arr) != 3 || lv.Arr[0].F != 1 {
		t.Errorf("a 4-element Levels replaced the 3-element one: %+v", lv)
	}
}

// TestArrayDiscoveryLine — an unmanifested array metric's suggested manifest
// line uses the element-plus-length form Validate accepts.
func TestArrayDiscoveryLine(t *testing.T) {
	d, _ := newTestDriver(t, arrayManifest(), Config{})
	d.handleMessage("spBv1.0/G/NBIRTH/W6", enc(t, nbirth(1,
		sparkplug.Metric{Name: "Extra", Datatype: spb.DataType_Int16Array, Value: []int64{1, 2, 3, 4}})))
	got := d.Discovered()
	if len(got) != 1 || !strings.Contains(got[0].YAML, "type: Int16, arraylen: 4") {
		t.Fatalf("discovered = %+v", got)
	}
	// And the line it suggests is one the manifest takes.
	m := arrayManifest()
	m.Tags = append(m.Tags, Binding{Name: "W6_Extra", Node: "W6", Metric: "Extra", Type: "Int16", ArrayLen: 4})
	if err := m.Validate(); err != nil {
		t.Errorf("suggested binding does not validate: %v", err)
	}
}

// TestMqttStructWriteSendsNoArrays — a writable struct binding whose type has
// an array member goes out as a template WITHOUT the array: the scalar
// member is commanded, the array is counted in WritesRefused.
func TestMqttStructWriteSendsNoArrays(t *testing.T) {
	srv, addr := startBroker(t, "")
	t.Cleanup(func() { _ = closeBroker(srv) })

	m := arrayManifest()
	m.Tags[0].Writable = true
	obs := newObserver(t, addr, "obs-arrwrite", "spBv1.0/G/DCMD/+/+")
	d, err := New(m, testConfig(addr, "h-arrwrite"), WithLogger(quietLogger()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d.Start(context.Background())
	t.Cleanup(d.Stop)
	waitConnected(t, d)
	bringW6Online(t, d, addr, "edge-arrwrite")
	baselineScan(t, d)

	v := ir.Zero(&ir.Type{Kind: ir.TypeStruct, Struct: d.defs["Get_TOD"]})
	v.Fld[0] = ir.BoolVal(true)
	v.Fld[1] = irInts(1, 2, 3, 4, 5, 6, 7)
	if err := d.WriteOutputs(map[string]any{"W6_RTC": v}); err != nil {
		t.Fatalf("WriteOutputs: %v", err)
	}
	msg := obs.wait(t, "DCMD", func(r recMsg) bool { return r.topic == "spBv1.0/G/DCMD/W6/RTU53" })
	p, err := sparkplug.DecodePayload(msg.payload)
	if err != nil {
		t.Fatal(err)
	}
	tm := tmplOf(t, p.Metrics[0])
	if len(tm.Metrics) != 1 || tm.Metrics[0].Name != "EnableIn" || tm.Metrics[0].Value != true {
		t.Errorf("DCMD template members = %+v, want EnableIn only", tm.Metrics)
	}
	deadline := time.Now().Add(2 * time.Second)
	for d.Status().WritesRefused == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := d.Status().WritesRefused; got != 1 {
		t.Errorf("WritesRefused = %d, want 1", got)
	}
}
