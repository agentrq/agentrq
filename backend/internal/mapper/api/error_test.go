// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// A refused launch answers with its own sentence, at the status the web
// launch form has always had for it.
func TestFromErrorToHTTPResponse_ARefusedLaunch(t *testing.T) {
	cases := []struct {
		kind error
		want int
	}{
		{entity.ErrLaunchInvalid, http.StatusUnprocessableEntity},
		{entity.ErrLaunchBusy, http.StatusConflict},
		{entity.ErrLaunchNoFolder, http.StatusPreconditionRequired},
		{entity.ErrLaunchUnavailable, http.StatusServiceUnavailable},
		{entity.ErrLaunchUnreachable, http.StatusBadGateway},
	}
	for _, tc := range cases {
		body, status := FromErrorToHTTPResponse(entity.NewLaunchError(tc.kind, "said to the person"))
		var e httpError
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatal(err)
		}
		if status != tc.want || e.Error.Code != tc.want || e.Error.Message != "said to the person" {
			t.Errorf("%v: status %d, body %s; want %d", tc.kind, status, body, tc.want)
		}
	}
}

// An export past the daily cap is 429 with the cap in the sentence.
func TestFromErrorToHTTPResponse_AnExportPastTheCap(t *testing.T) {
	body, status := FromErrorToHTTPResponse(&entity.AccountExportThrottled{Limit: 2})
	var e httpError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusTooManyRequests || e.Error.Code != http.StatusTooManyRequests || e.Error.Message != "Your data can be exported 2 times a day" {
		t.Errorf("status %d, body %s; want 429 with the cap", status, body)
	}
}
