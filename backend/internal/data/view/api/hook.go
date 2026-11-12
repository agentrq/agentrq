// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "time"

// Hook is a WebAssembly hook as its manifest describes it.
type Hook struct {
	ID          string      `json:"id"`
	BuiltIn     bool        `json:"builtIn"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	SHA256      string      `json:"sha256"`
	SizeBytes   int64       `json:"sizeBytes"`
	ABIVersion  int         `json:"abiVersion"`
	Inputs      []HookInput `json:"inputs"`
	Events      []HookEvent `json:"events"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// HookInput is a value each registration of a hook provides.
type HookInput struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type"`
	Items    string `json:"items,omitempty"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
}

// HookEvent is one kind of event a hook writes.
type HookEvent struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AvailableHooksResponse is the hooks an account may register, and whether
// it may add its own.
type AvailableHooksResponse struct {
	Hooks          []Hook `json:"hooks"`
	AllowUserHooks bool   `json:"allowUserHooks"`
}

// ListHooksResponse is a list of hooks.
type ListHooksResponse struct {
	Hooks []Hook `json:"hooks"`
}

// SetHookEnabledRequest turns a hook on or off.
type SetHookEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

// HookRegistration is one use of a hook in an account.
type HookRegistration struct {
	ID            string `json:"id"`
	HookID        string `json:"hookId"`
	HookName      string `json:"hookName"`
	HookAvailable bool   `json:"hookAvailable"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	// Inputs holds the values that are not secret; SecretsSet names the
	// secret inputs that have one.
	Inputs     map[string]any    `json:"inputs"`
	SecretsSet []string          `json:"secretsSet"`
	Routes     map[string]string `json:"routes"`
	URL        string            `json:"url"`
	// URLWithToken is given once, when the token is made.
	URLWithToken           string    `json:"urlWithToken,omitempty"`
	AllowAllCommandsEvents []string  `json:"allowAllCommandsEvents"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

// ListHookRegistrationsResponse is an account's registrations.
type ListHookRegistrationsResponse struct {
	Registrations []HookRegistration `json:"registrations"`
}

// HookRegistrationRequest creates or changes a registration; a field left out
// of an update is kept.
type HookRegistrationRequest struct {
	HookID  string            `json:"hookId"`
	Name    *string           `json:"name"`
	Enabled *bool             `json:"enabled"`
	Inputs  map[string]any    `json:"inputs"`
	Routes  map[string]string `json:"routes"`
}

// HookDelivery is one request to a registration's URL, as its log shows it.
type HookDelivery struct {
	ID             string    `json:"id"`
	RegistrationID string    `json:"registrationId"`
	ReceivedAt     time.Time `json:"receivedAt"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	EventNames     []string  `json:"eventNames"`
	Error          string    `json:"error"`
	DurationMs     int       `json:"durationMs"`
	Replayable     bool      `json:"replayable"`
}

// ListHookDeliveriesResponse is a page of a registration's log.
type ListHookDeliveriesResponse struct {
	Deliveries []HookDelivery `json:"deliveries"`
}

// HookReceivedResponse answers a request to a hook URL that was queued.
type HookReceivedResponse struct {
	DeliveryID string `json:"deliveryId"`
}
