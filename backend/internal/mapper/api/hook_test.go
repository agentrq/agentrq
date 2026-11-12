// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestFromHookEntitiesToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC)
	h := entity.Hook{
		ID: 1, BuiltIn: true, Name: "github", Version: "1.0.0", Description: "GitHub", SHA256: "ab", SizeBytes: 9, ABIVersion: 1,
		Inputs:  []entity.HookInput{{Name: "branches", Label: "Branches", Type: "array", Items: "string"}, {Name: "s", Type: "string", Secret: true, Required: true}},
		Events:  []entity.HookEvent{{Name: "ci.failed", Description: "A run failed"}, {Name: "push"}},
		Enabled: true, CreatedAt: at, UpdatedAt: at,
	}
	want := `{"hooks":[{"id":"00000000001","builtIn":true,"name":"github","version":"1.0.0","description":"GitHub","sha256":"ab","sizeBytes":9,"abiVersion":1,` +
		`"inputs":[{"name":"branches","label":"Branches","type":"array","items":"string","secret":false,"required":false},{"name":"s","type":"string","secret":true,"required":true}],` +
		`"events":[{"name":"ci.failed","description":"A run failed"},{"name":"push"}],"enabled":true,"createdAt":"2026-11-12T04:00:00Z","updatedAt":"2026-11-12T04:00:00Z"}]}`
	if got := string(FromHookEntitiesToHTTPResponse([]entity.Hook{h})); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := string(FromHookEntitiesToHTTPResponse(nil)); got != `{"hooks":[]}` {
		t.Errorf("none = %s", got)
	}
	if got := string(FromHookEntityToHTTPResponse(entity.Hook{ID: 2})); got[:20] != `{"id":"00000000002",` {
		t.Errorf("one = %s", got)
	}
}

func TestFromErrorToHTTPResponseAnswersAHookError(t *testing.T) {
	for kind, want := range map[entity.HookErrorKind]int{entity.HookInvalid: http.StatusUnprocessableEntity, entity.HookConflict: http.StatusConflict} {
		body, status := FromErrorToHTTPResponse(&entity.HookError{Kind: kind, Message: "why"})
		if status != want || string(body) != `{"error":{"message":"why","code":`+strconv.Itoa(want)+`}}` {
			t.Errorf("kind %d: %d %s", kind, status, body)
		}
	}
}
