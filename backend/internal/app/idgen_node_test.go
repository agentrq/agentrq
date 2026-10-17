// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"net"
	"os"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
)

func TestIdgenNodeFor(t *testing.T) {
	mac, _ := net.ParseMAC("52:54:00:12:34:56")
	withMAC := func() ([]net.Interface, error) {
		return []net.Interface{{Index: 2, Flags: net.FlagUp, HardwareAddr: mac}}, nil
	}
	derived, err := idgen.NodeFromMAC(withMAC)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a configured node wins", func(t *testing.T) {
		if got := idgenNodeFor(7, withMAC); got != 7 {
			t.Errorf("node = %d, want 7", got)
		}
	})
	t.Run("unset is derived from the MAC address", func(t *testing.T) {
		if got := idgenNodeFor(0, withMAC); got != derived {
			t.Errorf("node = %d, want %d", got, derived)
		}
	})
	t.Run("no MAC address falls back to 1", func(t *testing.T) {
		noInterfaces := func() ([]net.Interface, error) { return nil, errors.New("netlink: permission denied") }
		if got := idgenNodeFor(0, noInterfaces); got != 1 {
			t.Errorf("node = %d, want 1", got)
		}
	})
}

// The shipped config must leave the node unset when MONOFLAKE_NODE is, or
// idgenNodeFor never derives one.
func TestBaseConfigIdgenNode(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Chdir("../../cmd/server")

	load := func(t *testing.T) uint16 {
		t.Helper()
		svc, err := config.New()
		if err != nil {
			t.Fatal(err)
		}
		var cfg Config
		if err := svc.Populate("idgen", &cfg.Idgen); err != nil {
			t.Fatalf("config idgen: %v", err)
		}
		return cfg.Idgen.Node
	}

	t.Run("unset", func(t *testing.T) {
		t.Setenv("MONOFLAKE_NODE", "")
		os.Unsetenv("MONOFLAKE_NODE")
		if got := load(t); got != 0 {
			t.Errorf("node = %d, want 0 (unset)", got)
		}
	})
	t.Run("set", func(t *testing.T) {
		t.Setenv("MONOFLAKE_NODE", "42")
		if got := load(t); got != 42 {
			t.Errorf("node = %d, want 42", got)
		}
	})
}
