// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Viewer is one browser watching a session.
type Viewer interface {
	Send(wire.Frame) error
	Close() error
	// Name is what the other viewers are shown. A display name, never an
	// email address: presence answers "who else is typing", and that does not
	// require handing every viewer another person's contact details.
	Name() string
}

// Errors from attaching a viewer.
var (
	ErrSessionNotHere = errors.New("machine: that session's machine is held by another instance")
	ErrRateExceeded   = errors.New("machine: session output rate exceeded")
)

// RateLimit bounds one session's output through the relay.
//
// A terminal a person is reading produces a few KB a second. Megabytes a
// second is not somebody working, it is a process that has gone wrong — and
// the right response is to say so rather than to throttle it silently
// forever, which just moves the problem somewhere nobody is looking.
const RateLimit = 2 << 20 // 2 MiB/s

// RateWindow is the period the limit is measured over.
const RateWindow = time.Second

// Relay carries frames between the viewers of a session and its daemon.
//
// The backend never parses the stream. It authorises the attach and then
// copies bytes — a relay that interpreted terminal output would be a relay
// that could get it wrong.
type Relay struct {
	registry *Registry

	mu       sync.RWMutex
	viewers  map[uint64]map[Viewer]struct{} // session → viewers
	machines map[uint64]int64               // session → machine
	rates    map[uint64]*rate
}

// NewRelay makes one.
func NewRelay(r *Registry) *Relay {
	return &Relay{
		registry: r,
		viewers:  map[uint64]map[Viewer]struct{}{},
		machines: map[uint64]int64{},
		rates:    map[uint64]*rate{},
	}
}

// Attach registers a viewer and asks the daemon to start streaming.
//
// Refused when this instance does not hold the machine's socket. That is a
// real answer rather than a silent nothing: the alternative is a terminal
// panel that opens, shows nothing, and gives the person no idea why.
func (r *Relay) Attach(sessionID uint64, machineID int64, v Viewer) error {
	if _, err := r.registry.Get(machineID); err != nil {
		return ErrSessionNotHere
	}

	r.mu.Lock()
	if r.viewers[sessionID] == nil {
		r.viewers[sessionID] = map[Viewer]struct{}{}
	}
	r.viewers[sessionID][v] = struct{}{}
	r.machines[sessionID] = machineID
	first := len(r.viewers[sessionID]) == 1
	r.mu.Unlock()

	// Everyone watching is told who is now here, including the viewer that
	// just arrived — which is how it learns it is not alone.
	r.announce(sessionID)

	// Only the first viewer asks the daemon to attach; the rest join a stream
	// that is already flowing. Asking again would make the daemon repaint for
	// everyone every time somebody opened a second tab.
	if !first {
		return nil
	}
	return r.control(machineID, wire.OpAttach, wire.KillSession{SessionID: sessionID})
}

// Detach removes a viewer, and tells the daemon when the last one leaves.
func (r *Relay) Detach(sessionID uint64, v Viewer) {
	r.mu.Lock()
	vs := r.viewers[sessionID]
	delete(vs, v)
	last := len(vs) == 0
	machineID := r.machines[sessionID]
	if last {
		delete(r.viewers, sessionID)
		delete(r.machines, sessionID)
		delete(r.rates, sessionID)
	}
	r.mu.Unlock()

	if last && machineID != 0 {
		_ = r.control(machineID, wire.OpDetach, wire.KillSession{SessionID: sessionID})
		return
	}
	// Somebody left and somebody is still here. They are told, so a terminal
	// that has stopped being shared stops saying it is.
	r.announce(sessionID)
}

// announce tells every viewer of a session who is watching it.
//
// Send errors are ignored rather than detaching: a viewer whose socket has
// gone is already on its way out through its own read loop, and reaping it
// from here would re-enter [Relay.Detach] from inside an announcement it
// triggered.
func (r *Relay) announce(sessionID uint64) {
	type seat struct {
		v    Viewer
		name string
	}

	r.mu.RLock()
	seats := make([]seat, 0, len(r.viewers[sessionID]))
	for v := range r.viewers[sessionID] {
		seats = append(seats, seat{v: v, name: v.Name()})
	}
	r.mu.RUnlock()
	if len(seats) == 0 {
		return
	}
	// Sorted because the map is not: an unsorted list would reshuffle the
	// names in the UI on every announcement for no reason.
	sort.SliceStable(seats, func(i, j int) bool { return seats[i].name < seats[j].name })

	names := make([]string, len(seats))
	for i, s := range seats {
		names[i] = s.name
	}

	// Built per viewer, because each one is told which entry is itself. Two
	// tabs belonging to the same person are two identical names, and there is
	// no way to work out from the list alone which of them you are.
	for i, s := range seats {
		b, err := jsonMarshal(wire.Presence{SessionID: sessionID, Viewers: names, You: i})
		if err != nil {
			return
		}
		f, err := wire.ControlFrame(wire.Control{Op: wire.OpPresence, Body: b})
		if err != nil {
			return
		}
		_ = s.v.Send(f)
	}
}

// FromDaemon delivers a session frame to everyone watching it.
//
// A viewer that cannot keep up is dropped rather than allowed to block the
// others: one slow browser must not stall a session for every other person
// looking at it, nor back up into the daemon.
func (r *Relay) FromDaemon(f wire.Frame) {
	if !r.allow(f.SessionID, len(f.Payload)) {
		// Over the cap. The frame is not forwarded, and the daemon's own
		// resync is what repairs the viewer's screen — dropping here without
		// that would leave it permanently wrong.
		return
	}

	r.mu.RLock()
	vs := make([]Viewer, 0, len(r.viewers[f.SessionID]))
	for v := range r.viewers[f.SessionID] {
		vs = append(vs, v)
	}
	r.mu.RUnlock()

	for _, v := range vs {
		if err := v.Send(f); err != nil {
			r.Detach(f.SessionID, v)
			_ = v.Close()
		}
	}
}

// FromViewer sends a viewer's input to the daemon.
//
// Input is forwarded immediately and never batched. Output is coalesced on a
// timer because a progress bar redraws constantly; doing the same to input
// would add that delay to every keypress and, worse, merge a deliberate Esc
// with the next key into an escape sequence nobody typed.
func (r *Relay) FromViewer(sessionID uint64, f wire.Frame) error {
	r.mu.RLock()
	machineID := r.machines[sessionID]
	r.mu.RUnlock()
	if machineID == 0 {
		return ErrSessionNotHere
	}

	// Only input and resize may travel this way. A viewer must not be able to
	// synthesise output or an exit, which would let one browser lie to every
	// other viewer of the same session.
	switch f.Type {
	case wire.TypeInput, wire.TypeResize:
	default:
		return errors.New("machine: a viewer may only send input or a resize")
	}
	return r.registry.Send(machineID, f)
}

// Viewers is how many people are watching a session.
func (r *Relay) Viewers(sessionID uint64) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.viewers[sessionID])
}

func (r *Relay) control(machineID int64, op wire.Op, body any) error {
	b, err := jsonMarshal(body)
	if err != nil {
		return err
	}
	f, err := wire.ControlFrame(wire.Control{Op: op, Body: b})
	if err != nil {
		return err
	}
	return r.registry.Send(machineID, f)
}

// rate is a simple fixed-window counter.
//
// Fixed rather than a token bucket because the question being asked is "is
// this session producing an unreasonable amount of output", and a window is
// both sufficient for that and obvious to reason about at 3am.
type rate struct {
	windowStart time.Time
	bytes       int
}

func (r *Relay) allow(sessionID uint64, n int) bool {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.rates[sessionID]
	if st == nil {
		st = &rate{windowStart: now}
		r.rates[sessionID] = st
	}
	if now.Sub(st.windowStart) >= RateWindow {
		st.windowStart = now
		st.bytes = 0
	}
	st.bytes += n
	return st.bytes <= RateLimit
}
