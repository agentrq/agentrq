// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// adminCrud answers the operator's machine count.
type adminCrud struct {
	crud.Controller
	machines map[string]int
	err      error
}

func (a adminCrud) CountOnlineMachinesPerUser(context.Context) (map[string]int, error) {
	return a.machines, a.err
}

// adminApp serves the admin routes ahead of a middleware that refuses
// everything, so a pass proves they need the admin session and no user login.
func adminApp(rootToken string, mgr *fakeMCPManager) *fiber.App {
	return adminAppWith(rootToken, mgr, adminCrud{})
}

func adminAppWith(rootToken string, mgr *fakeMCPManager, ctrl adminCrud) *fiber.App {
	app := fiber.New()
	h := &handler{rootToken: rootToken, mcpManager: mgr, crud: ctrl, basePath: "/base"}
	h.registerAdminRoutes(app)
	app.Use(func(c *fiber.Ctx) error { return c.SendStatus(http.StatusForbidden) })
	return app
}

func adminRequest(t *testing.T, app *fiber.App, method, url, body string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func getAdminStats(t *testing.T, app *fiber.App, header map[string]string) (int, string) {
	t.Helper()
	resp, body := adminRequest(t, app, http.MethodGet, "/admin/v1/stats", "", header)
	return resp.StatusCode, body
}

func adminLogin(t *testing.T, app *fiber.App, rootToken string, header map[string]string) (*http.Response, string) {
	t.Helper()
	return adminRequest(t, app, http.MethodPost, "/auth/admin/login", `{"rootToken":"`+rootToken+`"}`, header)
}

func adminCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == _adminCookie {
			return c
		}
	}
	return nil
}

func TestAdminLogin_IssuesATokenAsACookieAndForScripts(t *testing.T) {
	app := adminAppWith("s3cret", &fakeMCPManager{connections: map[string]int{"bob": 1, "alice": 3}}, adminCrud{machines: map[string]int{"bob": 2, "carol": 1}})
	resp, body := adminLogin(t, app, "s3cret", nil)
	cookie := adminCookie(resp)
	if resp.StatusCode != http.StatusOK || cookie == nil {
		t.Fatalf("login answered %d %s with cookie %v, want 200 and an admin cookie", resp.StatusCode, body, cookie)
	}
	if cookie.Path != "/base/admin/v1" || !cookie.HttpOnly {
		t.Errorf("cookie path %q httpOnly %v, want /base/admin/v1 and httpOnly", cookie.Path, cookie.HttpOnly)
	}
	if left := time.Until(cookie.Expires); left <= 0 || left > _adminSessionTTL {
		t.Errorf("cookie expires in %v, want within %v", left, _adminSessionTTL)
	}
	var out struct{ Token string }
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Token != cookie.Value {
		t.Fatalf("login answered %s, want the cookie's token %q", body, cookie.Value)
	}

	want := `{"mcpConnections":4,"machines":3,"users":[` +
		`{"userId":"alice","mcpConnections":3,"machines":0},` +
		`{"userId":"bob","mcpConnections":1,"machines":2},` +
		`{"userId":"carol","mcpConnections":0,"machines":1}]}`
	for name, header := range map[string]map[string]string{
		"cookie": {"Cookie": _adminCookie + "=" + cookie.Value},
		"bearer": {"Authorization": "Bearer " + out.Token},
	} {
		if status, body := getAdminStats(t, app, header); status != http.StatusOK || body != want {
			t.Errorf("%s: answered %d %s, want 200 %s", name, status, body, want)
		}
	}
}

func TestAdminLogin_RefusesAWrongTokenOrABadPayload(t *testing.T) {
	for name, tc := range map[string]struct {
		rootToken, body string
		status          int
	}{
		"wrong token":          {rootToken: "s3cret", body: `{"rootToken":"guess"}`, status: http.StatusUnauthorized},
		"no root token is set": {body: `{"rootToken":""}`, status: http.StatusUnauthorized},
		"not json":             {rootToken: "s3cret", body: `{`, status: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			resp, _ := adminRequest(t, adminApp(tc.rootToken, &fakeMCPManager{}), http.MethodPost, "/auth/admin/login", tc.body, nil)
			if resp.StatusCode != tc.status || len(resp.Cookies()) != 0 {
				t.Errorf("answered %d with %d cookies, want %d and none", resp.StatusCode, len(resp.Cookies()), tc.status)
			}
		})
	}
}

// One try per client every ten minutes, right or wrong: the root token is
// accepted nowhere else, so this is all the guessing anyone gets.
func TestAdminLogin_AllowsOneTryPerClientInTheWindow(t *testing.T) {
	app := adminApp("s3cret", &fakeMCPManager{})
	first := map[string]string{"X-Forwarded-For": "203.0.113.7, 10.0.0.1"}
	if resp, _ := adminLogin(t, app, "guess", first); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a first wrong try answered %d, want 401", resp.StatusCode)
	}
	resp, body := adminLogin(t, app, "s3cret", first)
	if resp.StatusCode != http.StatusTooManyRequests || adminCookie(resp) != nil || body != `{"error":"too many attempts"}` {
		t.Fatalf("a second try answered %d %s, want 429 and no session", resp.StatusCode, body)
	}
	if after, _ := strconv.Atoi(resp.Header.Get("Retry-After")); after < 590 || after > 601 {
		t.Errorf("Retry-After = %q, want about ten minutes", resp.Header.Get("Retry-After"))
	}

	for name, header := range map[string]map[string]string{
		"a forwarded-for header of its own": {"X-Forwarded-For": "203.0.113.8"},
		"a real-ip header of its own":       {"X-Real-IP": "198.51.100.2"},
		"no header":                         nil,
	} {
		if resp, _ := adminLogin(t, app, "s3cret", header); resp.StatusCode != http.StatusTooManyRequests {
			t.Errorf("the same connection with %s answered %d, want 429: headers it sets itself must not buy another try", name, resp.StatusCode)
		}
	}
}

// Only a proxy on the host or the private network is believed, and only for
// the hop it wrote itself: the last X-Forwarded-For entry, which the client
// cannot choose, then X-Real-IP.
func TestForwardedClient_BelievesOnlyTheNearestPrivateProxy(t *testing.T) {
	for name, tc := range map[string]struct {
		peer, forwardedFor, realIP, want string
	}{
		"a public peer's own headers are ignored":     {"203.0.113.7", "198.51.100.1", "198.51.100.2", "203.0.113.7"},
		"a public IPv6 peer is named as it is":        {"2001:db8::1", "198.51.100.1", "", "2001:db8::1"},
		"a loopback proxy's last hop is the client":   {"127.0.0.1", "198.51.100.1, 203.0.113.9", "", "203.0.113.9"},
		"an IPv6 loopback proxy is believed too":      {"::1", " 203.0.113.9 ", "", "203.0.113.9"},
		"a private proxy's forwarded-for comes first": {"10.0.0.5", "203.0.113.9", "198.51.100.2", "203.0.113.9"},
		"a private proxy's real-ip is the fallback":   {"192.168.1.2", "", "203.0.113.9", "203.0.113.9"},
		"a private peer with no headers is itself":    {"172.16.0.3", "", "", "172.16.0.3"},
	} {
		if got := forwardedClient(net.ParseIP(tc.peer), tc.forwardedFor, tc.realIP); got != tc.want {
			t.Errorf("%s: forwardedClient named %q, want %q", name, got, tc.want)
		}
	}
}

func TestLoginAttempts_ReopensAfterTheWindowAndForgetsOldClients(t *testing.T) {
	var l loginAttempts
	start := time.Unix(1_000_000, 0)
	if ok, _ := l.allow("a", start, time.Minute); !ok {
		t.Fatal("a first try was refused")
	}
	if ok, wait := l.allow("a", start.Add(20*time.Second), time.Minute); ok || wait != 40*time.Second {
		t.Fatalf("a try inside the window: allowed %v, wait %v; want refused with 40s left", ok, wait)
	}
	if ok, _ := l.allow("b", start.Add(30*time.Second), time.Minute); !ok {
		t.Fatal("another client's first try was refused")
	}
	if ok, _ := l.allow("a", start.Add(61*time.Second), time.Minute); !ok {
		t.Fatal("a try after the window was refused")
	}
	if _, kept := l.last["b"]; !kept {
		t.Error("a client still inside its window was forgotten")
	}
	l.allow("c", start.Add(200*time.Second), time.Minute)
	if len(l.last) != 1 {
		t.Errorf("remembered %v, want only the latest client once the others' windows passed", l.last)
	}
}

func TestAdminLogout_ExpiresTheCookieAtItsPath(t *testing.T) {
	resp, _ := adminRequest(t, adminApp("s3cret", &fakeMCPManager{}), http.MethodPost, "/auth/admin/logout", "", nil)
	cookie := adminCookie(resp)
	if resp.StatusCode != http.StatusOK || cookie == nil || cookie.Value != "" ||
		cookie.Path != "/base/admin/v1" || !cookie.Expires.Before(time.Now()) {
		t.Errorf("answered %d with %+v, want 200 and an expired admin cookie", resp.StatusCode, cookie)
	}
}

func TestGetAdminStats_AnswersAFailedMachineCount(t *testing.T) {
	app := adminAppWith("s3cret", &fakeMCPManager{}, adminCrud{err: errors.New("database unavailable")})
	token := (&handler{rootToken: "s3cret"}).adminToken(time.Now().Add(time.Minute))
	if status, body := getAdminStats(t, app, map[string]string{"Authorization": "Bearer " + token}); status != http.StatusInternalServerError {
		t.Errorf("answered %d %s, want 500", status, body)
	}
}

func TestGetAdminStats_RefusesWithoutAnAdminToken(t *testing.T) {
	signer := &handler{rootToken: "s3cret"}
	valid := signer.adminToken(time.Now().Add(time.Minute))
	sign := func(method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	soon := jwt.NewNumericDate(time.Now().Add(time.Minute))
	admin := jwt.ClaimStrings{_adminAudience}
	for name, tc := range map[string]struct {
		rootToken string
		header    map[string]string
	}{
		"nothing":                     {rootToken: "s3cret"},
		"the raw root token":          {rootToken: "s3cret", header: map[string]string{"Authorization": "Bearer s3cret"}},
		"a token but not as a bearer": {rootToken: "s3cret", header: map[string]string{"Authorization": valid}},
		"not a jwt":                   {rootToken: "s3cret", header: map[string]string{"Cookie": _adminCookie + "=abc"}},
		"expired":                     {rootToken: "s3cret", header: map[string]string{"Authorization": "Bearer " + signer.adminToken(time.Now().Add(-time.Minute))}},
		"no expiry":                   {rootToken: "s3cret", header: map[string]string{"Authorization": "Bearer " + sign(jwt.SigningMethodHS256, signer.adminKey(), jwt.RegisteredClaims{Audience: admin})}},
		"another audience":            {rootToken: "s3cret", header: map[string]string{"Authorization": "Bearer " + sign(jwt.SigningMethodHS256, signer.adminKey(), jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"users"}, ExpiresAt: soon})}},
		"another algorithm":           {rootToken: "s3cret", header: map[string]string{"Authorization": "Bearer " + sign(jwt.SigningMethodHS512, signer.adminKey(), jwt.RegisteredClaims{Audience: admin, ExpiresAt: soon})}},
		"signed with the raw token":   {rootToken: "s3cret", header: map[string]string{"Cookie": _adminCookie + "=" + sign(jwt.SigningMethodHS256, []byte("s3cret"), jwt.RegisteredClaims{Audience: admin, ExpiresAt: soon})}},
		"signed with an old token":    {rootToken: "rotated", header: map[string]string{"Authorization": "Bearer " + valid}},
		"no root token is set":        {header: map[string]string{"Authorization": "Bearer " + (&handler{}).adminToken(time.Now().Add(time.Minute))}},
	} {
		t.Run(name, func(t *testing.T) {
			status, body := getAdminStats(t, adminApp(tc.rootToken, &fakeMCPManager{connections: map[string]int{"alice": 1}}), tc.header)
			if status != http.StatusUnauthorized || body != `{"error":"admin session required"}` {
				t.Errorf("answered %d %s, want 401 admin session required", status, body)
			}
		})
	}
}
