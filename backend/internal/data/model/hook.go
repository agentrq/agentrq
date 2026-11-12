// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Hook is a WebAssembly module that validates a request from an outside
// service and writes the events it stands for. UserID 0 is a built-in hook,
// registered by the root user for every account; any other is the account's
// own. The module is in storage under BlobKey, never in the database.
type Hook struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	UserID    int64  `gorm:"not null;uniqueIndex:uk_hooks_user_id_name,priority:1"`
	Name      string `gorm:"type:varchar(64);not null;uniqueIndex:uk_hooks_user_id_name,priority:2"`
	Version   string `gorm:"type:varchar(32);not null"`
	BlobKey   string `gorm:"type:varchar(255);not null"`
	// SHA256 is the module's hex digest; compiled modules are cached by it.
	SHA256     string `gorm:"type:char(64);not null"`
	SizeBytes  int64  `gorm:"not null"`
	ABIVersion int16  `gorm:"not null"`
	// Manifest is the module's own answer to "manifest", as JSON: its
	// description, inputs and events.
	Manifest string `gorm:"type:text;not null"`
	// Enabled has no default tag: GORM leaves a false out of the INSERT when
	// the column has one, and the default would win.
	Enabled bool `gorm:"not null"`
}

// HookRegistration is one use of a hook in an account, with its own id, and
// so its own URL, inputs and routes. A hook may be registered any number of
// times.
type HookRegistration struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	UserID    int64  `gorm:"not null;index:idx_hook_registrations_user_id"`
	HookID    int64  `gorm:"not null;index:idx_hook_registrations_hook_id"`
	Name      string `gorm:"type:varchar(255);not null"`
	Enabled   bool   `gorm:"not null"`
	// TokenHash is the bcrypt hash of the agentrq_hook_token the URL carries:
	// 8 base36 characters, rotatable, shown once. A registration is reached
	// by its id, so it is not indexed.
	TokenHash string `gorm:"type:varchar(60);not null"`
	// Inputs holds the values for the inputs the hook declares, encrypted.
	Inputs string `gorm:"type:text;not null"`
	// Routes maps the hook's event names to the account's event ids, as JSON.
	Routes string `gorm:"type:text;not null"`
}

// A delivery's status. Appended, never renumbered: the value is stored.
const (
	HookDeliveryQueued     int16 = 1
	HookDeliveryProcessing int16 = 2
	// HookDeliveryRouted is committed but not yet published.
	HookDeliveryRouted    int16 = 3
	HookDeliveryPublished int16 = 4
	HookDeliveryRejected  int16 = 5
	HookDeliveryIgnored   int16 = 6
	HookDeliveryDuplicate int16 = 7
	HookDeliveryFailed    int16 = 8
)

// HookDelivery is one request to a registration's URL: the queue's row while
// it waits, and the log's afterwards.
type HookDelivery struct {
	// A registration's log is read newest first, by id.
	ID             int64     `gorm:"primaryKey;autoIncrement:false;index:idx_hook_deliveries_registration_id_id,priority:2"`
	RegistrationID int64     `gorm:"not null;index:idx_hook_deliveries_registration_id_id,priority:1"`
	UserID         int64     `gorm:"not null"`
	ReceivedAt     time.Time `gorm:"not null;index:idx_hook_deliveries_received_at"`
	Status         int16     `gorm:"not null;index:idx_hook_deliveries_status_next_attempt_at,priority:1"`
	Attempts       int16     `gorm:"not null"`
	// NextAttemptAt is when a worker may claim the row: a retry's time while
	// queued, the lease's end while processing, so a crashed worker's row
	// is claimed again by the same query.
	NextAttemptAt time.Time `gorm:"not null;index:idx_hook_deliveries_status_next_attempt_at,priority:2"`
	// Request is the raw request, kept only until it is processed, or until
	// the retention passes for a failed one.
	Request    []byte
	BodySHA256 string `gorm:"type:char(64);not null"`
	// EventNames lists the hook's events the delivery routed, as JSON.
	EventNames string `gorm:"type:text;not null"`
	// Pending holds the events committed but not yet published, as JSON
	// HookPendingEvents, so a crash between the two cannot lose them.
	// Cleared once they are published.
	Pending    []byte
	Error      string `gorm:"type:text;not null"`
	DurationMs int32  `gorm:"not null"`
}

// HookDeliveryKey marks a key a hook gave a delivery, so a provider's retry
// of it is published once. It is a table of its own because the key is
// known only after the hook runs, not when the request is saved.
type HookDeliveryKey struct {
	RegistrationID int64     `gorm:"primaryKey;autoIncrement:false"`
	DedupKey       string    `gorm:"type:varchar(255);primaryKey"`
	CreatedAt      time.Time `gorm:"not null;index:idx_hook_delivery_keys_created_at"`
}

// HookPendingEvent is one event a delivery routed to one of the account's
// events, kept until it is published.
type HookPendingEvent struct {
	// HookEvent is the name the hook gave it; EventID the event it fires.
	HookEvent string           `json:"hookEvent"`
	EventID   int64            `json:"eventId"`
	DedupKey  string           `json:"dedupKey,omitempty"`
	Payload   string           `json:"payload"`
	FAQ       []HookPendingFAQ `json:"faq,omitempty"`
}

// HookPendingFAQ is a question about an event, answered up front.
type HookPendingFAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}
