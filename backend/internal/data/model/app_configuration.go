// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

// AppConfiguration is one block of the server's config, as the operator
// last saved it: Val is the block's JSON, encrypted with AES-256-GCM under
// auth.workspaceTokenKey, hex encoded; Nonce is its nonce, hex encoded.
type AppConfiguration struct {
	ConfigKey string `gorm:"type:varchar(64);primaryKey"`
	Val       string `gorm:"type:text;not null"`
	Nonce     string `gorm:"type:varchar(64);not null"`
}

// TableName names the table with a leading underscore, apart from the app's
// own data.
func (AppConfiguration) TableName() string { return "_app_configurations" }
