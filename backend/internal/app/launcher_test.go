// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

// Native ACP is offered only for v1, and only while its switch is on.
func TestNewLauncher_OffersNativeACPOnlyWhereItIsOn(t *testing.T) {
	for _, on := range []bool{false, true} {
		var cfg Config
		cfg.ACP.V1.Enabled = on
		l := newLauncher(cfg, true, nil, nil, nil, nil)
		if got := l.ACPEnabled("1"); got != on {
			t.Errorf("with acp.v1.enabled %v, v1 offered = %v", on, got)
		}
		if l.ACPEnabled("2") {
			t.Errorf("with acp.v1.enabled %v, v2 was offered, but this server has no v2", on)
		}
	}
}
