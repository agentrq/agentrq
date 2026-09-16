// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// fakeStore is the slice of the repository this package uses.
type fakeStore struct {
	byHash     map[string]model.Machine
	getErr     error
	touched    []touch
	released   []release
	touchErr   error
	releaseErr error
}

func (s *fakeStore) GetMachineByTokenHash(_ context.Context, hash string) (model.Machine, error) {
	if s.getErr != nil {
		return model.Machine{}, s.getErr
	}
	m, ok := s.byHash[hash]
	if !ok {
		return model.Machine{}, gorm.ErrRecordNotFound
	}
	return m, nil
}

func (s *fakeStore) TouchMachine(_ context.Context, id int64, _ time.Time, inst string) error {
	if s.touchErr != nil {
		return s.touchErr
	}
	s.touched = append(s.touched, touch{id, inst})
	return nil
}

func (s *fakeStore) ReleaseMachine(_ context.Context, id int64, inst string) error {
	if s.releaseErr != nil {
		return s.releaseErr
	}
	s.released = append(s.released, release{id, inst})
	return nil
}

func storeWith(token string, m model.Machine) *fakeStore {
	m.TokenHash = HashSecret(token)
	return &fakeStore{byHash: map[string]model.Machine{m.TokenHash: m}}
}

func TestAuthenticateMachineResolvesAToken(t *testing.T) {
	st := storeWith("tok-good", model.Machine{ID: 7, UserID: 42, Enabled: true})
	a := StoreAuthenticator{Store: st}

	id, err := a.AuthenticateMachine(t.Context(), "tok-good")
	if err != nil {
		t.Fatalf("AuthenticateMachine: %v", err)
	}
	if id.MachineID != 7 || id.UserID != 42 {
		t.Errorf("identity = %+v, want machine 7 user 42", id)
	}
}

func TestAuthenticateMachineRefusesWhatItShould(t *testing.T) {
	good := storeWith("tok-good", model.Machine{ID: 7, UserID: 42, Enabled: true})

	tests := []struct {
		name  string
		store Store
		token string
		want  error
	}{
		{"no token", good, "", ErrNoToken},
		{"unknown token", good, "tok-other", ErrUnknownToken},
		// A database failure is answered the same as an unknown token. The
		// caller says only that it was not accepted; distinguishing them here
		// would tell a caller when the database is down.
		{"store failure", &fakeStore{getErr: errors.New("db down")}, "tok-good", ErrUnknownToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := StoreAuthenticator{Store: tc.store}
			if _, err := a.AuthenticateMachine(t.Context(), tc.token); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// Disabled is reported separately because it is the kill switch, and whoever
// just used it is probably watching the machine disconnect.
func TestDisabledMachineIsDistinguishable(t *testing.T) {
	st := storeWith("tok-good", model.Machine{ID: 7, UserID: 42, Enabled: false})
	a := StoreAuthenticator{Store: st}

	_, err := a.AuthenticateMachine(t.Context(), "tok-good")
	if !errors.Is(err, ErrMachineDisabled) {
		t.Fatalf("error = %v, want ErrMachineDisabled", err)
	}
	// And it must not be confusable with an unknown token, which is the case
	// that deliberately says nothing.
	if errors.Is(err, ErrUnknownToken) {
		t.Error("a disabled machine is reported as an unknown token")
	}
}

// The lookup already matched on the hash; this confirms it in constant time
// rather than trusting the query alone. A stored row whose hash does not
// actually verify must not authenticate.
func TestAuthenticateMachineRechecksTheHash(t *testing.T) {
	st := &fakeStore{byHash: map[string]model.Machine{
		HashSecret("tok-good"): {ID: 7, Enabled: true, TokenHash: "not-the-right-hash"},
	}}
	a := StoreAuthenticator{Store: st}

	if _, err := a.AuthenticateMachine(t.Context(), "tok-good"); !errors.Is(err, ErrUnknownToken) {
		t.Errorf("error = %v, want ErrUnknownToken", err)
	}
}

func TestTouchAndReleasePassThroughToTheStore(t *testing.T) {
	st := storeWith("tok", model.Machine{ID: 7, Enabled: true})
	a := StoreAuthenticator{Store: st}

	if err := a.Touch(t.Context(), 7, time.Now(), "pod-a"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if len(st.touched) != 1 || st.touched[0].instanceID != "pod-a" {
		t.Errorf("touched = %+v", st.touched)
	}

	if err := a.Release(t.Context(), 7, "pod-a"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(st.released) != 1 || st.released[0].instanceID != "pod-a" {
		t.Errorf("released = %+v", st.released)
	}

	st.touchErr = errors.New("db down")
	if err := a.Touch(t.Context(), 7, time.Now(), "pod-a"); err == nil {
		t.Error("Touch swallowed a store failure")
	}
	st.releaseErr = errors.New("db down")
	if err := a.Release(t.Context(), 7, "pod-a"); err == nil {
		t.Error("Release swallowed a store failure")
	}
}

// Conformance, so the socket can be driven by the real store.
var _ Authenticator = StoreAuthenticator{}
