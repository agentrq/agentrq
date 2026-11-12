// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hookctrl "github.com/agentrq/agentrq/backend/internal/controller/hook"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// fakeHooks records what reaches the controller and answers with err.
type fakeHooks struct {
	hookctrl.Controller
	err     error
	wasm    []byte
	id      int64
	enabled bool
}

func (f *fakeHooks) hook() entity.Hook {
	return entity.Hook{ID: 7, BuiltIn: true, Name: "github", Version: "1.0.0", Enabled: f.enabled,
		Inputs: []entity.HookInput{{Name: "webhookSecret", Type: "string", Secret: true, Required: true}},
		Events: []entity.HookEvent{{Name: "ci.failed"}}}
}

func (f *fakeHooks) ListBuiltIns(context.Context) ([]entity.Hook, error) {
	return []entity.Hook{f.hook()}, f.err
}

func (f *fakeHooks) CreateBuiltIn(_ context.Context, wasm []byte) (entity.Hook, error) {
	f.wasm = wasm
	return f.hook(), f.err
}

func (f *fakeHooks) UpgradeBuiltIn(_ context.Context, id int64, wasm []byte) (entity.Hook, error) {
	f.id, f.wasm = id, wasm
	return f.hook(), f.err
}

func (f *fakeHooks) SetBuiltInEnabled(_ context.Context, id int64, enabled bool) (entity.Hook, error) {
	f.id, f.enabled = id, enabled
	return f.hook(), f.err
}

func (f *fakeHooks) DeleteBuiltIn(_ context.Context, id int64) error {
	f.id = id
	return f.err
}

func adminHookApp(hooks *fakeHooks) (*fiber.App, map[string]string) {
	app := fiber.New()
	h := &handler{rootToken: "s3cret", hooks: hooks, basePath: "/base"}
	h.registerAdminRoutes(app)
	app.Use(func(c *fiber.Ctx) error { return c.SendStatus(http.StatusForbidden) })
	return app, map[string]string{"Authorization": "Bearer " + h.adminToken(time.Now().Add(time.Minute))}
}

func hookRequest(t *testing.T, app *fiber.App, method, url string, body []byte, header map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func with(h map[string]string, kv ...string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

var hookID = monoflake.ID(7).String()

func TestAdminHooksNeedTheAdminSession(t *testing.T) {
	app, _ := adminHookApp(&fakeHooks{})
	for _, r := range []struct{ method, url string }{
		{http.MethodGet, "/admin/v1/hooks"},
		{http.MethodPost, "/admin/v1/hooks"},
		{http.MethodPut, "/admin/v1/hooks/" + hookID},
		{http.MethodPatch, "/admin/v1/hooks/" + hookID},
		{http.MethodDelete, "/admin/v1/hooks/" + hookID},
	} {
		if status, _ := hookRequest(t, app, r.method, r.url, []byte("x"), nil); status != http.StatusUnauthorized {
			t.Errorf("%s %s = %d", r.method, r.url, status)
		}
	}
}

func TestAdminHooks(t *testing.T) {
	hooks := &fakeHooks{}
	app, auth := adminHookApp(hooks)

	status, body := hookRequest(t, app, http.MethodGet, "/admin/v1/hooks", nil, auth)
	if status != http.StatusOK || !strings.Contains(body, `{"hooks":[{"id":"`+hookID+`","builtIn":true,"name":"github"`) ||
		!strings.Contains(body, `"inputs":[{"name":"webhookSecret","type":"string","secret":true,"required":true}]`) {
		t.Errorf("list = %d %s", status, body)
	}

	status, body = hookRequest(t, app, http.MethodPost, "/admin/v1/hooks", []byte("\x00asm"), with(auth, "Content-Type", "application/wasm"))
	if status != http.StatusCreated || string(hooks.wasm) != "\x00asm" || !strings.Contains(body, `"version":"1.0.0"`) {
		t.Errorf("create = %d %s, wasm %q", status, body, hooks.wasm)
	}

	// Gzipped, as a Go hook must be to fit the body limit.
	status, _ = hookRequest(t, app, http.MethodPut, "/admin/v1/hooks/"+hookID, gzipped([]byte("\x00asm2")), with(auth, "Content-Encoding", "GZIP"))
	if status != http.StatusOK || string(hooks.wasm) != "\x00asm2" || hooks.id != 7 {
		t.Errorf("upgrade = %d, wasm %q, id %d", status, hooks.wasm, hooks.id)
	}

	status, body = hookRequest(t, app, http.MethodPatch, "/admin/v1/hooks/"+hookID, []byte(`{"enabled":true}`), with(auth, "Content-Type", "application/json"))
	if status != http.StatusOK || !hooks.enabled || !strings.Contains(body, `"enabled":true`) {
		t.Errorf("enable = %d %s", status, body)
	}

	hooks.id = 0
	if status, _ = hookRequest(t, app, http.MethodDelete, "/admin/v1/hooks/"+hookID, nil, auth); status != http.StatusNoContent || hooks.id != 7 {
		t.Errorf("delete = %d, id %d", status, hooks.id)
	}
}

func TestAdminHooksRefuseABadRequest(t *testing.T) {
	app, auth := adminHookApp(&fakeHooks{})
	json := with(auth, "Content-Type", "application/json")
	for name, r := range map[string]struct {
		method, url string
		body        []byte
		header      map[string]string
	}{
		"an empty module":        {http.MethodPost, "/admin/v1/hooks", nil, auth},
		"another encoding":       {http.MethodPost, "/admin/v1/hooks", []byte("x"), with(auth, "Content-Encoding", "br")},
		"not gzip":               {http.MethodPost, "/admin/v1/hooks", []byte("x"), with(auth, "Content-Encoding", "gzip")},
		"a cut gzip":             {http.MethodPost, "/admin/v1/hooks", gzipped([]byte("abcdefgh"))[:15], with(auth, "Content-Encoding", "gzip")},
		"an upgrade of no id":    {http.MethodPut, "/admin/v1/hooks/!", []byte("x"), auth},
		"an upgrade of nothing":  {http.MethodPut, "/admin/v1/hooks/" + hookID, nil, auth},
		"a switch of no id":      {http.MethodPatch, "/admin/v1/hooks/!", []byte(`{"enabled":true}`), json},
		"a switch with no value": {http.MethodPatch, "/admin/v1/hooks/" + hookID, []byte(`{}`), json},
		"a switch not JSON":      {http.MethodPatch, "/admin/v1/hooks/" + hookID, []byte(`{`), json},
		"a delete of no id":      {http.MethodDelete, "/admin/v1/hooks/!", nil, auth},
	} {
		if status, body := hookRequest(t, app, r.method, r.url, r.body, r.header); status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, status, body)
		}
	}
}

func TestAdminHooksCutAnOversizedModule(t *testing.T) {
	hooks := &fakeHooks{}
	app, auth := adminHookApp(hooks)
	big := gzipped(make([]byte, hookrunner.MaxModuleSize+100))
	if status, _ := hookRequest(t, app, http.MethodPost, "/admin/v1/hooks", big, with(auth, "Content-Encoding", "gzip")); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	// One byte past the cap is enough for the runner to refuse it.
	if len(hooks.wasm) != hookrunner.MaxModuleSize+1 {
		t.Errorf("module = %d bytes", len(hooks.wasm))
	}
}

func TestAdminHooksAnswerTheControllersError(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		text   string
	}{
		"invalid":   {&entity.HookError{Kind: entity.HookInvalid, Message: "invalid hook: no"}, http.StatusUnprocessableEntity, "invalid hook: no"},
		"conflict":  {&entity.HookError{Kind: entity.HookConflict, Message: "registered"}, http.StatusConflict, "registered"},
		"not found": {base.ErrNotFound, http.StatusNotFound, "not found"},
		"failed":    {errors.New("disk"), http.StatusInternalServerError, "internal server error"},
	} {
		app, auth := adminHookApp(&fakeHooks{err: tc.err})
		json := with(auth, "Content-Type", "application/json")
		for _, r := range []struct {
			method, url string
			body        []byte
			header      map[string]string
		}{
			{http.MethodGet, "/admin/v1/hooks", nil, auth},
			{http.MethodPost, "/admin/v1/hooks", []byte("x"), auth},
			{http.MethodPut, "/admin/v1/hooks/" + hookID, []byte("x"), auth},
			{http.MethodPatch, "/admin/v1/hooks/" + hookID, []byte(`{"enabled":false}`), json},
			{http.MethodDelete, "/admin/v1/hooks/" + hookID, nil, auth},
		} {
			status, body := hookRequest(t, app, r.method, r.url, r.body, r.header)
			if status != tc.status || !strings.Contains(body, tc.text) {
				t.Errorf("%s: %s %s = %d %s", name, r.method, r.url, status, body)
			}
		}
	}
}
