// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/internal/stream"
	"github.com/agentrq/agentrq/daemon/wire"
)

// Timings for the socket. The read deadline is generous relative to the ping
// period so a slow network costs a round trip rather than the connection.
const (
	writeWait  = 10 * time.Second
	pongWait   = 90 * time.Second
	pingPeriod = 30 * time.Second
	maxFrame   = 1 << 20
)

// SendQueue is how many frames may be waiting to go out.
//
// Bounded, and that bound is the whole point. A terminal producing more than
// the network can carry must not grow this queue until the daemon runs out of
// memory; it is told the send would block, and the pump answers by discarding
// the backlog and repainting the screen instead. Losing intermediate frames
// and keeping a correct screen is the only safe way to shed load from a
// stateful stream.
const SendQueue = 256

// Conn is one connection to the backend.
//
// Writes go through a queue and a single goroutine because gorilla permits one
// concurrent writer, and this socket is written by every session's pump, the
// keepalive and the control replies at once.
type Conn struct {
	ws   *websocket.Conn
	out  chan []byte
	done chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// NewConn wraps an open socket.
func NewConn(ws *websocket.Conn) *Conn {
	c := &Conn{ws: ws, out: make(chan []byte, SendQueue), done: make(chan struct{})}
	go c.write()
	return c
}

// Send queues a frame.
//
// Never blocks. A full queue is reported as [stream.ErrBackpressure] rather
// than waited on: blocking here would stall the pseudo-terminal read loop,
// which would stall the program on the other end of it — a slow network would
// quietly become a slow machine.
func (c *Conn) Send(f wire.Frame) error {
	b, err := f.Encode()
	if err != nil {
		return err
	}
	// Checked before the send rather than as another case of it: a select
	// whose cases are both ready picks at random, so a closed connection would
	// accept frames about half the time.
	select {
	case <-c.done:
		return websocket.ErrCloseSent
	default:
	}

	select {
	case c.out <- b:
		return nil
	default:
		return stream.ErrBackpressure
	}
}

// ReportSessionState sends a state change, satisfying supervisor.Reporter.
func (c *Conn) ReportSessionState(st wire.SessionState) error {
	return c.Control(wire.Control{Op: wire.OpSessionState, Body: mustJSON(st)})
}

// Control sends a control message.
func (c *Conn) Control(ctl wire.Control) error {
	f, err := wire.ControlFrame(ctl)
	if err != nil {
		return err
	}
	b, err := f.Encode()
	if err != nil {
		return err
	}
	select {
	case <-c.done:
		return websocket.ErrCloseSent
	default:
	}

	// Control messages are low-rate and each one means something — a refused
	// start, a session that ended. They wait rather than being dropped, which
	// is the opposite of the choice made for terminal output, because there is
	// no later frame that supersedes them.
	select {
	case <-c.done:
		return websocket.ErrCloseSent
	case c.out <- b:
		return nil
	case <-time.After(writeWait):
		return errors.New("link: control send timed out")
	}
}

// Close ends the connection. Safe to call more than once.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeErr = c.ws.Close()
	})
	return c.closeErr
}

// write is the single writer.
func (c *Conn) write() {
	ping := time.NewTicker(pingPeriod)
	defer ping.Stop()
	for {
		select {
		case <-c.done:
			return
		case b := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
				_ = c.Close()
				return
			}
		case <-ping.C:
			// The keepalive is what turns a machine that has been unplugged
			// into a connection that eventually fails, rather than one that
			// stays open forever on both sides.
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				_ = c.Close()
				return
			}
		}
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value marshalled here is a struct of scalars defined in wire.
		// There is no input that makes this fail.
		return json.RawMessage(`{}`)
	}
	return b
}
