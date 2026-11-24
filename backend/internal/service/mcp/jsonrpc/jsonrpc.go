// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package jsonrpc is the JSON-RPC 2.0 layer of the MCP server: message
// framing, request ids and wire errors.
package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Error is a JSON-RPC error object. Two errors are the same error when their
// codes are, so errors.Is matches a wrapped one by code.
type Error struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Is reports whether target is an *Error with the same code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t != nil && e.Code == t.Code
}

// The errors every request can fail with. Wrap them to keep the code and
// change the message: fmt.Errorf("%w: ...", ErrInvalidParams).
var (
	ErrParse          = &Error{Code: CodeParseError, Message: "parse error"}
	ErrInvalidRequest = &Error{Code: CodeInvalidRequest, Message: "invalid request"}
	ErrMethodNotFound = &Error{Code: CodeMethodNotFound, Message: "method not found"}
	ErrInvalidParams  = &Error{Code: CodeInvalidParams, Message: "invalid params"}
	ErrInternal       = &Error{Code: CodeInternalError, Message: "internal error"}
)

// ErrNotHandled is a method nobody serves. It answers as ErrMethodNotFound.
var ErrNotHandled = errors.New("JSON RPC not handled")

// ToWire is the error object err is sent as: itself when it is one, otherwise
// its message with the code of the *Error it wraps, if any (0 if none).
func ToWire(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	wire := &Error{Message: err.Error()}
	var wrapped *Error
	if errors.As(err, &wrapped) {
		wire.Code = wrapped.Code
	}
	return wire
}

// ID identifies a request: an integer, a string, or nothing for a
// notification. IDs are comparable, so they can key maps.
type ID struct {
	value any
}

// Int64ID is an integer id.
func Int64ID(i int64) ID { return ID{value: i} }

// StringID is a string id.
func StringID(s string) ID { return ID{value: s} }

// IsValid reports whether the id is set; a notification's is not.
func (id ID) IsValid() bool { return id.value != nil }

// Raw is the id's value: an int64, a string, or nil.
func (id ID) Raw() any { return id.value }

// MakeID turns a decoded JSON id into an ID. Integers keep every digit; a
// number with a fraction keeps its integer part, which is what a peer reading
// it as a double would see.
func MakeID(v any) (ID, error) {
	switch v := v.(type) {
	case nil:
		return ID{}, nil
	case string:
		return StringID(v), nil
	case json.Number:
		if i, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
			return Int64ID(i), nil
		}
		f, err := v.Float64()
		if err != nil {
			return ID{}, fmt.Errorf("%w: invalid ID %q", ErrParse, v.String())
		}
		return Int64ID(int64(f)), nil
	case float64:
		return Int64ID(int64(v)), nil
	case int64:
		return Int64ID(v), nil
	}
	return ID{}, fmt.Errorf("%w: invalid ID type %T", ErrParse, v)
}

// Message is a *Request or a *Response.
type Message interface {
	isMessage()
}

// Request is a call, which has an ID and expects a response, or a
// notification, which has neither.
type Request struct {
	ID     ID
	Method string
	Params json.RawMessage
	// Extra travels with the request in process and never on the wire.
	Extra any
}

// IsCall reports whether the request expects a response.
func (r *Request) IsCall() bool { return r.ID.IsValid() }

func (*Request) isMessage() {}

// Response answers the call with the same ID: Result, or Error.
type Response struct {
	ID     ID
	Result json.RawMessage
	Error  error
}

func (*Response) isMessage() {}

// NewNotification is a notification of method with params marshaled.
func NewNotification(method string, params any) (*Request, error) {
	raw, err := Marshal(params)
	return &Request{Method: method, Params: raw}, err
}

// NewCall is a call of method with params marshaled.
func NewCall(id ID, method string, params any) (*Request, error) {
	raw, err := Marshal(params)
	return &Request{ID: id, Method: method, Params: raw}, err
}

// NewResponse answers id with result marshaled, or with err.
func NewResponse(id ID, result any, err error) (*Response, error) {
	raw, merr := Marshal(result)
	return &Response{ID: id, Result: raw, Error: err}, merr
}

// wire is every field of a request or a response, in wire order.
type wire struct {
	Version string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Encode is a message's wire form.
func Encode(msg Message) ([]byte, error) {
	w := wire{Version: "2.0"}
	switch m := msg.(type) {
	case *Request:
		w.ID, w.Method, w.Params = m.ID.value, m.Method, m.Params
	case *Response:
		w.ID, w.Result, w.Error = m.ID.value, m.Result, ToWire(m.Error)
	}
	return Marshal(&w)
}

// Marshal is json.Marshal without escaping <, > and &, and nil for nil.
func Marshal(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decodeWire has method raw, to tell a missing method from an empty one.
type decodeWire struct {
	Version string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  json.RawMessage `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
}

// Decode reads one message: a request when it has a method, otherwise a
// response, which must have an id.
func Decode(data []byte) (Message, error) {
	var w decodeWire
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("unmarshaling jsonrpc message: %w", err)
	}
	if w.Version != "2.0" {
		return nil, fmt.Errorf("invalid message version tag %q; expected %q", w.Version, "2.0")
	}
	id, err := MakeID(w.ID)
	if err != nil {
		return nil, err
	}
	if len(w.Method) > 0 {
		var method string
		if err := json.Unmarshal(w.Method, &method); err != nil {
			return nil, fmt.Errorf("unmarshaling jsonrpc message: %w", err)
		}
		return &Request{ID: id, Method: method, Params: w.Params}, nil
	}
	if !id.IsValid() {
		return nil, ErrInvalidRequest
	}
	resp := &Response{ID: id, Result: w.Result}
	if w.Error != nil {
		resp.Error = w.Error
	}
	return resp, nil
}

// DecodeBatch reads a body holding one message or a JSON array of them,
// reporting which it was. A body that is not an array is read as a single
// message, so a malformed array is reported as a malformed message.
func DecodeBatch(data []byte) (msgs []Message, isBatch bool, err error) {
	var raws []json.RawMessage
	if json.Unmarshal(data, &raws) == nil {
		if len(raws) == 0 {
			return nil, true, errors.New("empty batch")
		}
		for _, raw := range raws {
			msg, err := Decode(raw)
			if err != nil {
				return nil, true, err
			}
			msgs = append(msgs, msg)
		}
		return msgs, true, nil
	}
	msg, err := Decode(data)
	if err != nil {
		return nil, false, err
	}
	return []Message{msg}, false, nil
}
