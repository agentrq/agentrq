// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/daemon/wire"
)

// wsConn adapts a WebSocket to [Conn].
//
// The write mutex is not optional: gorilla permits one concurrent writer, and
// this socket is written to by the relay delivering terminal input, by control
// messages, and by the keepalive ticker. Without it those interleave and
// corrupt a frame — which surfaces as a terminal that garbles under load, far
// from the code that caused it.
type wsConn struct {
	ws *websocket.Conn

	mu     sync.Mutex
	closed bool
}

func (c *wsConn) Send(f wire.Frame) error {
	b, err := f.Encode()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("machine: socket closed")
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (c *wsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.ws.Close()
}

const (
	// writeWait bounds a single write. A daemon on a bad connection must not
	// hold a goroutine forever.
	writeWait = 10 * time.Second
	// pongWait is how long to wait for a daemon to answer a ping before the
	// connection is considered dead.
	pongWait = 2 * HeartbeatInterval
	// pingPeriod must be shorter than pongWait, or the connection is declared
	// dead between pings that were never sent.
	pingPeriod = pongWait * 9 / 10
	// maxFrame bounds an inbound message, matching the protocol's own cap so
	// a peer cannot make the server allocate on demand.
	maxFrame = wire.HeaderSize + wire.MaxPayload
)

// Handler serves the daemon's WebSocket endpoint.
type Handler struct {
	Registry *Registry
	Auth     Authenticator
	// DecodeID turns a base62 id header into its numeric form, for the header
	// cross-check. Injected so this package does not depend on the id scheme.
	DecodeID func(string) int64
	// OnFrame handles a frame from the daemon. Returning an error ends the
	// connection.
	OnFrame func(ctx context.Context, s *Session, f wire.Frame) error
}

// upgrader accepts the connection.
//
// CheckOrigin always allows, and that is correct here rather than lax: this
// endpoint authenticates with a bearer token, not a cookie, so it is not
// reachable by a browser acting on someone's behalf. Origin checks exist to
// stop a page using ambient credentials; there are none to use.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// ServeHTTP authenticates a daemon and runs its connection.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, err := BearerToken(r.Header.Get("Authorization"))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := h.Auth.AuthenticateMachine(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrMachineDisabled) {
			// Said out loud, because it is the kill switch and the person who
			// turned it off is probably watching. An unknown token gets no
			// such explanation.
			http.Error(w, "machine disabled", http.StatusForbidden)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// The headers are routing and correlation; the token decided who this is.
	// A mismatch has no legitimate cause, so it is refused and logged rather
	// than ignored.
	if err := CheckIdentityHeaders(id,
		r.Header.Get("X-AgentRQ-Machine-Id"),
		r.Header.Get("X-AgentRQ-User-Id"),
		h.DecodeID,
	); err != nil {
		log.Warn().
			Int64("machine_id", id.MachineID).
			Str("claimed_machine", r.Header.Get("X-AgentRQ-Machine-Id")).
			Str("claimed_user", r.Header.Get("X-AgentRQ-User-Id")).
			Msg("[machine] identity headers contradict the token")
		http.Error(w, "identity mismatch", http.StatusForbidden)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written its own response.
		return
	}

	conn := &wsConn{ws: ws}
	session, err := NewSession(r.Context(), h.Registry, h.Auth, id, conn)
	if err != nil {
		_ = conn.Close()
		return
	}

	h.run(session, ws, conn)
}

// run drives one connection until it ends.
func (h *Handler) run(s *Session, ws *websocket.Conn, conn *wsConn) {
	// A context of its own: the request's is cancelled the moment ServeHTTP
	// returns, and this connection outlives that by design.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		if err := s.Close(ctx); err != nil {
			log.Debug().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] close")
		}
	}()

	ws.SetReadLimit(maxFrame)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		// Every pong is evidence the daemon is alive, so it extends the
		// deadline and counts as a heartbeat. A machine sending nothing but
		// pongs is still online.
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		if err := s.Heartbeat(ctx); err != nil {
			log.Debug().Err(err).Msg("[machine] heartbeat")
		}
		return nil
	})

	go h.ping(ctx, conn, ws)

	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.BinaryMessage {
			// The protocol is binary. A text frame is a client that has
			// misunderstood it, and continuing would mean guessing.
			return
		}

		f, err := wire.Decode(data)
		if err != nil {
			log.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] undecodable frame")
			return
		}
		if h.OnFrame == nil {
			continue
		}
		if err := h.OnFrame(ctx, s, f); err != nil {
			log.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] frame rejected")
			return
		}
	}
}

// ping keeps the connection warm and notices a daemon that has gone silent.
//
// A TCP connection to a machine that has been unplugged stays open on this
// side indefinitely; the ping is what turns that into a read deadline that
// eventually fires.
func (h *Handler) ping(ctx context.Context, conn *wsConn, ws *websocket.Conn) {
	t := time.NewTicker(pingPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			conn.mu.Lock()
			closed := conn.closed
			if !closed {
				_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
				err := ws.WriteMessage(websocket.PingMessage, nil)
				conn.mu.Unlock()
				if err != nil {
					return
				}
				continue
			}
			conn.mu.Unlock()
			return
		}
	}
}
