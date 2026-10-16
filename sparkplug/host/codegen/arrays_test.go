package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/sparkplug"
	"github.com/joyautomation/nautilus/sparkplug/host"
	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// arrayBirths is an RTU53-shaped edge as nautilus publishes it: the Get_TOD
// definition's gettime is an Int64Array with no value (and so no length);
// the DBIRTH's instance carries the seven elements that give it one, beside
// an array metric of its own and an array that cannot be bound.
func arrayBirths(t *testing.T) []host.Birth {
	t.Helper()
	def := &sparkplug.Template{IsDefinition: true, Metrics: []sparkplug.Metric{
		{Name: "EnableIn", Datatype: spb.DataType_Boolean, IsNull: true},
		{Name: "gettime", Datatype: spb.DataType_Int64Array, IsNull: true},
		{Name: "orphan", Datatype: spb.DataType_Int32Array, IsNull: true},
	}}
	tod := func(xs ...int64) *sparkplug.Template {
		return &sparkplug.Template{TemplateRef: "Get_TOD", Metrics: []sparkplug.Metric{
			{Name: "EnableIn", Datatype: spb.DataType_Boolean, Value: true},
			{Name: "gettime", Datatype: spb.DataType_Int64Array, Value: xs},
			{Name: "orphan", Datatype: spb.DataType_Int32Array, IsNull: true},
		}}
	}
	node := sparkplug.Payload{Timestamp: 1, Metrics: []sparkplug.Metric{
		{Name: "bdSeq", Datatype: spb.DataType_Int64, Value: int64(0)},
		{Name: "Node Control/Rebirth", Datatype: spb.DataType_Boolean, Value: false},
		{Name: "Get_TOD", Datatype: spb.DataType_Template, Value: def},
	}}
	dev := sparkplug.Payload{Timestamp: 1, Seq: 1, Metrics: []sparkplug.Metric{
		{Name: "RTC", Datatype: spb.DataType_Template, Value: tod(2026, 10, 7, 23, 59, 1, 0)},
		{Name: "Levels", Datatype: spb.DataType_DoubleArray, Value: []float64{1, 2, 3}},
		{Name: "Flags", Datatype: spb.DataType_BooleanArray, Value: make([]bool, 10)},
		{Name: "Empty", Datatype: spb.DataType_Int16Array, Value: []int64{}},
	}}
	return []host.Birth{
		{Group: "G", EdgeNode: "edge1", Payload: wire(t, node)},
		{Group: "G", EdgeNode: "edge1", Device: "RTU53", Payload: wire(t, dev)},
	}
}

// TestFromBirthsArrays — the generator records arraylen: for array metrics
// (from the value) and array template members (from an instance, since the
// definition cannot say), reports what has no length, renders ARRAY
// declarations, and the manifest YAML round-trips.
func TestFromBirthsArrays(t *testing.T) {
	var skipped []string
	opts := Options{Writable: []string{"RTU53/Levels"}, OnSkip: func(s string) { skipped = append(skipped, s) }}
	m, err := FromBirths(arrayBirths(t), opts)
	if err != nil {
		t.Fatalf("FromBirths: %v", err)
	}

	if len(m.Types) != 1 {
		t.Fatalf("types = %+v", m.Types)
	}
	wantFields := []host.FieldDef{
		{Name: "EnableIn", Type: "Boolean"},
		{Name: "gettime", Type: "Int64", ArrayLen: 7},
	}
	if !reflect.DeepEqual(m.Types[0].Fields, wantFields) {
		t.Errorf("Get_TOD fields = %+v, want %+v", m.Types[0].Fields, wantFields)
	}
	levels := bindingNamed(t, m, "edge1_RTU53_Levels")
	if levels.Type != "Double" || levels.ArrayLen != 3 || levels.Writable {
		t.Errorf("Levels binding = %+v, want Double arraylen 3, read-only", levels)
	}
	if flags := bindingNamed(t, m, "edge1_RTU53_Flags"); flags.Type != "Boolean" || flags.ArrayLen != 10 {
		t.Errorf("Flags binding = %+v", flags)
	}
	for _, b := range m.Tags {
		if b.Metric == "Empty" {
			t.Errorf("an empty array was bound (ARRAY [0..-1] is not a type): %+v", b)
		}
	}
	for _, want := range []string{"orphan", "Empty", "Levels: Double array bound read-only"} {
		found := false
		for _, s := range skipped {
			found = found || strings.Contains(s, want)
		}
		if !found {
			t.Errorf("no skip report mentions %q: %v", want, skipped)
		}
	}

	st, err := TypesST(m)
	if err != nil {
		t.Fatalf("TypesST: %v", err)
	}
	for _, want := range []string{"gettime : ARRAY [0..6] OF LINT;", "edge1_RTU53_Levels : ARRAY [0..2] OF LREAL;",
		"edge1_RTU53_Flags : ARRAY [0..9] OF BOOL;"} {
		if !strings.Contains(string(st), want) {
			t.Errorf("TypesST lacks %q:\n%s", want, st)
		}
	}

	y, err := ManifestYAML(m)
	if err != nil {
		t.Fatalf("ManifestYAML: %v", err)
	}
	back, err := host.ParseManifest(y)
	if err != nil {
		t.Fatalf("ParseManifest: %v\n%s", err, y)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("round-tripped manifest invalid: %v", err)
	}
	if !reflect.DeepEqual(back.Types, m.Types) || !reflect.DeepEqual(back.Bindings(), m.Bindings()) {
		t.Errorf("YAML round trip changed the manifest:\n%s", y)
	}
}

// TestArrayLengthsMustAgree — two instances of one type with different
// array lengths cannot share one manifest type.
func TestArrayLengthsMustAgree(t *testing.T) {
	births := arrayBirths(t)
	second := sparkplug.Payload{Timestamp: 1, Seq: 2, Metrics: []sparkplug.Metric{
		{Name: "RTC2", Datatype: spb.DataType_Template, Value: &sparkplug.Template{TemplateRef: "Get_TOD", Metrics: []sparkplug.Metric{
			{Name: "gettime", Datatype: spb.DataType_Int64Array, Value: []int64{1, 2}},
		}}},
	}}
	births = append(births, host.Birth{Group: "G", EdgeNode: "edge1", Device: "RTU54", Payload: wire(t, second)})
	if _, err := FromBirths(births, Options{}); err == nil || !strings.Contains(err.Error(), "one length per member") {
		t.Errorf("err = %v, want a length conflict", err)
	}
}
