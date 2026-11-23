// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
	"github.com/agentrq/agentrq/backend/internal/controller/machine"
)

// The launcher gives agents the addresses the API hands out, so an agent the
// supervisor launches connects to the same host as one launched from the web.
func TestNewLauncher_UsesTheDeploymentsAddresses(t *testing.T) {
	var cfg Config
	cfg.App.BaseURL = "https://agentrq.com"
	cfg.App.Domain = "agentrq.com"
	reg := machine.NewRegistry("test-instance")

	l := newLauncher(cfg, true, nil, nil, reg, nil)

	if l.URLs != (agentlaunch.URLs{Base: "https://agentrq.com", Domain: "agentrq.com", Secure: true}) {
		t.Errorf("urls = %+v", l.URLs)
	}
	if l.Machines != reg {
		t.Errorf("machines = %v", l.Machines)
	}
}

// Native ACP is offered only for v1, the one version this server has.
func TestNewLauncher_OffersNativeACPOnlyForV1(t *testing.T) {
	l := newLauncher(Config{}, true, nil, nil, nil, nil)
	if !l.ACPEnabled("1") {
		t.Error("v1 was not offered")
	}
	if l.ACPEnabled("2") {
		t.Error("v2 was offered, but this server has no v2")
	}
}
