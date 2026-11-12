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
	in          entity.HookIncoming
	before      int64
	limit       int
	deliveryID  int64
	allow       bool
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

func (f *fakeRegistrations) Receive(_ context.Context, in entity.HookIncoming) (int64, error) {
	f.called, f.in = "Receive", in
	return 11, f.err
}

func (f *fakeRegistrations) ListDeliveries(_ context.Context, user, regID, before int64, limit int) ([]entity.HookDelivery, error) {
	f.called, f.user, f.regID, f.before, f.limit = "ListDeliveries", user, regID, before, limit
	return []entity.HookDelivery{{ID: 11, RegistrationID: regID, Status: "failed", Replayable: true}}, f.err
}

func (f *fakeRegistrations) ReplayDelivery(_ context.Context, user, regID, id int64) error {
	f.called, f.user, f.regID, f.deliveryID = "ReplayDelivery", user, regID, id
	return f.err
}

// publicHookApp serves the hook URL as the server does: before, and so
// without, the session check.
func publicHookApp(hooks *fakeRegistrations) *fiber.App {
	app := fiber.New()
	h := &handler{router: app.Group("/api/v1"), hooks: hooks}
	h.registerPublicHookRoutes()
	h.router.Use(func(c *fiber.Ctx) error { return c.SendStatus(http.StatusUnauthorized) })
	h.registerHookRoutes()
	return app
}

var deliveryText = monoflake.ID(11).String()

func TestReceiveHookRequest(t *testing.T) {
	hooks := &fakeRegistrations{}
	app := publicHookApp(hooks)
	url := "/api/v1/hooks/in/" + regIDText + "?agentrq_hook_token=abcd1234&ref=main&ref=dev"
	status, body := hookRequest(t, app, http.MethodPost, url, []byte(`{"action":"completed"}`),
		map[string]string{"X-GitHub-Event": "workflow_run", "Cookie": "at=session", "Content-Type": "application/json"})
	if status != http.StatusAccepted || body != `{"deliveryId":"`+deliveryText+`"}` {
		t.Fatalf("= %d %s", status, body)
	}
	in := hooks.in
	if in.RegistrationID != 9 || in.Token != "abcd1234" || in.Method != http.MethodPost || in.Path != "/api/v1/hooks/in/"+regIDText ||
		string(in.Body) != `{"action":"completed"}` || in.Peer == "" {
		t.Errorf("incoming = %+v", in)
	}
	if len(in.Query) != 1 || strings.Join(in.Query["ref"], ",") != "main,dev" {
		t.Errorf("query = %v, the token must not be in it", in.Query)
	}
	if in.Headers["x-github-event"][0] != "workflow_run" || in.Headers["cookie"] != nil || in.Headers["X-GitHub-Event"] != nil {
		t.Errorf("headers = %v", in.Headers)
	}
}

func TestReceiveHookRequestRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		url    string
		status int
	}{
		"no id":     {nil, "/api/v1/hooks/in/!", http.StatusNotFound},
		"not found": {base.ErrNotFound, "/api/v1/hooks/in/" + regIDText, http.StatusNotFound},
		"too large": {&entity.HookError{Kind: entity.HookTooLarge, Message: "big"}, "/api/v1/hooks/in/" + regIDText, http.StatusRequestEntityTooLarge},
		"throttled": {&entity.HookError{Kind: entity.HookThrottled, Message: "slow down"}, "/api/v1/hooks/in/" + regIDText, http.StatusTooManyRequests},
	} {
		hooks := &fakeRegistrations{fakeHooks: fakeHooks{err: tc.err}}
		if status, body := hookRequest(t, publicHookApp(hooks), http.MethodPost, tc.url, nil, nil); status != tc.status {
			t.Errorf("%s: %d %s", name, status, body)
		}
	}
}

func TestHookDeliveryRoutes(t *testing.T) {
	hooks := &fakeRegistrations{}
	app := hookApp(hooks)
	path := "/api/v1/hooks/registrations/" + regIDText + "/deliveries"

	status, body := hookRequest(t, app, http.MethodGet, path, nil, nil)
	if status != http.StatusOK || !strings.Contains(body, `{"deliveries":[{"id":"`+deliveryText+`","registrationId":"`+regIDText+`"`) ||
		hooks.user != 42 || hooks.regID != 9 || hooks.before != 0 || hooks.limit != 50 {
		t.Errorf("list = %d %s, %+v", status, body, hooks)
	}
	if status, _ = hookRequest(t, app, http.MethodGet, path+"?limit=200&before="+deliveryText, nil, nil); status != http.StatusOK || hooks.before != 11 || hooks.limit != 200 {
		t.Errorf("page = %d, before %d, limit %d", status, hooks.before, hooks.limit)
	}
	if status, _ = hookRequest(t, app, http.MethodPost, path+"/"+deliveryText+"/replay", nil, nil); status != http.StatusNoContent || hooks.called != "ReplayDelivery" || hooks.deliveryID != 11 {
		t.Errorf("replay = %d, %+v", status, hooks)
	}

	for _, url := range []string{
		"/api/v1/hooks/registrations/!/deliveries", path + "?limit=0", path + "?limit=201", path + "?before=!",
	} {
		if status, _ := hookRequest(t, app, http.MethodGet, url, nil, nil); status != http.StatusBadRequest {
			t.Errorf("GET %s = %d", url, status)
		}
	}
	for _, url := range []string{"/api/v1/hooks/registrations/!/deliveries/" + deliveryText + "/replay", path + "/!/replay"} {
		if status, _ := hookRequest(t, app, http.MethodPost, url, nil, nil); status != http.StatusBadRequest {
			t.Errorf("POST %s = %d", url, status)
		}
	}

	hooks.err = base.ErrNotFound
	if status, _ := hookRequest(t, app, http.MethodGet, path, nil, nil); status != http.StatusNotFound {
		t.Errorf("list of none = %d", status)
	}
	if status, _ := hookRequest(t, app, http.MethodPost, path+"/"+deliveryText+"/replay", nil, nil); status != http.StatusNotFound {
		t.Errorf("replay of none = %d", status)
	}
}

// The hook URL is public; the deliveries are not.
func TestOnlyTheHookURLIsPublic(t *testing.T) {
	app := publicHookApp(&fakeRegistrations{})
	if status, _ := hookRequest(t, app, http.MethodGet, "/api/v1/hooks/registrations/"+regIDText+"/deliveries", nil, nil); status != http.StatusUnauthorized {
		t.Errorf("deliveries without a session = %d", status)
	}
}

func (f *fakeRegistrations) AllowsUserHooks() bool { return f.allow }

func (f *fakeRegistrations) CreateOwn(_ context.Context, user int64, wasm []byte) (entity.Hook, error) {
	f.called, f.user, f.wasm = "CreateOwn", user, wasm
	return f.hook(), f.err
}

func (f *fakeRegistrations) UpgradeOwn(_ context.Context, user, id int64, wasm []byte) (entity.Hook, error) {
	f.called, f.user, f.id, f.wasm = "UpgradeOwn", user, id, wasm
	return f.hook(), f.err
}

func (f *fakeRegistrations) DeleteOwn(_ context.Context, user, id int64) error {
	f.called, f.user, f.id = "DeleteOwn", user, id
	return f.err
}

func TestOwnHookRoutes(t *testing.T) {
	hooks := &fakeRegistrations{allow: true}
	app := hookApp(hooks)
	if status, body := hookRequest(t, app, http.MethodGet, "/api/v1/hooks", nil, nil); status != http.StatusOK || !strings.HasSuffix(body, `],"allowUserHooks":true}`) {
		t.Errorf("list = %d %s", status, body)
	}

	status, body := hookRequest(t, app, http.MethodPost, "/api/v1/hooks", gzipped([]byte("\x00asm")), map[string]string{"Content-Encoding": "gzip"})
	if status != http.StatusCreated || hooks.called != "CreateOwn" || hooks.user != 42 || string(hooks.wasm) != "\x00asm" || !strings.Contains(body, `"name":"github"`) {
		t.Errorf("create = %d %s, %+v", status, body, hooks)
	}
	if status, _ = hookRequest(t, app, http.MethodPut, "/api/v1/hooks/"+hookID, []byte("\x00asm2"), nil); status != http.StatusOK || hooks.called != "UpgradeOwn" || hooks.id != 7 || string(hooks.wasm) != "\x00asm2" {
		t.Errorf("upgrade = %d, %+v", status, hooks)
	}
	if status, _ = hookRequest(t, app, http.MethodDelete, "/api/v1/hooks/"+hookID, nil, nil); status != http.StatusNoContent || hooks.called != "DeleteOwn" || hooks.id != 7 {
		t.Errorf("delete = %d, %+v", status, hooks)
	}

	for _, r := range []struct{ method, url string }{
		{http.MethodPost, "/api/v1/hooks"}, {http.MethodPut, "/api/v1/hooks/!"}, {http.MethodPut, "/api/v1/hooks/" + hookID}, {http.MethodDelete, "/api/v1/hooks/!"},
	} {
		hooks.called = ""
		if status, _ := hookRequest(t, app, r.method, r.url, nil, nil); status != http.StatusBadRequest || hooks.called != "" {
			t.Errorf("%s %s = %d", r.method, r.url, status)
		}
	}

	hooks.err = &entity.HookError{Kind: entity.HookForbidden, Message: "not allowed"}
	for _, r := range []struct {
		method, url string
		body        []byte
	}{{http.MethodPost, "/api/v1/hooks", []byte("x")}, {http.MethodPut, "/api/v1/hooks/" + hookID, []byte("x")}, {http.MethodDelete, "/api/v1/hooks/" + hookID, nil}} {
		if status, body := hookRequest(t, app, r.method, r.url, r.body, nil); status != http.StatusForbidden || !strings.Contains(body, "not allowed") {
			t.Errorf("%s %s = %d %s", r.method, r.url, status, body)
		}
	}
}
