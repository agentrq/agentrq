// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "time"

type (
	// GitHubInstallation is the server's GitHub App installed on a GitHub
	// account the person reaches.
	GitHubInstallation struct {
		InstallationID int64
		Account        string
		AccountType    string
		CreatedAt      time.Time
	}

	GetGitHubConnectionRequest struct {
		UserID string
	}

	// GitHubConnectionResponse says whether the server has a GitHub App, and
	// if so where to install it and the installations the account has.
	GitHubConnectionResponse struct {
		Enabled       bool
		InstallURL    string
		Installations []GitHubInstallation
	}

	// ConnectGitHubRequest is GitHub's return from an install: the OAuth code
	// of the person who installed, and the state the server gave them.
	ConnectGitHubRequest struct {
		UserID string
		Code   string
		State  string
	}

	DisconnectGitHubRequest struct {
		UserID         string
		InstallationID int64
	}
)
