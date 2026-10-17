// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package agentlink is what a workspace and a connected agent say to each
// other, whatever the protocol: the Link a native connection implements, and
// what a session reports (models, reasoning level, commands, concurrency,
// identity, telemetry, permission requests, questions).
//
// It imports no MCP package, so native ACP can use it without depending on the
// MCP layer. PermissionRequest's JSON tags are Claude Code's channel wire
// format, which the MCP side decodes.
package agentlink
