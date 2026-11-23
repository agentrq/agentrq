// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

// SentryConfig is where the web app reports its errors: the DSN its events
// name, which it posts to the server's /sentry route, and the environment.
type SentryConfig struct {
	DSN         string `json:"dsn"`
	Environment string `json:"environment"`
}
