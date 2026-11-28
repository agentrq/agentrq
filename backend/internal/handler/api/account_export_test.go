// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v3"
)

type exportCrud struct {
	crud.Controller
	err error
	saw entity.ExportAccountDataRequest
}

func (e *exportCrud) ExportAccountData(_ context.Context, req entity.ExportAccountDataRequest) (*entity.AccountExport, error) {
	e.saw = req
	if e.err != nil {
		return nil, e.err
	}
	return &entity.AccountExport{
		Version:    entity.AccountExportVersion,
		ExportedAt: time.Date(2026, 11, 28, 18, 0, 0, 0, time.UTC),
		Profile:    entity.AccountExportProfile{Email: req.Email, Name: req.Name},
	}, nil
}

func exportRequest(t *testing.T, c crud.Controller, locals map[string]any) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		for k, v := range locals {
			ctx.Locals(k, v)
		}
		return ctx.Next()
	})
	h := &handler{crud: c, router: app.Group("")}
	h.registerAccountRoutes()
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/account/export", nil))
	if err != nil {
		t.Fatalf("GET /account/export: %v", err)
	}
	return res
}

func TestExportAccountData_DownloadsTheAccountsDataAsAFile(t *testing.T) {
	c := &exportCrud{}
	res := exportRequest(t, c, map[string]any{"user_id": "user-1", "user_email": "ada@example.com", "user_name": "Ada"})
	if res.StatusCode != http.StatusOK || res.Header.Get(fiber.HeaderContentType) != "application/gzip" || res.Header.Get(fiber.HeaderContentEncoding) != "" {
		t.Fatalf("answered %d %v, want 200 with a gzip file, not a gzip transfer", res.StatusCode, res.Header)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("the file is not gzip: %v", err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.Contains(string(body), `"profile":{"id":"","email":"ada@example.com","name":"Ada"}`) {
		t.Fatalf("the file holds %s", body)
	}
	if got := res.Header.Get(fiber.HeaderContentDisposition); got != `attachment; filename="agentrq-export-2026-11-28.json.gz"` {
		t.Errorf("Content-Disposition %q", got)
	}
	if got := res.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
	if c.saw != (entity.ExportAccountDataRequest{UserID: "user-1", Email: "ada@example.com", Name: "Ada"}) {
		t.Errorf("the controller was asked for %+v", c.saw)
	}
}

func TestExportAccountData_ASessionWithNoEmailOrNameStillExports(t *testing.T) {
	c := &exportCrud{}
	if res := exportRequest(t, c, map[string]any{"user_id": "user-1"}); res.StatusCode != http.StatusOK {
		t.Fatalf("answered %d, want 200", res.StatusCode)
	}
	if c.saw != (entity.ExportAccountDataRequest{UserID: "user-1"}) {
		t.Errorf("the controller was asked for %+v", c.saw)
	}
}

func TestExportAccountData_AFailedReadIsNoFile(t *testing.T) {
	res := exportRequest(t, &exportCrud{err: errors.New("database unavailable")}, map[string]any{"user_id": "user-1"})
	if res.StatusCode != http.StatusInternalServerError || res.Header.Get(fiber.HeaderContentType) != "application/json" {
		t.Fatalf("answered %d, want 500", res.StatusCode)
	}
	if got := res.Header.Get(fiber.HeaderContentDisposition); got != "" {
		t.Errorf("a failure was offered as a download: %q", got)
	}
}
