// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// rpcMessage is one JSON-RPC 2.0 message in either direction.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error, ours or the agent's.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

var (
	// errPeerClosed is what every waiting call gets when the connection ends.
	errPeerClosed = errors.New("bridge disconnected")
	// errTooManyCalls refuses a call past maxPendingCalls.
	errTooManyCalls = errors.New("too many requests waiting for the agent")
	// errCancelledByAgent marks a request the agent withdrew.
	errCancelledByAgent = errors.New("the agent withdrew the request")
)

// rpcHandler is the engine, as the peer sees it.
type rpcHandler interface {
	// request answers one agent request on its own goroutine. ctx ends when
	// the agent withdraws the request or the connection closes for good.
	request(ctx context.Context, method string, params json.RawMessage) (any, *rpcError)
	// notification handles one agent notification on the reader goroutine,
	// in arrival order. It must not block.
	notification(method string, params json.RawMessage)
}

// peer is one JSON-RPC conversation: our calls waiting for answers, and the
// agent's calls we are answering. send must not block (the transport queues).
//
// A socket that drops does not close the peer: it lives as long as the logical
// connection, grace window included, so calls in flight are answered after a
// resume.
type peer struct {
	send   func([]byte) error
	h      rpcHandler
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	nextID int64
	calls  map[int64]chan rpcMessage
	served map[string]context.CancelCauseFunc // by the id's raw JSON
	closed error
}

func newPeer(send func([]byte) error, h rpcHandler) *peer {
	ctx, cancel := context.WithCancel(context.Background())
	return &peer{send: send, h: h, ctx: ctx, cancel: cancel,
		calls: map[int64]chan rpcMessage{}, served: map[string]context.CancelCauseFunc{}}
}

// call sends a request and waits for its answer. Giving up through ctx tells
// the agent with $/cancel_request and returns ctx's error.
func (p *peer) call(ctx context.Context, method string, params, result any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed != nil {
		err := p.closed
		p.mu.Unlock()
		return err
	}
	if len(p.calls) >= maxPendingCalls {
		p.mu.Unlock()
		return errTooManyCalls
	}
	p.nextID++
	id := p.nextID
	ch := make(chan rpcMessage, 1)
	p.calls[id] = ch
	p.mu.Unlock()

	idRaw := json.RawMessage(strconv.FormatInt(id, 10))
	frame, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw})
	if err := p.send(frame); err != nil {
		p.forget(id)
		return err
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return p.closedErr()
		}
		if msg.Error != nil {
			return msg.Error
		}
		if result == nil || len(msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	case <-ctx.Done():
		p.forget(id)
		_ = p.notify(methodCancelRequest, cancelRequestNotification{RequestID: idRaw})
		return ctx.Err()
	}
}

func (p *peer) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	frame, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
	return p.send(frame)
}

// receive routes one ACP frame. It runs on the reader goroutine.
func (p *peer) receive(frame []byte) {
	if len(frame) > 0 && frame[0] == '[' {
		p.reply(json.RawMessage("null"), nil, &rpcError{Code: codeInvalidRequest, Message: "batches are not part of ACP v1"})
		return
	}
	var msg rpcMessage
	if err := json.Unmarshal(frame, &msg); err != nil || msg.JSONRPC != "2.0" {
		p.reply(json.RawMessage("null"), nil, &rpcError{Code: codeParseError, Message: "not a JSON-RPC 2.0 message"})
		return
	}
	switch {
	case msg.Method == "" && len(msg.ID) > 0:
		p.answered(msg)
	case msg.Method == methodCancelRequest:
		var n cancelRequestNotification
		if json.Unmarshal(msg.Params, &n) == nil {
			p.mu.Lock()
			cancel := p.served[string(n.RequestID)]
			p.mu.Unlock()
			if cancel != nil {
				cancel(errCancelledByAgent)
			}
		}
	case len(msg.ID) > 0:
		p.serve(msg)
	default:
		p.h.notification(msg.Method, msg.Params)
	}
}

func (p *peer) answered(msg rpcMessage) {
	id, err := strconv.ParseInt(string(msg.ID), 10, 64)
	if err != nil {
		return // not an id this peer minted
	}
	p.mu.Lock()
	ch, ok := p.calls[id]
	delete(p.calls, id)
	p.mu.Unlock()
	if ok {
		ch <- msg
	}
}

func (p *peer) serve(msg rpcMessage) {
	key := string(msg.ID)
	ctx, cancel := context.WithCancelCause(p.ctx)
	p.mu.Lock()
	if len(p.served) >= maxServedRequests {
		p.mu.Unlock()
		cancel(nil)
		p.reply(msg.ID, nil, &rpcError{Code: codeInternalError, Message: "too many requests in flight"})
		return
	}
	p.served[key] = cancel
	p.mu.Unlock()
	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.served, key)
			p.mu.Unlock()
			cancel(nil)
		}()
		result, rerr := p.h.request(ctx, msg.Method, msg.Params)
		if errors.Is(context.Cause(ctx), errCancelledByAgent) {
			result, rerr = nil, &rpcError{Code: codeCancelled, Message: "request cancelled"}
		}
		p.reply(msg.ID, result, rerr)
	}()
}

func (p *peer) reply(id json.RawMessage, result any, rerr *rpcError) {
	msg := rpcMessage{JSONRPC: "2.0", ID: id, Error: rerr}
	if rerr == nil {
		raw, err := json.Marshal(result)
		if err != nil {
			msg.Error = &rpcError{Code: codeInternalError, Message: "could not encode the answer"}
		} else {
			msg.Result = raw
		}
	}
	frame, _ := json.Marshal(msg)
	_ = p.send(frame)
}

func (p *peer) forget(id int64) {
	p.mu.Lock()
	delete(p.calls, id)
	p.mu.Unlock()
}

// close ends the conversation for good: waiting calls fail with err, and every
// request we are still answering sees its context end.
func (p *peer) close(err error) {
	p.mu.Lock()
	if p.closed != nil {
		p.mu.Unlock()
		return
	}
	p.closed = err
	calls := p.calls
	p.calls = map[int64]chan rpcMessage{}
	p.mu.Unlock()
	p.cancel()
	for _, ch := range calls {
		close(ch)
	}
}

// closedErr is the reason a closed peer gave; only a closed peer closes a
// call's channel, so it is always set here.
func (p *peer) closedErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
