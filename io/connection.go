package io

// Connection is one field connection inside a driver — one PLC, one meter,
// one BMC — and the input tags it delivers. A Sparkplug edge node publishes
// each as its own device, born and killed by that connection's health
// (docs/design/edge-collector.md §3), so one dead meter takes only its own
// tags offline.
type Connection struct {
	ID     string   // the driver's own id for it (a manifest sources: id)
	Inputs []string // the input tags it delivers, sorted
}

// ConnectionReporter is an optional refinement for a driver that holds
// several field connections: which tags each delivers, and whether each is
// up right now. A driver without it is one connection, as a whole.
type ConnectionReporter interface {
	Connections() []Connection
	// ConnectionUp is the connection's __Online verdict: true while it is
	// connected and answering. False for an id the driver does not have.
	ConnectionUp(id string) bool
}
