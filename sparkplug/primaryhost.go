package sparkplug

import (
	"encoding/json"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// primaryhost.go is the *edge node's* watcher for its configured primary
// host's STATE topic. It is not the host application driver — that lives in
// the sparkplug/host subpackage.
//
// Primary-host STATE gating. When a PrimaryHostID is configured the edge node
// tracks that host's STATE birth-certificate: it defers its own birth until
// the host is ONLINE. What it does when the host goes offline is
// Config.PrimaryHostOffline:
//   - HostOfflineBuffer (the default): it keeps its session (no NDEATH,
//     still born) and stops publishing data, which buffers under
//     store-and-forward until the host returns.
//   - HostOfflineDisconnect, the Sparkplug 3.0 behaviour: it publishes
//     NDEATH, disconnects, connects again with the next bdSeq and waits for
//     the host to be ONLINE before it births (endSession).
//
// Either way the node buffers exactly when its consumer isn't listening.
// With no primary host these are no-ops and the node births on connect.
//
// Two STATE encodings coexist in the wild and we accept both:
//   - Sparkplug 2.x: topic "STATE/<host>", retained payload "ONLINE"/"OFFLINE".
//   - Sparkplug 3.0: topic "spBv1.0/STATE/<host>", retained JSON
//     {"online":bool,"timestamp":ms}.

// HostOfflineAction is what an edge node with a primary host does when that
// host's STATE goes offline.
type HostOfflineAction int

const (
	// HostOfflineBuffer keeps the MQTT session and the birth: no NDEATH,
	// no DATA until the host returns, and with store-and-forward the data
	// from meanwhile replays as historical. The default.
	HostOfflineBuffer HostOfflineAction = iota
	// HostOfflineDisconnect ends the session as Sparkplug 3.0 specifies
	// (tck-id-message-flow-edge-node-birth-publish-phid-offline): publish
	// NDEATH, disconnect, connect again and birth once the host is back.
	HostOfflineDisconnect
)

type hostState struct {
	Online    bool  `json:"online"`
	Timestamp int64 `json:"timestamp"`
}

// subscribeHost subscribes to the primary host's STATE topics. Safe to call on
// every (re)connect; no-op without a primary host.
func (n *Node) subscribeHost(c mqtt.Client) {
	if n.cfg.PrimaryHostID == "" {
		return
	}
	c.Subscribe("STATE/"+n.cfg.PrimaryHostID, 1, n.handleState)
	c.Subscribe("spBv1.0/STATE/"+n.cfg.PrimaryHostID, 1, n.handleState)
}

// disconnectsOnHostOffline reports whether the node ends its session when
// its primary host goes offline (HostOfflineDisconnect with a host set).
func (n *Node) disconnectsOnHostOffline() bool {
	return n.cfg.PrimaryHostID != "" && n.cfg.PrimaryHostOffline == HostOfflineDisconnect
}

// handleState processes a primary-host STATE message and drives birth/death on
// transitions. A monotonic-timestamp guard ignores stale retained replays.
func (n *Node) handleState(c mqtt.Client, msg mqtt.Message) {
	online, ts, ok := parseState(msg.Payload())
	if !ok {
		return
	}
	n.mu.Lock()
	if ts != 0 && ts < n.hostTS {
		n.mu.Unlock()
		return // older than what we've already seen — stale retained message
	}
	if ts != 0 {
		n.hostTS = ts
	}
	was := n.hostOnline
	n.hostOnline = online
	born := n.born
	n.mu.Unlock()

	if online && !was {
		n.log.Info("sparkplug: primary host online", "host", n.cfg.PrimaryHostID)
		if !born {
			if err := n.birth(); err != nil {
				n.log.Error("sparkplug: birth on host-online failed", "error", err)
			}
		}
		// If already born, the next scan will drain any store-forward backlog.
	} else if !online && was {
		if n.disconnectsOnHostOffline() {
			n.log.Warn("sparkplug: primary host offline, ending the session", "host", n.cfg.PrimaryHostID)
			// Not here: a paho message handler must not block on a publish
			// or on Disconnect (OrderMatters).
			go n.endSession(c)
			return
		}
		n.log.Warn("sparkplug: primary host offline", "host", n.cfg.PrimaryHostID)
		// Data now buffers (store-and-forward) until the host returns.
	}
}

// endSession is HostOfflineDisconnect's answer to an offline primary host,
// per Sparkplug 3.0: "it MUST immediately publish an NDEATH message and
// disconnect from the MQTT Server and start the connection establishment
// process over" (tck-id-message-flow-edge-node-birth-publish-phid-offline).
//
// The NDEATH is published, not left to the will: a clean MQTT DISCONNECT
// makes the broker discard the will, and under MQTT 3.1.1 the edge node
// MUST publish NDEATH before it sends DISCONNECT
// (tck-id-payloads-ndeath-will-message-publisher-disconnect-mqtt311). It
// carries this session's bdSeq, like the will it replaces.
//
// Then a new session (newSession, as after a lost connection): a new
// client whose will carries bdSeq+1, connecting with retry. Its onConnect
// subscribes to the host's STATE and births only once the host is ONLINE
// again (birth refuses while it is not). Meanwhile the node is unborn: with
// store-and-forward it keeps sampling into the buffer, and the first tick
// after the new birth replays it as historical.
//
// c is the client the offline STATE arrived on. If it is no longer n.cli,
// an earlier endSession has already ended that session (offline, online,
// offline in quick succession), and this one does nothing.
func (n *Node) endSession(c mqtt.Client) {
	n.newSession(c, true)
}

// parseState decodes either STATE encoding. Returns online, timestamp (0 if
// absent), and ok.
func parseState(payload []byte) (online bool, ts int64, ok bool) {
	s := strings.TrimSpace(string(payload))
	switch strings.ToUpper(s) {
	case "ONLINE":
		return true, 0, true
	case "OFFLINE":
		return false, 0, true
	}
	var hs hostState
	if err := json.Unmarshal(payload, &hs); err != nil {
		return false, 0, false
	}
	return hs.Online, hs.Timestamp, true
}

// primaryOnline reports whether the configured primary host is online. With no
// host configured it returns true (no gating).
func (n *Node) primaryOnline() bool {
	if n.cfg.PrimaryHostID == "" {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.hostOnline
}
