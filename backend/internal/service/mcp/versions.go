// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import "slices"

// The protocol revisions this server speaks: the two latest a session can be
// opened with, and 2026-07-28, which has no session and so is offered only
// by a stateless transport.
const (
	ProtocolVersion20260728 = "2026-07-28"
	ProtocolVersion20251125 = "2025-11-25"
	ProtocolVersion20250618 = "2025-06-18"
)

// supportedProtocolVersions are every revision spoken, newest first.
var supportedProtocolVersions = []string{
	ProtocolVersion20260728,
	ProtocolVersion20251125,
	ProtocolVersion20250618,
}

// SupportedProtocolVersions are every revision this server speaks, newest
// first.
func SupportedProtocolVersions() []string { return slices.Clone(supportedProtocolVersions) }

// Error codes the protocol adds to JSON-RPC's.
const (
	CodeHeaderMismatch                    = -32020
	CodeMissingRequiredClientCapabilities = -32021
	CodeUnsupportedProtocolVersion        = -32022
)

// Keys of the _meta objects 2026-07-28 puts its per-request identity in.
const (
	MetaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaKeyServerInfo         = "io.modelcontextprotocol/serverInfo"
	MetaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaKeyLogLevel           = "io.modelcontextprotocol/logLevel"
	MetaKeySubscriptionID     = "io.modelcontextprotocol/subscriptionId"
)

// negotiatedVersion is the revision a handshake settles on: the client's,
// when the server speaks it, and otherwise the newest the server offers to
// a handshake. 2026-07-28 has no handshake, so it is never the answer.
func negotiatedVersion(client string, supported []string) string {
	if slices.Contains(supported, client) && client < ProtocolVersion20260728 {
		return client
	}
	for _, v := range supported {
		if v < ProtocolVersion20260728 {
			return v
		}
	}
	return ProtocolVersion20251125
}

// transportVersions are the revisions a transport can serve: a stateful one
// cannot serve 2026-07-28, which has no session.
func transportVersions(supported []string, stateless bool) []string {
	return slices.DeleteFunc(slices.Clone(supported), func(v string) bool {
		return !stateless && v >= ProtocolVersion20260728
	})
}
