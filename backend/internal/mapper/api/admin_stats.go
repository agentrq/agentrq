// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"cmp"
	"encoding/json"
	"slices"

	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// FromAdminStatsToHTTPResponse renders each user's MCP connections and online
// machines, keyed by user id, as one row per user with either. Busiest first
// by connections, then machines, then user id, so two reads of the same state
// agree.
func FromAdminStatsToHTTPResponse(mcpConnections, machines map[string]int) []byte {
	rows := make(map[string]*view.UserAdminStats, len(mcpConnections)+len(machines))
	row := func(userID string) *view.UserAdminStats {
		if rows[userID] == nil {
			rows[userID] = &view.UserAdminStats{UserID: userID}
		}
		return rows[userID]
	}
	out := view.AdminStats{Users: make([]view.UserAdminStats, 0, len(rows))}
	for userID, n := range mcpConnections {
		row(userID).MCPConnections = n
		out.MCPConnections += n
	}
	for userID, n := range machines {
		row(userID).Machines = n
		out.Machines += n
	}
	for _, r := range rows {
		out.Users = append(out.Users, *r)
	}
	slices.SortFunc(out.Users, func(a, b view.UserAdminStats) int {
		return cmp.Or(
			cmp.Compare(b.MCPConnections, a.MCPConnections),
			cmp.Compare(b.Machines, a.Machines),
			cmp.Compare(a.UserID, b.UserID),
		)
	})
	data, _ := json.Marshal(out)
	return data
}
