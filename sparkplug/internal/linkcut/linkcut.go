// Package linkcut is a TCP proxy for tests that need to drop a live MQTT
// connection the way a network does: both sides see the socket close, with
// no MQTT DISCONNECT, so the broker fires the client's will.
package linkcut

import (
	"io"
	"net"
	"sync"
	"testing"
)

// Proxy forwards every connection it accepts to a target address.
type Proxy struct {
	ln     net.Listener
	target string

	mu      sync.Mutex
	conns   []net.Conn
	refused bool
}

// New starts a proxy on 127.0.0.1 in front of target; it closes with the test.
func New(t *testing.T, target string) *Proxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{ln: ln, target: target}
	t.Cleanup(func() {
		_ = ln.Close()
		p.closeAll()
	})
	go p.serve()
	return p
}

// Addr is the address clients dial instead of the target.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Cut closes every proxied connection, both sides, and refuses new ones
// (accepted, then closed at once) until Restore.
func (p *Proxy) Cut() {
	p.mu.Lock()
	p.refused = true
	p.mu.Unlock()
	p.closeAll()
}

// Restore lets connections through again.
func (p *Proxy) Restore() {
	p.mu.Lock()
	p.refused = false
	p.mu.Unlock()
}

func (p *Proxy) closeAll() {
	p.mu.Lock()
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (p *Proxy) serve() {
	for {
		down, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		refused := p.refused
		p.mu.Unlock()
		if refused {
			_ = down.Close()
			continue
		}
		up, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = down.Close()
			continue
		}
		p.mu.Lock()
		if p.refused { // cut while this one was dialling
			p.mu.Unlock()
			_ = up.Close()
			_ = down.Close()
			continue
		}
		// The broker side first, so it is the broker that notices first.
		p.conns = append(p.conns, up, down)
		p.mu.Unlock()
		go pipe(up, down)
		go pipe(down, up)
	}
}

func pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}
