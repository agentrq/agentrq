// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

type (
	// AdminStats is the operator's view of the server: in total and per user,
	// busiest first. MCP connections are the streams open on the instance that
	// answered; machines are those online anywhere.
	AdminStats struct {
		MCPConnections int              `json:"mcpConnections"`
		Machines       int              `json:"machines"`
		Users          []UserAdminStats `json:"users"`
	}

	UserAdminStats struct {
		UserID         string `json:"userId"`
		MCPConnections int    `json:"mcpConnections"`
		Machines       int    `json:"machines"`
	}
)
