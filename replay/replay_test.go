package replay

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	nio "github.com/joyautomation/nautilus/io"
	"github.com/joyautomation/nautilus/lang/ir"
)

// A four-sample recording, 60 s apart: SW1_Port25 carries traffic and
// drops link for one sample; the first InBps sample was never scraped.
const recording = `{"t0": 1000, "step": 60, "n": 4,
 "bool": ["SW1_Port25.OperUp"],
 "series": {
  "SW1_Port25.InBps":  [null, 600, 1200, 1800],
  "SW1_Port25.OperUp": [1, 1, 0, 1],
  "SW1_Port25.SpeedMbps": [10000, 10000, null, 10000],
  "NEVER.Seen": [null, null, null, null]
 }}`

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

func TestLoadHistoryFillsGaps(t *testing.T) {
	for name, raw := range map[string][]byte{"plain": []byte(recording), "gzip": gz(t, recording)} {
		h, err := LoadHistory(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := h.Series["SW1_Port25.InBps"]; got[0] != 600 {
			t.Errorf("%s: a late series back-fills its first value: %v", name, got)
		}
		if got := h.Series["SW1_Port25.SpeedMbps"]; got[2] != 10000 {
			t.Errorf("%s: a gap holds the last value: %v", name, got)
		}
		if _, ok := h.Series["NEVER.Seen"]; ok {
			t.Errorf("%s: a series never sampled is absent", name)
		}
		if h.End() != 1180 {
			t.Errorf("%s: End = %d", name, h.End())
		}
	}
	if _, err := LoadHistory(strings.NewReader(`{"t0":1,"step":60,"n":2,"series":{"A.B":[1]}}`)); err == nil {
		t.Error("a short series must be an error")
	}
}

func TestHistoryAt(t *testing.T) {
	h, _ := LoadHistory(strings.NewReader(recording))
	cases := []struct {
		name string
		t    float64
		want float64
	}{
		{"SW1_Port25.InBps", 1090, 900},  // halfway between 600 and 1200
		{"SW1_Port25.InBps", 0, 600},     // before: clamped
		{"SW1_Port25.InBps", 9999, 1800}, // after: clamped
		{"SW1_Port25.OperUp", 1119, 1},   // a bool steps: still the 1 at 1060
		{"SW1_Port25.OperUp", 1120, 0},   // and drops exactly at its sample
	}
	for _, c := range cases {
		if got, _ := h.At(c.name, c.t); got != c.want {
			t.Errorf("%s at %v = %v, want %v", c.name, c.t, got, c.want)
		}
	}
}

func TestClock(t *testing.T) {
	c := Clock{From: 1000, To: 1180, Speed: 1}
	c.Seek(1000)
	t0 := time.Unix(0, 0)
	c.Advance(t0)
	if got := c.Advance(t0.Add(10 * time.Second)); got != 1010 {
		t.Fatalf("1×: %v", got)
	}
	c.Speed = 60
	if got := c.Advance(t0.Add(11 * time.Second)); got != 1070 {
		t.Fatalf("60×: %v", got)
	}
	c.Pause = true
	if got := c.Advance(t0.Add(20 * time.Second)); got != 1070 {
		t.Fatalf("paused: %v", got)
	}
	c.Pause = false
	if got := c.Advance(t0.Add(23 * time.Second)); got != 1070+180-180 || c.Loops != 1 {
		t.Fatalf("wrap: pos %v loops %d", got, c.Loops)
	}
	c.Seek(5000)
	if c.Pos != 1179 {
		t.Fatalf("seek past the end clamps inside: %v", c.Pos)
	}
}

const manifest = `history: rec.json.gz
tags:
  - name: Rec_SW1_Port25
    type: SwitchPort
    series: SW1_Port25
    const: { Name: "TGigaEthernet0/25", Index: 189, AdminUp: true }
`

func member(t *testing.T, v any, name string) ir.Value {
	t.Helper()
	iv, ok := v.(ir.Value)
	if !ok {
		t.Fatalf("not a struct: %T", v)
	}
	return iv.Fld[iv.Struct.FieldIndex[name]]
}

func TestDriverDelivers(t *testing.T) {
	m, err := ParseManifest([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	fsys := fstest.MapFS{"rec.json.gz": {Data: gz(t, recording)}}
	d, err := New(m, fsys, WithClock(func() time.Time { return now }), WithLogger(quiet()))
	if err != nil {
		t.Fatal(err)
	}
	d.Start(context.Background())
	v, err := d.ReadInputs()
	if err != nil {
		t.Fatal(err)
	}
	// InBps is back-filled before its first sample (1060): the loop starts there.
	if v[TagAt] != int64(1060) || v[TagFrom] != int64(1060) || v[TagTo] != int64(1180) {
		t.Fatalf("clock tags: %v %v %v", v[TagAt], v[TagFrom], v[TagTo])
	}
	p := v["Rec_SW1_Port25"]
	if member(t, p, "InBps").F != 600 || !member(t, p, "OperUp").B || member(t, p, "SpeedMbps").F != 10000 {
		t.Fatalf("recorded members: %+v", p)
	}
	if member(t, p, "Name").S != "TGigaEthernet0/25" || member(t, p, "Index").I != 189 || !member(t, p, "AdminUp").B {
		t.Fatal("constants")
	}
	if member(t, p, "OutBps").F != 0 {
		t.Fatal("a member neither recorded nor constant is zero")
	}

	// Steer it: 60× for 1 s lands on the link-down sample.
	if err := d.WriteOutputs(nio.Values{TagSpeed: 60.0, TagPause: false}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	v, _ = d.ReadInputs()
	if v[TagAt] != int64(1120) || member(t, v["Rec_SW1_Port25"], "OperUp").B {
		t.Fatalf("at %v: OperUp %v", v[TagAt], member(t, v["Rec_SW1_Port25"], "OperUp").B)
	}
	// Seek, as the HMI's scrubber does; the same value again is not a new seek.
	_ = d.WriteOutputs(nio.Values{TagSeek: int64(1060), TagPause: true})
	now = now.Add(5 * time.Second)
	v, _ = d.ReadInputs()
	if v[TagAt] != int64(1060) {
		t.Fatalf("seek: %v", v[TagAt])
	}
	_ = d.WriteOutputs(nio.Values{TagSeek: int64(1060), TagPause: false})
	now = now.Add(time.Second)
	v, _ = d.ReadInputs()
	if v[TagAt] != int64(1120) {
		t.Fatalf("an unchanged seek must not jump back: %v", v[TagAt])
	}
	h := d.Health()
	if !h.Loaded || h.Matched != 1 || h.Speed != 60 {
		t.Fatalf("health %+v", h)
	}
	if q := d.Quality(); q != nil {
		t.Fatalf("loaded: quality %v", q)
	}
}

func TestDriverWithoutItsRecording(t *testing.T) {
	m, _ := ParseManifest([]byte(manifest))
	d, err := New(m, fstest.MapFS{}, WithLogger(quiet()))
	if err != nil {
		t.Fatal("building must not need the recording: ", err)
	}
	d.Start(context.Background())
	v, err := d.ReadInputs()
	if err != nil || v[TagAt] != int64(0) {
		t.Fatalf("no recording: the clock reads 0 (%v, %v)", v[TagAt], err)
	}
	// Every tag is still delivered, at its constants, so a program reading
	// it does not fault.
	if p := v["Rec_SW1_Port25"]; member(t, p, "Index").I != 189 || member(t, p, "InBps").F != 0 {
		t.Fatalf("no recording: %+v", p)
	}
	if d.Quality()["Rec_SW1_Port25"] != nio.NotConnected {
		t.Fatal("tags must read NotConnected")
	}
	if h := d.Health(); h.Loaded || !strings.Contains(h.LastError, "rec.json.gz") {
		t.Fatalf("health %+v", h)
	}
}

// SeekS is a pulse: the driver jumps on a change, and 0 re-arms it, so
// T, 0, T jumps to T twice. Outputs arrive as a whole set whenever any one
// changes, so an unchanged SeekS beside a new Speed is not a seek.
func TestDriverSeekPulse(t *testing.T) {
	m, _ := ParseManifest([]byte(manifest))
	now := time.Unix(0, 0)
	d, err := New(m, fstest.MapFS{"rec.json.gz": {Data: gz(t, recording)}}, WithClock(func() time.Time { return now }), WithLogger(quiet()))
	if err != nil {
		t.Fatal(err)
	}
	d.Start(context.Background())
	at := func() int64 {
		t.Helper()
		v, err := d.ReadInputs()
		if err != nil {
			t.Fatal(err)
		}
		return v[TagAt].(int64)
	}
	step := func(vals nio.Values, wall time.Duration) int64 {
		t.Helper()
		if err := d.WriteOutputs(vals); err != nil {
			t.Fatal(err)
		}
		now = now.Add(wall)
		return at()
	}
	at()
	if got := step(nio.Values{TagSeek: int64(1100), TagSpeed: 10.0}, 2*time.Second); got != 1120 {
		t.Fatalf("seek to 1100, then 2 s at 10×: %v", got)
	}
	if got := step(nio.Values{TagSeek: int64(1100), TagSpeed: 20.0}, time.Second); got != 1140 {
		t.Fatalf("an unchanged SeekS beside a new Speed must not jump: %v", got)
	}
	if got := step(nio.Values{TagSeek: int64(0), TagSpeed: 20.0}, 0); got != 1140 {
		t.Fatalf("0 is idle, not a seek: %v", got)
	}
	if got := step(nio.Values{TagSeek: int64(1100), TagSpeed: 20.0}, 0); got != 1100 {
		t.Fatalf("T, 0, T must jump to T again: %v", got)
	}
}

// A recording that is absent at start is picked up once it appears: the
// driver looks again every retryEvery, without a restart.
func TestDriverRetriesAFailedLoad(t *testing.T) {
	m, _ := ParseManifest([]byte(manifest))
	now := time.Unix(0, 0)
	fsys := fstest.MapFS{}
	d, _ := New(m, fsys, WithClock(func() time.Time { return now }), WithLogger(quiet()))
	d.Start(context.Background())
	if h := d.Health(); h.Loaded || h.LastError == "" {
		t.Fatalf("absent: health %+v", h)
	}

	fsys["rec.json.gz"] = &fstest.MapFile{Data: []byte("not a recording"), ModTime: time.Unix(1, 0)}
	now = now.Add(retryEvery / 2)
	if _, err := d.ReadInputs(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Health().LastError, "does not exist") {
		t.Fatalf("looked again before retryEvery: %+v", d.Health())
	}
	now = now.Add(retryEvery)
	d.ReadInputs()
	if h := d.Health(); h.Loaded || strings.Contains(h.LastError, "does not exist") {
		t.Fatalf("a recording that appeared must be read: %+v", h)
	}
	if d.Quality()["Rec_SW1_Port25"] != nio.NotConnected {
		t.Fatal("a corrupt recording leaves the tags NotConnected")
	}

	// Fixed in place: read again once it has changed.
	fsys["rec.json.gz"] = &fstest.MapFile{Data: gz(t, recording), ModTime: time.Unix(2, 0)}
	now = now.Add(retryEvery)
	v, _ := d.ReadInputs()
	if h := d.Health(); !h.Loaded || h.LastError != "" || v[TagAt] != int64(1060) {
		t.Fatalf("after the fix: health %+v, at %v", h, v[TagAt])
	}
	if q := d.Quality(); q != nil {
		t.Fatalf("loaded: quality %v", q)
	}
}

// A file that has not changed since it failed is not parsed again.
func TestDriverDoesNotRereadAnUnchangedFailure(t *testing.T) {
	m, _ := ParseManifest([]byte(manifest))
	now := time.Unix(0, 0)
	opens := 0
	fsys := countingFS{MapFS: fstest.MapFS{"rec.json.gz": {Data: []byte("{"), ModTime: time.Unix(1, 0)}}, opens: &opens}
	d, _ := New(m, fsys, WithClock(func() time.Time { return now }), WithLogger(quiet()))
	d.Start(context.Background())
	for i := 0; i < 5; i++ {
		now = now.Add(retryEvery)
		d.ReadInputs()
	}
	if opens != 1 {
		t.Fatalf("an unchanged corrupt recording was opened %d times", opens)
	}
}

type countingFS struct {
	fstest.MapFS
	opens *int
}

func (c countingFS) Open(name string) (fs.File, error) {
	*c.opens++
	return c.MapFS.Open(name)
}

func (c countingFS) Stat(name string) (fs.FileInfo, error) { return c.MapFS.Stat(name) }

// A loop window that comes out empty — from: past the recording's end,
// to: before it is complete — is an error with the numbers that explain
// it, not a clock that runs off the end and holds the last sample forever.
func TestDriverEmptyWindowIsAnError(t *testing.T) {
	for name, bounds := range map[string]string{
		"from past the end":     "from: 1970-01-01T00:30:00Z\n", // 1800 > End 1180
		"to before complete":    "to: 1970-01-01T00:17:10Z\n",   // 1030 < 1060, InBps's first sample
		"from and to too early": "from: 1970-01-01T00:00:10Z\nto: 1970-01-01T00:00:20Z\n",
	} {
		t.Run(name, func(t *testing.T) {
			m, err := ParseManifest([]byte(strings.Replace(manifest, "tags:\n", bounds+"tags:\n", 1)))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(0, 0)
			d, _ := New(m, fstest.MapFS{"rec.json.gz": {Data: gz(t, recording)}}, WithClock(func() time.Time { return now }), WithLogger(quiet()))
			d.Start(context.Background())
			v, err := d.ReadInputs()
			if err != nil {
				t.Fatal(err)
			}
			h := d.Health()
			if h.Loaded || !strings.Contains(h.LastError, "loop window is empty") || !strings.Contains(h.LastError, "1970-01-01T00:17:40Z") {
				t.Fatalf("health %+v", h)
			}
			if v[TagAt] != int64(0) || d.Quality()["Rec_SW1_Port25"] != nio.NotConnected {
				t.Fatalf("an empty window plays nothing: at %v, quality %v", v[TagAt], d.Quality())
			}
		})
	}
}

// A tag the recording holds no series for delivers its constants and
// zeros — at Bad quality, not as a Good reading; its recorded siblings
// stay Good.
func TestDriverUnrecordedTagIsBad(t *testing.T) {
	m, err := ParseManifest([]byte(manifest + `  - name: Rec_SW9_Port01
    type: SwitchPort
    series: SW9_Port01
    const: { Index: 1 }
`))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := New(m, fstest.MapFS{"rec.json.gz": {Data: gz(t, recording)}}, WithLogger(quiet()))
	d.Start(context.Background())
	v, _ := d.ReadInputs()
	if member(t, v["Rec_SW9_Port01"], "Index").I != 1 {
		t.Fatal("constants are still delivered, so a program reading the tag does not fault")
	}
	q := d.Quality()
	if q["Rec_SW9_Port01"] != nio.Bad || len(q) != 1 {
		t.Fatalf("quality %v: want Rec_SW9_Port01 Bad and nothing else", q)
	}
	if h := d.Health(); !h.Loaded || h.Matched != 1 || h.Tags != 2 {
		t.Fatalf("health %+v", h)
	}
}

func TestManifestErrors(t *testing.T) {
	for _, bad := range []string{
		"tags: []",                    // no history
		"history: x\nfrom: yesterday", // bad time
		"history: x\nfrom: 2026-09-29T00:00:00Z\nto: 2026-09-28T00:00:00Z",
		"history: x\ntags: [{name: A, type: Blender, series: A}]",
		"history: x\ntags: [{name: A, type: SwitchPort}]",
		"history: x\ntags: [{name: A, type: SwitchPort, series: A, const: {Nmae: x}}]",
		"history: x\ntags: [{name: Replay_At, type: SwitchPort, series: A}]",
		"history: x\ntags: [{name: A, type: SwitchPort, series: A, const: {OperUp: maybe}}]",
		"history: x\ntgas: []",
	} {
		if _, err := ParseManifest([]byte(bad)); err == nil {
			t.Errorf("accepted:\n%s", bad)
		}
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A recording handed over directly (WithHistory) is judged once: an empty
// window is reported, and not re-derived (and re-logged) on every read.
func TestDriverGivenHistoryEmptyWindow(t *testing.T) {
	m, err := ParseManifest([]byte(strings.Replace(manifest, "tags:\n", "from: 1970-01-01T00:30:00Z\ntags:\n", 1)))
	if err != nil {
		t.Fatal(err)
	}
	h, _ := LoadHistory(strings.NewReader(recording))
	var logged bytes.Buffer
	d, _ := New(m, nil, WithHistory(h), WithLogger(slog.New(slog.NewTextHandler(&logged, nil))))
	d.Start(context.Background())
	for i := 0; i < 3; i++ {
		v, _ := d.ReadInputs()
		if v[TagAt] != int64(0) || member(t, v["Rec_SW1_Port25"], "InBps").F != 0 {
			t.Fatalf("an empty window delivers constants only: %v", v)
		}
	}
	if n := strings.Count(logged.String(), "loop window is empty"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, logged.String())
	}
	if h := d.Health(); h.Loaded || h.Series != 3 {
		t.Fatalf("health %+v", h)
	}
}
