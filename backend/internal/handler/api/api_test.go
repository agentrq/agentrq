// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

type mockAuthService struct {
	auth.Service
	exchangeFunc func(ctx context.Context, code string) (*auth.User, error)
}

func (m *mockAuthService) GetAuthURL(state string) string {
	return "https://google.com/auth?state=" + state
}

func (m *mockAuthService) Exchange(ctx context.Context, code string) (*auth.User, error) {
	return m.exchangeFunc(ctx, code)
}

type mockTokenSvc struct {
	auth.TokenService
	createTokenFunc    func(userID, email, name, picture string) (string, error)
	createMCPTokenFunc func(userID, workspaceID, tokenType string) (string, error)
}

func (m *mockTokenSvc) CreateToken(userID, email, name, picture string) (string, error) {
	return m.createTokenFunc(userID, email, name, picture)
}

func (m *mockTokenSvc) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return m.createMCPTokenFunc(userID, workspaceID, tokenType)
}

func (m *mockTokenSvc) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	return redirectURL, nil // passthrough for tests that don't need real JWT signing
}

func (m *mockTokenSvc) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	return tokenStr, nil // treat the raw value as the redirect URL in simple tests
}

type mockCrudController struct {
	crud.Controller
	findOrCreateUserFunc func(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error)
}

func (m *mockCrudController) FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
	return m.findOrCreateUserFunc(ctx, req)
}

// TestSanitizeRedirectURL verifies the open-redirect prevention helper directly.
func TestSanitizeRedirectURL(t *testing.T) {
	h := &handler{baseURL: "http://localhost:3000"}

	tests := []struct {
		input string
		want  string
	}{
		{"/workspaces", "/workspaces"},
		{"http://localhost:3000/safe", "http://localhost:3000/safe"},
		{"//evil.com", "/"},
		{"/\\evil.com", "/"},
		{"http://localhost:3000.evil.com", "/"},
		{"http://evil.com/phish", "/"},
		{"", "/"},
	}

	for _, tt := range tests {
		got := h.sanitizeRedirectURL(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeRedirectURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGoogleCallback_StateJWT(t *testing.T) {
	// Use a real token service so JWT state round-trips correctly.
	realTokenSvc := auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"})

	app := fiber.New()
	authSvc := &mockAuthService{}
	crudCtrl := &mockCrudController{}

	h := &handler{
		auth:     authSvc,
		tokenSvc: realTokenSvc,
		crud:     crudCtrl,
		baseURL:  "http://localhost:3000",
	}
	app.Get("/google/callback", h.googleCallback())

	authSvc.exchangeFunc = func(ctx context.Context, code string) (*auth.User, error) {
		return &auth.User{ID: "123", Email: "test@example.com", Name: "Test"}, nil
	}
	crudCtrl.findOrCreateUserFunc = func(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
		return &entity.FindOrCreateUserResponse{User: entity.User{ID: 1}}, nil
	}

	t.Run("Valid JWT state redirects correctly", func(t *testing.T) {
		state, _ := realTokenSvc.CreateOAuthStateToken("/workspaces", "google")
		req := httptest.NewRequest("GET", "/google/callback?code=valid-code&state="+state, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/workspaces" {
			t.Errorf("expected /workspaces, got %s", loc)
		}
	})

	t.Run("Forged state falls back to /", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/google/callback?code=valid-code&state=forged-not-a-jwt", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("expected /, got %s", loc)
		}
	})

	t.Run("Wrong provider state falls back to /", func(t *testing.T) {
		// State signed for github should be rejected by google callback
		state, _ := realTokenSvc.CreateOAuthStateToken("/workspaces", "github")
		req := httptest.NewRequest("GET", "/google/callback?code=valid-code&state="+state, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("expected /, got %s", loc)
		}
	})

	t.Run("Missing state falls back to /", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/google/callback?code=valid-code", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("expected /, got %s", loc)
		}
	})
}

type mockCrudGetWorkspace struct {
	crud.Controller
	getWorkspaceFunc func(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
}

func (m *mockCrudGetWorkspace) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return m.getWorkspaceFunc(ctx, req)
}

func TestGetWorkspaceToken_Unauthorized(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	tokenSvc := &mockTokenSvc{}

	h := &handler{
		crud:     crudCtrl,
		tokenSvc: tokenSvc,
	}

	app.Get("/api/v1/workspaces/:id/token", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.getWorkspaceToken()(c)
	})

	t.Run("Unauthorized access to workspace", func(t *testing.T) {
		workspaceID := "work1"
		crudCtrl.getWorkspaceFunc = func(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
			// Simulate "not found" or "no access" from repository
			return nil, base.ErrNotFound // Using a known error that maps to 404
		}

		req := httptest.NewRequest("GET", "/api/v1/workspaces/"+workspaceID+"/token", nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})

	t.Run("Authorized access to workspace", func(t *testing.T) {
		workspaceID := "work1"
		crudCtrl.getWorkspaceFunc = func(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
			return &entity.GetWorkspaceResponse{}, nil
		}
		tokenSvc.createMCPTokenFunc = func(userID, workspaceID, tokenType string) (string, error) {
			return "token123", nil
		}

		req := httptest.NewRequest("GET", "/api/v1/workspaces/"+workspaceID+"/token", nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

type mockCrudTaskCounts struct {
	crud.Controller
	getWorkspaceTaskCountsFunc func(ctx context.Context, req entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error)
}

func (m *mockCrudTaskCounts) GetWorkspaceTaskCounts(ctx context.Context, req entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error) {
	return m.getWorkspaceTaskCountsFunc(ctx, req)
}

type mockCrudListTasks struct {
	crud.Controller
	listTasksFunc func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error)
}

func (m *mockCrudListTasks) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	return m.listTasksFunc(ctx, req)
}

func TestListTasks_InvalidWorkspaceID(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudListTasks{}
	called := false

	h := &handler{
		crud: crudCtrl,
	}

	app.Get("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.listTasks()(c)
	})

	crudCtrl.listTasksFunc = func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		called = true
		return &entity.ListTasksResponse{}, nil
	}

	req := httptest.NewRequest("GET", "/api/v1/workspaces/!/tasks", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("Expected status 422, got %d", resp.StatusCode)
	}
	if called {
		t.Fatal("ListTasks should not be called for invalid workspace IDs")
	}
}

func TestListTasks_GlobalRouteAllowsMissingWorkspaceID(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudListTasks{}

	h := &handler{
		crud: crudCtrl,
	}

	app.Get("/api/v1/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.listTasks()(c)
	})

	crudCtrl.listTasksFunc = func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		if req.WorkspaceID != 0 {
			t.Fatalf("expected global task list workspace ID 0, got %d", req.WorkspaceID)
		}
		if req.UserID != "user1" {
			t.Fatalf("expected user ID user1, got %s", req.UserID)
		}
		return &entity.ListTasksResponse{}, nil
	}

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestGetWorkspaceTaskCounts(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudTaskCounts{}

	h := &handler{
		crud: crudCtrl,
	}

	app.Get("/api/v1/workspaces/:id/tasks/counts", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.getWorkspaceTaskCounts()(c)
	})

	t.Run("Success fetching counts", func(t *testing.T) {
		crudCtrl.getWorkspaceTaskCountsFunc = func(ctx context.Context, req entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error) {
			return map[string]int64{
				"ongoing":    2,
				"notstarted": 3,
			}, nil
		}

		req := httptest.NewRequest("GET", "/api/v1/workspaces/work1/tasks/counts", nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

type mockCrudWorkspaceAccess struct {
	crud.Controller
	checkWorkspaceAccessFunc func(ctx context.Context, id int64, userID string) (bool, error)
	// Only the paths that succeed reach telemetry, so this stayed nil until
	// there was a test that got that far. Left optional: a refusal never counts
	// anything, and a test about a refusal should not have to say so.
	recordTelemetryFunc func(ctx context.Context, rq entity.RecordTelemetryRequest) error
}

func (m *mockCrudWorkspaceAccess) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	return m.checkWorkspaceAccessFunc(ctx, id, userID)
}

func (m *mockCrudWorkspaceAccess) RecordTelemetry(ctx context.Context, rq entity.RecordTelemetryRequest) error {
	if m.recordTelemetryFunc == nil {
		return nil
	}
	return m.recordTelemetryFunc(ctx, rq)
}

func TestSendPermissionVerdict_RequiresWorkspaceAccess(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{}

	h := &handler{
		crud: crudCtrl,
		// Intentionally leave MCPManager nil: unauthorized requests must fail
		// before any permission verdict can be dispatched to a workspace server.
	}

	workspaceID := monoflake.ID(1).String()
	taskID := monoflake.ID(2).String()
	userID := monoflake.ID(100).String()

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/permission", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.sendPermissionVerdict()(c)
	})

	crudCtrl.checkWorkspaceAccessFunc = func(ctx context.Context, id int64, gotUserID string) (bool, error) {
		if id != 1 {
			t.Fatalf("expected workspace ID 1, got %d", id)
		}
		if gotUserID != userID {
			t.Fatalf("expected user ID %s, got %s", userID, gotUserID)
		}
		return false, nil
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+workspaceID+"/tasks/"+taskID+"/permission",
		bytes.NewBufferString(`{"requestId":"req-1","behavior":"allow"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", resp.StatusCode)
	}
}

func TestStopTask_RequiresWorkspaceAccess(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{}

	// Intentionally no MCPManager: an unauthorized request must be refused
	// before anything reaches a workspace server.
	h := &handler{crud: crudCtrl}

	workspaceID := monoflake.ID(1).String()
	taskID := monoflake.ID(2).String()
	userID := monoflake.ID(100).String()

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/stop", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.stopTask()(c)
	})

	crudCtrl.checkWorkspaceAccessFunc = func(ctx context.Context, id int64, gotUserID string) (bool, error) {
		return false, nil
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+workspaceID+"/tasks/"+taskID+"/stop",
		nil,
	)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestStopTask_RejectsATaskIDThatIsNotOne(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	h := &handler{crud: crudCtrl}

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/stop", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.stopTask()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/0/stop",
		nil,
	)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// A verdict claiming to come from something that could not be an extension is a
// bad request, not a server error.
//
// `decidedBy` is written into the permission message's metadata and drawn in the
// task feed as the thing that made a decision. The caller is already
// authenticated and already allowed to answer this request, so nothing here is
// about privilege — it is that an identifier rendered in the feed has to be an
// identifier. Answering 500 would also have the desktop app retrying something
// that can never succeed.
func TestSendPermissionVerdict_RefusesADeciderThatIsNotAnExtension(t *testing.T) {
	cases := map[string]string{
		"a name with markup in it":          `{"requestId":"req-1","behavior":"allow","decidedBy":"<b>you</b>"}`,
		"a standing rule from an extension": `{"requestId":"req-1","behavior":"allow_always","decidedBy":"guardrail"}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			h := &handler{
				crud: crudCtrl,
				// A real server, because the refusal under test is the real
				// one: this is checked before anything about the request is
				// looked up, and a double here would be asserting on the
				// double. The fake manager only stands in for the lookup.
				mcpManager: &fakeMCPManager{server: &mcpctrl.WorkspaceServer{}},
			}

			app.Post("/api/v1/workspaces/:id/tasks/:taskID/permission", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.sendPermissionVerdict()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(2).String()+"/permission",
				bytes.NewBufferString(body),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", resp.StatusCode)
			}
		})
	}
}

// Nothing connected can be stopped, so the request is refused rather than
// answered with a success the human would read as "the task stopped".
func TestStopTask_RefusesWhenTheAgentCannotBeStopped(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	h := &handler{
		crud: crudCtrl,
		// A real workspace server with nothing connected to it. The 409 under
		// test is the controller's own answer — that a server holding no
		// session cannot stop anything — so the real one has to give it.
		mcpManager: &fakeMCPManager{server: &mcpctrl.WorkspaceServer{}},
	}

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/stop", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.stopTask()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(2).String()+"/stop",
		nil,
	)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

// No server for the workspace at all: there is nothing even to ask.
func TestStopTask_WithoutAWorkspaceServer(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	h := &handler{
		crud: crudCtrl,
		// No server for the workspace: the zero value of the fake's server
		// field is a nil interface, which is what "nothing to ask" looks like.
		mcpManager: &fakeMCPManager{},
	}

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/stop", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.stopTask()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(2).String()+"/stop",
		nil,
	)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRespondToElicitation_RequiresWorkspaceAccess(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{}

	h := &handler{
		crud: crudCtrl,
		// Intentionally leave MCPManager nil: unauthorized requests must fail
		// before any elicitation response can be dispatched to a workspace server.
	}

	workspaceID := monoflake.ID(1).String()
	taskID := monoflake.ID(2).String()
	userID := monoflake.ID(100).String()

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/elicitation", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.respondToElicitation()(c)
	})

	crudCtrl.checkWorkspaceAccessFunc = func(ctx context.Context, id int64, gotUserID string) (bool, error) {
		if id != 1 {
			t.Fatalf("expected workspace ID 1, got %d", id)
		}
		if gotUserID != userID {
			t.Fatalf("expected user ID %s, got %s", userID, gotUserID)
		}
		return false, nil
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+workspaceID+"/tasks/"+taskID+"/elicitation",
		bytes.NewBufferString(`{"requestId":"req-1","action":"accept"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", resp.StatusCode)
	}
}

func TestRespondToElicitation_InvalidPayload(t *testing.T) {
	app := fiber.New()
	h := &handler{}

	app.Post("/api/v1/workspaces/:id/tasks/:taskID/elicitation", h.respondToElicitation())

	tests := []struct {
		name string
		body string
	}{
		{"malformed JSON", `not json`},
		{"missing requestId", `{"action":"accept"}`},
		{"invalid action", `{"requestId":"req-1","action":"maybe"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(2).String()+"/elicitation",
				bytes.NewBufferString(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", resp.StatusCode)
			}
		})
	}
}

// refreshTokenSvc is the minimum TokenService the refresh handler touches.
type refreshTokenSvc struct {
	auth.TokenService
	validateRefreshFunc func(tokenStr string) (*auth.Claims, error)
	createTokenFunc     func(userID, email, name, picture string) (string, error)
	createRefreshFunc   func(userID string) (string, error)
}

func (m *refreshTokenSvc) ValidateRefreshToken(tokenStr string) (*auth.Claims, error) {
	return m.validateRefreshFunc(tokenStr)
}

func (m *refreshTokenSvc) CreateToken(userID, email, name, picture string) (string, error) {
	return m.createTokenFunc(userID, email, name, picture)
}

func (m *refreshTokenSvc) CreateRefreshToken(userID string) (string, error) {
	if m.createRefreshFunc == nil {
		return "new-refresh", nil
	}
	return m.createRefreshFunc(userID)
}

type refreshCrud struct {
	crud.Controller
	findUserByIDFunc func(ctx context.Context, id int64) (entity.User, error)
}

func (m *refreshCrud) FindUserByID(ctx context.Context, id int64) (entity.User, error) {
	return m.findUserByIDFunc(ctx, id)
}

// TestRefreshCookiePath pins the refresh cookie to the route that reads it.
//
// This is the one part of the flow that fails silently: if the path does not
// match where the route is mounted, the browser simply never sends the cookie,
// refreshing never happens, and sessions expire exactly as they did before —
// with nothing in any log to say why.
func TestRefreshCookiePath(t *testing.T) {
	if got, want := (&handler{}).refreshCookiePath(), _routeBasePath+"/auth/refresh"; got != want {
		t.Errorf("refreshCookiePath() = %q, want %q — the route is mounted at %q", got, want, want)
	}

	// A reverse-proxied deployment serves the API under a prefix; the cookie
	// has to carry it or the browser will not match the request path.
	if got, want := (&handler{basePath: "/agentrq"}).refreshCookiePath(), "/agentrq/api/v1/auth/refresh"; got != want {
		t.Errorf("with a base path: got %q, want %q", got, want)
	}
}

func TestRefreshSession(t *testing.T) {
	newApp := func(h *handler) *fiber.App {
		app := fiber.New()
		app.Post("/api/v1/auth/refresh", h.refreshSession())
		return app
	}

	post := func(app *fiber.App, cookie string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		if cookie != "" {
			req.Header.Set("Cookie", "rt="+cookie)
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		return res
	}

	t.Run("renews a session and reissues both cookies", func(t *testing.T) {
		h := &handler{
			tokenSvc: &refreshTokenSvc{
				validateRefreshFunc: func(string) (*auth.Claims, error) {
					return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "1"}}, nil
				},
				createTokenFunc: func(userID, email, name, picture string) (string, error) {
					if email != "a@b.com" {
						t.Errorf("access token minted with email %q, want the one from the database", email)
					}
					return "new-access", nil
				},
			},
			crud: &refreshCrud{
				findUserByIDFunc: func(context.Context, int64) (entity.User, error) {
					return entity.User{ID: 1, Email: "a@b.com", Name: "Ada"}, nil
				},
			},
		}

		res := post(newApp(h), "a-valid-refresh-token")

		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		cookies := res.Header.Values("Set-Cookie")
		joined := strings.Join(cookies, "\n")
		if !strings.Contains(joined, "at=new-access") {
			t.Errorf("no new access cookie in %q", joined)
		}
		if !strings.Contains(joined, "rt=new-refresh") {
			t.Errorf("refresh cookie was not rotated: %q", joined)
		}
	})

	t.Run("refuses and clears when the token is rejected", func(t *testing.T) {
		// Clearing matters: leaving a dead refresh cookie in place makes the
		// client retry a credential that can never work again.
		h := &handler{
			tokenSvc: &refreshTokenSvc{
				validateRefreshFunc: func(string) (*auth.Claims, error) {
					return nil, errors.New("expired")
				},
			},
		}

		res := post(newApp(h), "an-expired-token")

		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
		if joined := strings.Join(res.Header.Values("Set-Cookie"), "\n"); !strings.Contains(joined, "rt=;") {
			t.Errorf("refresh cookie was not cleared: %q", joined)
		}
	})

	t.Run("refuses when there is no cookie at all", func(t *testing.T) {
		h := &handler{
			tokenSvc: &refreshTokenSvc{
				validateRefreshFunc: func(tokenStr string) (*auth.Claims, error) {
					if tokenStr != "" {
						t.Errorf("expected an empty token, got %q", tokenStr)
					}
					return nil, errors.New("no token")
				},
			},
		}

		if res := post(newApp(h), ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("refuses a token that outlived its account", func(t *testing.T) {
		h := &handler{
			tokenSvc: &refreshTokenSvc{
				validateRefreshFunc: func(string) (*auth.Claims, error) {
					return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "1"}}, nil
				},
			},
			crud: &refreshCrud{
				findUserByIDFunc: func(context.Context, int64) (entity.User, error) {
					return entity.User{}, errors.New("not found")
				},
			},
		}

		if res := post(newApp(h), "valid-but-orphaned"); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", res.StatusCode)
		}
	})
}

// TestSignInIssuesBothCookies covers what a sign-in actually puts in the
// browser. The refresh half is invisible in normal use — nothing breaks without
// it until a day later — so a login that quietly set only the access cookie
// would look completely healthy right up until everyone was signed out.
func TestSignInIssuesBothCookies(t *testing.T) {
	h := &handler{
		rootLoginEnabled: true,
		rootToken:        "root-secret",
		crud: &mockCrudController{
			findOrCreateUserFunc: func(context.Context, entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
				return &entity.FindOrCreateUserResponse{User: entity.User{ID: 1, Email: "root@agentrq.local"}}, nil
			},
		},
		tokenSvc: &refreshTokenSvc{
			createTokenFunc:   func(string, string, string, string) (string, error) { return "access-token", nil },
			createRefreshFunc: func(string) (string, error) { return "refresh-token", nil },
		},
	}

	app := fiber.New()
	app.Post("/api/v1/auth/root/login", h.rootLogin())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/root/login",
		bytes.NewBufferString(`{"rootToken":"root-secret"}`))
	req.Header.Set("Content-Type", "application/json")

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	joined := strings.Join(res.Header.Values("Set-Cookie"), "\n")
	if !strings.Contains(joined, "at=access-token") {
		t.Errorf("no access cookie: %q", joined)
	}
	if !strings.Contains(joined, "rt=refresh-token") {
		t.Errorf("no refresh cookie — signing in set only half a session: %q", joined)
	}
	if !strings.Contains(joined, "path=/api/v1/auth/refresh") {
		t.Errorf("refresh cookie is not scoped to the route that reads it: %q", joined)
	}
}

func TestAgentCommandsEntity(t *testing.T) {
	t.Run("nil stays nil, so nothing is advertised", func(t *testing.T) {
		if got := agentCommandsEntity(nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("carries the snapshot across into the API layer's own shape", func(t *testing.T) {
		got := agentCommandsEntity(&mcpctrl.AgentCommandsSnapshot{
			SessionID: "acp-session",
			Commands: []mcpctrl.AgentCommand{
				{Name: "review", Description: "Review the diff", Hint: "what to review"},
				{Name: "init"},
			},
		})

		if got == nil || len(got.Commands) != 2 {
			t.Fatalf("got %+v", got)
		}
		if got.Commands[0].Name != "review" || got.Commands[0].Description != "Review the diff" || got.Commands[0].Hint != "what to review" {
			t.Errorf("commands[0] = %+v", got.Commands[0])
		}
		if got.Commands[1].Description != "" || got.Commands[1].Hint != "" {
			t.Errorf("optional fields should stay empty: %+v", got.Commands[1])
		}
	})
}

func TestAgentClientEntity(t *testing.T) {
	t.Run("nil stays nil, so nothing is advertised", func(t *testing.T) {
		if got := agentClientEntity(nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("carries the live identity into the API layer's own shape", func(t *testing.T) {
		got := agentClientEntity(&mcpctrl.AgentClientInfo{Name: "claude-code", Version: "2.0.1"})
		if got == nil || got.Name != "claude-code" || got.Version != "2.0.1" {
			t.Errorf("got %+v", got)
		}
	})
}

// Choosing a model is refused with a reason worth reading, and refused before
// anything reaches a workspace server when the caller has no business there.
// "Failed" tells someone whose picker just did nothing precisely nothing.

func TestSetAgentModel_RefusesACallerWithoutAccess(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return false, nil
		},
	}
	// Intentionally no MCPManager: an unauthorized request must be refused
	// before anything reaches a workspace server.
	h := &handler{crud: crudCtrl}

	app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentModel()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
		strings.NewReader(`{"modelId":"gemini-2.5-pro"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSetAgentModel_RejectsARequestNamingNoModel(t *testing.T) {
	// Checked before the access lookup would matter, and worth its own answer:
	// an empty modelId is a client bug, and reporting it as "no agent" would
	// send someone looking at their gateway.
	cases := []struct {
		name string
		body string
	}{
		{"an empty model id", `{"modelId":""}`},
		{"a model id of only spaces", `{"modelId":"   "}`},
		{"no model id at all", `{}`},
		{"a body that is not JSON", `not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			h := &handler{crud: crudCtrl}

			app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentModel()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
				strings.NewReader(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestSetAgentModel_ReportsThatNothingIsConnected(t *testing.T) {
	// No MCP server for the workspace at all. 404 rather than a 409, because
	// the distinction is real: nothing is running to be asked, as against
	// something running that will not act.
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	// A manager that builds no server for the workspace, which is what "nothing
	// is connected" looks like from here.
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{}}

	app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentModel()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
		strings.NewReader(`{"modelId":"gemini-2.5-pro"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAgentConcurrencyEntity(t *testing.T) {
	t.Run("nil stays nil, so nothing is advertised", func(t *testing.T) {
		if got := agentConcurrencyEntity(nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("carries the snapshot across into the API layer's own shape", func(t *testing.T) {
		got := agentConcurrencyEntity(&mcpctrl.AgentConcurrencySnapshot{
			MaxConcurrency: 4,
			Active:         2,
			Queued:         3,
			Min:            1,
			Max:            64,
			CanSet:         true,
		})

		if got == nil {
			t.Fatal("got nil")
		}
		if got.MaxConcurrency != 4 || got.Active != 2 || got.Queued != 3 {
			t.Errorf("queue state = %+v", got)
		}
		if got.Min != 1 || got.Max != 64 || !got.CanSet {
			t.Errorf("range and permission = %+v", got)
		}
	})

	t.Run("a gateway willing to be told a limit but naming none cannot set", func(t *testing.T) {
		// Settable(), not the raw flag. A control offered on that promise would
		// have no number to start from and no way to tell a change had landed.
		got := agentConcurrencyEntity(&mcpctrl.AgentConcurrencySnapshot{CanSet: true})
		if got == nil || got.CanSet {
			t.Errorf("got %+v, want canSet false", got)
		}
	})
}

// Changing the limit is refused with a reason worth reading, the same way
// choosing a model is: "failed" tells someone whose control just sprang back
// precisely nothing, and the three refusals below mean three different things.

func TestSetAgentConcurrency_RefusesACallerWithoutAccess(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return false, nil
		},
	}
	// Intentionally no MCPManager: an unauthorized request must be refused
	// before anything reaches a workspace server.
	h := &handler{crud: crudCtrl}

	app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentConcurrency()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
		strings.NewReader(`{"maxConcurrency":4}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSetAgentConcurrency_RejectsARequestNamingNoLimit(t *testing.T) {
	// Absent is not zero. A missing field read as 0 would ask the gateway to
	// stall its queue, so it is refused here and told apart from a body that
	// deliberately names a number.
	cases := []struct {
		name string
		body string
	}{
		{"no limit at all", `{}`},
		{"an explicitly null limit", `{"maxConcurrency":null}`},
		{"a body that is not JSON", `not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			h := &handler{crud: crudCtrl}

			app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentConcurrency()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
				strings.NewReader(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestSetAgentConcurrency_ReportsThatNothingIsConnected(t *testing.T) {
	// 404 rather than 409, for the same reason as the model picker: nothing is
	// running to be asked, as against something running that will not act.
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{}}

	app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentConcurrency()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
		strings.NewReader(`{"maxConcurrency":4}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestSetAgentConcurrency_AnswersTheServerRefusals(t *testing.T) {
	// A server with nothing attached, which is what both refusals below look
	// like from the handler: a limit that is not a limit is refused before any
	// session is consulted, and a workspace whose gateway never offered the
	// control answers that it will not act rather than that it is missing.
	cases := []struct {
		name string
		body string
		want int
	}{
		{"a limit of zero is a stalled queue, not a setting", `{"maxConcurrency":0}`, http.StatusBadRequest},
		{"a negative limit is refused the same way", `{"maxConcurrency":-1}`, http.StatusBadRequest},
		{"a gateway that never offered the control will not act", `{"maxConcurrency":4}`, http.StatusConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			// A real server throughout: every expectation in this table is
			// the controller's own — a limit below 1 refused as invalid, and a
			// server that never offered the control refusing to act. A double
			// would answer whatever it was told to and prove none of it.
			h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: &mcpctrl.WorkspaceServer{}}}

			app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentConcurrency()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
				strings.NewReader(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

// The successful paths, which had no test before this package could inject a
// server. Both assert what was asked of the agent, not just the status code:
// a 202 means "asked", so a handler that answered 202 without passing the value
// on would be exactly as wrong as one that refused, and indistinguishable from
// the outside.

func TestSetAgentModel_AsksTheConnectedAgent(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	// Counted on the asking, not on the agent's confirmation: what is worth
	// knowing is how often people reach for this, and an agent that then
	// refuses is a different question.
	var counted []entity.Action
	crudCtrl.recordTelemetryFunc = func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
		counted = append(counted, rq.Action)
		return nil
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

	app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentModel()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
		strings.NewReader(`{"modelId":"gemini-2.5-pro"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Accepted, not OK: the agent has been asked, and only its own next models
	// notification says whether it switched.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if srv.modelCalls != 1 {
		t.Errorf("asked the agent %d times, want exactly 1", srv.modelCalls)
	}
	if srv.modelID != "gemini-2.5-pro" {
		t.Errorf("asked for model %q, want the one the request named", srv.modelID)
	}

	var body struct {
		Requested string `json:"requested"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// What was asked for, never a claim about what is now in force.
	if body.Requested != "gemini-2.5-pro" {
		t.Errorf("body named %q, want the requested model", body.Requested)
	}
	if len(counted) != 1 || counted[0] != entity.ActionAgentModelSelect {
		t.Errorf("counted %v, want one agent-model-select", counted)
	}
}

func TestSetAgentConcurrency_AsksTheConnectedGateway(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

	app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentConcurrency()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
		strings.NewReader(`{"maxConcurrency":8}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if srv.concurrencyCalls != 1 {
		t.Errorf("asked the gateway %d times, want exactly 1", srv.concurrencyCalls)
	}
	if srv.limit != 8 {
		t.Errorf("asked for a limit of %d, want the one the request named", srv.limit)
	}

	var body struct {
		Requested int `json:"requested"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Requested != 8 {
		t.Errorf("body named %d, want the requested limit", body.Requested)
	}
}

// A value above the gateway's own ceiling is forwarded rather than refused: the
// gateway clamps it and reports what it settled on, which is more truthful than
// refusing against a range this server only holds a cached copy of.
func TestSetAgentConcurrency_ForwardsAValueTheGatewayWillClamp(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

	app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentConcurrency()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
		strings.NewReader(`{"maxConcurrency":9000}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if srv.limit != 9000 {
		t.Errorf("forwarded %d, want the value passed through untouched", srv.limit)
	}
}

// Which refusal becomes which status is a real decision, and a wrong one is a
// real bug: a 409 tells a client "nothing here will do that", a 400 tells it
// "ask for something else", and a 500 sends someone to the logs. Injecting the
// error is the only way to assert the mapping — before the seam these branches
// needed a live gateway in a particular state, or a race, to reach at all.
func TestSetAgentModel_MapsEveryRefusalToItsOwnStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nothing connected can switch model", mcpctrl.ErrModelSelectUnsupported, http.StatusConflict},
		{"that model is not on offer", mcpctrl.ErrModelNotOffered, http.StatusBadRequest},
		{"the agent went away before it could be told", mcpctrl.ErrModelSetNotDelivered, http.StatusConflict},
		// No path in the controller produces this today — it has four exits and
		// the three above are all the errors. The mapping is asserted anyway so
		// that a fourth, when it arrives, lands somewhere deliberate.
		{"anything else is ours, not the caller's", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			srv := &fakeWorkspaceServer{modelErr: tc.err}
			h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

			app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentModel()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
				strings.NewReader(`{"modelId":"gemini-2.5-pro"}`),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

func TestSetAgentConcurrency_MapsEveryRefusalToItsOwnStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not a limit at all", mcpctrl.ErrConcurrencyInvalid, http.StatusBadRequest},
		{"nothing connected will act on it", mcpctrl.ErrConcurrencySetUnsupported, http.StatusConflict},
		// The gateway disconnecting between being picked and being notified.
		// Unreachable through a real server — the in-memory transport cannot
		// stage the race — which is exactly why injecting the error is worth it.
		{"the gateway went away before it could be told", mcpctrl.ErrConcurrencySetNotDelivered, http.StatusConflict},
		{"anything else is ours, not the caller's", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
					return true, nil
				},
			}
			srv := &fakeWorkspaceServer{concurrencyErr: tc.err}
			h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

			app.Post("/api/v1/workspaces/:id/agent/concurrency", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentConcurrency()(c)
			})

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/concurrency",
				strings.NewReader(`{"maxConcurrency":4}`),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

// The switch happened; the count did not. Reporting that as a failed switch
// would be a lie the caller acts on — the picker would spring back to a model
// the agent has already been asked for.
func TestSetAgentModel_StillSucceedsWhenTheCountFails(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudWorkspaceAccess{
		checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) {
			return true, nil
		},
		recordTelemetryFunc: func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
			return errors.New("the counter is down")
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}}

	app.Post("/api/v1/workspaces/:id/agent/model", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.setAgentModel()(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/model",
		strings.NewReader(`{"modelId":"gemini-2.5-pro"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}
	if srv.modelCalls != 1 {
		t.Errorf("asked the agent %d times, want exactly 1", srv.modelCalls)
	}
}
