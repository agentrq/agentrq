package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/security"
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
}

func (m *mockTokenSvc) CreateToken(userID, email, name, picture string) (string, error) {
	return m.createTokenFunc(userID, email, name, picture)
}

func (m *mockTokenSvc) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return m.createMCPTokenFunc(userID, workspaceID, tokenType)
}

type mockCrudController struct {
	crud.Controller
	findOrCreateUserFunc func(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error)
}

func (m *mockCrudController) FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
	return m.findOrCreateUserFunc(ctx, req)
}

func TestGoogleCallback_OpenRedirectPrevention(t *testing.T) {
	app := fiber.New()
	authSvc := &mockAuthService{}
	tokenSvc := &mockTokenSvc{}
	crudCtrl := &mockCrudController{}

	h := &handler{
		auth:     authSvc,
		tokenSvc: tokenSvc,
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

	tokenSvc.createTokenFunc = func(userID, email, name, picture string) (string, error) {
		return "valid-jwt", nil
	}

	tests := []struct {
		name        string
		state       string
		expectedLoc string
	}{
		{
			name:        "Safe local redirect",
			state:       "/workspaces",
			expectedLoc: "/workspaces",
		},
		{
			name:        "Malicious absolute redirect",
			state:       "http://localhost:3000.evil.com",
			expectedLoc: "/",
		},
		{
			name:        "Malicious relative redirect //",
			state:       "//evil.com",
			expectedLoc: "/",
		},
		{
			name:        "Malicious relative redirect /\\",
			state:       "/\\evil.com",
			expectedLoc: "/",
		},
		{
			name:        "Safe absolute redirect",
			state:       "http://localhost:3000/safe",
			expectedLoc: "http://localhost:3000/safe",
		},
	}

	tokenKey := "test-token-key-32-bytes-long-123"
	h.tokenKey = tokenKey

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateData := fmt.Sprintf("%s:nonce123", tt.state)
			sig := security.Sign(stateData, tokenKey)
			state := fmt.Sprintf("%s.%s", stateData, sig)

			req := httptest.NewRequest("GET", "/google/callback?code=valid-code&state="+state, nil)
			req.Header.Set("Cookie", "oauth_state=nonce123")
			resp, _ := app.Test(req)

			if resp.StatusCode != http.StatusFound {
				t.Errorf("Expected status 302, got %d", resp.StatusCode)
			}
			loc := resp.Header.Get("Location")
			if loc != tt.expectedLoc {
				t.Errorf("Expected Location %s, got %s", tt.expectedLoc, loc)
			}
		})
	}
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
