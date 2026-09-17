// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package wire is the frame format spoken between agentrqd and the AgentRQ
// backend, and it is deliberately the only definition of it.
//
// The daemon and the relay are built from different modules, so the temptation
// is for each to declare "its own" copy of the format. Protocol drift between
// the two is the worst bug available in this design — a daemon in the field is
// not upgradable on demand, so old daemons talking to new backends is the
// normal case rather than the exception. Hence one exported package, imported
// by both sides.
//
// For that to remain possible this package must import nothing from
// daemon/internal, and nothing outside the standard library. Adding a
// dependency here is how the backend stops being able to depend on it.
//
// # Why binary
//
// PTY traffic is arbitrary bytes at volume. JSON with base64 costs a third more
// on every frame and invites questions about how 0x1b or invalid UTF-8 survives
// the trip. As bytes, they simply do.
//
//	[ type:1 ][ sessionID:8 big-endian ][ payload... ]
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Version is the protocol version announced in the hello control message.
//
// Sent from the very first commit on purpose: the field is worthless if it is
// added once there is already a fleet in the field that never sent it.
const Version = 1

// Type is the first byte of every frame.
type Type byte

const (
	// TypeControl carries a JSON [Control] message and always has session 0.
	TypeControl Type = 0x00
	// TypeInput carries raw bytes to write to a session's PTY. Esc is simply 0x1b.
	TypeInput Type = 0x01
	// TypeOutput carries raw bytes read from a session's PTY.
	TypeOutput Type = 0x02
	// TypeResize carries a JSON [Resize] for a session.
	TypeResize Type = 0x03
	// TypeExit reports that a session's process ended.
	TypeExit Type = 0x04
	// TypeReplay carries a synthesised redraw of a session's current screen,
	// sent on attach before live output. Not a replay of byte history: output
	// that redraws in place (a progress bar) makes byte history useless.
	TypeReplay Type = 0x05
)

// HeaderSize is the fixed prefix: one type byte plus an eight-byte session id.
const HeaderSize = 9

// MaxPayload bounds a single frame's payload.
//
// A cap has to exist somewhere, and it belongs here rather than only at the
// socket: Decode is handed whatever arrived, and a length taken on trust is how
// a remote peer allocates memory on your behalf. 1 MiB is far above any real
// frame — output is coalesced into ~30ms batches — and far below trouble.
const MaxPayload = 1 << 20

// Errors returned by [Decode]. Distinguished so a caller can tell a framing bug
// from a protocol violation; both close the connection, but only one is a bug
// on this side.
var (
	ErrShortFrame    = errors.New("wire: frame shorter than header")
	ErrPayloadTooBig = errors.New("wire: payload exceeds maximum")
	ErrUnknownType   = errors.New("wire: unknown frame type")
	ErrSessionOnCtrl = errors.New("wire: control frame must have session 0")
	ErrNoSession     = errors.New("wire: session frame must have a non-zero session")
)

// Frame is one message on the socket.
type Frame struct {
	Type      Type
	SessionID uint64
	Payload   []byte
}

// String makes a frame readable in a test failure or a log without dumping the
// payload, which is someone's terminal and may hold a secret.
func (f Frame) String() string {
	return fmt.Sprintf("wire.Frame{%s session=%d payload=%dB}", f.Type, f.SessionID, len(f.Payload))
}

// String names the type, so an unexpected byte reports itself rather than
// appearing as an integer nobody can look up.
func (t Type) String() string {
	switch t {
	case TypeControl:
		return "CONTROL"
	case TypeInput:
		return "INPUT"
	case TypeOutput:
		return "OUTPUT"
	case TypeResize:
		return "RESIZE"
	case TypeExit:
		return "EXIT"
	case TypeReplay:
		return "REPLAY"
	default:
		return fmt.Sprintf("Type(0x%02x)", byte(t))
	}
}

// known reports whether t is a type this version defines.
func (t Type) known() bool {
	switch t {
	case TypeControl, TypeInput, TypeOutput, TypeResize, TypeExit, TypeReplay:
		return true
	default:
		return false
	}
}

// Encode renders the frame as the bytes to put on the socket.
//
// It returns an error for the same invariants Decode enforces, so a bug on the
// sending side is caught where it happens rather than becoming a protocol
// violation the far end has to report.
func (f Frame) Encode() ([]byte, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	out := make([]byte, HeaderSize+len(f.Payload))
	out[0] = byte(f.Type)
	binary.BigEndian.PutUint64(out[1:HeaderSize], f.SessionID)
	copy(out[HeaderSize:], f.Payload)
	return out, nil
}

// Decode parses one frame.
//
// The payload is copied rather than sliced out of b. A WebSocket read buffer is
// reused by the next read, so a frame that aliased it would change underneath
// anyone holding it — a bug that shows up as corrupted terminal output long
// after the read that caused it.
func Decode(b []byte) (Frame, error) {
	if len(b) < HeaderSize {
		return Frame{}, fmt.Errorf("%w: got %d bytes, need %d", ErrShortFrame, len(b), HeaderSize)
	}
	payload := b[HeaderSize:]
	if len(payload) > MaxPayload {
		return Frame{}, fmt.Errorf("%w: %d > %d", ErrPayloadTooBig, len(payload), MaxPayload)
	}

	f := Frame{
		Type:      Type(b[0]),
		SessionID: binary.BigEndian.Uint64(b[1:HeaderSize]),
		Payload:   append([]byte(nil), payload...),
	}
	if err := f.validate(); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// DecodeFromViewer reads a frame from a browser, which does not name a session.
//
// It cannot, and must not. The session a viewer may drive is decided by the
// socket it attached to and is overwritten on arrival — that is what stops an
// attached browser typing into another session by changing a number. Requiring
// it to send one anyway asks for information it does not have, and that is not
// a hypothetical: the browser holds base62 ids, so the first version of this
// made every keystroke throw while output kept arriving, which looks exactly
// like a terminal that ignores the keyboard.
//
// So a zero session is expected here, and everything else is checked as
// normal.
func DecodeFromViewer(b []byte) (Frame, error) {
	if len(b) < HeaderSize {
		return Frame{}, fmt.Errorf("%w: got %d bytes, need %d", ErrShortFrame, len(b), HeaderSize)
	}
	payload := b[HeaderSize:]
	if len(payload) > MaxPayload {
		return Frame{}, fmt.Errorf("%w: %d > %d", ErrPayloadTooBig, len(payload), MaxPayload)
	}

	f := Frame{
		Type:      Type(b[0]),
		SessionID: binary.BigEndian.Uint64(b[1:HeaderSize]),
		Payload:   append([]byte(nil), payload...),
	}
	if !f.Type.known() {
		return Frame{}, fmt.Errorf("%w: 0x%02x", ErrUnknownType, byte(f.Type))
	}
	if f.Type == TypeControl && f.SessionID != 0 {
		return Frame{}, fmt.Errorf("%w: got %d", ErrSessionOnCtrl, f.SessionID)
	}
	return f, nil
}

// validate holds the invariants that make the format self-checking: an unknown
// type, or a session id that contradicts the type, is caught at the boundary
// instead of becoming a confusing failure further in.
func (f Frame) validate() error {
	if !f.Type.known() {
		return fmt.Errorf("%w: 0x%02x", ErrUnknownType, byte(f.Type))
	}
	if len(f.Payload) > MaxPayload {
		return fmt.Errorf("%w: %d > %d", ErrPayloadTooBig, len(f.Payload), MaxPayload)
	}
	if f.Type == TypeControl && f.SessionID != 0 {
		return fmt.Errorf("%w: got %d", ErrSessionOnCtrl, f.SessionID)
	}
	if f.Type != TypeControl && f.SessionID == 0 {
		return fmt.Errorf("%w: type %s", ErrNoSession, f.Type)
	}
	return nil
}
