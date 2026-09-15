// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeAuth records what the socket asked it to do.
type fakeAuth struct {
	mu         sync.Mutex
	touched    []touch
	released   []release
	touchErr   error
	releaseErr error
}

type touch struct {
	machineID  int64
	instanceID string
}
type release struct {
	machineID  int64
	instanceID string
}

func (a *fakeAuth) AuthenticateMachine(context.Context, string) (Identity, error) {
	return Identity{}, ErrUnknownToken
}

func (a *fakeAuth) Touch(_ context.Context, id int64, _ time.Time, inst string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.touchErr != nil {
		return a.touchErr
	}
	a.touched = append(a.touched, touch{id, inst})
	return nil
}

func (a *fakeAuth) Release(_ context.Context, id int64, inst string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.releaseErr != nil {
		return a.releaseErr
	}
	a.released = append(a.released, release{id, inst})
	return nil
}

func (a *fakeAuth) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.touched), len(a.released)
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name, header, want string
		wantErr            bool
	}{
		{"ordinary", "Bearer abc123", "abc123", false},
		{"padded", "Bearer   abc123  ", "abc123", false},
		{"no prefix", "abc123", "", true},
		{"wrong scheme", "Basic abc123", "", true},
		{"empty", "", "", true},
		{"prefix only", "Bearer ", "", true},
		// Case matters: "bearer" is not the scheme RFC 6750 defines, and
		// quietly accepting variants makes the check something nobody can
		// reason about.
		{"lowercase scheme", "bearer abc123", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BearerToken(tc.header)
			if (err != nil) != tc.wantErr {
				t.Fatalf("BearerToken(%q) error = %v, wantErr %v", tc.header, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("BearerToken(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

// Identity comes from the token, never from a header. Stating it a second time
// turns a redundancy into a detection signal.
func TestHeaderMismatchIsRefused(t *testing.T) {
	id := Identity{MachineID: 7, UserID: 42}
	decode := func(s string) int64 {
		switch s {
		case "m7":
			return 7
		case "m9":
			return 9
		case "u42":
			return 42
		case "u43":
			return 43
		}
		return 0
	}

	if err := CheckIdentityHeaders(id, "m7", "u42", decode); err != nil {
		t.Errorf("matching headers rejected: %v", err)
	}
	if err := CheckIdentityHeaders(id, "m9", "u42", decode); !errors.Is(err, ErrHeaderMismatch) {
		t.Errorf("machine mismatch error = %v, want ErrHeaderMismatch", err)
	}
	if err := CheckIdentityHeaders(id, "m7", "u43", decode); !errors.Is(err, ErrHeaderMismatch) {
		t.Errorf("user mismatch error = %v, want ErrHeaderMismatch", err)
	}
}

// An older daemon that predates the headers is not an attacker, and refusing it
// would make adding a header a breaking change.
func TestAbsentHeadersAreAccepted(t *testing.T) {
	id := Identity{MachineID: 7, UserID: 42}
	decode := func(string) int64 { return 0 }
	if err := CheckIdentityHeaders(id, "", "", decode); err != nil {
		t.Errorf("a daemon sending no identity headers was refused: %v", err)
	}
}

func TestNewSessionRegistersAndRecordsTheInstance(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{}
	c := &fakeConn{}

	s, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7, UserID: 42}, c)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := r.Get(7); err != nil {
		t.Errorf("machine not registered: %v", err)
	}
	touched, _ := auth.counts()
	if touched != 1 || auth.touched[0].instanceID != "pod-a" {
		t.Errorf("connection not recorded against this instance: %+v", auth.touched)
	}

	if err := s.Heartbeat(t.Context()); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if touched, _ := auth.counts(); touched != 2 {
		t.Errorf("heartbeat not recorded: %d touches", touched)
	}
}

// Registered but unrecorded means another instance cannot find us. Better to
// refuse the connection than serve a machine nothing can route to.
func TestNewSessionUndoesItselfIfTheInstanceCannotBeRecorded(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{touchErr: errors.New("database down")}
	c := &fakeConn{}

	if _, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, c); err == nil {
		t.Fatal("expected NewSession to fail")
	}
	if _, err := r.Get(7); !errors.Is(err, ErrNotConnected) {
		t.Error("a machine that failed to record was left in the registry")
	}
}

// The old socket is almost always a dead connection nobody has noticed.
func TestASecondConnectionClosesTheOneItDisplaces(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{}
	old := &fakeConn{name: "old"}
	fresh := &fakeConn{name: "fresh"}

	if _, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, old); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, fresh); err != nil {
		t.Fatal(err)
	}

	if !old.isClosed() {
		t.Error("the displaced socket was left open")
	}
	if fresh.isClosed() {
		t.Error("the new socket was closed")
	}
}

func TestCloseClearsTheRegistryAndThePairing(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{}
	c := &fakeConn{}

	s, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := r.Get(7); !errors.Is(err, ErrNotConnected) {
		t.Error("still registered after Close")
	}
	if !c.isClosed() {
		t.Error("the socket was not closed")
	}
	_, released := auth.counts()
	if released != 1 || auth.released[0].instanceID != "pod-a" {
		t.Errorf("pairing not cleared: %+v", auth.released)
	}
}

// Called from the reader loop ending, from a deferred cleanup, and from
// revocation — all three can happen at once.
func TestCloseIsIdempotent(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{}
	s, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, &fakeConn{})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.Close(context.Background()) }()
	}
	wg.Wait()

	if _, released := auth.counts(); released != 1 {
		t.Errorf("the pairing was cleared %d times, want once", released)
	}
}

// The guard that matters: if the daemon has already reconnected, another
// connection owns the pairing and clearing it would unroute a live machine.
func TestALateCloseDoesNotClearAReconnectedMachinesPairing(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &fakeAuth{}

	old, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, &fakeConn{name: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSession(t.Context(), r, auth, Identity{MachineID: 7}, &fakeConn{name: "fresh"}); err != nil {
		t.Fatal(err)
	}

	// The old session finally notices it is finished and cleans up.
	if err := old.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, released := auth.counts(); released != 0 {
		t.Errorf("a stale session cleared the live machine's pairing: %+v", auth.released)
	}
	if _, err := r.Get(7); err != nil {
		t.Errorf("the reconnected machine was unrouted: %v", err)
	}
}

// Well under OnlineThreshold, so a machine has to miss several beats before it
// is called offline.
func TestHeartbeatIntervalLeavesRoomBeforeOffline(t *testing.T) {
	if HeartbeatInterval*2 > OnlineThreshold {
		t.Errorf("HeartbeatInterval %v is too close to OnlineThreshold %v", HeartbeatInterval, OnlineThreshold)
	}
}
