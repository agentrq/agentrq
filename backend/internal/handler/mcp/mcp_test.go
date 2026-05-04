package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

type mockTokenSvc struct {
	auth.TokenService
	validCode  string
	validToken string
}

func (m *mockTokenSvc) ValidateToken(tokenStr string) (*auth.Claims, error) {
	if tokenStr == "valid-auth-cookie" || tokenStr == m.validCode || tokenStr == "valid-refresh-token" {
		return &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: monoflake.IDFromBase62("user123").String(),
				Audience: jwt.ClaimStrings{"ws123", "authorization_code"},
			},
		}, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

func (m *mockTokenSvc) CreateOAuthCodeToken(userID, workspaceID string) (string, error) {
	m.validCode = "mocked-code-" + userID + "-" + workspaceID
	return m.validCode, nil
}

func (m *mockTokenSvc) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	m.validToken = "mocked-mcp-" + userID + "-" + workspaceID + "-" + tokenType
	return m.validToken, nil
}

type mockRepo struct {
	base.Repository
}

func (m *mockRepo) SystemGetWorkspace(ctx context.Context, id int64) (model.Workspace, error) {
	return model.Workspace{
		UserID: int64(monoflake.IDFromBase62("user123").Int64()),
	}, nil
}

func setupTestRouter() (*http.ServeMux, *mockTokenSvc) {
	mux := http.NewServeMux()
	tokenSvc := &mockTokenSvc{}
	
	New(Params{
		TokenSvc:   tokenSvc,
		Repository: &mockRepo{},
		BaseURL:    "https://agentrq.com",
		Mux:        mux,
	})
	
	return mux, tokenSvc
}

func TestOAuthMetadataHandler(t *testing.T) {
	mux, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/mcp/12345/.well-known/oauth-authorization-server", nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "12345.mcp.agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", w.Code)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}

	if meta["issuer"] != "https://12345.mcp.agentrq.com" {
		t.Errorf("Unexpected issuer: %v", meta["issuer"])
	}
}

func TestOAuthAuthorizeHandler_Unauthenticated(t *testing.T) {
	mux, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/mcp/12345/oauth2/authorize?client_id=test&redirect_uri=https://agentrq.com/callback&state=somestate", nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "12345.mcp.agentrq.com"
	
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("Expected 302 Found, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "redirect_url=") {
		t.Errorf("Expected redirect_url in Location, got %s", loc)
	}
	if !strings.HasPrefix(loc, "https://agentrq.com/api/v1/auth/google/login") {
		t.Errorf("Expected login redirect, got %s", loc)
	}
}

func TestOAuthAuthorizeHandler_Authenticated(t *testing.T) {
	mux, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/mcp/12345/oauth2/authorize?client_id=test&redirect_uri=https://agentrq.com/callback&state=somestate", nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "12345.mcp.agentrq.com"
	req.AddCookie(&http.Cookie{Name: "at", Value: "valid-auth-cookie"})
	
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("Expected 302 Found, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://agentrq.com/callback") {
		t.Errorf("Expected redirect to client redirect_uri, got %s", loc)
	}
	
	u, _ := url.Parse(loc)
	if u.Query().Get("code") == "" {
		t.Errorf("Expected code in redirect query")
	}
	if u.Query().Get("state") != "somestate" {
		t.Errorf("Expected state=somestate, got %s", u.Query().Get("state"))
	}
}

func TestOAuthAuthorizeHandler_OpenRedirect(t *testing.T) {
	mux, _ := setupTestRouter()

	tests := []struct {
		name        string
		redirectURI string
		expectedCode int
	}{
		{
			name:         "Safe relative redirect",
			redirectURI:  "/callback",
			expectedCode: http.StatusFound,
		},
		{
			name:         "Safe absolute redirect",
			redirectURI:  "https://agentrq.com/callback",
			expectedCode: http.StatusFound,
		},
		{
			name:         "External absolute redirect (allowed for MCP)",
			redirectURI:  "https://claude.ai/callback",
			expectedCode: http.StatusFound,
		},
		{
			name:         "Malicious relative redirect //",
			redirectURI:  "//evil.com",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Malicious relative redirect /\\",
			redirectURI:  "/\\evil.com",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "HTTP localhost redirect (allowed)",
			redirectURI:  "http://localhost:8080/callback",
			expectedCode: http.StatusFound,
		},
		{
			name:         "HTTP external redirect (blocked)",
			redirectURI:  "http://claude.ai/callback",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "Host mismatch in absolute redirect (allowed for MCP)",

			redirectURI:  "https://other-client.com/callback",
			expectedCode: http.StatusFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/mcp/12345/oauth2/authorize?client_id=test&redirect_uri="+url.QueryEscape(tt.redirectURI), nil)
			req.SetPathValue("workspaceID", "12345")
			req.Host = "12345.mcp.agentrq.com"
			req.AddCookie(&http.Cookie{Name: "at", Value: "valid-auth-cookie"})

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}
		})
	}
}

func TestOAuthTokenHandler(t *testing.T) {
	mux, mockSvc := setupTestRouter()

	// Initial setup of the code so mock token svc recognizes it
	mockSvc.validCode = "mocked-code-user123-ws123"

	formData := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {mockSvc.validCode},
	}
	
	req := httptest.NewRequest("POST", "/mcp/12345/oauth2/token", strings.NewReader(formData.Encode()))
	req.SetPathValue("workspaceID", "12345")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}

	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Errorf("Expected access_token in response")
	}
	if resp["refresh_token"] == nil || resp["refresh_token"] == "" {
		t.Errorf("Expected refresh_token in response")
	}
}
