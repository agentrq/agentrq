// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"
	"strconv"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v3"
)

func FromHTTPRequestToConnectGitHubRequestEntity(c fiber.Ctx) *entity.ConnectGitHubRequest {
	var rq view.ConnectGitHubRequest
	if err := c.Bind().Body(&rq); err != nil || rq.Code == "" || rq.State == "" {
		return nil
	}
	return &entity.ConnectGitHubRequest{Code: rq.Code, State: rq.State}
}

// FromHTTPRequestToDisconnectGitHubRequestEntity reads GitHub's installation
// number from the path: GitHub's, so not a base-62 id.
func FromHTTPRequestToDisconnectGitHubRequestEntity(c fiber.Ctx) *entity.DisconnectGitHubRequest {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	return &entity.DisconnectGitHubRequest{InstallationID: id}
}

func FromGitHubConnectionResponseEntityToHTTPResponse(rs *entity.GitHubConnectionResponse) []byte {
	out := view.GitHubConnectionResponse{
		Enabled: rs.Enabled, InstallURL: rs.InstallURL,
		Installations: make([]view.GitHubInstallation, len(rs.Installations)),
	}
	for i, in := range rs.Installations {
		out.Installations[i] = view.GitHubInstallation{
			InstallationID: in.InstallationID, Account: in.Account, AccountType: in.AccountType, CreatedAt: in.CreatedAt,
		}
	}
	payload, _ := json.Marshal(out)
	return payload
}
