package server

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mustafaturan/monoflake"
)

func TestServeHTTP(t *testing.T) {
	mockRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "path:%s", r.URL.Path)
	})

	cfg := Config{
		Domain: "example.com",
	}
	s, _ := New(Params{Config: cfg, Router: mockRouter})

	tests := []struct {
		name       string
		host       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "AppDomain",
			host:       "app.example.com",
			path:       "/api/tasks",
			wantStatus: http.StatusOK,
			wantBody:   "path:/api/tasks",
		},
		{
			name:       "MCPSubdomain",
			host:       "1.mcp.example.com", // 1 in base36 is 1
			path:       "/some/path",
			wantStatus: http.StatusOK,
			wantBody:   "path:/mcp/" + monoflake.ID(1).String(),
		},
		{
			name:       "MCPSubdomainLargerID",
			host:       strconv.FormatInt(123456789, 36) + ".mcp.example.com",
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   "path:/mcp/" + monoflake.ID(123456789).String(),
		},
		{
			name:       "InvalidIDSubdomain",
			host:       "invalid!.mcp.example.com",
			path:       "/",
			wantStatus: http.StatusBadRequest,
			wantBody:   "Invalid workspace ID",
		},
		{
			name:       "UnauthorizedHost",
			host:       "attacker.com",
			path:       "/",
			wantStatus: http.StatusNotFound,
			wantBody:   "Host attacker.com not allowed",
		},
		{
			name:       "CoreMCPHost",
			host:       "mcp.example.com",
			path:       "/coremcp/sse",
			wantStatus: http.StatusOK,
			wantBody:   "path:/coremcp/sse",
		},
		{
			name:       "LocalhostAllowed",
			host:       "localhost:8080",
			path:       "/ping",
			wantStatus: http.StatusOK,
			wantBody:   "path:/ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://"+tt.host+tt.path, nil)
			rr := httptest.NewRecorder()
			s.(*service).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status mismatch: got %v want %v", rr.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && !contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body mismatch: got %v want %v", rr.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestLegoUser(t *testing.T) {
	u := &legoUser{Email: "test@ex.com"}
	if u.GetEmail() != "test@ex.com" {
		t.Error("email mismatch")
	}
	if u.GetRegistration() != nil {
		t.Error("registration should be nil initially")
	}
	if u.GetPrivateKey() != nil {
		t.Error("key should be nil initially")
	}
}

func TestCertificateCoversDomains(t *testing.T) {
	s := &service{}
	cert := &x509.Certificate{
		DNSNames: []string{"app.ex.com", "mcp.ex.com", "*.mcp.ex.com"},
	}

	tests := []struct {
		name    string
		domains []string
		want    bool
	}{
		{
			name:    "MatchAll",
			domains: []string{"app.ex.com", "mcp.ex.com", "*.mcp.ex.com"},
			want:    true,
		},
		{
			name:    "MissingOne",
			domains: []string{"app.ex.com", "mcp.ex.com", "extra.ex.com"},
			want:    false,
		},
		{
			name:    "WildcardMismatch",
			domains: []string{"*.other.ex.com"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.certificateCoversDomains(cert, tt.domains); got != tt.want {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && stringsContains(s, substr)))
}

func stringsContains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
