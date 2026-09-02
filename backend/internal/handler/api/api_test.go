package api

import (
	"bytes"
	"context"
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
}

func (m *mockCrudWorkspaceAccess) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	return m.checkWorkspaceAccessFunc(ctx, id, userID)
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
		mcpManager: mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
			// A workspace server with nothing connected to it.
			return &mcpctrl.WorkspaceServer{}
		}),
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
		mcpManager: mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
			return nil
		}),
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
