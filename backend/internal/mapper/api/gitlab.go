// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v3"
)

func FromHTTPRequestToConnectGitLabRequestEntity(c fiber.Ctx) *entity.ConnectGitLabRequest {
	var rq view.ConnectGitLabRequest
	if err := c.Bind().Body(&rq); err != nil || rq.Code == "" || rq.State == "" {
		return nil
	}
	return &entity.ConnectGitLabRequest{Code: rq.Code, State: rq.State}
}

func FromGitLabConnectionResponseEntityToHTTPResponse(rs *entity.GitLabConnectionResponse) []byte {
	payload, _ := json.Marshal(view.GitLabConnectionResponse{Host: rs.Host, Enabled: rs.Enabled, AuthorizeURL: rs.AuthorizeURL, Username: rs.Username})
	return payload
}
