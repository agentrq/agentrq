// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// A Pano refusal, however wrapped, answers 422 with its own sentence.
func TestFromErrorToHTTPResponse_APanoRefusal(t *testing.T) {
	err := fmt.Errorf("save: %w", &entity.PanoError{Message: "chart.png is sent twice"})
	body, status := FromErrorToHTTPResponse(err)
	var e httpError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusUnprocessableEntity || e.Error.Code != http.StatusUnprocessableEntity || e.Error.Message != "chart.png is sent twice" {
		t.Errorf("a Pano refusal answered %d %s, want 422 with the refusal's sentence", status, body)
	}
}

// A write on a stale version answers 409, so the interface can tell it from
// a refusal.
func TestFromErrorToHTTPResponse_AStalePanoWrite(t *testing.T) {
	body, status := FromErrorToHTTPResponse(&entity.PanoError{Message: "the Pano doc changed", Conflict: true})
	if status != http.StatusConflict || !strings.Contains(string(body), `"code":409`) || !strings.Contains(string(body), "the Pano doc changed") {
		t.Errorf("a stale write answered %d %s, want 409 with its sentence", status, body)
	}
}
