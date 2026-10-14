// client_test.go pins the exported tooling surface — Client, Decode, Encode
// — against the in-process slave. The internals are covered by
// slave_roundtrip_test.go; what matters here is that the exported seam the
// CLI builds on stays wired to the same codec the driver uses.
package modbus

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/modbus/slave"
)

func TestClientReadsAndDecodes(t *testing.T) {
	srv := slave.NewServer("", nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("slave start: %v", err)
	}
	t.Cleanup(srv.Stop)

	f, err := ParseFormat("float32")
	if err != nil {
		t.Fatal(err)
	}
	// Seed 12.5 word-swapped, the way an Anybus-fronted device stores it.
	regs, err := Encode(f, 12.5, OrderLittle, "", 0, 0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	u := srv.Unit(3)
	for i, r := range regs {
		u.SetHolding(uint16(10+i), r)
	}
	u.SetCoil(7, true)

	// The client and slave share our own framing code, so a symmetric
	// address-offset bug (client sends addr+1, slave subtracts 1) would pass
	// a plain round trip. Two independent anchors close that: a recording
	// proxy captures what the client really puts on the wire, and a raw
	// hand-built request checks what the slave really serves at an address.
	proxy := startRecordingProxy(t, srv.Addr())

	cl, err := Dial(context.Background(), proxy.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { cl.Close() })

	ctx := context.Background()
	got, err := cl.ReadRegisters(ctx, 3, TableHolding, 10, 2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	v, err := Decode(f, got, OrderLittle, "", 0, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v != 12.5 {
		t.Errorf("decoded %v, want 12.5", v)
	}
	// The same registers read as big word order must NOT be 12.5 — the
	// order actually matters, or this test proves nothing.
	if wrong, _ := Decode(f, got, OrderBig, "", 0, 0); wrong == v {
		t.Errorf("word order ignored: big and little both decode %v", v)
	}

	bits, err := cl.ReadBits(ctx, 3, TableCoil, 7, 1)
	if err != nil {
		t.Fatalf("read bits: %v", err)
	}
	if !bits[0] {
		t.Errorf("coil 7 = false, want true")
	}

	// Client side: unit 3, FC3 at address 10 x2, then FC1 at address 7 x1,
	// exactly as the spec lays them out (txids 1 and 2).
	wantSent := wire(t, "00 01 00 00 00 06 03 03 00 0A 00 02"+"00 02 00 00 00 06 03 01 00 07 00 01")
	if sent := proxy.sent(); !bytes.Equal(sent, wantSent) {
		t.Errorf("client put % X on the wire, want % X", sent, wantSent)
	}

	// Slave side: a hand-built FC3 for address 10 x2 must return the seeded
	// words, and address 9 (one below) must not.
	raw, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	reply := func(req string) []byte {
		t.Helper()
		raw.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := raw.Write(wire(t, req)); err != nil {
			t.Fatal(err)
		}
		hdr := make([]byte, 9)
		if _, err := io.ReadFull(raw, hdr); err != nil {
			t.Fatal(err)
		}
		rest := make([]byte, int(hdr[5])-3)
		if _, err := io.ReadFull(raw, rest); err != nil {
			t.Fatal(err)
		}
		return append(hdr, rest...)
	}
	if got, want := reply("00 09 00 00 00 06 03 03 00 0A 00 02"), wire(t, "00 09 00 00 00 07 03 03 04 00 00 41 48"); !bytes.Equal(got, want) {
		t.Errorf("slave served % X for holding 10, want % X", got, want)
	}
	if got := reply("00 0A 00 00 00 06 03 03 00 09 00 02"); bytes.Contains(got, wire(t, "41 48")) {
		t.Errorf("slave served the seeded words for holding 9: % X", got)
	}
	if got, want := reply("00 0B 00 00 00 06 03 01 00 07 00 01"), wire(t, "00 0B 00 00 00 04 03 01 01 01"); !bytes.Equal(got, want) {
		t.Errorf("slave served % X for coil 7, want % X", got, want)
	}
}

// recordingProxy forwards one client to the slave and keeps every byte the
// client sent, so a test can assert the request frames themselves.
type recordingProxy struct {
	addr string
	mu   sync.Mutex
	buf  bytes.Buffer
}

func (p *recordingProxy) sent() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.buf.Bytes()...)
}

func (p *recordingProxy) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}

func startRecordingProxy(t *testing.T, target string) *recordingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{addr: ln.Addr().String()}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		up, err := net.Dial("tcp", target)
		if err != nil {
			return
		}
		defer up.Close()
		go io.Copy(c, up)
		io.Copy(up, io.TeeReader(c, p))
	}()
	return p
}

func TestClientRejectsWrongTables(t *testing.T) {
	cl := &Client{}
	if _, err := cl.ReadRegisters(context.Background(), 1, TableCoil, 0, 1); err == nil {
		t.Errorf("ReadRegisters accepted a bit table")
	}
	if _, err := cl.ReadBits(context.Background(), 1, TableHolding, 0, 1); err == nil {
		t.Errorf("ReadBits accepted a register table")
	}
}
