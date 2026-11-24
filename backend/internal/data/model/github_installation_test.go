// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "testing"

func TestGitHubInstallationTableName(t *testing.T) {
	if got := (GitHubInstallation{}).TableName(); got != "github_installations" {
		t.Errorf("table = %q", got)
	}
}
