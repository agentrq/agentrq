// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v3"
)

type accountCrud struct {
	presetCrud
	settingsErr       error
	sawGetSettings    entity.GetAccountSettingsRequest
	sawUpdateSettings entity.UpdateAccountSettingsRequest
}

func (a *accountCrud) GetAccountSettings(_ context.Context, req entity.GetAccountSettingsRequest) (*entity.AccountSettingsResponse, error) {
	a.sawGetSettings = req
	if a.settingsErr != nil {
		return nil, a.settingsErr
	}
	return &entity.AccountSettingsResponse{Settings: entity.AccountSettings{entity.SettingsDisplay: {entity.SettingTimeFormat: "browser"}}}, nil
}

func (a *accountCrud) UpdateAccountSettings(_ context.Context, req entity.UpdateAccountSettingsRequest) (*entity.AccountSettingsResponse, error) {
	a.sawUpdateSettings = req
	if a.settingsErr != nil {
		return nil, a.settingsErr
	}
	return &entity.AccountSettingsResponse{Settings: req.Settings}, nil
}

func accountRequest(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h := &handler{crud: c, router: app.Group("")}
	h.registerAccountRoutes()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestGetAccountSettings(t *testing.T) {
	c := &accountCrud{}
	status, body := accountRequest(t, c, http.MethodGet, "/account/settings", "")
	if status != http.StatusOK || body != `{"settings":{"display":{"timeFormat":"browser"}}}` {
		t.Fatalf("answered %d %s, want 200 with the browser clock", status, body)
	}
	if c.sawGetSettings.UserID != "user-1" {
		t.Errorf("the controller was asked for %+v, want the session's user", c.sawGetSettings)
	}
}

func TestUpdateAccountSettings(t *testing.T) {
	c := &accountCrud{}
	status, body := accountRequest(t, c, http.MethodPut, "/account/settings", `{"display":{"timeFormat":"12h"}}`)
	if status != http.StatusOK || body != `{"settings":{"display":{"timeFormat":"12h"}}}` {
		t.Fatalf("answered %d %s, want 200 with 12h", status, body)
	}
	if c.sawUpdateSettings.UserID != "user-1" || len(c.sawUpdateSettings.Settings) != 1 || c.sawUpdateSettings.Settings[entity.SettingsDisplay][entity.SettingTimeFormat] != "12h" {
		t.Errorf("the controller was asked for %+v", c.sawUpdateSettings)
	}
}

func TestAccountSettings_RefusalsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		err                error
		want               int
	}{
		{"a body that is not JSON", http.MethodPut, `{`, nil, http.StatusUnprocessableEntity},
		{"a clock the server refuses", http.MethodPut, `{"display":{"timeFormat":"13h"}}`, &entity.AccountSettingsError{Message: "refused"}, http.StatusUnprocessableEntity},
		{"a save failing in the database", http.MethodPut, `{"display":{"timeFormat":"12h"}}`, errors.New("database unavailable"), http.StatusInternalServerError},
		{"a read failing in the database", http.MethodGet, "", errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := accountRequest(t, &accountCrud{settingsErr: tc.err}, tc.method, "/account/settings", tc.body); status != tc.want {
				t.Errorf("status %d, want %d: %s", status, tc.want, body)
			}
		})
	}
}

func TestAccountPresets_ReachTheControllerWithNoWorkspace(t *testing.T) {
	c := &accountCrud{}
	if status, body := accountRequest(t, c, http.MethodGet, "/account/presets", ""); status != http.StatusOK {
		t.Fatalf("list answered %d %s, want 200", status, body)
	}
	if c.sawList != (entity.ListPresetsRequest{UserID: "user-1"}) {
		t.Errorf("list asked for %+v, want the session's own presets, workspace 0", c.sawList)
	}
	if status, _ := accountRequest(t, c, http.MethodPost, "/account/presets", `{"name":"standup","body":"post it"}`); status != http.StatusCreated {
		t.Errorf("create answered %d, want 201", status)
	}
	if c.sawCreate != (entity.CreatePresetRequest{UserID: "user-1", Name: "standup", Body: "post it"}) {
		t.Errorf("create asked for %+v", c.sawCreate)
	}
	if status, _ := accountRequest(t, c, http.MethodPut, "/account/presets/standup", `{"body":"post it now"}`); status != http.StatusOK {
		t.Errorf("update answered %d, want 200", status)
	}
	if c.sawUpdate != (entity.UpdatePresetRequest{UserID: "user-1", Name: "standup", Body: "post it now"}) {
		t.Errorf("update asked for %+v", c.sawUpdate)
	}
	if status, _ := accountRequest(t, c, http.MethodDelete, "/account/presets/standup", ""); status != http.StatusNoContent {
		t.Errorf("delete answered %d, want 204", status)
	}
	if c.sawDelete != (entity.DeletePresetRequest{UserID: "user-1", Name: "standup"}) {
		t.Errorf("delete asked for %+v", c.sawDelete)
	}
}
