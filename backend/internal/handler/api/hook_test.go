// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// fakeRegistrations records what reaches the controller and answers with err.
type fakeRegistrations struct {
	fakeHooks
	user, regID int64
	req         entity.HookRegistrationRequest
	called      string
}

func (f *fakeRegistrations) reg(token string) entity.HookRegistration {
	r := entity.HookRegistration{ID: 9, HookID: 7, HookName: "github", Name: "CI", URL: "https://rq/api/v1/hooks/in/" + regIDText}
	if token != "" {
		r.URLWithToken = r.URL + "?agentrq_hook_token=" + token
	}
	return r
}

func (f *fakeRegistrations) ListAvailable(_ context.Context, user int64) ([]entity.Hook, error) {
	f.called, f.user = "ListAvailable", user
	return []entity.Hook{f.hook()}, f.err
}

func (f *fakeRegistrations) ListRegistrations(_ context.Context, user int64) ([]entity.HookRegistration, error) {
	f.called, f.user = "ListRegistrations", user
	return []entity.HookRegistration{f.reg("")}, f.err
}

func (f *fakeRegistrations) GetRegistration(_ context.Context, user, id int64) (entity.HookRegistration, error) {
	f.called, f.user, f.regID = "GetRegistration", user, id
	return f.reg(""), f.err
}

func (f *fakeRegistrations) CreateRegistration(_ context.Context, user int64, req entity.HookRegistrationRequest) (entity.HookRegistration, error) {
	f.called, f.user, f.req = "CreateRegistration", user, req
	return f.reg("abcd1234"), f.err
}

func (f *fakeRegistrations) UpdateRegistration(_ context.Context, user, id int64, req entity.HookRegistrationRequest) (entity.HookRegistration, error) {
	f.called, f.user, f.regID, f.req = "UpdateRegistration", user, id, req
	return f.reg(""), f.err
}

func (f *fakeRegistrations) DeleteRegistration(_ context.Context, user, id int64) error {
	f.called, f.user, f.regID = "DeleteRegistration", user, id
	return f.err
}

func (f *fakeRegistrations) RotateToken(_ context.Context, user, id int64) (entity.HookRegistration, error) {
	f.called, f.user, f.regID = "RotateToken", user, id
	return f.reg("wxyz5678"), f.err
}

var (
	regIDText = monoflake.ID(9).String()
	userText  = monoflake.ID(42).String()
)

func hookApp(hooks *fakeRegistrations) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userText)
		return c.Next()
	})
	h := &handler{router: app.Group("/api/v1"), hooks: hooks}
	h.registerHookRoutes()
	return app
}

var jsonHeader = map[string]string{"Content-Type": "application/json"}

func TestHookRegistrationRoutes(t *testing.T) {
	hooks := &fakeRegistrations{}
	app := hookApp(hooks)
	regURL := "/api/v1/hooks/registrations/" + regIDText

	for _, tc := range []struct {
		method, url, body string
		status            int
		called            string
		want              string
	}{
		{http.MethodGet, "/api/v1/hooks", "", http.StatusOK, "ListAvailable", `{"hooks":[{"id":"` + hookID + `","builtIn":true,"name":"github"`},
		{http.MethodGet, "/api/v1/hooks/registrations", "", http.StatusOK, "ListRegistrations", `{"registrations":[{"id":"` + regIDText + `","hookId":"` + hookID + `"`},
		{http.MethodPost, "/api/v1/hooks/registrations", `{"hookId":"` + hookID + `","name":"CI","inputs":{"webhookSecret":"s"}}`, http.StatusCreated, "CreateRegistration", `?agentrq_hook_token=abcd1234"`},
		{http.MethodGet, regURL, "", http.StatusOK, "GetRegistration", `"name":"CI"`},
		{http.MethodPatch, regURL, `{"name":"CI 2","enabled":false}`, http.StatusOK, "UpdateRegistration", `"name":"CI"`},
		{http.MethodPost, regURL + "/rotateToken", "", http.StatusOK, "RotateToken", `?agentrq_hook_token=wxyz5678"`},
		{http.MethodDelete, regURL, "", http.StatusNoContent, "DeleteRegistration", ``},
	} {
		hooks.called, hooks.user, hooks.regID = "", 0, 0
		status, body := hookRequest(t, app, tc.method, tc.url, []byte(tc.body), jsonHeader)
		if status != tc.status || hooks.called != tc.called || hooks.user != 42 || !strings.Contains(body, tc.want) {
			t.Errorf("%s %s = %d %s, called %q as %d", tc.method, tc.url, status, body, hooks.called, hooks.user)
		}
		if strings.Contains(tc.url, regIDText) && hooks.regID != 9 {
			t.Errorf("%s %s reached registration %d", tc.method, tc.url, hooks.regID)
		}
		// Only a new token is ever shown.
		if strings.Contains(body, "agentrq_hook_token") != (tc.called == "CreateRegistration" || tc.called == "RotateToken") {
			t.Errorf("%s %s shows a token: %s", tc.method, tc.url, body)
		}
	}
	if hooks.req.Name == nil || *hooks.req.Name != "CI 2" || hooks.req.Enabled == nil || *hooks.req.Enabled {
		t.Errorf("update request = %+v", hooks.req)
	}
}

func TestHookRegistrationRoutesRefuseABadRequest(t *testing.T) {
	hooks := &fakeRegistrations{}
	app := hookApp(hooks)
	regURL := "/api/v1/hooks/registrations/" + regIDText
	for name, r := range map[string]struct{ method, url, body string }{
		"a create not JSON":          {http.MethodPost, "/api/v1/hooks/registrations", `{`},
		"a create of no hook":        {http.MethodPost, "/api/v1/hooks/registrations", `{"name":"CI"}`},
		"a get of no id":             {http.MethodGet, "/api/v1/hooks/registrations/!", ""},
		"an update of no id":         {http.MethodPatch, "/api/v1/hooks/registrations/!", `{}`},
		"an update not JSON":         {http.MethodPatch, regURL, `{`},
		"an update to another hook":  {http.MethodPatch, regURL, `{"hookId":"` + hookID + `"}`},
		"a rotation of no id":        {http.MethodPost, "/api/v1/hooks/registrations/!/rotateToken", ""},
		"a delete of no id":          {http.MethodDelete, "/api/v1/hooks/registrations/!", ""},
		"a route to an id not an id": {http.MethodPost, "/api/v1/hooks/registrations", `{"hookId":"` + hookID + `","routes":{"push":"!"}}`},
	} {
		hooks.called = ""
		if status, body := hookRequest(t, app, r.method, r.url, []byte(r.body), jsonHeader); status != http.StatusBadRequest || hooks.called != "" {
			t.Errorf("%s: %d %s, called %q", name, status, body, hooks.called)
		}
	}
}

func TestHookRegistrationRoutesAnswerTheControllersError(t *testing.T) {
	regURL := "/api/v1/hooks/registrations/" + regIDText
	for name, tc := range map[string]struct {
		err    error
		status int
		text   string
	}{
		"invalid":   {&entity.HookError{Kind: entity.HookInvalid, Message: "the input \"x\" is required"}, http.StatusUnprocessableEntity, `the input \"x\" is required`},
		"not found": {base.ErrNotFound, http.StatusNotFound, "not found"},
		"failed":    {errors.New("disk"), http.StatusInternalServerError, "internal server error"},
	} {
		app := hookApp(&fakeRegistrations{fakeHooks: fakeHooks{err: tc.err}})
		for _, r := range []struct{ method, url, body string }{
			{http.MethodGet, "/api/v1/hooks", ""},
			{http.MethodGet, "/api/v1/hooks/registrations", ""},
			{http.MethodPost, "/api/v1/hooks/registrations", `{"hookId":"` + hookID + `"}`},
			{http.MethodGet, regURL, ""},
			{http.MethodPatch, regURL, `{}`},
			{http.MethodPost, regURL + "/rotateToken", ""},
			{http.MethodDelete, regURL, ""},
		} {
			status, body := hookRequest(t, app, r.method, r.url, []byte(r.body), jsonHeader)
			if status != tc.status || !strings.Contains(body, tc.text) || strings.Contains(body, "agentrq_hook_token") {
				t.Errorf("%s: %s %s = %d %s", name, r.method, r.url, status, body)
			}
		}
	}
}
