package host

import (
	"context"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joyautomation/nautilus/sparkplug"
	"github.com/joyautomation/nautilus/sparkplug/internal/linkcut"
)

// TestEdgeBrokerDropIsANewSessionToTheHost runs a real edge through a link
// that drops, against a real host (#364). The broker fires the edge's will
// (bdSeq 0) and the host takes the node offline; the edge reconnects as a
// new session, bdSeq 1, and the host follows it. A will from the old
// session delivered late — the broker may fire it on takeover, after the
// new NBIRTH — is stale and leaves the node online; the next drop's will
// carries bdSeq 1 and is applied.
func TestEdgeBrokerDropIsANewSessionToTheHost(t *testing.T) {
	srv, addr := startBroker(t, "")
	t.Cleanup(func() { _ = closeBroker(srv) })
	px := linkcut.New(t, addr)

	host := startHostDriver(t, edgeDogfoodManifest(), testConfig(addr, "h-reconnect"))
	waitConnected(t, host)

	rt := buildEdgeRuntime(t)
	rtCtx, rtCancel := context.WithCancel(context.Background())
	t.Cleanup(rtCancel)
	go rt.Run(rtCtx)
	edge, err := sparkplug.New(rt, sparkplug.Config{
		BrokerURL:       brokerURL(px.Addr()),
		GroupID:         "G",
		EdgeNode:        "W6",
		PublishInterval: 50 * time.Millisecond,
		Log:             quietLogger(),
	})
	if err != nil {
		t.Fatalf("sparkplug.New: %v", err)
	}
	if err := edge.Start(context.Background()); err != nil {
		t.Fatalf("edge Start: %v", err)
	}
	t.Cleanup(edge.Stop)

	waitForValue(t, host, "W6__Online", func(v any) bool { return v == true })
	if bd := host.Status().Nodes[0].BdSeq; bd != 0 {
		t.Fatalf("host bdSeq %d after the first birth, want 0", bd)
	}

	// Drop: the will (bdSeq 0) matches, so the host applies it.
	px.Cut()
	waitForValue(t, host, "W6__Online", func(v any) bool { return v == false })

	// Reconnect: a new session, bdSeq 1. (The edge's first attempt went to
	// the cut link; the next is its retry interval later.)
	px.Restore()
	deadline := time.Now().Add(15 * time.Second)
	for {
		vals, _ := host.ReadInputs()
		if vals["W6__Online"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never saw the edge's new session")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bd := host.Status().Nodes[0].BdSeq; bd != 1 {
		t.Fatalf("host bdSeq %d after the reconnect, want 1", bd)
	}

	// A late will from the first session: stale, ignored.
	late := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL(addr)).
		SetClientID("late-will").SetCleanSession(true))
	if tok := late.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("connect: %v", tok.Error())
	}
	t.Cleanup(func() { late.Disconnect(50) })
	p, err := sparkplug.Payload{OmitSeq: true, Metrics: []sparkplug.Metric{bdSeq(0)}}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if tok := late.Publish("spBv1.0/G/NDEATH/W6", 1, false, p); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("publish: %v", tok.Error())
	}
	time.Sleep(300 * time.Millisecond)
	if vals, _ := host.ReadInputs(); vals["W6__Online"] != true {
		t.Fatal("a stale NDEATH (bdSeq 0) took the reconnected node offline")
	}

	// The will the broker holds now carries bdSeq 1: the next drop applies.
	px.Cut()
	waitForValue(t, host, "W6__Online", func(v any) bool { return v == false })
}
