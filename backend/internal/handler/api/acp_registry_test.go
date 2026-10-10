// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/agentrq/agentrq/backend/internal/controller/acpregistry"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
)

// fakeAcpRegistry answers every load the same way.
type fakeAcpRegistry struct {
	body []byte
	err  error
}

func (f fakeAcpRegistry) Load(context.Context) ([]byte, error) { return f.body, f.err }

func acpRegistryResponse(t *testing.T, reg acpregistry.Controller) (int, string) {
	t.Helper()
	h := &handler{acpRegistry: reg}
	app := fiber.New()
	app.Get(_routePathAcpRegistry, h.listAcpRegistryAgents())
	res, err := app.Test(httptest.NewRequest(http.MethodGet, _routePathAcpRegistry, nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	if ct := res.Header.Get(fiber.HeaderContentType); ct != fiber.MIMEApplicationJSON {
		t.Errorf("content type = %q, want JSON", ct)
	}
	return res.StatusCode, string(body)
}

func TestListAcpRegistryAgentsAnswersTheCatalogue(t *testing.T) {
	body := mapper.FromAcpRegistryAgentsEntityToHTTPResponse([]entity.AcpRegistryAgent{
		{ID: "codex-acp", Name: "Codex", Version: "1.2.0", Description: "OpenAI's agent", Runtimes: []string{"npx"}},
	})
	code, got := acpRegistryResponse(t, fakeAcpRegistry{body: body})
	want := `{"agents":[{"id":"codex-acp","name":"Codex","version":"1.2.0","description":"OpenAI's agent","runtimes":["npx"]}]}`
	if code != http.StatusOK || got != want {
		t.Fatalf("response = %d %s, want 200 %s", code, got, want)
	}
}

// A registry that cannot be reached is an empty list, not an error: the forms
// fall back to the machine's own list and free text.
func TestListAcpRegistryAgentsFailsOpen(t *testing.T) {
	code, got := acpRegistryResponse(t, fakeAcpRegistry{err: acpregistry.ErrUnavailable})
	if code != http.StatusOK || got != `{"agents":[]}` {
		t.Fatalf("response = %d %s, want 200 with no agents", code, got)
	}
}
