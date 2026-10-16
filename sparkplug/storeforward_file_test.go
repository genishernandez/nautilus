// storeforward_file_test.go: the store-and-forward file (#392). A process
// that exits during an outage keeps its backlog: the next one loads it and
// replays it, as historical and with its original timestamps, after its
// first birth. Without a file nothing touches the disk.

package sparkplug

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/sparkplug/internal/linkcut"
	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// logBuf is a concurrency-safe slog sink a test can search.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newLogBuf() (*logBuf, *slog.Logger) {
	lb := &logBuf{}
	return lb, slog.New(slog.NewTextHandler(lb, nil))
}

// fileSF is a store-forward ring of max journaled at path.
func fileSF(path string, max int, log *slog.Logger) *storeForward {
	return &storeForward{max: max, path: path, log: log}
}

func rec(i int) sfRecord {
	return sfRecord{
		topic: "spBv1.0/Plant/NDATA/Edge1",
		ts:    uint64(1_700_000_000_000 + i),
		metrics: []Metric{{
			Name: "LevelSP", Datatype: spb.DataType_Double, Value: float64(i),
			Timestamp: uint64(1_700_000_000_000 + i),
		}},
	}
}

// loadedTS opens a fresh ring of max on path and returns the timestamps it
// loaded, closing it again.
func loadedTS(t *testing.T, path string, max int) []uint64 {
	t.Helper()
	_, log := newLogBuf()
	return loadedTSWith(t, path, max, log)
}

func tsRange(from, to int) []uint64 {
	var out []uint64
	for i := from; i <= to; i++ {
		out = append(out, rec(i).ts)
	}
	return out
}

func eqTS(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// What is buffered is what the next process loads, in order, with every
// field the replay needs; what a drain delivered is not.
func TestStoreForwardFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sf.log")
	_, log := newLogBuf()
	sf := fileSF(path, 100, log)
	if err := sf.open(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		sf.enqueue(rec(i))
	}
	sf.close()

	sf2 := fileSF(path, 100, log)
	if err := sf2.open(); err != nil {
		t.Fatal(err)
	}
	got := sf2.drainBatch(10)
	if len(got) != 5 {
		t.Fatalf("loaded %d records, want 5", len(got))
	}
	for i, r := range got {
		want := rec(i + 1)
		m := r.metrics[0]
		if r.topic != want.topic || r.ts != want.ts || len(r.metrics) != 1 ||
			m.Name != "LevelSP" || m.Value != float64(i+1) || m.Timestamp != want.metrics[0].Timestamp || m.IsHistorical {
			t.Fatalf("record %d = %+v, want %+v", i, r, want)
		}
	}
	// Two of them went out: the next process loads the other three.
	sf2.requeue(got[2:])
	sf2.delivered(got[:2])
	sf2.close()
	if got := loadedTS(t, path, 100); !eqTS(got, tsRange(3, 5)) {
		t.Fatalf("after delivering two, loaded %v, want %v", got, tsRange(3, 5))
	}

	// All of them went out: the file is cleared to its header.
	sf3 := fileSF(path, 100, log)
	if err := sf3.open(); err != nil {
		t.Fatal(err)
	}
	sf3.delivered(sf3.drainBatch(10))
	if sz := fileSize(t, path); sz != int64(len(sfMagic)) {
		t.Fatalf("after the backlog went out the file is %d bytes, want the %d-byte header", sz, len(sfMagic))
	}
	sf3.close()
	if got := loadedTS(t, path, 100); len(got) != 0 {
		t.Fatalf("a cleared file loaded %v", got)
	}
}

// The file is bounded like the ring: it loads as the newest max records,
// and compaction keeps it from growing without bound in a long outage.
func TestStoreForwardFileIsBoundedLikeTheRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sf.log")
	_, log := newLogBuf()
	const max = 10
	sf := fileSF(path, max, log)
	if err := sf.open(); err != nil {
		t.Fatal(err)
	}
	var peak, frame int64
	for i := 1; i <= 1000; i++ {
		sf.enqueue(rec(i))
		if i == 1 {
			frame = fileSize(t, path) - int64(len(sfMagic))
		}
		if sz := fileSize(t, path); sz > peak {
			peak = sz
		}
		if f := sf.j.frames; f > 2*max {
			t.Fatalf("after %d records the file holds %d frames, over twice the ring", i, f)
		}
	}
	if sf.len() != max || sf.dropped != 1000-max {
		t.Fatalf("ring len=%d dropped=%d, want %d/%d", sf.len(), sf.dropped, max, 1000-max)
	}
	// A drain interleaved with drops: the acks still line up.
	b := sf.drainBatch(4)
	sf.delivered(b)
	for i := 1001; i <= 1003; i++ {
		sf.enqueue(rec(i))
	}
	sf.close()
	if got, want := loadedTS(t, path, max), tsRange(995, 1003); !eqTS(got, want) {
		t.Fatalf("loaded %v, want %v", got, want)
	}
	// A smaller ring on the next start keeps the newest.
	if got, want := loadedTS(t, path, 3), tsRange(1001, 1003); !eqTS(got, want) {
		t.Fatalf("a ring of 3 loaded %v, want %v", got, want)
	}
	if limit := int64(len(sfMagic)) + (2*max+1)*frame; peak > limit {
		t.Fatalf("the file peaked at %d bytes, over %d (2×ring+1 frames of %d)", peak, limit, frame)
	}
}

// A crash mid-write leaves a torn last frame: the records before it load,
// the torn one is skipped with a log line, and nothing is set aside.
func TestStoreForwardFileSkipsATornLastRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sf.log")
	_, log := newLogBuf()
	sf := fileSF(path, 100, log)
	if err := sf.open(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		sf.enqueue(rec(i))
	}
	sf.close()
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{1, 5, 9, 20} {
		if err := os.WriteFile(path, full[:len(full)-cut], 0o644); err != nil {
			t.Fatal(err)
		}
		lb, log := newLogBuf()
		sf := fileSF(path, 100, log)
		if err := sf.open(); err != nil {
			t.Fatalf("cut %d: open: %v", cut, err)
		}
		got := sf.drainBatch(10)
		sf.close()
		if len(got) != 2 || got[0].ts != rec(1).ts || got[1].ts != rec(2).ts {
			t.Fatalf("cut %d: loaded %d records, want the first 2", cut, len(got))
		}
		if !strings.Contains(lb.String(), "torn last record") {
			t.Fatalf("cut %d: no log line for the torn record:\n%s", cut, lb)
		}
		if aside, _ := filepath.Glob(path + ".corrupt-*"); len(aside) != 0 {
			t.Fatalf("cut %d: a torn tail was set aside: %v", cut, aside)
		}
	}
	// A crash while creating the file: a torn header is an empty backlog.
	if err := os.WriteFile(path, []byte(sfMagic[:3]), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadedTS(t, path, 100); len(got) != 0 {
		t.Fatalf("a torn header loaded %v", got)
	}
}

// A file that is not a journal at all is renamed aside and logged, and
// buffering starts empty; one damaged part-way loads the records before the
// damage and is set aside too, so nothing past the damage is overwritten.
func TestStoreForwardFileSetsAnUnreadableFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sf.log")
	if err := os.WriteFile(path, []byte("this is not a store-forward file at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lb, log := newLogBuf()
	sf := fileSF(path, 100, log)
	if err := sf.open(); err != nil {
		t.Fatalf("an unreadable file failed the start: %v", err)
	}
	if sf.len() != 0 {
		t.Fatalf("loaded %d records from garbage", sf.len())
	}
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) != 1 {
		t.Fatalf("set aside: %v, want one file", aside)
	}
	if b, _ := os.ReadFile(aside[0]); !strings.HasPrefix(string(b), "this is not") {
		t.Fatalf("the set-aside file is not the original: %q", b)
	}
	if !strings.Contains(lb.String(), "unreadable") {
		t.Fatalf("no log line for the unreadable file:\n%s", lb)
	}
	sf.enqueue(rec(1)) // and the journal works from empty
	sf.close()
	if got := loadedTS(t, path, 100); !eqTS(got, tsRange(1, 1)) {
		t.Fatalf("after starting over, loaded %v", got)
	}
	_ = os.Remove(aside[0])

	// Damage in the middle: the second of three frames' body.
	sf = fileSF(path, 100, log)
	if err := sf.open(); err != nil {
		t.Fatal(err)
	}
	sf.enqueue(rec(2))
	sf.enqueue(rec(3))
	sf.close()
	b, _ := os.ReadFile(path)
	first := len(sfMagic) + 8 + int(beUint32(b[len(sfMagic):]))
	b[first+8+2] ^= 0xff // inside frame 2's body
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	lb, log = newLogBuf()
	if got := loadedTSWith(t, path, 100, log); !eqTS(got, tsRange(1, 1)) {
		t.Fatalf("damaged part-way, loaded %v, want the record before the damage", got)
	}
	if aside, _ := filepath.Glob(path + ".corrupt-*"); len(aside) != 1 {
		t.Fatalf("damaged part-way, set aside: %v, want one file", aside)
	}
	if !strings.Contains(lb.String(), "damaged part-way") {
		t.Fatalf("no log line for the damage:\n%s", lb)
	}
}

func beUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func loadedTSWith(t *testing.T, path string, max int, log *slog.Logger) []uint64 {
	t.Helper()
	sf := fileSF(path, max, log)
	if err := sf.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sf.close()
	var out []uint64
	for _, r := range sf.drainBatch(sf.len()) {
		out = append(out, r.ts)
	}
	return out
}

// A file the node cannot create is an error at Start, not a silent
// memory-only buffer; and the file needs the buffer it journals.
func TestStoreForwardFileMisconfiguration(t *testing.T) {
	rt := newRuntimeForGate(t)
	cfg := Config{BrokerURL: "tcp://127.0.0.1:1", GroupID: "Plant", EdgeNode: "Edge1"}
	if _, err := New(rt, cfg, WithStoreForwardFile("sf.log")); err == nil ||
		!strings.Contains(err.Error(), "needs WithStoreForward") {
		t.Fatalf("a file without store-forward: %v", err)
	}
	n, err := New(rt, cfg, WithStoreForward(10),
		WithStoreForwardFile(filepath.Join(t.TempDir(), "no", "such", "dir", "sf.log")))
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "store-forward file") {
		t.Fatalf("Start with an unwritable file: %v", err)
	}
}

// Without a file (the default) a node buffering through an outage creates
// nothing on disk.
func TestStoreForwardWithoutAFileTouchesNoDisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	addr := testBroker(t)
	px := linkcut.New(t, addr)
	events := watchNode(t, addr)
	n, rt := startProxiedNode(t, px.Addr(), Config{}, WithStoreForward(100))
	if n.sf.path != "" || n.sf.j != nil {
		t.Fatalf("the default has a journal: path %q", n.sf.path)
	}
	nextOf(t, events, "NBIRTH")
	px.Cut()
	nextOf(t, events, "NDEATH")
	rt.Tags().Set("LevelSP", ir.RealVal(77))
	waitFor(t, "the change to buffer", func() bool { return n.Status().StoreForward.Buffered >= 1 })
	px.Restore()
	nextOf(t, events, "NBIRTH")
	if ev := nextOf(t, events, "NDATA"); !ev.historical || ev.level != 77 {
		t.Fatalf("replay %+v", ev)
	}
	n.Stop()
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("a node without store-forward-file wrote %v", ents)
	}
}

// rawEvent is one edge-node message with its decoded payload.
type rawEvent struct {
	kind string
	p    Payload
}

func watchRaw(t *testing.T, addr string) chan rawEvent {
	t.Helper()
	ch := make(chan rawEvent, 256)
	cli := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + addr).
		SetClientID("raw-watcher").SetCleanSession(true).SetOrderMatters(true))
	if tok := cli.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("watcher connect: %v", tok.Error())
	}
	t.Cleanup(func() { cli.Disconnect(50) })
	tok := cli.Subscribe("spBv1.0/Plant/+/Edge1", 1, func(_ mqtt.Client, m mqtt.Message) {
		p, err := DecodePayload(m.Payload())
		if err != nil {
			return
		}
		ch <- rawEvent{kind: strings.Split(m.Topic(), "/")[2], p: p}
	})
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("watcher subscribe: %v", tok.Error())
	}
	return ch
}

func nextRaw(t *testing.T, ch chan rawEvent, kinds ...string) rawEvent {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-ch:
			for _, k := range kinds {
				if ev.kind == k {
					return ev
				}
			}
		case <-deadline:
			t.Fatalf("no %v arrived", kinds)
			return rawEvent{}
		}
	}
}

// bufferedRec is one record a node's ring holds: each record's payload timestamp
// and LevelSP's metric timestamp and value.
type bufferedRec struct {
	ts, metricTS uint64
	level        float64
}

func buffered(n *Node) []bufferedRec {
	n.sf.mu.Lock()
	defer n.sf.mu.Unlock()
	var out []bufferedRec
	for _, r := range n.sf.buf {
		for _, m := range r.metrics {
			if m.Name == "LevelSP" {
				out = append(out, bufferedRec{r.ts, m.Timestamp, m.Value.(float64)})
			}
		}
	}
	return out
}

// expectReplay reads the NDATA after the next NBIRTH and checks it is the
// backlog want — historical, in order, with the original timestamps — and
// that the file is cleared once it has gone out.
func expectReplay(t *testing.T, ch chan rawEvent, want []bufferedRec, path string) {
	t.Helper()
	nextRaw(t, ch, "NBIRTH")
	for i, w := range want {
		ev := nextRaw(t, ch, "NDATA", "NBIRTH", "NDEATH")
		if ev.kind != "NDATA" {
			t.Fatalf("replay %d: %s", i, ev.kind)
		}
		if ev.p.Timestamp != w.ts {
			t.Errorf("replay %d: payload timestamp %d, want the original %d", i, ev.p.Timestamp, w.ts)
		}
		var found bool
		for _, m := range ev.p.Metrics {
			if m.Name != "LevelSP" {
				continue
			}
			found = true
			if !m.IsHistorical || m.Value != w.level || m.Timestamp != w.metricTS {
				t.Errorf("replay %d: LevelSP %v at %d historical=%v, want %v at %d historical",
					i, m.Value, m.Timestamp, m.IsHistorical, w.level, w.metricTS)
			}
		}
		if !found {
			t.Fatalf("replay %d carries no LevelSP: %+v", i, ev.p)
		}
	}
	waitFor(t, "the file to be cleared after the replay", func() bool {
		fi, err := os.Stat(path)
		return err == nil && fi.Size() == int64(len(sfMagic))
	})
}

// The broker drops; the node buffers to its file; the process stops in the
// middle of the outage (a restart to fix it). A new node on the same file
// births and replays that backlog as historical, with its original
// timestamps, then clears the file.
func TestStoreForwardFileSurvivesARestartDuringABrokerOutage(t *testing.T) {
	addr := testBroker(t)
	px := linkcut.New(t, addr)
	events := watchRaw(t, addr)
	path := filepath.Join(t.TempDir(), "sf.log")
	n1, rt1 := startProxiedNode(t, px.Addr(), Config{}, WithStoreForward(100), WithStoreForwardFile(path))
	nextRaw(t, events, "NBIRTH")

	px.Cut()
	nextRaw(t, events, "NDEATH") // the broker fires the will
	for _, v := range []float64{77, 78, 79} {
		rt1.Tags().Set("LevelSP", ir.RealVal(v))
		want := int(v - 76)
		waitFor(t, "the change to buffer", func() bool { return n1.Status().StoreForward.Buffered >= want })
	}
	backlog := buffered(n1)
	if len(backlog) != 3 {
		t.Fatalf("buffered %+v, want 77, 78, 79", backlog)
	}
	n1.Stop() // the process exits mid-outage
	if fileSize(t, path) <= int64(len(sfMagic)) {
		t.Fatal("the stopped node left an empty file")
	}

	// The restart, with the broker back.
	time.Sleep(5 * time.Millisecond) // a later clock, so a fresh timestamp could not pass for the original
	n2, _ := startProxiedNode(t, addr, Config{}, WithStoreForward(100), WithStoreForwardFile(path))
	expectReplay(t, events, backlog, path)
	if got := n2.Status().StoreForward.Buffered; got != 0 {
		t.Errorf("after the replay %d still buffered", got)
	}
}

// The same across a primary host outage, in both primary-host-offline
// modes: the node buffers while the host is offline, restarts, waits for
// the host, and replays after the birth.
func TestStoreForwardFileSurvivesARestartDuringAHostOutage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action HostOfflineAction
	}{{"buffer", HostOfflineBuffer}, {"disconnect", HostOfflineDisconnect}} {
		t.Run(tc.name, func(t *testing.T) {
			addr := testBroker(t)
			events := watchRaw(t, addr)
			setState := stateHost(t, addr)
			path := filepath.Join(t.TempDir(), "sf.log")
			setState(true, 1000)
			n1, rt1 := startHostNode(t, addr, tc.action, "", WithStoreForward(100), WithStoreForwardFile(path))
			nextRaw(t, events, "NBIRTH")

			setState(false, 2000)
			if tc.action == HostOfflineDisconnect {
				nextRaw(t, events, "NDEATH")
			} else {
				waitFor(t, "the host to go offline", func() bool { return !n1.Status().HostOnline })
			}
			for _, v := range []float64{55, 56} {
				rt1.Tags().Set("LevelSP", ir.RealVal(v))
				want := int(v - 54)
				waitFor(t, "the change to buffer", func() bool { return n1.Status().StoreForward.Buffered >= want })
			}
			backlog := buffered(n1)
			n1.Stop()

			n2, _ := startHostNode(t, addr, tc.action, "", WithStoreForward(100), WithStoreForwardFile(path))
			if got := n2.Status().StoreForward.Buffered; got != 2 {
				t.Fatalf("the restarted node loaded %d records, want 2", got)
			}
			// The host is still offline: no birth, so no replay.
			time.Sleep(300 * time.Millisecond)
			if n2.Status().Born {
				t.Fatal("born with the primary host offline")
			}
			setState(true, 3000)
			expectReplay(t, events, backlog, path)
		})
	}
}
