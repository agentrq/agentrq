package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/google/callback?code=valid-code&state="+tt.state, nil)
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
