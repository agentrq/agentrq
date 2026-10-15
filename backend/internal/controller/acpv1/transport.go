// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	zlog "github.com/rs/zerolog/log"
)

// bridgeSubprotocol names the control protocol this endpoint shares with
// acp-gateway's native mode. A change to the control frames is a new name.
const bridgeSubprotocol = "agentrq.bridge.1"

// controlPrefix starts every control frame and no ACP frame. AgentRQ writes
// its ACP frames with "jsonrpc" first, and the bridge refuses to forward an
// agent line that starts with this prefix, so the agent cannot forge one.
var controlPrefix = []byte(`{"agentrqBridge":`)

func isControl(frame []byte) bool { return bytes.HasPrefix(frame, controlPrefix) }

// frameReadLimit is maxFrameBytes as a variable, so that a test can shrink it.
var frameReadLimit int64 = maxFrameBytes

// Close codes, spec §5.3.
const (
	closeRestart      = 4000
	closeGraceOrGone  = 4001
	closeVersion      = 4002
	closeAuthRequired = 4003
)

// control is one control frame. Field order is the wire order: the kind first.
type control struct {
	Kind         string   `json:"agentrqBridge"`
	ConnectionID string   `json:"connectionId,omitempty"`
	Resumed      *bool    `json:"resumed,omitempty"`
	Received     *int64   `json:"received,omitempty"`
	GraceSeconds int      `json:"graceSeconds,omitempty"`
	Lines        []string `json:"lines,omitempty"`
	Code         *int     `json:"code,omitempty"`
	Signal       *string  `json:"signal,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Error        string   `json:"error,omitempty"`
	Message      string   `json:"message,omitempty"`
}

func encodeControl(c control) []byte { b, _ := json.Marshal(c); return b }

func decodeControl(frame []byte) (control, error) {
	var c control
	err := json.Unmarshal(frame, &c)
	return c, err
}

// ring keeps the ACP frames one side sent that the other has not yet
// acknowledged, oldest first, so that a resume can send them again. Frames
// expiring in the order they were sent make a FIFO the right shape: an ack
// pops from the head in amortised O(1), and the slice is compacted once half
// of it is spent.
type ring struct {
	frames [][]byte
	head   int
	first  int64 // sequence number of frames[head]
	bytes  int
	sent   int64 // frames ever pushed
	broken bool  // overflowed: a resume would leave a gap, so none is allowed
}

func newRing() ring { return ring{first: 1} }

func (r *ring) push(f []byte) {
	r.sent++
	if r.broken {
		return
	}
	r.frames = append(r.frames, f)
	r.bytes += len(f)
	if len(r.frames)-r.head > replayMaxFrames || r.bytes > replayMaxBytes {
		r.broken, r.frames, r.head, r.bytes = true, nil, 0, 0
	}
}

func (r *ring) ack(received int64) {
	for r.head < len(r.frames) && r.first <= received {
		r.bytes -= len(r.frames[r.head])
		r.frames[r.head] = nil
		r.head++
		r.first++
	}
	if r.head > 0 && r.head*2 >= len(r.frames) {
		r.frames = append([][]byte(nil), r.frames[r.head:]...)
		r.head = 0
	}
}

// after returns the frames past the peer's count, or false when they are no
// longer all here: acknowledged already, never sent, or lost to an overflow.
func (r *ring) after(received int64) ([][]byte, bool) {
	switch {
	case r.broken, received > r.sent, received+1 < r.first:
		return nil, false
	case received == r.sent:
		return nil, true
	}
	start := r.head + int(received+1-r.first)
	return append([][]byte(nil), r.frames[start:]...), true
}

var (
	errGraceExpired = errors.New("the bridge did not come back within the grace window")
	errBridgeBye    = errors.New("the bridge shut down")
	errReplaced     = errors.New("the bridge started over on a new connection")
	errNotResumable = errors.New("too much was sent while the bridge was away to resume")
	errSlowBridge   = errors.New("the bridge stopped reading")
	errNewSocket    = errors.New("the bridge resumed on a new socket")
)

// engine is the ACP side of one connection (Task 5). run returns when the
// connection is over, and its error is the reason kept.
type engine interface {
	run(ctx context.Context, c *connection) error
}

// bridgeMeta is what the bridge said about itself in its upgrade headers.
type bridgeMeta struct {
	BridgeVersion, Agent, Model, Effort, AuthMethod, Cwd, Platform string
	MaxConcurrency                                                 int           // 0 means the default, 1
	PermissionTimeout                                              time.Duration // <0 means the default; 0 means none
}

// timing is a connection's copy of the package's timings, taken when it is
// created, so that a test shrinking one never races an older connection.
type timing struct {
	grace, ping, pong, ack time.Duration
	readLimit              int64
}

// connection is one logical bridge connection. It outlives the sockets that
// carry it by up to graceWindow between them, so a network blip costs nothing:
// frames sent meanwhile wait in the ring and are replayed on resume.
//
// Lock order: deliverMu, then mu. Nothing is called back while mu is held.
type connection struct {
	id          string
	workspaceID int64
	userID      string
	token       string // the bridge's bearer token: handed to the agent, never logged
	meta        bridgeMeta
	timing      timing

	// deliverMu makes counting a frame and handing it to the engine one step,
	// which a resume waits for: a frame read on a dropping socket is either
	// counted before the welcome reports the count, or never delivered at all.
	deliverMu sync.Mutex

	mu        sync.Mutex
	ws        *websocket.Conn
	out       chan []byte
	sent      ring
	received  int64
	unacked   int
	grace     *time.Timer
	graceGen  int  // which detach the running grace timer belongs to
	attached  bool // the state onState last learned, or is about to
	ended     bool
	err       error
	closeCode int // written by the writer once it has sent what was queued
	closeText string
	done      chan struct{}
	stateKick chan struct{}
	onFrame   func([]byte)
	onControl func(control)
	onState   func(bool)
}

func newConnection(id string, workspaceID int64, userID string, meta bridgeMeta) *connection {
	c := &connection{id: id, workspaceID: workspaceID, userID: userID, meta: meta,
		timing: timing{grace: graceWindow, ping: pingEvery, pong: pongWait, ack: ackEvery, readLimit: frameReadLimit},
		sent:   newRing(), done: make(chan struct{}), stateKick: make(chan struct{}, 1),
		onFrame: func([]byte) {}, onControl: func(control) {}, onState: func(bool) {}}
	go c.dispatchState()
	return c
}

func (c *connection) ID() string                 { return c.id }
func (c *connection) Meta() bridgeMeta           { return c.meta }
func (c *connection) WorkspaceID() int64         { return c.workspaceID }
func (c *connection) UserID() string             { return c.userID }
func (c *connection) Token() string              { return c.token }
func (c *connection) OnFrame(fn func([]byte))    { c.onFrame = fn }
func (c *connection) OnControl(fn func(control)) { c.onControl = fn }
func (c *connection) Done() <-chan struct{}      { return c.done }

// OnState is told when a socket attaches (true) and when one drops into the
// grace window (false), in order and from one goroutine. A drop and a resume
// close together may be told as nothing at all.
func (c *connection) OnState(fn func(bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onState = fn
}

func (c *connection) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Send queues one ACP frame for the bridge. It never blocks: while detached the
// frame waits in the ring, and a socket that cannot keep up is dropped into
// the grace window rather than allowed to stall the engine.
func (c *connection) Send(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended {
		return errPeerClosed
	}
	c.sent.push(frame)
	if c.ws == nil {
		if c.sent.broken {
			c.endLocked(errNotResumable)
		}
		return nil
	}
	select {
	case c.out <- frame:
	default:
		c.detachLocked(errSlowBridge)
	}
	return nil
}

// Control sends a control frame on the current socket, if there is one.
func (c *connection) Control(ctl control) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil || c.ended {
		return
	}
	select {
	case c.out <- encodeControl(ctl):
	default:
		c.detachLocked(errSlowBridge)
	}
}

// attach puts a socket under the connection: the welcome first, then every
// frame the bridge has not received, then whatever comes next — in that
// order, because nothing else can be queued while the lock is held. A socket
// still attached is dropped first: the bridge has given up on it.
func (c *connection) attach(ws *websocket.Conn, peerReceived int64, resumed bool) error {
	c.deliverMu.Lock()
	defer c.deliverMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended {
		return errPeerClosed
	}
	replay, ok := c.sent.after(peerReceived)
	if !ok {
		return errNotResumable
	}
	c.sent.ack(peerReceived)
	if c.ws != nil {
		zlog.Info().Str("connection", c.id).Msg("acp v1: " + errNewSocket.Error() + "; dropping the old one")
		c.releaseLocked()
	}
	if c.grace != nil {
		c.grace.Stop()
		c.grace = nil
	}
	c.ws = ws
	c.out = make(chan []byte, outboundQueueCap+len(replay)+1)
	received := c.received
	c.out <- encodeControl(control{Kind: "welcome", ConnectionID: c.id, Resumed: &resumed,
		Received: &received, GraceSeconds: int(c.timing.grace / time.Second)})
	for _, f := range replay {
		c.out <- f
	}
	c.unacked = 0
	go c.writer(ws, c.out)
	go c.reader(ws)
	c.setStateLocked(true)
	return nil
}

func (c *connection) reader(ws *websocket.Conn) {
	ws.SetReadLimit(c.timing.readLimit)
	_ = ws.SetReadDeadline(time.Now().Add(c.timing.pong))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(c.timing.pong)) })
	for {
		kind, frame, err := ws.ReadMessage()
		if err != nil {
			c.detach(ws, err)
			return
		}
		_ = ws.SetReadDeadline(time.Now().Add(c.timing.pong))
		switch {
		case kind != websocket.TextMessage:
			c.closeSocket(ws, websocket.CloseUnsupportedData, "text frames only")
			return
		case isControl(frame):
			c.handleControl(frame)
		default:
			c.deliver(ws, frame)
		}
	}
}

// deliver counts one ACP frame from the bridge and hands it to the engine,
// unless ws is no longer the connection's socket: then it was closed, and its
// reader's next read fails.
func (c *connection) deliver(ws *websocket.Conn, frame []byte) {
	c.deliverMu.Lock()
	defer c.deliverMu.Unlock()
	c.mu.Lock()
	if c.ws != ws {
		c.mu.Unlock()
		return
	}
	c.received++
	c.unacked++
	ack := c.unacked >= ackEveryFrames
	if ack {
		c.unacked = 0
	}
	received := c.received
	c.mu.Unlock()
	c.onFrame(frame)
	if ack {
		c.Control(control{Kind: "ack", Received: &received})
	}
}

func (c *connection) handleControl(frame []byte) {
	ctl, err := decodeControl(frame)
	if err != nil {
		zlog.Warn().Str("connection", c.id).Msg("acp v1: a control frame that is not JSON; ignoring it")
		return
	}
	switch ctl.Kind {
	case "ack":
		if ctl.Received != nil {
			c.mu.Lock()
			c.sent.ack(*ctl.Received)
			c.mu.Unlock()
		}
	case "bye":
		c.End(errBridgeBye)
	default:
		c.onControl(ctl)
	}
}

// writer is the socket's only writer, as gorilla requires; WriteControl is
// the one call allowed from elsewhere. Once out is closed it sends the close
// End asked for, after everything queued before it.
func (c *connection) writer(ws *websocket.Conn, out chan []byte) {
	ping := time.NewTicker(c.timing.ping)
	acks := time.NewTicker(c.timing.ack)
	defer func() {
		ping.Stop()
		acks.Stop()
		_ = ws.Close()
	}()
	for {
		var err error
		select {
		case frame, open := <-out:
			if !open {
				c.mu.Lock()
				code, text := c.closeCode, c.closeText
				c.mu.Unlock()
				if code != 0 {
					_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(writeWait))
				}
				return
			}
			_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
			err = ws.WriteMessage(websocket.TextMessage, frame)
		case <-ping.C:
			err = ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
		case <-acks.C:
			c.ackPending()
		}
		if err != nil {
			c.detach(ws, err)
			return
		}
	}
}

// ackPending acknowledges what arrived since the last ack, if anything did.
func (c *connection) ackPending() {
	c.mu.Lock()
	pending, received := c.unacked > 0, c.received
	c.unacked = 0
	c.mu.Unlock()
	if pending {
		c.Control(control{Kind: "ack", Received: &received})
	}
}

// detach drops one socket (if it is still the current one) into the grace
// window.
func (c *connection) detach(ws *websocket.Conn, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != ws {
		return
	}
	c.detachLocked(err)
}

// detachLocked needs a socket attached, as every caller has checked.
func (c *connection) detachLocked(err error) {
	zlog.Info().Str("connection", c.id).Err(err).Msg("acp v1: bridge socket lost; holding the connection for its grace window")
	c.releaseLocked()
	if c.sent.broken {
		c.endLocked(errNotResumable)
		return
	}
	c.graceGen++
	gen := c.graceGen
	c.grace = time.AfterFunc(c.timing.grace, func() { c.expireGrace(gen) })
	c.setStateLocked(false)
}

// releaseLocked closes the current socket at once; the frames still queued
// for it are in the ring.
func (c *connection) releaseLocked() {
	_ = c.ws.Close()
	close(c.out)
	c.ws, c.out = nil, nil
}

// expireGrace ends the connection, unless the grace timer of detach gen was
// overtaken by a resume after it had already fired.
func (c *connection) expireGrace(gen int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil || c.graceGen != gen {
		return
	}
	c.endLocked(errGraceExpired)
}

func (c *connection) closeSocket(ws *websocket.Conn, code int, text string) {
	_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(writeWait))
	c.detach(ws, errors.New(text))
}

// setStateLocked records the attached state and wakes dispatchState, which
// tells onState outside the lock.
func (c *connection) setStateLocked(attached bool) {
	c.attached = attached
	select {
	case c.stateKick <- struct{}{}:
	default:
	}
}

func (c *connection) dispatchState() {
	told := false
	for {
		select {
		case <-c.done:
			return
		case <-c.stateKick:
		}
		c.mu.Lock()
		attached, fn := c.attached, c.onState
		c.mu.Unlock()
		if attached != told {
			told = attached
			fn(attached)
		}
	}
}

// End finishes the logical connection: the grace timer goes, the socket is
// closed once what was queued for it is written, and Done closes. The first
// reason given is the one kept.
func (c *connection) End(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endLocked(err)
}

func (c *connection) endLocked(err error) {
	code, text := websocket.CloseNormalClosure, "bye"
	if errors.Is(err, errGraceExpired) || errors.Is(err, errReplaced) {
		code, text = closeGraceOrGone, err.Error()
	}
	c.endWithLocked(code, text, err)
}

// endWith is End with the close code given rather than the one err implies.
func (c *connection) endWith(code int, text string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endWithLocked(code, text, err)
}

func (c *connection) endWithLocked(code int, text string, err error) {
	if c.ended {
		return
	}
	c.ended, c.err = true, err
	if c.grace != nil {
		c.grace.Stop()
		c.grace = nil
	}
	if c.ws != nil {
		c.closeCode, c.closeText = code, text
		close(c.out)
		c.ws, c.out = nil, nil
	}
	close(c.done)
}
