// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// The assertion the whole feature rests on: a terminal stream is arbitrary
// bytes, and anything that "helpfully" re-encodes them breaks Esc, colour
// resets and half the box-drawing characters in the world.
func TestRoundTripPreservesArbitraryBytes(t *testing.T) {
	payloads := map[string][]byte{
		"escape":       {0x1b},
		"csi clear":    []byte("\x1b[2K"),
		"cursor up":    []byte("\x1b[1A"),
		"carriage ret": []byte("progress: 42%\r"),
		"invalid utf8": {0xff, 0xfe, 0x80, 0x00},
		"nul bytes":    {0x00, 0x00, 0x00},
		"every byte":   allBytes(),
		"empty":        {},
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			in, err := SessionFrame(TypeOutput, 7, payload)
			if err != nil {
				t.Fatalf("SessionFrame: %v", err)
			}
			encoded, err := in.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			out, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if out.Type != TypeOutput || out.SessionID != 7 {
				t.Errorf("header lost: got %s", out)
			}
			if !bytes.Equal(out.Payload, payload) {
				t.Errorf("payload changed:\n got %#v\nwant %#v", out.Payload, payload)
			}
		})
	}
}

func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// A WebSocket read buffer is reused by the next read. A frame that aliased it
// would mutate under anyone holding it — corrupted output, far from the cause.
func TestDecodeCopiesPayload(t *testing.T) {
	f, err := SessionFrame(TypeOutput, 1, []byte("hello"))
	if err != nil {
		t.Fatalf("SessionFrame: %v", err)
	}
	buf, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// Simulate the socket reusing its buffer for the next read.
	for i := range buf {
		buf[i] = 'X'
	}
	if string(got.Payload) != "hello" {
		t.Errorf("payload aliased the read buffer: got %q", got.Payload)
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrShortFrame},
		{"header truncated", make([]byte, HeaderSize-1), ErrShortFrame},
		{"unknown type", append([]byte{0x7f}, make([]byte, 8)...), ErrUnknownType},
		{"control with session", append([]byte{byte(TypeControl)}, 0, 0, 0, 0, 0, 0, 0, 9), ErrSessionOnCtrl},
		{"session frame without session", append([]byte{byte(TypeOutput)}, make([]byte, 8)...), ErrNoSession},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("Decode() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A length taken on trust is how a remote peer allocates memory on your behalf.
func TestPayloadCapEnforcedBothWays(t *testing.T) {
	big := make([]byte, MaxPayload+1)
	f := Frame{Type: TypeOutput, SessionID: 1, Payload: big}
	if _, err := f.Encode(); !errors.Is(err, ErrPayloadTooBig) {
		t.Errorf("Encode() error = %v, want ErrPayloadTooBig", err)
	}

	buf := append([]byte{byte(TypeOutput), 0, 0, 0, 0, 0, 0, 0, 1}, big...)
	if _, err := Decode(buf); !errors.Is(err, ErrPayloadTooBig) {
		t.Errorf("Decode() error = %v, want ErrPayloadTooBig", err)
	}
}

// Encode refuses what Decode refuses, so a bug on the sending side is caught
// where it happens rather than at the far end.
func TestEncodeEnforcesTheSameInvariants(t *testing.T) {
	tests := []struct {
		name string
		in   Frame
		want error
	}{
		{"unknown type", Frame{Type: 0x42, SessionID: 1}, ErrUnknownType},
		{"control with session", Frame{Type: TypeControl, SessionID: 3}, ErrSessionOnCtrl},
		{"output without session", Frame{Type: TypeOutput}, ErrNoSession},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.Encode(); !errors.Is(err, tc.want) {
				t.Errorf("Encode() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSessionFrameRejectsBadCombinations(t *testing.T) {
	if _, err := SessionFrame(TypeOutput, 0, nil); !errors.Is(err, ErrNoSession) {
		t.Errorf("SessionFrame(session 0) error = %v, want ErrNoSession", err)
	}
	if _, err := SessionFrame(TypeControl, 5, nil); !errors.Is(err, ErrSessionOnCtrl) {
		t.Errorf("SessionFrame(control) error = %v, want ErrSessionOnCtrl", err)
	}
}

func TestTypeStringNamesEveryType(t *testing.T) {
	want := map[Type]string{
		TypeControl: "CONTROL", TypeInput: "INPUT", TypeOutput: "OUTPUT",
		TypeResize: "RESIZE", TypeExit: "EXIT", TypeReplay: "REPLAY",
	}
	for typ, name := range want {
		if got := typ.String(); got != name {
			t.Errorf("Type(%#x).String() = %q, want %q", byte(typ), got, name)
		}
	}
	// An unexpected byte must report itself, not appear as an integer nobody
	// can look up.
	if got := Type(0x42).String(); got != "Type(0x42)" {
		t.Errorf("unknown Type.String() = %q", got)
	}
}

// A frame's String must not print the payload: it is someone's terminal.
func TestFrameStringOmitsPayload(t *testing.T) {
	f := Frame{Type: TypeOutput, SessionID: 3, Payload: []byte("hunter2")}
	got := f.String()
	if bytes.Contains([]byte(got), []byte("hunter2")) {
		t.Errorf("Frame.String() leaked the payload: %s", got)
	}
	if want := "wire.Frame{OUTPUT session=3 payload=7B}"; got != want {
		t.Errorf("Frame.String() = %q, want %q", got, want)
	}
}

func TestControlRoundTrip(t *testing.T) {
	body, err := json.Marshal(Resize{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	f, err := ControlFrame(Control{ID: "c1", Op: OpStartSession, Body: body})
	if err != nil {
		t.Fatalf("ControlFrame: %v", err)
	}
	if f.SessionID != 0 {
		t.Errorf("control frame session = %d, want 0", f.SessionID)
	}

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, err := ParseControl(decoded)
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}
	if got.ID != "c1" || got.Op != OpStartSession {
		t.Errorf("control changed: %+v", got)
	}
	var size Resize
	if err := json.Unmarshal(got.Body, &size); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if size.Cols != 120 || size.Rows != 40 {
		t.Errorf("body changed: %+v", size)
	}
}

// An op from a newer backend must not stop an older daemon parsing the
// envelope — that is the difference between "ignored this message" and
// "dropped the connection".
func TestParseControlAcceptsUnknownOps(t *testing.T) {
	f, err := ControlFrame(Control{Op: "somethingNewer"})
	if err != nil {
		t.Fatalf("ControlFrame: %v", err)
	}
	got, err := ParseControl(f)
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}
	if got.Op != "somethingNewer" {
		t.Errorf("op = %q", got.Op)
	}
}

func TestControlErrors(t *testing.T) {
	if _, err := ControlFrame(Control{}); err == nil {
		t.Error("ControlFrame with no op should fail")
	}
	if _, err := ParseControl(Frame{Type: TypeOutput, SessionID: 1}); err == nil {
		t.Error("ParseControl on a non-control frame should fail")
	}
	if _, err := ParseControl(Frame{Type: TypeControl, Payload: []byte("{not json")}); err == nil {
		t.Error("ParseControl on bad JSON should fail")
	}
	if _, err := ParseControl(Frame{Type: TypeControl, Payload: []byte(`{"id":"x"}`)}); err == nil {
		t.Error("ParseControl with no op should fail")
	}
}

// json.RawMessage is compacted on marshal, so invalid JSON in Body fails there
// rather than producing a frame the far end cannot parse. Worth proving: it is
// the difference between a caught bug and a mystery at the other end.
func TestControlFrameRejectsUnmarshalableBody(t *testing.T) {
	_, err := ControlFrame(Control{Op: OpHello, Body: json.RawMessage("{not json")})
	if err == nil {
		t.Fatal("ControlFrame with invalid Body should fail")
	}
}

func TestVersionIsAnnouncedFromTheStart(t *testing.T) {
	// Worthless if added once there is already a fleet that never sent it.
	if Version < 1 {
		t.Errorf("Version = %d, want >= 1", Version)
	}
}
