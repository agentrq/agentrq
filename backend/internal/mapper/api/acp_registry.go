// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// FromAcpRegistryAgentsEntityToHTTPResponse is the registry's agents as the
// API answers them.
func FromAcpRegistryAgentsEntityToHTTPResponse(agents []entity.AcpRegistryAgent) []byte {
	out := view.AcpRegistryAgents{Agents: make([]view.AcpRegistryAgent, len(agents))}
	for i, a := range agents {
		out.Agents[i] = view.AcpRegistryAgent{
			ID:          a.ID,
			Name:        a.Name,
			Version:     a.Version,
			Description: a.Description,
			Runtimes:    a.Runtimes,
		}
	}
	payload, _ := json.Marshal(out)
	return payload
}
