// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// SiteShare is a website shared from the Chrome extension into a workspace,
// with the WebMCP tools its page last registered. One origin is shared into
// at most one workspace per account.
type SiteShare struct {
	ID          int64  `gorm:"primaryKey;autoIncrement:false"`
	UserID      int64  `gorm:"not null;uniqueIndex:idx_site_shares_user_origin"`
	WorkspaceID int64  `gorm:"not null;index"`
	Origin      string `gorm:"type:varchar(255);not null;uniqueIndex:idx_site_shares_user_origin"`
	BrowserID   string `gorm:"type:varchar(64);not null"`
	InstanceID  string `gorm:"type:varchar(128)"`
	LastURL     string `gorm:"type:text"`
	Tools       string `gorm:"type:text;not null;default:'[]'"` // JSON []sitetools.Tool
	AlwaysAllow string `gorm:"type:text;not null;default:'[]'"` // JSON []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
