// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// Store is the slice of the repository this package needs.
//
// Narrow on purpose: it is what lets the socket's rules be tested against a
// fake rather than a database, and it documents exactly how much of the
// persistence layer a daemon connection can reach.
type Store interface {
	GetMachineByTokenHash(ctx context.Context, tokenHash string) (model.Machine, error)
	TouchMachine(ctx context.Context, id int64, at time.Time, instanceID string) error
	ReleaseMachine(ctx context.Context, id int64, instanceID string) error
}

// StoreAuthenticator resolves machine tokens against the database.
type StoreAuthenticator struct {
	Store Store
}

// AuthenticateMachine turns a presented token into the machine that holds it.
//
// The lookup is by hash because that is all that is stored — the token itself
// was returned once, at enrolment, and never kept.
func (a StoreAuthenticator) AuthenticateMachine(ctx context.Context, token string) (Identity, error) {
	if token == "" {
		return Identity{}, ErrNoToken
	}

	m, err := a.Store.GetMachineByTokenHash(ctx, HashSecret(token))
	if err != nil {
		// Including "not found". A token nobody holds and a database that is
		// down are both answered the same way here; the caller says only that
		// the token was not accepted.
		return Identity{}, ErrUnknownToken
	}

	// Belt and braces against a hash collision or a malformed stored row: the
	// lookup already matched on the hash, and this confirms it in constant
	// time rather than trusting the query alone.
	if !SecretMatches(m.TokenHash, token) {
		return Identity{}, ErrUnknownToken
	}

	// Disabled is reported separately, and that difference is deliberate. It
	// is the kill switch, and somebody who has just disabled a machine and is
	// watching it disconnect deserves to see why. An unknown token gets no
	// such explanation, because it would confirm something.
	if !m.Enabled {
		return Identity{}, fmt.Errorf("%w: machine %d", ErrMachineDisabled, m.ID)
	}

	return Identity{MachineID: m.ID, UserID: m.UserID}, nil
}

// Touch records a heartbeat and which instance holds the socket.
func (a StoreAuthenticator) Touch(ctx context.Context, machineID int64, at time.Time, instanceID string) error {
	return a.Store.TouchMachine(ctx, machineID, at, instanceID)
}

// Release clears the pairing, conditional on this instance still holding it.
func (a StoreAuthenticator) Release(ctx context.Context, machineID int64, instanceID string) error {
	return a.Store.ReleaseMachine(ctx, machineID, instanceID)
}
