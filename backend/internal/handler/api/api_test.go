package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
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
	validateTokenFunc  func(tokenStr string) (*auth.Claims, error)
}

func (m *mockTokenSvc) CreateToken(userID, email, name, picture string) (string, error) {
	return m.createTokenFunc(userID, email, name, picture)
}

func (m *mockTokenSvc) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return m.createMCPTokenFunc(userID, workspaceID, tokenType)
}

func (m *mockTokenSvc) ValidateToken(tokenStr string) (*auth.Claims, error) {
	if m.validateTokenFunc != nil {
		return m.validateTokenFunc(tokenStr)
	}
	return nil, nil
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

func TestAuthMiddleware_AudienceEnforcement(t *testing.T) {
	app := fiber.New()
	tokenSvc := &mockTokenSvc{}

	h := &handler{
		tokenSvc: tokenSvc,
	}

	app.Use(h.authMiddleware())
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	tests := []struct {
		name           string
		token          string
		claims         *auth.Claims
		expectedStatus int
	}{
		{
			name:  "Valid human token",
			token: "valid-human",
			claims: &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:  "user1",
					Audience: jwt.ClaimStrings{auth.ActorHumanAudience},
				},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:  "Invalid audience (agent token)",
			token: "agent-token",
			claims: &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:  "user1",
					Audience: jwt.ClaimStrings{"ws123", "access"},
				},
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:  "Missing audience",
			token: "no-aud",
			claims: &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user1",
				},
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenSvc.validateTokenFunc = func(tokenStr string) (*auth.Claims, error) {
				if tokenStr == tt.token {
					return tt.claims, nil
				}
				return nil, jwt.ErrSignatureInvalid
			}

			req := httptest.NewRequest("GET", "/test", nil)
			req.AddCookie(&http.Cookie{Name: "at", Value: tt.token})
			resp, _ := app.Test(req)

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}
