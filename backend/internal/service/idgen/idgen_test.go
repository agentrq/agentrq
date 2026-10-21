// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package idgen

import (
	"strings"
	"testing"
)

func TestIdgen(t *testing.T) {
	s, err := New(1)
	if err != nil {
		t.Fatalf("failed to create idgen: %v", err)
	}

	t.Run("NextID", func(t *testing.T) {
		id1 := s.NextID()
		id2 := s.NextID()
		if id1 == 0 {
			t.Error("expected non-zero ID")
		}
		if id1 == id2 {
			t.Error("expected different IDs")
		}
	})
}

// monoflake keeps 10 bits of the node, so 1024 and up would alias a lower
// node's ids; New refuses them instead.
func TestNewRejectsANodeOutOfRange(t *testing.T) {
	for _, node := range []uint16{MaxNodes, 1025, 65535} {
		if _, err := New(node); err == nil || !strings.Contains(err.Error(), "must be 1-1023") {
			t.Errorf("New(%d): %v, want an out-of-range error", node, err)
		}
	}
	if _, err := New(MaxNodes - 1); err != nil {
		t.Errorf("New(%d): %v, want no error", MaxNodes-1, err)
	}
}
