// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// MachineController is enrolment: handing out a code, and trading it for a
// machine identity.
type MachineController interface {
	CreateEnrolmentCode(ctx context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error)
	EnrolMachine(ctx context.Context, req entity.EnrolMachineRequest) (*entity.EnrolMachineResponse, error)
}

// ErrEnrolmentRejected covers every reason an enrolment did not happen.
//
// Deliberately one error. "Unknown code", "expired code" and "already used"
// are different facts, and telling them apart lets someone with a stolen code
// learn whether it was ever real. The person who just typed it does not need
// the distinction either: in all three cases they fetch a new code.
var ErrEnrolmentRejected = errors.New("enrolment rejected")

// CreateEnrolmentCode issues a short-lived, single-use code.
//
// Returned in plain text exactly once, because only its hash is stored. That is
// the same reason a password reset link cannot be shown twice.
func (c *controller) CreateEnrolmentCode(ctx context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	code, err := machinerules.NewEnrolmentCode()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	stored := model.EnrolmentCode{
		ID:        c.idgen.NextID(),
		CreatedAt: now,
		UserID:    uid,
		CodeHash:  machinerules.HashSecret(code),
		ExpiresAt: now.Add(machinerules.CodeTTL),
	}
	if _, err := c.repository.CreateEnrolmentCode(ctx, stored); err != nil {
		return nil, err
	}

	return &entity.CreateEnrolmentCodeResponse{Code: code, ExpiresAt: stored.ExpiresAt}, nil
}

// EnrolMachine trades a code for a machine identity.
//
// The caller is not authenticated: the code decides whose machine this becomes.
// That is what makes enrolment a local act — somebody has to have read the code
// off the control panel and typed it on the machine.
func (c *controller) EnrolMachine(ctx context.Context, req entity.EnrolMachineRequest) (*entity.EnrolMachineResponse, error) {
	normalised := machinerules.NormalizeCode(req.Code)
	if normalised == "" {
		return nil, ErrEnrolmentRejected
	}

	stored, err := c.repository.GetEnrolmentCode(ctx, machinerules.HashSecret(normalised))
	if err != nil {
		// Including "not found". The caller learns only that it was rejected.
		return nil, ErrEnrolmentRejected
	}
	if stored.UsedAt != nil || time.Now().After(stored.ExpiresAt) {
		return nil, ErrEnrolmentRejected
	}

	token, tokenHash, err := machinerules.NewMachineToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	m := model.Machine{
		ID:        c.idgen.NextID(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    stored.UserID,
		Name:      firstNonEmpty(trim(req.Name, 128), trim(req.Hostname, 128), "machine"),
		Hostname:  trim(req.Hostname, 255),
		OS:        trim(req.OS, 32),
		Arch:      trim(req.Arch, 32),
		Version:   trim(req.Version, 64),
		TokenHash: tokenHash,
		Enabled:   true,
	}

	// The machine is created before the code is consumed, so that a failure
	// between the two leaves an unused code and an orphan machine row that
	// nothing can authenticate as — rather than a spent code and no machine,
	// which would strand the person with nothing to retry.
	created, err := c.repository.CreateMachine(ctx, m)
	if err != nil {
		return nil, err
	}

	// Single-use is enforced here, in the database, not by the check above.
	// Two requests arriving together would both pass that check; only one can
	// win this conditional update.
	won, err := c.repository.ConsumeEnrolmentCode(ctx, stored.ID, created.ID, now)
	if err != nil {
		return nil, err
	}
	if !won {
		// Somebody else used the code first. Undo the machine we just made,
		// so a losing race does not leave a machine nobody asked for. If the
		// cleanup fails the row is harmless — its token was never returned to
		// anyone and cannot be presented.
		_ = c.repository.DeleteMachine(ctx, created.ID, created.UserID)
		return nil, ErrEnrolmentRejected
	}

	return &entity.EnrolMachineResponse{
		MachineID:    monoflake.ID(created.ID).String(),
		MachineToken: token,
	}, nil
}

// trim bounds a reported string to what its column holds.
//
// The daemon sends these, and a daemon is not necessarily one of ours. A
// hostname of a megabyte should be cut here rather than rejected by the
// database as a server error.
func trim(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
