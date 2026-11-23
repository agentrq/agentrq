// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package kv

import (
	"context"
	"testing"
)

func TestService(t *testing.T) {
	ctx := context.Background()
	s := New()

	if v, ok := s.Get(ctx, "a"); ok || v != "" {
		t.Errorf("Get(missing) = %q, %v; want nothing", v, ok)
	}
	s.Set(ctx, "a", "1")
	if v, ok := s.Get(ctx, "a"); !ok || v != "1" {
		t.Errorf("Get = %q, %v; want 1", v, ok)
	}
	s.Set(ctx, "a", "")
	if v, ok := s.Get(ctx, "a"); !ok || v != "" {
		t.Errorf("Get = %q, %v; want the empty value, stored", v, ok)
	}
}
