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

// HookRegistration is one use of a hook in an account, with its own URL.
type HookRegistration struct {
	ID       int64
	HookID   int64
	HookName string
	// HookAvailable is false once its hook is deleted, turned off, or an
	// account's own while user hooks are not allowed: requests are refused.
	HookAvailable bool
	Name          string
	Enabled       bool
	// Inputs holds the values of the hook's inputs that are not secret;
	// SecretsSet names the secret ones that have a value, never shown.
	Inputs     map[string]any
	SecretsSet []string
	// Routes maps the hook's event names to the account's event ids.
	Routes map[string]int64
	// URL is the registration's address without its token; URLWithToken is
	// the whole of it, given only when the token is made.
	URL          string
	URLWithToken string
	// AllowAllCommandsEvents are the routed hook events whose event has a
	// trigger that runs every command without asking.
	AllowAllCommandsEvents []string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// HookRegistrationRequest is what a person sets on a registration. On an
// update a nil field is left as it is, and a secret input left out of Inputs
// keeps its value.
type HookRegistrationRequest struct {
	HookID  int64
	Name    *string
	Enabled *bool
	Inputs  map[string]any
	// Routes maps the hook's event names to the account's event ids.
	Routes map[string]int64
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
