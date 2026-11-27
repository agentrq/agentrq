// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

type (
	// GitLabConnectionResponse is the GitLab skills are imported from and,
	// when the server can connect to it, where to connect and who is.
	GitLabConnectionResponse struct {
		Host         string `json:"host"`
		Enabled      bool   `json:"enabled"`
		AuthorizeURL string `json:"authorizeUrl,omitempty"`
		Username     string `json:"username,omitempty"`
	}

	// ConnectGitLabRequest is what GitLab returned to /gitlab/callback.
	ConnectGitLabRequest struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
)
