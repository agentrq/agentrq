// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Two errors are the same error when their codes are, so a wrapped one is
// still recognised by what it is.
func TestErrorIsMatchesByCode(t *testing.T) {
	wrapped := fmt.Errorf("%w: details", ErrInvalidParams)
	if !errors.Is(wrapped, ErrInvalidParams) {
		t.Error("a wrapped invalid-params error is not recognised as one")
	}
	if errors.Is(wrapped, ErrMethodNotFound) {
		t.Error("an invalid-params error passed for method-not-found")
	}
	if ErrParse.Is(errors.New("plain")) {
		t.Error("a plain error matched a JSON-RPC error")
	}
	var nilErr *Error
	if ErrParse.Is(nilErr) {
		t.Error("a nil *Error matched")
	}
	if ErrParse.Error() != "parse error" {
		t.Errorf("Error() = %q, want the message", ErrParse.Error())
	}
}

// ToWire sends an error with its own code when it is one, with the code of
// the one it wraps, or with none.
func TestToWire(t *testing.T) {
	if ToWire(nil) != nil {
		t.Error("nil became an error object")
	}
	if got := ToWire(ErrInternal); got != ErrInternal {
		t.Errorf("an *Error was not sent as itself: %+v", got)
	}
	got := ToWire(fmt.Errorf("%w: no such thing", ErrMethodNotFound))
	if got.Code != CodeMethodNotFound || got.Message != "method not found: no such thing" {
		t.Errorf("wrapped error sent as %+v", got)
	}
	got = ToWire(errors.New("boom"))
	if got.Code != 0 || got.Message != "boom" {
		t.Errorf("plain error sent as %+v", got)
	}
}

// An id is an integer, a string or nothing, whatever JSON shape it came in.
func TestMakeID(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{"abc", "abc"},
		{json.Number("12345678901234567"), int64(12345678901234567)},
		{json.Number("1.5"), int64(1)},
		{json.Number("-7"), int64(-7)},
		{float64(3), int64(3)},
		{int64(9), int64(9)},
	}
	for _, c := range cases {
		id, err := MakeID(c.in)
		if err != nil {
			t.Fatalf("MakeID(%v): %v", c.in, err)
		}
		if id.Raw() != c.want {
			t.Errorf("MakeID(%v) = %v, want %v", c.in, id.Raw(), c.want)
		}
	}
	if id, _ := MakeID(nil); id.IsValid() {
		t.Error("a nil id is valid")
	}
	if _, err := MakeID(json.Number("1e999")); !errors.Is(err, ErrParse) {
		t.Errorf("an id out of range = %v, want a parse error", err)
	}
	if _, err := MakeID(map[string]any{}); err == nil || err.Error() != "parse error: invalid ID type map[string]interface {}" {
		t.Errorf("an object id = %v, want a parse error naming its type", err)
	}
}

// A request is a call when it has an id.
func TestRequestIsCall(t *testing.T) {
	if (&Request{Method: "x"}).IsCall() {
		t.Error("a request without an id is a call")
	}
	if !(&Request{ID: StringID("a"), Method: "x"}).IsCall() {
		t.Error("a request with an id is not a call")
	}
}

// Messages are encoded in wire order, leaving out what they lack, and <, >
// and & are not escaped.
func TestEncode(t *testing.T) {
	call, err := NewCall(Int64ID(1), "tools/call", map[string]string{"q": "<a&b>"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(call)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"q":"<a&b>"}}` {
		t.Errorf("call encoded as %s", data)
	}

	note, _ := NewNotification("notifications/x", nil)
	data, _ = Encode(note)
	if string(data) != `{"jsonrpc":"2.0","method":"notifications/x"}` {
		t.Errorf("notification encoded as %s", data)
	}

	resp, _ := NewResponse(StringID("s"), map[string]int{"n": 1}, nil)
	data, _ = Encode(resp)
	if string(data) != `{"jsonrpc":"2.0","id":"s","result":{"n":1}}` {
		t.Errorf("response encoded as %s", data)
	}

	failed := &Response{ID: Int64ID(2), Error: fmt.Errorf("%w: nope", ErrInvalidParams)}
	data, _ = Encode(failed)
	if string(data) != `{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"invalid params: nope"}}` {
		t.Errorf("error response encoded as %s", data)
	}

	if _, err := Encode(&Request{Method: "x", Params: json.RawMessage("{")}); err == nil {
		t.Error("params that are not JSON were encoded")
	}
}

// Marshal reports a value JSON cannot hold, and gives nil for nil.
func TestMarshal(t *testing.T) {
	if raw, err := Marshal(nil); raw != nil || err != nil {
		t.Errorf("Marshal(nil) = %s, %v", raw, err)
	}
	if _, err := Marshal(make(chan int)); err == nil {
		t.Error("a channel was marshaled")
	}
	if _, err := NewNotification("x", make(chan int)); err == nil {
		t.Error("a notification with unmarshalable params was built without error")
	}
}

// Decode reads requests, notifications and responses, and refuses what is
// not JSON-RPC 2.0.
func TestDecode(t *testing.T) {
	msg, err := Decode([]byte(`{"jsonrpc":"2.0","id":7,"method":"ping","params":{"a":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	req := msg.(*Request)
	if req.ID.Raw() != int64(7) || req.Method != "ping" || string(req.Params) != `{"a":1}` {
		t.Errorf("request decoded as %+v", req)
	}

	msg, _ = Decode([]byte(`{"jsonrpc":"2.0","id":"x","result":{"ok":true}}`))
	if resp := msg.(*Response); resp.ID.Raw() != "x" || string(resp.Result) != `{"ok":true}` || resp.Error != nil {
		t.Errorf("response decoded as %+v", resp)
	}

	msg, _ = Decode([]byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-1,"message":"m"}}`))
	if resp := msg.(*Response); resp.Error == nil || resp.Error.Error() != "m" {
		t.Errorf("error response decoded as %+v", resp)
	}

	msg, _ = Decode([]byte(`{"jsonrpc":"2.0","method":"tools/call","params":null}`))
	if req := msg.(*Request); string(req.Params) != "null" {
		t.Errorf("null params decoded as %q, want them kept as null", req.Params)
	}

	for body, want := range map[string]string{
		`{"jsonrpc":`:                            "unmarshaling jsonrpc message",
		`{"id":1,"method":"ping"}`:               `invalid message version tag ""`,
		`{"jsonrpc":"2.0","id":{},"method":"x"}`: "invalid ID type",
		`{"jsonrpc":"2.0","id":1,"method":5}`:    "unmarshaling jsonrpc message",
		`{"jsonrpc":"2.0","result":{}}`:          "invalid request",
	} {
		if _, err := Decode([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Decode(%s) = %v, want an error containing %q", body, err, want)
		}
	}
}

// A batch is an array of messages; anything else is one message.
func TestDecodeBatch(t *testing.T) {
	msgs, batch, err := DecodeBatch([]byte(`[{"jsonrpc":"2.0","id":1,"method":"a"},{"jsonrpc":"2.0","method":"b"}]`))
	if err != nil || !batch || len(msgs) != 2 {
		t.Fatalf("batch decoded as %v, %v, %v", msgs, batch, err)
	}
	msgs, batch, err = DecodeBatch([]byte(`{"jsonrpc":"2.0","id":1,"method":"a"}`))
	if err != nil || batch || len(msgs) != 1 {
		t.Fatalf("single message decoded as %v, %v, %v", msgs, batch, err)
	}
	if _, batch, err := DecodeBatch([]byte(`[]`)); err == nil || !batch || err.Error() != "empty batch" {
		t.Errorf("empty batch = %v, %v", batch, err)
	}
	if _, batch, err := DecodeBatch([]byte(`[{"jsonrpc":"1.0","method":"a"}]`)); err == nil || !batch {
		t.Errorf("a batch with a bad message = %v, %v", batch, err)
	}
	if _, batch, err := DecodeBatch([]byte(`[1,`)); err == nil || batch {
		t.Errorf("a malformed array = %v, %v, want a malformed message", batch, err)
	}
}

// Requests and responses are the only messages.
func TestMessageKinds(t *testing.T) {
	for _, m := range []Message{&Request{}, &Response{}} {
		m.isMessage()
	}
}
