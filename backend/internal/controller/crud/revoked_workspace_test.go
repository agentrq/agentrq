// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
)

// The revoked ids come straight from the repository, for the time asked.
func TestListRevokedWorkspaceIDs(t *testing.T) {
	e := newTestController(t)
	since := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	e.repo.EXPECT().ListRevokedWorkspaceIDs(gomock.Any(), since).Return([]int64{7, 8}, nil)

	got, err := e.controller.ListRevokedWorkspaceIDs(context.Background(), since)
	if err != nil || !slices.Equal(got, []int64{7, 8}) {
		t.Errorf("got %v, %v", got, err)
	}
}
