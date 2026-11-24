// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import "time"

type (
	// GitHubInstallation is the server's GitHub App installed on a GitHub
	// account the person reaches. installationId is GitHub's own number.
	GitHubInstallation struct {
		InstallationID int64     `json:"installationId"`
		Account        string    `json:"account"`
		AccountType    string    `json:"accountType"`
		CreatedAt      time.Time `json:"createdAt"`
	}

	// GitHubConnectionResponse says whether private imports are offered, and
	// where to install the app to make one.
	GitHubConnectionResponse struct {
		Enabled       bool                 `json:"enabled"`
		InstallURL    string               `json:"installUrl,omitempty"`
		Installations []GitHubInstallation `json:"installations"`
	}

	// ConnectGitHubRequest is what GitHub's return from an install carries.
	ConnectGitHubRequest struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
)
