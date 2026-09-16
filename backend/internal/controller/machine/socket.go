// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Authenticator turns a presented machine token into the machine it belongs
// to, or refuses.
//
// An interface so the socket's rules can be tested without a database.
type Authenticator interface {
	// AuthenticateMachine resolves a bearer token. It returns ErrUnknownToken
	// for a token nobody holds and ErrMachineDisabled for a machine that has
	// been turned off — the caller tells the daemon apart only in the second
	// case, because "your machine was disabled" is actionable and "no such
	// token" must not confirm anything to whoever presented it.
	AuthenticateMachine(ctx context.Context, token string) (Identity, error)
	// Touch records a heartbeat and which instance holds the socket.
	Touch(ctx context.Context, machineID int64, at time.Time, instanceID string) error
	// Release clears the pairing, but only if this instance still holds it.
	Release(ctx context.Context, machineID int64, instanceID string) error
}

// Identity is the machine behind a token.
type Identity struct {
	MachineID int64
	UserID    int64
}

// Errors from authenticating a socket.
var (
	ErrUnknownToken    = errors.New("machine: unknown token")
	ErrMachineDisabled = errors.New("machine: disabled")
	ErrNoToken         = errors.New("machine: no token presented")
	ErrHeaderMismatch  = errors.New("machine: header does not match the token")
)

// BearerToken pulls the token out of an Authorization header.
func BearerToken(header string) (string, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", ErrNoToken
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", ErrNoToken
	}
	return token, nil
}

// CheckIdentityHeaders compares the self-declared headers against the identity
// the token actually resolved to.
//
// Identity comes from the token and never from a header. The headers are there
// for routing and correlation, and stating them a second time turns a
// redundancy into a detection signal: there is no legitimate reason for a
// mismatch, so one is worth refusing and recording rather than shrugging at.
//
// Empty headers are accepted. An older daemon that predates them is not an
// attacker, and refusing it would make adding a header a breaking change.
func CheckIdentityHeaders(id Identity, machineHeader, userHeader string, decode func(string) int64) error {
	if machineHeader != "" && decode(machineHeader) != id.MachineID {
		return fmt.Errorf("%w: machine id", ErrHeaderMismatch)
	}
	if userHeader != "" && decode(userHeader) != id.UserID {
		return fmt.Errorf("%w: user id", ErrHeaderMismatch)
	}
	return nil
}

// HeartbeatInterval is how often a daemon is expected to speak.
//
// Well under OnlineThreshold, so a machine has to miss several beats before it
// is called offline.
const HeartbeatInterval = 30 * time.Second

// Session is one daemon's connection, from the backend's side.
//
// It owns the socket for the life of the connection: the registry holds it,
// the reader loop drives it, and closing it is what revocation does.
type Session struct {
	Identity Identity
	conn     Conn

	registry *Registry
	auth     Authenticator

	closeOnce sync.Once
}

// NewSession registers a freshly authenticated daemon.
//
// Any connection it displaces is closed **after** the registry lock is
// released, which is why Add hands it back rather than closing it itself.
func NewSession(ctx context.Context, r *Registry, auth Authenticator, id Identity, c Conn) (*Session, error) {
	s := &Session{Identity: id, conn: c, registry: r, auth: auth}

	if displaced := r.Add(id.MachineID, c); displaced != nil {
		// The daemon reconnected while we still held a socket we thought was
		// live. The old one is almost always a dead TCP connection nobody has
		// noticed; closing it here is what stops it lingering.
		_ = displaced.Close()
	}

	if err := auth.Touch(ctx, id.MachineID, time.Now(), r.InstanceID()); err != nil {
		// Registered but unrecorded: another instance would not find us. Undo
		// rather than serve a machine nothing can route to.
		r.Remove(id.MachineID, c)
		return nil, fmt.Errorf("machine: record connection: %w", err)
	}
	return s, nil
}

// Heartbeat records that the daemon is still there.
func (s *Session) Heartbeat(ctx context.Context) error {
	return s.auth.Touch(ctx, s.Identity.MachineID, time.Now(), s.registry.InstanceID())
}

// Send writes a frame to this daemon.
func (s *Session) Send(f wire.Frame) error { return s.conn.Send(f) }

// Close ends the connection and clears the pairing.
//
// Idempotent, because it is called from the reader loop ending, from a
// deferred cleanup, and from revocation — all three can happen at once.
//
// The pairing is cleared **conditionally**: if the daemon has already
// reconnected elsewhere, another instance owns the pairing now and clearing it
// would unroute a live machine.
func (s *Session) Close(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		// Only if we are still the holder. A reconnect to this same instance
		// will have replaced the entry, and removing it would strand the new
		// connection.
		removed := s.registry.Remove(s.Identity.MachineID, s.conn)
		closeErr := s.conn.Close()

		var releaseErr error
		if removed {
			releaseErr = s.auth.Release(ctx, s.Identity.MachineID, s.registry.InstanceID())
		}
		err = errors.Join(closeErr, releaseErr)
	})
	return err
}
