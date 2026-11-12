// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "time"

// Hook is a WebAssembly module that turns a request from an outside service
// into the account's events, as its manifest describes it.
type Hook struct {
	ID          int64
	BuiltIn     bool
	Name        string
	Version     string
	Description string
	SHA256      string
	SizeBytes   int64
	ABIVersion  int
	Inputs      []HookInput
	Events      []HookEvent
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HookInput is a value each registration of a hook provides.
type HookInput struct {
	Name     string
	Label    string
	Type     string
	Items    string
	Secret   bool
	Required bool
}

// HookEvent is one kind of event a hook writes.
type HookEvent struct {
	Name        string
	Description string
}

// HookErrorKind says why a hook was refused.
type HookErrorKind uint8

const (
	// HookInvalid is a module that cannot be a hook.
	HookInvalid HookErrorKind = iota + 1
	// HookConflict is a change the hooks already there forbid: a second hook
	// of a name, an upgrade that breaks a registration, deleting a hook that
	// is registered.
	HookConflict
)

// HookError is a hook refused, with a message for the person.
type HookError struct {
	Kind    HookErrorKind
	Message string
}

func (e *HookError) Error() string { return e.Message }
