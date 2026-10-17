// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// ConcurrencyReport is what a session reports about how many tasks it runs at
// once, and what its queue is doing.
//
// Pointers, because zero is a meaningful answer for none of these and an absent
// field has to be told apart from a reported one: a limit of 0 is a gateway
// that said nothing rather than one that will run nothing, and `canSet` absent
// means no.
//
// TaskID and SessionID are always empty; nothing reads them. The limit belongs
// to the gateway process, not to any one task or ACP session.
type ConcurrencyReport struct {
	TaskID    string
	SessionID string

	MaxConcurrency *int

	// Active is how many tasks are running and Queued how many are waiting.
	// Both are for display: they are the only way to see that a limit did
	// anything.
	Active int
	Queued int

	// Min and Max are the range the control should offer. Read from the report
	// rather than assumed, because the gateway owns its own ceiling and a
	// number compiled in here would be wrong the first time it moved.
	Min *int
	Max *int

	// CanSet is the gateway saying it will act on SetConcurrency. Absent means
	// no, so a control is never offered that would do nothing.
	CanSet *bool
}

// Limit is the ceiling the payload reports, 0 when it reported none.
func (p ConcurrencyReport) Limit() int {
	if p.MaxConcurrency == nil {
		return 0
	}
	return *p.MaxConcurrency
}

// OffersSet is whether the payload says the limit can be changed. Absent is no.
func (p ConcurrencyReport) OffersSet() bool {
	return p.CanSet != nil && *p.CanSet
}

// ConcurrencySnapshot is what one connected session last reported.
type ConcurrencySnapshot struct {
	// MaxConcurrency is how many tasks the gateway will run at once.
	MaxConcurrency int
	// Active is how many it is running, and may legitimately exceed
	// MaxConcurrency for a while: lowering the limit never interrupts a running
	// task, so the queue simply stops handing out new ones until the active
	// count falls below it. A reader that treated that as an error state would
	// be flagging the feature working correctly.
	Active int
	// Queued is how many are waiting for a slot.
	Queued int
	// Min and Max are the range a control may offer, as the gateway reported
	// it. Max is 0 when the gateway named no ceiling.
	Min int
	Max int
	// CanSet reports whether the session that sent this will act on a
	// set_concurrency notification. See the field on ConcurrencyReport.
	CanSet bool
}

// Reported says whether this snapshot carries a limit worth showing.
//
// A gateway that named no limit has told the interface nothing it can render —
// there is no number for the control to start from — so such a snapshot is
// skipped in favour of one from another session, exactly as an empty model list
// is.
func (s ConcurrencySnapshot) Reported() bool {
	return s.MaxConcurrency > 0
}

// Settable reports whether a change can actually be written back.
//
// The one definition of that rule, because it is asked in three places — the
// predicate the interface reads, the guard on the notification, and the flag on
// the workspace payload — and three copies would eventually disagree.
//
// Reported() is half of it for the same reason ConfigID is half of the models'
// rule: a gateway willing to be told a new limit but never saying what the
// current one is leaves a control with nothing to show and no way to tell
// whether a change landed.
func (s ConcurrencySnapshot) Settable() bool {
	return s.CanSet && s.Reported()
}
