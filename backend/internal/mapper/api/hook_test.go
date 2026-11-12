// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
)

func TestFromHookEntitiesToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC)
	h := entity.Hook{
		ID: 1, BuiltIn: true, Name: "github", Version: "1.0.0", Description: "GitHub", SHA256: "ab", SizeBytes: 9, ABIVersion: 1,
		Inputs:  []entity.HookInput{{Name: "branches", Label: "Branches", Type: "array", Items: "string"}, {Name: "s", Type: "string", Secret: true, Required: true}},
		Events:  []entity.HookEvent{{Name: "ci.failed", Description: "A run failed"}, {Name: "push"}},
		Enabled: true, CreatedAt: at, UpdatedAt: at,
	}
	want := `{"hooks":[{"id":"00000000001","builtIn":true,"name":"github","version":"1.0.0","description":"GitHub","sha256":"ab","sizeBytes":9,"abiVersion":1,` +
		`"inputs":[{"name":"branches","label":"Branches","type":"array","items":"string","secret":false,"required":false},{"name":"s","type":"string","secret":true,"required":true}],` +
		`"events":[{"name":"ci.failed","description":"A run failed"},{"name":"push"}],"enabled":true,"createdAt":"2026-11-12T04:00:00Z","updatedAt":"2026-11-12T04:00:00Z"}]}`
	if got := string(FromHookEntitiesToHTTPResponse([]entity.Hook{h})); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := string(FromHookEntitiesToHTTPResponse(nil)); got != `{"hooks":[]}` {
		t.Errorf("none = %s", got)
	}
	if got := string(FromHookEntityToHTTPResponse(entity.Hook{ID: 2})); got[:20] != `{"id":"00000000002",` {
		t.Errorf("one = %s", got)
	}
}

func TestFromErrorToHTTPResponseAnswersAHookError(t *testing.T) {
	for kind, want := range map[entity.HookErrorKind]int{entity.HookInvalid: http.StatusUnprocessableEntity, entity.HookConflict: http.StatusConflict,
		entity.HookTooLarge: http.StatusRequestEntityTooLarge, entity.HookThrottled: http.StatusTooManyRequests,
		entity.HookForbidden: http.StatusForbidden} {
		body, status := FromErrorToHTTPResponse(&entity.HookError{Kind: kind, Message: "why"})
		if status != want || string(body) != `{"error":{"message":"why","code":`+strconv.Itoa(want)+`}}` {
			t.Errorf("kind %d: %d %s", kind, status, body)
		}
	}
}

func TestFromHookRegistrationEntityToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC)
	reg := entity.HookRegistration{
		ID: 3, HookID: 1, HookName: "github", HookAvailable: true, Name: "CI", Enabled: true,
		Inputs: map[string]any{"branches": []any{"main"}}, SecretsSet: []string{"webhookSecret"},
		Routes: map[string]int64{"ci.failed": 5}, URL: "https://rq/api/v1/hooks/in/00000000003",
		URLWithToken: "https://rq/api/v1/hooks/in/00000000003?agentrq_hook_token=abcd1234", AllowAllCommandsEvents: []string{"ci.failed"},
		CreatedAt: at, UpdatedAt: at,
	}
	want := `{"id":"00000000003","hookId":"00000000001","hookName":"github","hookAvailable":true,"name":"CI","enabled":true,` +
		`"inputs":{"branches":["main"]},"secretsSet":["webhookSecret"],"routes":{"ci.failed":"00000000005"},` +
		`"url":"https://rq/api/v1/hooks/in/00000000003","urlWithToken":"https://rq/api/v1/hooks/in/00000000003?agentrq_hook_token=abcd1234",` +
		`"allowAllCommandsEvents":["ci.failed"],"createdAt":"2026-11-12T04:00:00Z","updatedAt":"2026-11-12T04:00:00Z"}`
	if got := string(FromHookRegistrationEntityToHTTPResponse(reg)); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// Nothing set is empty, never null; no token, no urlWithToken.
	got := string(FromHookRegistrationEntitiesToHTTPResponse([]entity.HookRegistration{{ID: 4, CreatedAt: at, UpdatedAt: at}}))
	want = `{"registrations":[{"id":"00000000004","hookId":"00000000000","hookName":"","hookAvailable":false,"name":"","enabled":false,` +
		`"inputs":{},"secretsSet":[],"routes":{},"url":"","allowAllCommandsEvents":[],"createdAt":"2026-11-12T04:00:00Z","updatedAt":"2026-11-12T04:00:00Z"}]}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := string(FromHookRegistrationEntitiesToHTTPResponse(nil)); got != `{"registrations":[]}` {
		t.Errorf("none = %s", got)
	}
}

func TestFromHTTPRequestToHookRegistrationRequestEntity(t *testing.T) {
	parse := func(body string) *entity.HookRegistrationRequest {
		app := fiber.New()
		var rq *entity.HookRegistrationRequest
		app.Post("/", func(c *fiber.Ctx) error {
			rq = FromHTTPRequestToHookRegistrationRequestEntity(c)
			return nil
		})
		if _, err := app.Test(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))); err != nil {
			t.Fatal(err)
		}
		return rq
	}

	rq := parse(`{"hookId":"00000000001","name":"CI","enabled":false,"inputs":{"webhookSecret":"s"},"routes":{"push":"00000000005"}}`)
	if rq == nil || rq.HookID != 1 || *rq.Name != "CI" || *rq.Enabled || rq.Inputs["webhookSecret"] != "s" || rq.Routes["push"] != 5 {
		t.Errorf("full = %+v", rq)
	}
	// A field left out stays unset, so an update keeps it.
	if rq = parse(`{}`); rq == nil || rq.HookID != 0 || rq.Name != nil || rq.Enabled != nil || rq.Inputs != nil || rq.Routes != nil {
		t.Errorf("empty = %+v", rq)
	}
	// Routes given empty clear them.
	if rq = parse(`{"routes":{}}`); rq == nil || rq.Routes == nil || len(rq.Routes) != 0 {
		t.Errorf("no routes = %+v", rq)
	}
	for _, body := range []string{`{`, `{"hookId":"!"}`, `{"routes":{"push":"!"}}`, `{"inputs":[]}`} {
		if rq := parse(body); rq != nil {
			t.Errorf("%s = %+v", body, rq)
		}
	}
}

func TestFromHTTPRequestToHookIncomingRequestEntity(t *testing.T) {
	parse := func(url string, header map[string]string) *entity.HookIncoming {
		app := fiber.New()
		var in *entity.HookIncoming
		app.Post("/in/:id", func(c *fiber.Ctx) error {
			in = FromHTTPRequestToHookIncomingRequestEntity(c)
			return nil
		})
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("body"))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
		return in
	}
	in := parse("/in/00000000009?agentrq_hook_token=abcd1234&a=1&a=2", map[string]string{"X-Hub-Signature-256": "sha256=x", "Cookie": "at=s"})
	if in == nil || in.RegistrationID != 9 || in.Token != "abcd1234" || in.Method != "POST" || in.Path != "/in/00000000009" || string(in.Body) != "body" {
		t.Fatalf("in = %+v", in)
	}
	if len(in.Query) != 1 || len(in.Query["a"]) != 2 || in.Headers["x-hub-signature-256"][0] != "sha256=x" || in.Headers["cookie"] != nil || in.Peer != "0.0.0.0" {
		t.Errorf("query %v, headers %v, peer %q", in.Query, in.Headers, in.Peer)
	}
	if in := parse("/in/00000000009", nil); in == nil || in.Token != "" || len(in.Query) != 0 {
		t.Errorf("no token = %+v", in)
	}
	if in := parse("/in/!", nil); in != nil {
		t.Errorf("no id = %+v", in)
	}
}

func TestFromHookDeliveryEntitiesToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)
	got := string(FromHookDeliveryEntitiesToHTTPResponse([]entity.HookDelivery{
		{ID: 11, RegistrationID: 9, ReceivedAt: at, Status: "failed", Attempts: 6, EventNames: []string{"push"}, Error: "boom", DurationMs: 3, Replayable: true},
		{ID: 12, RegistrationID: 9, ReceivedAt: at, Status: "queued"},
	}))
	want := `{"deliveries":[{"id":"0000000000B","registrationId":"00000000009","receivedAt":"2026-11-12T07:00:00Z","status":"failed","attempts":6,"eventNames":["push"],"error":"boom","durationMs":3,"replayable":true},` +
		`{"id":"0000000000C","registrationId":"00000000009","receivedAt":"2026-11-12T07:00:00Z","status":"queued","attempts":0,"eventNames":[],"error":"","durationMs":0,"replayable":false}]}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := string(FromHookDeliveryEntitiesToHTTPResponse(nil)); got != `{"deliveries":[]}` {
		t.Errorf("none = %s", got)
	}
	if got := string(FromHookDeliveryIDToHTTPResponse(11)); got != `{"deliveryId":"0000000000B"}` {
		t.Errorf("id = %s", got)
	}
}

func TestFromAvailableHookEntitiesToHTTPResponse(t *testing.T) {
	if got := string(FromAvailableHookEntitiesToHTTPResponse(nil, false)); got != `{"hooks":[],"allowUserHooks":false}` {
		t.Errorf("none = %s", got)
	}
	if got := string(FromAvailableHookEntitiesToHTTPResponse([]entity.Hook{{ID: 2}}, true)); !strings.HasPrefix(got, `{"hooks":[{"id":"00000000002",`) || !strings.HasSuffix(got, `"allowUserHooks":true}`) {
		t.Errorf("one = %s", got)
	}
}
