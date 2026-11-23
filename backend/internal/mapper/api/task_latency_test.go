// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import "testing"

func TestFromGetTaskLatencyStatsResponseEntityToHTTPResponse_Nil(t *testing.T) {
	if FromGetTaskLatencyStatsResponseEntityToHTTPResponse(nil) != nil {
		t.Fatal("nothing to render renders nothing")
	}
}
