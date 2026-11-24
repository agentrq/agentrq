// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// GitHubInstallation is an installation of the server's GitHub App that the
// account's person showed, through GitHub, they can reach. Its skills can be
// imported from the account's private repositories.
type GitHubInstallation struct {
	ID             int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt      time.Time
	UserID         int64  `gorm:"uniqueIndex:uk_github_installations_user_id_installation_id,priority:1"`
	InstallationID int64  `gorm:"uniqueIndex:uk_github_installations_user_id_installation_id,priority:2"`
	Account        string `gorm:"type:varchar(100)"`
	AccountType    string `gorm:"type:varchar(32)"`
}

// TableName keeps GitHub one word; gorm would make it git_hub.
func (GitHubInstallation) TableName() string { return "github_installations" }
