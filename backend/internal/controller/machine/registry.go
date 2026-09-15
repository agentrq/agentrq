// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"errors"
	"sync"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Conn is one daemon's socket, narrowed to what the registry needs.
//
// An interface rather than the WebSocket itself so the routing rules can be
// tested without a network — the same reason the pty layer sits behind one.
type Conn interface {
	// Send writes a frame to the daemon.
	Send(wire.Frame) error
	// Close ends the connection.
	Close() error
}

// ErrNotConnected is returned when nothing holds a machine's socket here.
//
// It does not mean the machine is offline: another backend instance may hold
// it. The caller checks the stored pairing and relays, and only then decides
// the machine is really gone.
var ErrNotConnected = errors.New("machine: not connected to this instance")

// Registry is the machines whose sockets this process holds.
//
// Per-process by nature, which is why the (machineId, instanceId) pairing is
// also written to the database: an attach arriving at a different instance
// finds nothing here and needs to know where to look.
type Registry struct {
	instanceID string

	mu    sync.RWMutex
	conns map[int64]Conn
}

// NewRegistry makes a registry for one backend instance.
//
// The instance id is what the pairing points at, so it must be stable for the
// life of the process and unique across instances — a pod name, or a
// configured value. Two instances sharing an id would route to each other's
// sockets.
func NewRegistry(instanceID string) *Registry {
	return &Registry{instanceID: instanceID, conns: map[int64]Conn{}}
}

// InstanceID names this process.
func (r *Registry) InstanceID() string { return r.instanceID }

// Add records a machine's socket, returning any connection it displaced.
//
// A reconnect must displace rather than be refused: the old socket is usually
// a dead TCP connection nobody has noticed yet, and refusing the new one would
// leave the machine unreachable until the old one timed out. The displaced
// connection is returned rather than closed here, so the caller decides —
// closing while holding the lock would block every other machine's traffic on
// one socket's shutdown.
func (r *Registry) Add(machineID int64, c Conn) (displaced Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	displaced = r.conns[machineID]
	r.conns[machineID] = c
	return displaced
}

// Remove drops a machine's socket, but **only if it is still this one**.
//
// The guard is the whole point. A slow disconnect on an old connection arrives
// after the daemon has already reconnected, and an unconditional delete would
// unroute a machine that is connected and healthy. This mirrors the same
// condition on the stored pairing, which is guarded for the same reason.
//
// Reports whether it removed anything, so a caller can tell "I was the holder"
// from "somebody else already took over".
func (r *Registry) Remove(machineID int64, c Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.conns[machineID]; !ok || current != c {
		return false
	}
	delete(r.conns, machineID)
	return true
}

// Get returns a machine's socket if this instance holds it.
func (r *Registry) Get(machineID int64) (Conn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[machineID]
	if !ok {
		return nil, ErrNotConnected
	}
	return c, nil
}

// Send delivers a frame to a machine held by this instance.
func (r *Registry) Send(machineID int64, f wire.Frame) error {
	c, err := r.Get(machineID)
	if err != nil {
		return err
	}
	return c.Send(f)
}

// Count is how many daemons this instance is holding, for metrics and for
// deciding whether a shutdown has drained.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns)
}

// Machines lists the machine ids this instance holds.
//
// A copy, because the caller iterating a live map while a daemon reconnects is
// a panic waiting for a busy afternoon.
func (r *Registry) Machines() []int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]int64, 0, len(r.conns))
	for id := range r.conns {
		out = append(out, id)
	}
	return out
}

// Drop closes and removes a machine's socket, whichever connection it is.
//
// This is revocation: disabling or deleting a machine has to take effect
// without the daemon's cooperation, so the socket is closed from this end
// rather than waiting for the daemon to notice.
func (r *Registry) Drop(machineID int64) bool {
	r.mu.Lock()
	c, ok := r.conns[machineID]
	if ok {
		delete(r.conns, machineID)
	}
	r.mu.Unlock()

	if !ok {
		return false
	}
	_ = c.Close()
	return true
}
