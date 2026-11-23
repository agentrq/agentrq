// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import "encoding/json"

// Frame types. The browser sends announce, withdraw and result; the backend
// sends call, shares and refused.
const (
	FrameAnnounce = "announce"
	FrameWithdraw = "withdraw"
	FrameResult   = "result"
	FrameCall     = "call"
	FrameShares   = "shares"
	FrameRefused  = "refused"
)

// Frame is every message on the browser socket; Type selects the fields.
type Frame struct {
	Type        string          `json:"type"`
	Origin      string          `json:"origin,omitempty"`
	WorkspaceID string          `json:"workspaceId,omitempty"` // base62
	LastURL     string          `json:"lastUrl,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	CallID      string          `json:"callId,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	Text        string          `json:"text,omitempty"`  // result
	Error       string          `json:"error,omitempty"` // result or refused
	Shares      []ShareState    `json:"shares,omitempty"`
}

// ShareState is one share as the backend holds it, sent so the extension can
// drop shares it still remembers but the backend no longer has.
type ShareState struct {
	Origin      string   `json:"origin"`
	WorkspaceID string   `json:"workspaceId"`
	AlwaysAllow []string `json:"alwaysAllow"`
}
