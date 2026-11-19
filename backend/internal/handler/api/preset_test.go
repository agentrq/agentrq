// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

type presetCrud struct {
	crud.Controller
	err       error
	sawList   entity.ListPresetsRequest
	sawCreate entity.CreatePresetRequest
	sawDelete entity.DeletePresetRequest
}

func (p *presetCrud) ListPresets(_ context.Context, req entity.ListPresetsRequest) (*entity.ListPresetsResponse, error) {
	p.sawList = req
	if p.err != nil {
		return nil, p.err
	}
	return &entity.ListPresetsResponse{Presets: []entity.Preset{
		{ID: 900, CreatedAt: time.Now(), WorkspaceID: 4242, Name: "pull-n-push", Body: "pull latest main, tag and push"},
	}}, nil
}

func (p *presetCrud) CreatePreset(_ context.Context, req entity.CreatePresetRequest) (*entity.CreatePresetResponse, error) {
	p.sawCreate = req
	if p.err != nil {
		return nil, p.err
	}
	return &entity.CreatePresetResponse{Preset: entity.Preset{ID: 900, WorkspaceID: 4242, Name: req.Name, Body: req.Body}}, nil
}

func (p *presetCrud) DeletePreset(_ context.Context, req entity.DeletePresetRequest) error {
	p.sawDelete = req
	return p.err
}

var testPresetWorkspaceID = monoflake.ID(4242).String()

func presetApp(c crud.Controller) *fiber.App {
	h := &handler{crud: c}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Get("/workspaces/:id/presets", h.listWorkspacePresets())
	app.Post("/workspaces/:id/presets", h.createWorkspacePreset())
	app.Delete("/workspaces/:id/presets/:name", h.deleteWorkspacePreset())
	return app
}

func presetRequest(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := presetApp(c).Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestListWorkspacePresets(t *testing.T) {
	c := &presetCrud{}
	status, body := presetRequest(t, c, http.MethodGet, "/workspaces/"+testPresetWorkspaceID+"/presets", "")
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", status, body)
	}
	var got struct {
		Presets []map[string]any `json:"presets"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Presets) != 1 || got.Presets[0]["name"] != "pull-n-push" || got.Presets[0]["body"] != "pull latest main, tag and push" || got.Presets[0]["workspaceId"] != testPresetWorkspaceID {
		t.Fatalf("the list is %s, want pull-n-push in camelCase", body)
	}
	if c.sawList.WorkspaceID != 4242 || c.sawList.UserID != "user-1" {
		t.Errorf("the controller was asked for %+v, want workspace 4242 for the session's user", c.sawList)
	}
}

func TestCreateWorkspacePreset(t *testing.T) {
	c := &presetCrud{}
	status, body := presetRequest(t, c, http.MethodPost, "/workspaces/"+testPresetWorkspaceID+"/presets", `{"name":"pull-n-push","body":"pull latest main, tag and push"}`)
	if status != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", status, body)
	}
	if !strings.Contains(body, `"preset":{`) || !strings.Contains(body, `"name":"pull-n-push"`) {
		t.Errorf("the response is %s, want the saved preset", body)
	}
	if c.sawCreate != (entity.CreatePresetRequest{WorkspaceID: 4242, UserID: "user-1", Name: "pull-n-push", Body: "pull latest main, tag and push"}) {
		t.Errorf("the controller was asked for %+v", c.sawCreate)
	}
}

func TestDeleteWorkspacePreset(t *testing.T) {
	c := &presetCrud{}
	status, body := presetRequest(t, c, http.MethodDelete, "/workspaces/"+testPresetWorkspaceID+"/presets/pull-n-push", "")
	if status != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", status, body)
	}
	if c.sawDelete != (entity.DeletePresetRequest{WorkspaceID: 4242, UserID: "user-1", Name: "pull-n-push"}) {
		t.Errorf("the controller was asked for %+v", c.sawDelete)
	}
}

func TestWorkspacePresets_RefuseAnUnusableRequest(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"list with no workspace id", http.MethodGet, "/workspaces/0/presets", ""},
		{"create with no workspace id", http.MethodPost, "/workspaces/0/presets", `{"name":"a","body":"b"}`},
		{"create with a body that is not JSON", http.MethodPost, "/workspaces/" + testPresetWorkspaceID + "/presets", `{`},
		{"delete with no workspace id", http.MethodDelete, "/workspaces/0/presets/a", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := presetRequest(t, &presetCrud{}, tc.method, tc.path, tc.body); status != http.StatusUnprocessableEntity {
				t.Errorf("status %d, want 422: %s", status, body)
			}
		})
	}
}

// httptest refuses a broken escape, so the request line is written as is.
func TestDeleteWorkspacePreset_RefusesABrokenEscapeInTheName(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.URL.Opaque = "/workspaces/" + testPresetWorkspaceID + "/presets/a%zz"
	res, err := presetApp(&presetCrud{}).Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", res.StatusCode)
	}
}

func TestWorkspacePresets_ControllerErrorsMapToStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		want                     int
		message                  string
	}{
		{"list of another account's workspace", http.MethodGet, "/workspaces/" + testPresetWorkspaceID + "/presets", "", base.ErrNotFound, http.StatusNotFound, "not found"},
		{"create with a name taken", http.MethodPost, "/workspaces/" + testPresetWorkspaceID + "/presets", `{"name":"a","body":"b"}`, &entity.PresetError{Message: `a preset named "a" exists`, Conflict: true}, http.StatusConflict, `a preset named \"a\" exists`},
		{"create with an empty body", http.MethodPost, "/workspaces/" + testPresetWorkspaceID + "/presets", `{"name":"a","body":""}`, &entity.PresetError{Message: "preset body is empty"}, http.StatusUnprocessableEntity, "preset body is empty"},
		{"delete of a preset not there", http.MethodDelete, "/workspaces/" + testPresetWorkspaceID + "/presets/a", "", base.ErrNotFound, http.StatusNotFound, "not found"},
		{"delete failing in the database", http.MethodDelete, "/workspaces/" + testPresetWorkspaceID + "/presets/a", "", errors.New("database unavailable"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := presetRequest(t, &presetCrud{err: tc.err}, tc.method, tc.path, tc.body)
			if status != tc.want || !strings.Contains(body, tc.message) {
				t.Errorf("answered %d %s, want %d with %q", status, body, tc.want, tc.message)
			}
		})
	}
}
