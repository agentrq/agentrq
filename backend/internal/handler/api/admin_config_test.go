// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/appconfig"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v3"
)

// fakeAppConfig records the update that reaches it and answers with err.
type fakeAppConfig struct {
	appconfig.Controller
	err error
	rq  entity.UpdateAppConfigurationRequest
}

func (f *fakeAppConfig) List(context.Context) ([]entity.AppConfiguration, error) {
	return []entity.AppConfiguration{{Key: "smtp", Value: json.RawMessage(`{"enabled":true,"port":587}`)}}, f.err
}

func (f *fakeAppConfig) Update(_ context.Context, rq entity.UpdateAppConfigurationRequest) (*entity.AppConfiguration, error) {
	f.rq = rq
	if f.err != nil {
		return nil, f.err
	}
	return &entity.AppConfiguration{Key: rq.Key, Value: rq.Value}, nil
}

func adminConfigApp(cfg appconfig.Controller) (*fiber.App, map[string]string) {
	app := fiber.New()
	h := &handler{rootToken: "s3cret", appConfig: cfg}
	h.registerAdminRoutes(app)
	return app, map[string]string{"Authorization": "Bearer " + h.adminToken(time.Now().Add(time.Minute))}
}

func TestAdminConfigNeedsTheAdminSession(t *testing.T) {
	app, _ := adminConfigApp(&fakeAppConfig{})
	for _, r := range []struct{ method, url string }{
		{http.MethodGet, "/admin/v1/configurations"},
		{http.MethodPut, "/admin/v1/configurations/smtp"},
	} {
		if status, _ := hookRequest(t, app, r.method, r.url, []byte(`{"value":{}}`), nil); status != http.StatusUnauthorized {
			t.Errorf("%s %s = %d", r.method, r.url, status)
		}
	}
}

func TestAdminConfigListsTheBlocks(t *testing.T) {
	app, auth := adminConfigApp(&fakeAppConfig{})
	status, body := hookRequest(t, app, http.MethodGet, "/admin/v1/configurations", nil, auth)
	if want := `{"configurations":[{"key":"smtp","value":{"enabled":true,"port":587}}]}`; status != http.StatusOK || body != want {
		t.Fatalf("list = %d %s, want %s", status, body, want)
	}

	app, auth = adminConfigApp(&fakeAppConfig{err: errors.New("database unavailable")})
	if status, body := hookRequest(t, app, http.MethodGet, "/admin/v1/configurations", nil, auth); status != http.StatusInternalServerError {
		t.Fatalf("list on a failing store = %d %s", status, body)
	}
}

func TestAdminConfigUpdatesABlock(t *testing.T) {
	cfg := &fakeAppConfig{}
	app, auth := adminConfigApp(cfg)
	status, body := hookRequest(t, app, http.MethodPut, "/admin/v1/configurations/smtp", []byte(`{"value":{"port":2525}}`), auth)
	if want := `{"key":"smtp","value":{"port":2525}}`; status != http.StatusOK || body != want {
		t.Fatalf("update = %d %s, want %s", status, body, want)
	}
	if cfg.rq.Key != "smtp" || string(cfg.rq.Value) != `{"port":2525}` {
		t.Fatalf("the controller got %+v", cfg.rq)
	}

	for _, payload := range []string{`not json`, `{}`, `{"value":null}`} {
		if status, _ := hookRequest(t, app, http.MethodPut, "/admin/v1/configurations/smtp", []byte(payload), auth); status != http.StatusBadRequest {
			t.Errorf("update with %s = %d, want 400", payload, status)
		}
	}
}

func TestAdminConfigUpdateErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{
		{&entity.AppConfigurationError{Message: "memory.maxFileSize: -1 bytes is not between 0 and 131072"}, http.StatusUnprocessableEntity,
			`{"error":{"message":"memory.maxFileSize: -1 bytes is not between 0 and 131072","code":422}}`},
		{base.ErrNotFound, http.StatusNotFound, `{"error":{"message":"not found","code":404}}`},
		{errors.New("database unavailable"), http.StatusInternalServerError, `{"error":{"message":"internal server error","code":500}}`},
	} {
		app, auth := adminConfigApp(&fakeAppConfig{err: tc.err})
		status, body := hookRequest(t, app, http.MethodPut, "/admin/v1/configurations/memory", []byte(`{"value":{"maxFileSize":-1}}`), auth)
		if status != tc.status || body != tc.body {
			t.Errorf("%v: %d %s, want %d %s", tc.err, status, body, tc.status, tc.body)
		}
	}
}

// Without a stored config, as in a test server, there is no config page.
func TestAdminConfigRoutesNeedTheStoredConfig(t *testing.T) {
	app, auth := adminConfigApp(nil)
	if status, _ := hookRequest(t, app, http.MethodGet, "/admin/v1/configurations", nil, auth); status != http.StatusNotFound {
		t.Fatalf("list without a stored config = %d, want 404", status)
	}
}

// fakeLoginIcons records the image that reaches it and answers with err.
type fakeLoginIcons struct {
	err  error
	data []byte
}

func (f *fakeLoginIcons) Upload(_ context.Context, rq entity.UploadLoginIconRequest) (*entity.UploadLoginIconResponse, error) {
	f.data = rq.Data
	if f.err != nil {
		return nil, f.err
	}
	return &entity.UploadLoginIconResponse{URL: "https://cdn.example/login-icons/0123456789abcdef"}, nil
}

func loginIconApp(icons *fakeLoginIcons) (*fiber.App, map[string]string) {
	app := fiber.New()
	h := &handler{rootToken: "s3cret", appConfig: &fakeAppConfig{}}
	if icons != nil {
		h.loginIcons = icons
	}
	h.registerAdminRoutes(app)
	return app, map[string]string{"Authorization": "Bearer " + h.adminToken(time.Now().Add(time.Minute)), "Content-Type": "image/png"}
}

func TestAdminUploadsALoginIcon(t *testing.T) {
	icons := &fakeLoginIcons{}
	app, auth := loginIconApp(icons)
	if status, _ := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", []byte("png"), nil); status != http.StatusUnauthorized {
		t.Errorf("without the admin session = %d, want 401", status)
	}
	status, body := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", []byte("png"), auth)
	if want := `{"url":"https://cdn.example/login-icons/0123456789abcdef"}`; status != http.StatusCreated || body != want {
		t.Fatalf("upload = %d %s, want 201 %s", status, body, want)
	}
	if string(icons.data) != "png" {
		t.Errorf("the controller got %q, want the body", icons.data)
	}
	if status, _ := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", nil, auth); status != http.StatusBadRequest {
		t.Errorf("an empty upload = %d, want 400", status)
	}

	refused := &fakeLoginIcons{err: &entity.AppConfigurationError{Message: "the icon must be a PNG"}}
	app, auth = loginIconApp(refused)
	if status, body := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", []byte("x"), auth); status != http.StatusUnprocessableEntity || !json.Valid([]byte(body)) {
		t.Errorf("a refused icon = %d %s, want 422", status, body)
	}

	app, auth = loginIconApp(&fakeLoginIcons{err: errors.New("bucket unavailable")})
	if status, _ := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", []byte("x"), auth); status != http.StatusInternalServerError {
		t.Errorf("a failing store = %d, want 500", status)
	}

	app, auth = loginIconApp(nil)
	if status, _ := hookRequest(t, app, http.MethodPost, "/admin/v1/login-icons", []byte("x"), auth); status != http.StatusNotFound {
		t.Errorf("with no icon store = %d, want 404", status)
	}
}
