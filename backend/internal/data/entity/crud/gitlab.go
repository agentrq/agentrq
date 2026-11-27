// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

type (
	GetGitLabConnectionRequest struct {
		UserID string
	}

	// GitLabConnectionResponse is the GitLab skills are imported from and,
	// when the server can connect to it, where to connect and who is.
	GitLabConnectionResponse struct {
		Host         string
		Enabled      bool
		AuthorizeURL string
		// Username is the connected GitLab account, empty when none is.
		Username string
	}

	// ConnectGitLabRequest is GitLab's return from an authorization: the
	// code it granted and the state the server gave.
	ConnectGitLabRequest struct {
		UserID string
		Code   string
		State  string
	}

	DisconnectGitLabRequest struct {
		UserID string
	}
)
