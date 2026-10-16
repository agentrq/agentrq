// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

import "time"

// Question is something a native agent asks the human, exactly as the elicit
// tool would: a form to fill in, or a link to visit and confirm.
type Question struct {
	Mode            string         // "form" | "url"
	Message         string         // the question
	RequestedSchema map[string]any // form only
	URL             string         // url only
	Timeout         time.Duration  // <= 0 means an hour; capped at an hour
}

// Answer is what came back.
type Answer struct {
	Action  string         // "accept" | "decline" | "cancel"
	Content map[string]any // form answers when accepted
}
