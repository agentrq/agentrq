// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v3"
)

func TestFromHTTPRequestToConnectGitHubRequestEntity(t *testing.T) {
	parse := func(body string) *entity.ConnectGitHubRequest {
		app := fiber.New()
		var rq *entity.ConnectGitHubRequest
		app.Post("/", func(c fiber.Ctx) error {
			rq = FromHTTPRequestToConnectGitHubRequestEntity(c)
			return nil
		})
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
		return rq
	}
	if rq := parse(`{"code":"c","state":"s"}`); rq == nil || rq.Code != "c" || rq.State != "s" {
		t.Errorf("full = %+v", rq)
	}
	for _, body := range []string{`{`, `{"code":"c"}`, `{"state":"s"}`} {
		if rq := parse(body); rq != nil {
			t.Errorf("%s = %+v", body, rq)
		}
	}
}

func TestFromHTTPRequestToDisconnectGitHubRequestEntity(t *testing.T) {
	parse := func(id string) *entity.DisconnectGitHubRequest {
		app := fiber.New()
		var rq *entity.DisconnectGitHubRequest
		app.Delete("/:id", func(c fiber.Ctx) error {
			rq = FromHTTPRequestToDisconnectGitHubRequestEntity(c)
			return nil
		})
		if _, err := app.Test(httptest.NewRequest(http.MethodDelete, "/"+id, nil)); err != nil {
			t.Fatal(err)
		}
		return rq
	}
	if rq := parse("12345"); rq == nil || rq.InstallationID != 12345 {
		t.Errorf("number = %+v", rq)
	}
	for _, id := range []string{"0", "-1", "abc"} {
		if rq := parse(id); rq != nil {
			t.Errorf("%s = %+v", id, rq)
		}
	}
}

func TestFromGitHubConnectionResponseEntityToHTTPResponse(t *testing.T) {
	if got := string(FromGitHubConnectionResponseEntityToHTTPResponse(&entity.GitHubConnectionResponse{Installations: []entity.GitHubInstallation{}})); got != `{"enabled":false,"installations":[]}` {
		t.Errorf("off = %s", got)
	}
	got := string(FromGitHubConnectionResponseEntityToHTTPResponse(&entity.GitHubConnectionResponse{
		Enabled: true, InstallURL: "u",
		Installations: []entity.GitHubInstallation{{InstallationID: 9, Account: "acme", AccountType: "Organization", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}},
	}))
	if got != `{"enabled":true,"installUrl":"u","installations":[{"installationId":9,"account":"acme","accountType":"Organization","createdAt":"2026-01-02T00:00:00Z"}]}` {
		t.Errorf("on = %s", got)
	}
}
