// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

func TestFromErrorToHTTPResponse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{"not found, however wrapped", fmt.Errorf("get: %w", base.ErrNotFound), http.StatusNotFound, "not found"},
		{"a fork with forks", entity.NewForkError(entity.ErrHasForks, "it has forks"), http.StatusConflict, "it has forks"},
		{"an unfinished fork", entity.NewForkError(entity.ErrForkUnfinished, "tasks are open"), http.StatusConflict, "tasks are open"},
		{"a fork that cannot be deleted", entity.NewForkError(entity.ErrForkNoDelete, "merge it"), http.StatusConflict, "merge it"},
		{"a fork whose agent runs", entity.NewForkError(entity.ErrForkAgentRunning, "stop it"), http.StatusConflict, "stop it"},
		{"any other fork refusal", entity.NewForkError(errors.New("other"), "never"), http.StatusUnprocessableEntity, "never"},
		{"any other launch refusal", entity.NewLaunchError(errors.New("other"), "no"), http.StatusUnprocessableEntity, "no"},
		{"a locked title", fmt.Errorf("rename: %w", entity.ErrTaskTitleLocked), http.StatusConflict, "rename: " + entity.ErrTaskTitleLocked.Error()},
		{"rate limited", errors.New("rate limit exceeded"), http.StatusTooManyRequests, "rate limit exceeded"},
		// Anything else never leaks its text.
		{"anything else", errors.New("dial tcp 10.0.0.1: refused"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, code := FromErrorToHTTPResponse(tc.err)
			if code != tc.wantCode {
				t.Errorf("status %d, want %d", code, tc.wantCode)
			}
			got := decodeResponse[httpError](t, body)
			if got.Error.Code != tc.wantCode || got.Error.Message != tc.wantMsg {
				t.Errorf("body %s, want code %d and %q", body, tc.wantCode, tc.wantMsg)
			}
		})
	}
}

func TestFromMessageToHTTPResponse(t *testing.T) {
	// The message is the point here, so it is sent as it is.
	raw := FromMessageToHTTPResponse("a -> b makes a cycle", http.StatusUnprocessableEntity)
	if got := decodeResponse[httpError](t, raw); got.Error.Code != http.StatusUnprocessableEntity || got.Error.Message != "a -> b makes a cycle" {
		t.Errorf("got %s", raw)
	}
}
