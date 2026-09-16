// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/daemon/wire"
)

// SessionLookup resolves a session to its machine, for the caller who owns the
// database. Narrow so this package stays testable without one.
type SessionLookup interface {
	// LookupSession returns the machine holding a session, and the instance
	// that machine's socket is on. An error means the caller may not see it —
	// the reason is the caller's to decide, not this package's to leak.
	LookupSession(r *http.Request, sessionID uint64) (machineID int64, instanceID string, err error)
}

// viewerConn adapts a browser's WebSocket to [Viewer].
//
// Same write mutex as the daemon side, for the same reason: gorilla permits
// one concurrent writer, and this socket is written by the relay and the
// keepalive at once.
type viewerConn struct {
	ws *websocket.Conn

	mu     sync.Mutex
	closed bool
}

func (c *viewerConn) Send(f wire.Frame) error {
	b, err := f.Encode()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return websocket.ErrCloseSent
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (c *viewerConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.ws.Close()
}

// ViewerHandler serves a browser watching a session's terminal.
type ViewerHandler struct {
	Relay  *Relay
	Lookup SessionLookup
	// SessionID pulls the session out of the request path.
	SessionID func(*http.Request) uint64
}

// ServeHTTP attaches a browser to a session.
func (h *ViewerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := h.SessionID(r)
	if sessionID == 0 {
		http.Error(w, "invalid session", http.StatusUnprocessableEntity)
		return
	}

	machineID, instanceID, err := h.Lookup.LookupSession(r, sessionID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Refused with a reason rather than opening a socket that will never carry
	// anything. A terminal panel that connects and then shows nothing is the
	// worst version of this failure, because it looks like the agent is quiet.
	if _, regErr := h.Relay.registry.Get(machineID); regErr != nil {
		log.Info().
			Int64("machine_id", machineID).
			Str("held_by", instanceID).
			Str("this_instance", h.Relay.registry.InstanceID()).
			Msg("[viewer] attach for a machine this instance does not hold")
		http.Error(w, "that machine is connected to another server instance", http.StatusConflict)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := &viewerConn{ws: ws}

	if err := h.Relay.Attach(sessionID, machineID, conn); err != nil {
		_ = conn.Close()
		return
	}
	defer func() {
		h.Relay.Detach(sessionID, conn)
		_ = conn.Close()
	}()

	ws.SetReadLimit(maxFrame)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	done := make(chan struct{})
	defer close(done)
	go h.ping(done, conn, ws)

	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			return
		}
		f, err := wire.Decode(data)
		if err != nil {
			return
		}
		// The session a viewer may drive is the one it attached to, whatever
		// the frame claims. Without this, an attached browser could type into
		// any session on that machine by changing a number.
		f.SessionID = sessionID
		if err := h.Relay.FromViewer(sessionID, f); err != nil {
			return
		}
	}
}

func (h *ViewerHandler) ping(done <-chan struct{}, conn *viewerConn, ws *websocket.Conn) {
	t := time.NewTicker(pingPeriod)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			conn.mu.Lock()
			closed := conn.closed
			if closed {
				conn.mu.Unlock()
				return
			}
			_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
			err := ws.WriteMessage(websocket.PingMessage, nil)
			conn.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
