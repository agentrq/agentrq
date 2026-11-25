// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"

	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
)

// The catalog wins over the static handler's miss and the app shell, in the
// order app.go mounts them.
func TestA2UICatalogIsServed(t *testing.T) {
	app := fiber.New()
	app.Use("/", staticHandler(fstest.MapFS{"assets/app.js": {Data: []byte("PLAIN")}}))
	app.Get(a2ui.CatalogPath, a2uiCatalogHandler(a2ui.Catalog))
	app.Get("/*", func(c fiber.Ctx) error { return c.SendString("APP SHELL") })

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, a2ui.CatalogPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want, _ := a2ui.Catalog()
	if resp.StatusCode != http.StatusOK || string(body) != string(want) {
		t.Fatalf("GET %s: %d, %d bytes; want 200 and the catalog", a2ui.CatalogPath, resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get(fiber.HeaderContentType); ct != fiber.MIMEApplicationJSONCharsetUTF8 {
		t.Errorf("Content-Type %q", ct)
	}
}

func TestA2UICatalogFailure(t *testing.T) {
	app := fiber.New()
	app.Get(a2ui.CatalogPath, a2uiCatalogHandler(func() ([]byte, error) { return nil, errors.New("broken") }))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, a2ui.CatalogPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
}
