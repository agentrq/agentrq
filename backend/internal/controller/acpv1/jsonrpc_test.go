// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// wire is both ends of a peer under test: frames the peer sent, and a handler
// that records what reached it.
type wire struct {
	mu      sync.Mutex
	sent    [][]byte
	notifs  []string
	sendErr error
	answer  func(ctx context.Context, method string, params json.RawMessage) (any, *rpcError)
}

func (w *wire) send(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sendErr != nil {
		return w.sendErr
	}
	w.sent = append(w.sent, append([]byte(nil), b...))
	return nil
}

func (w *wire) request(ctx context.Context, m string, p json.RawMessage) (any, *rpcError) {
	return w.answer(ctx, m, p)
}

func (w *wire) notification(m string, p json.RawMessage) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.notifs = append(w.notifs, m+" "+string(p))
}

func (w *wire) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.sent)
}

// waitSent waits until the peer has sent n frames and returns them decoded.
func (w *wire) waitSent(t *testing.T, n int) []rpcMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for w.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("the peer sent %d frames within two seconds, want %d", w.count(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]rpcMessage, len(w.sent))
	for i, b := range w.sent {
		_ = json.Unmarshal(b, &out[i])
	}
	return out
}

func (w *wire) last(t *testing.T) rpcMessage {
	t.Helper()
	sent := w.waitSent(t, 1)
	return sent[len(sent)-1]
}

func TestPeer(t *testing.T) {
	t.Run("a call is answered by the response carrying its id", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, 1)
		var got promptResponse
		go func() { done <- p.call(context.Background(), methodSessionPrompt, promptRequest{SessionID: "s"}, &got) }()

		req := w.last(t)
		if req.Method != methodSessionPrompt || string(req.ID) != "1" {
			t.Fatalf("sent %s with id %s, want session/prompt with id 1", req.Method, req.ID)
		}
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn"}}`))
		if err := <-done; err != nil || got.StopReason != "end_turn" {
			t.Fatalf("call returned %v with %+v, want nil and end_turn", err, got)
		}
	})

	t.Run("an answer that does not fit the result is a decoding error", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, 1)
		var got promptResponse
		go func() { done <- p.call(context.Background(), methodSessionPrompt, promptRequest{SessionID: "s"}, &got) }()
		w.last(t)
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"result":{"stopReason":7}}`))
		if err := <-done; err == nil {
			t.Fatal("call returned nil, want a decoding error")
		}
	})

	t.Run("a call that wants no result returns once answered", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, 1)
		go func() {
			done <- p.call(context.Background(), methodSessionSetMode, setModeRequest{SessionID: "s", ModeID: "agent"}, nil)
		}()
		w.last(t)
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ignored":true}}`))
		if err := <-done; err != nil {
			t.Fatalf("call returned %v, want nil", err)
		}
	})

	t.Run("an error answer comes back as the agent's rpcError", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, 1)
		go func() { done <- p.call(context.Background(), methodSessionNew, newSessionRequest{}, nil) }()
		w.last(t)
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Authentication required"}}`))
		var rerr *rpcError
		if err := <-done; !errors.As(err, &rerr) || rerr.Code != codeAuthRequired {
			t.Fatalf("call returned %v, want an rpcError with code -32000", err)
		}
		if want := "acp error -32000: Authentication required"; rerr.Error() != want {
			t.Errorf("error read %q, want %q", rerr.Error(), want)
		}
	})

	t.Run("giving up on a call tells the agent with $/cancel_request", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.call(ctx, methodSessionPrompt, promptRequest{SessionID: "s"}, nil) }()
		w.last(t)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("call returned %v, want context.Canceled", err)
		}
		if m := w.last(t); m.Method != methodCancelRequest || string(m.Params) != `{"requestId":1}` {
			t.Fatalf("last frame was %s %s, want $/cancel_request for id 1", m.Method, m.Params)
		}
		// The answer that comes after giving up reaches nobody.
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		if n := w.count(); n != 2 {
			t.Errorf("the peer sent %d frames, want 2 (the call and its cancel)", n)
		}
	})

	t.Run("a call whose params cannot be encoded sends nothing", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		if err := p.call(context.Background(), methodSessionPrompt, make(chan int), nil); err == nil {
			t.Fatal("call returned nil, want an encoding error")
		}
		if err := p.notify(methodSessionCancel, make(chan int)); err == nil {
			t.Fatal("notify returned nil, want an encoding error")
		}
		if n := w.count(); n != 0 {
			t.Errorf("the peer sent %d frames, want none", n)
		}
	})

	t.Run("a call the transport cannot send fails and is not left waiting", func(t *testing.T) {
		w := &wire{sendErr: errors.New("socket closed")}
		p := newPeer(w.send, w)
		if err := p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil); err == nil || err.Error() != "socket closed" {
			t.Fatalf("call returned %v, want socket closed", err)
		}
		if n := len(p.calls); n != 0 {
			t.Errorf("%d calls left waiting, want none", n)
		}
	})

	t.Run("a notification is sent without an id", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		if err := p.notify(methodSessionCancel, sessionIDParams{SessionID: "s"}); err != nil {
			t.Fatalf("notify returned %v, want nil", err)
		}
		if m := w.last(t); m.Method != methodSessionCancel || m.ID != nil || string(m.Params) != `{"sessionId":"s"}` {
			t.Errorf("sent %+v, want session/cancel for s with no id", m)
		}
	})

	t.Run("the agent's request is answered with the same id, string or number", func(t *testing.T) {
		w := &wire{answer: func(context.Context, string, json.RawMessage) (any, *rpcError) {
			return requestPermissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}}, nil
		}}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":"abc","method":"session/request_permission","params":{}}`))
		if m := w.last(t); string(m.ID) != `"abc"` || string(m.Result) != `{"outcome":{"outcome":"cancelled"}}` {
			t.Fatalf("answered id %s with %s, want id \"abc\" and the handler's result", m.ID, m.Result)
		}
		p.receive([]byte(`{"jsonrpc":"2.0","id":4,"method":"session/request_permission","params":{}}`))
		if m := w.waitSent(t, 2)[1]; string(m.ID) != `4` {
			t.Fatalf("answered id %s, want 4", m.ID)
		}
	})

	t.Run("the handler's error is the answer", func(t *testing.T) {
		w := &wire{answer: func(_ context.Context, m string, _ json.RawMessage) (any, *rpcError) {
			return nil, &rpcError{Code: codeMethodNotFound, Message: m + " not found"}
		}}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":2,"method":"fs/read_text_file","params":{}}`))
		if m := w.last(t); m.Error == nil || m.Error.Code != codeMethodNotFound || m.Result != nil {
			t.Fatalf("answered %+v, want error -32601 and no result", m)
		}
	})

	t.Run("an answer that cannot be encoded becomes an internal error", func(t *testing.T) {
		w := &wire{answer: func(context.Context, string, json.RawMessage) (any, *rpcError) {
			return make(chan int), nil
		}}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":3,"method":"session/request_permission","params":{}}`))
		if m := w.last(t); m.Error == nil || m.Error.Code != codeInternalError || string(m.ID) != "3" {
			t.Fatalf("answered %+v, want error -32603 for id 3", m)
		}
	})

	t.Run("$/cancel_request for a request in flight answers it with -32800", func(t *testing.T) {
		release := make(chan struct{})
		w := &wire{answer: func(ctx context.Context, _ string, _ json.RawMessage) (any, *rpcError) {
			<-ctx.Done()
			close(release)
			return struct{}{}, nil
		}}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":9,"method":"session/request_permission","params":{}}`))
		p.receive([]byte(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":9}}`))
		<-release
		if m := w.last(t); m.Error == nil || m.Error.Code != codeCancelled || string(m.ID) != "9" {
			t.Fatalf("answered %+v, want error -32800 for id 9", m)
		}
	})

	t.Run("$/cancel_request for nothing in flight changes nothing", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":41}}`))
		p.receive([]byte(`{"jsonrpc":"2.0","method":"$/cancel_request","params":"garbled"}`))
		if n := w.count(); n != 0 || len(w.notifs) != 0 {
			t.Errorf("sent %d frames and handled %v, want nothing", n, w.notifs)
		}
	})

	t.Run("closing fails every waiting call with the reason", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, 1)
		go func() { done <- p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil) }()
		w.last(t)
		p.close(errPeerClosed)
		if err := <-done; !errors.Is(err, errPeerClosed) {
			t.Fatalf("call returned %v, want errPeerClosed", err)
		}
	})

	t.Run("closing ends the requests being answered, and a second close keeps the first reason", func(t *testing.T) {
		ended := make(chan struct{})
		w := &wire{answer: func(ctx context.Context, _ string, _ json.RawMessage) (any, *rpcError) {
			<-ctx.Done()
			close(ended)
			return struct{}{}, nil
		}}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":1,"method":"session/request_permission","params":{}}`))
		p.close(errPeerClosed)
		p.close(errors.New("socket closed"))
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Fatal("the request being answered did not see the close within two seconds")
		}
		if err := p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil); !errors.Is(err, errPeerClosed) {
			t.Errorf("call after two closes returned %v, want the first reason, errPeerClosed", err)
		}
	})

	t.Run("notifications reach the handler in arrival order", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		for i := 1; i <= 3; i++ {
			p.receive([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"n":%d}}`, i)))
		}
		want := []string{`session/update {"n":1}`, `session/update {"n":2}`, `session/update {"n":3}`}
		if strings.Join(w.notifs, "|") != strings.Join(want, "|") {
			t.Errorf("handled %v, want %v", w.notifs, want)
		}
		if n := w.count(); n != 0 {
			t.Errorf("the peer sent %d frames, want none for notifications", n)
		}
	})

	t.Run("a frame that is not JSON-RPC is answered with a parse error and a null id", func(t *testing.T) {
		for _, frame := range []string{`{"jsonrpc":"1.0"}`, `not json`} {
			w := &wire{}
			p := newPeer(w.send, w)
			p.receive([]byte(frame))
			w.mu.Lock()
			raw := string(w.sent[0])
			w.mu.Unlock()
			if !strings.HasPrefix(raw, `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,`) {
				t.Errorf("%s was answered with %s, want a -32700 error with a null id", frame, raw)
			}
		}
	})

	t.Run("a batch array is refused in v1", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		p.receive([]byte(`[{"jsonrpc":"2.0","method":"session/update","params":{}},{"jsonrpc":"2.0","method":"session/update","params":{}}]`))
		m := w.last(t)
		if m.Error == nil || m.Error.Code != codeInvalidRequest || string(m.ID) != "null" {
			t.Errorf("answered %+v, want error -32600 with a null id", m)
		}
		if len(w.notifs) != 0 {
			t.Errorf("handled %v, want nothing from a batch", w.notifs)
		}
	})

	t.Run("a response for an id nobody is waiting for is ignored", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		p.receive([]byte(`{"jsonrpc":"2.0","id":77,"result":{}}`))
		p.receive([]byte(`{"jsonrpc":"2.0","id":"x","result":{}}`))
		if n := w.count(); n != 0 {
			t.Errorf("the peer sent %d frames, want none", n)
		}
	})

	t.Run("concurrent calls each get their own answer", func(t *testing.T) {
		const n = 50
		w := &wire{}
		p := newPeer(w.send, w)
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			go func(i int) {
				want := fmt.Sprintf("s%d", i)
				var got promptResponse
				if err := p.call(context.Background(), methodSessionPrompt, promptRequest{SessionID: want}, &got); err != nil {
					errs <- err
					return
				}
				if got.StopReason != want {
					errs <- fmt.Errorf("call for %s got %s", want, got.StopReason)
					return
				}
				errs <- nil
			}(i)
		}
		sent := w.waitSent(t, n)
		for i := len(sent) - 1; i >= 0; i-- {
			var req promptRequest
			_ = json.Unmarshal(sent[i].Params, &req)
			p.receive([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"stopReason":%q}}`, sent[i].ID, req.SessionID)))
		}
		for i := 0; i < n; i++ {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
	})

	t.Run("a call after close fails at once", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		p.close(errPeerClosed)
		if err := p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil); !errors.Is(err, errPeerClosed) {
			t.Fatalf("call returned %v, want errPeerClosed", err)
		}
		if n := w.count(); n != 0 {
			t.Errorf("the peer sent %d frames, want none", n)
		}
	})

	t.Run("the 257th waiting call is refused", func(t *testing.T) {
		w := &wire{}
		p := newPeer(w.send, w)
		done := make(chan error, maxPendingCalls)
		for i := 0; i < maxPendingCalls; i++ {
			go func() { done <- p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil) }()
		}
		w.waitSent(t, maxPendingCalls)
		if err := p.call(context.Background(), methodSessionPrompt, promptRequest{}, nil); !errors.Is(err, errTooManyCalls) {
			t.Fatalf("call %d returned %v, want errTooManyCalls", maxPendingCalls+1, err)
		}
		p.close(errPeerClosed)
		for i := 0; i < maxPendingCalls; i++ {
			<-done
		}
	})

	t.Run("the 65th request in flight is answered with an internal error", func(t *testing.T) {
		release := make(chan struct{})
		w := &wire{answer: func(context.Context, string, json.RawMessage) (any, *rpcError) {
			<-release
			return struct{}{}, nil
		}}
		p := newPeer(w.send, w)
		for i := 1; i <= maxServedRequests; i++ {
			p.receive([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"session/request_permission","params":{}}`, i)))
		}
		p.receive([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"session/request_permission","params":{}}`, maxServedRequests+1)))
		m := w.last(t)
		if m.Error == nil || m.Error.Code != codeInternalError || string(m.ID) != fmt.Sprint(maxServedRequests+1) {
			t.Fatalf("answered %+v, want error -32603 for id %d", m, maxServedRequests+1)
		}
		close(release)
		w.waitSent(t, maxServedRequests+1)
	})
}
