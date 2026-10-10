// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

// AcpRegistryAgent is one agent in the official ACP registry, the catalogue
// acp-gateway resolves an agent id against.
type AcpRegistryAgent struct {
	ID          string
	Name        string
	Version     string
	Description string
	// Runtimes are the ways the registry can run it (npx, uvx, binary), sorted.
	Runtimes []string
}
