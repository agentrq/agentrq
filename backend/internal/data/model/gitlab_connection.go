// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// GitLabConnection is an account's connection to the server's GitLab
// application, read by imports of its private projects. Both tokens are
// encrypted with the server's token key; each nonce is its own.
type GitLabConnection struct {
	ID           int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	UserID       int64  `gorm:"uniqueIndex:uk_gitlab_connections_user_id"`
	Host         string `gorm:"type:varchar(255)"`
	Username     string `gorm:"type:varchar(255)"`
	AccessToken  string `gorm:"type:text"`
	AccessNonce  string `gorm:"type:varchar(32)"`
	RefreshToken string `gorm:"type:text"`
	RefreshNonce string `gorm:"type:varchar(32)"`
	ExpiresAt    time.Time
}

// TableName keeps GitLab one word; gorm would make it git_lab.
func (GitLabConnection) TableName() string { return "gitlab_connections" }
