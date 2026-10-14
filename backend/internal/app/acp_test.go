// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/acpv1"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// fakeACPWorkspaces answers the token check's two questions.
type fakeACPWorkspaces struct {
	noAccess  bool
	accessErr error
	getErr    error
	archived  bool
	asked     []int64 // the workspaces access was checked for
}

func (f *fakeACPWorkspaces) CheckWorkspaceAccess(_ context.Context, workspaceID int64, _ string) (bool, error) {
	f.asked = append(f.asked, workspaceID)
	return !f.noAccess, f.accessErr
}

func (f *fakeACPWorkspaces) GetWorkspace(_ context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	ws := entity.Workspace{ID: req.ID}
	if f.archived {
		now := time.Now()
		ws.ArchivedAt = &now
	}
	return &entity.GetWorkspaceResponse{Workspace: ws}, nil
}

// fakeACPCounter records what the ACP telemetry hooks were asked to count.
type fakeACPCounter struct{ got []entity.RecordACPV1Request }

func (f *fakeACPCounter) RecordACPV1(_ context.Context, req entity.RecordACPV1Request) {
	f.got = append(f.got, req)
}

func TestACPTokenAuth(t *testing.T) {
	ctx := context.Background()
	tokens := auth.NewTokenService(auth.TokenConfig{JWTSecret: "acp-test-secret"})
	user := monoflake.ID(5).String()
	ws7 := monoflake.ID(7).String()
	mint := func(t *testing.T, fn func() (string, error)) string {
		t.Helper()
		tok, err := fn()
		if err != nil {
			t.Fatalf("minting the token failed: %v", err)
		}
		return tok
	}
	access := func(t *testing.T) string {
		return mint(t, func() (string, error) { return tokens.CreateMCPToken(user, ws7, "access") })
	}

	t.Run("a workspace access token resolves to its workspace and user", func(t *testing.T) {
		a := acpTokenAuth{tokens: tokens, workspaces: &fakeACPWorkspaces{}}
		id, got, err := a.resolve(ctx, access(t))
		if err != nil || id != 7 || got != user {
			t.Fatalf("resolve gave (%d, %q, %v), want (7, %q, nil)", id, got, err, user)
		}
	})

	unauthorized := map[string]func() (string, error){
		"a person's session token is unauthorized": func() (string, error) { return tokens.CreateToken(user, "a@b.c", "A", "") },
		"a terminal ticket is unauthorized":        func() (string, error) { return tokens.CreateTerminalTicket(user, "123") },
		"a refresh token is unauthorized":          func() (string, error) { return tokens.CreateMCPToken(user, ws7, "refresh") },
		"an OAuth code is unauthorized":            func() (string, error) { return tokens.CreateOAuthCodeToken(user, ws7) },
		"the supervisor's token is unauthorized":   func() (string, error) { return tokens.CreateMCPToken(user, "coremcp", "access") },
		"a token naming no workspace is unauthorized": func() (string, error) {
			return tokens.CreateMCPToken(user, "", "access")
		},
		"a token whose workspace is not an id is unauthorized": func() (string, error) {
			return tokens.CreateMCPToken(user, "not-an-id!", "access")
		},
		"a token with no user is unauthorized": func() (string, error) { return tokens.CreateMCPToken("", ws7, "access") },
		"a token from another secret is unauthorized": func() (string, error) {
			return auth.NewTokenService(auth.TokenConfig{JWTSecret: "another-secret"}).CreateMCPToken(user, ws7, "access")
		},
	}
	for name, fn := range unauthorized {
		t.Run(name, func(t *testing.T) {
			ws := &fakeACPWorkspaces{}
			a := acpTokenAuth{tokens: tokens, workspaces: ws}
			if _, _, err := a.resolve(ctx, mint(t, fn)); !errors.Is(err, errACPUnauthorized) {
				t.Fatalf("resolve returned %v, want %v", err, errACPUnauthorized)
			}
			if len(ws.asked) != 0 {
				t.Fatalf("workspace access was checked for %v, want no check for a token that is not a workspace's", ws.asked)
			}
		})
	}

	forbidden := map[string]*fakeACPWorkspaces{
		"a token for a workspace the user lost access to is forbidden": {noAccess: true},
		"a failed access check is forbidden":                           {accessErr: errors.New("database unavailable")},
		"a workspace that cannot be read is forbidden":                 {getErr: errors.New("workspace not found")},
	}
	for name, ws := range forbidden {
		t.Run(name, func(t *testing.T) {
			a := acpTokenAuth{tokens: tokens, workspaces: ws}
			if _, _, err := a.resolve(ctx, access(t)); !errors.Is(err, errACPForbidden) {
				t.Fatalf("resolve returned %v, want %v", err, errACPForbidden)
			}
		})
	}

	t.Run("an archived workspace is reported as archived", func(t *testing.T) {
		a := acpTokenAuth{tokens: tokens, workspaces: &fakeACPWorkspaces{archived: true}}
		if _, _, err := a.resolve(ctx, access(t)); !errors.Is(err, errACPArchived) {
			t.Fatalf("resolve returned %v, want %v", err, errACPArchived)
		}
	})

	t.Run("acpV1Auth maps each failure to acpv1's error", func(t *testing.T) {
		for _, c := range []struct {
			ws    *fakeACPWorkspaces
			token string
			want  error
		}{
			{&fakeACPWorkspaces{}, "not a token", acpv1.ErrUnauthorized},
			{&fakeACPWorkspaces{noAccess: true}, access(t), acpv1.ErrForbidden},
			{&fakeACPWorkspaces{archived: true}, access(t), acpv1.ErrArchived},
		} {
			if _, _, err := (acpV1Auth{acpTokenAuth{tokens: tokens, workspaces: c.ws}}).WorkspaceForToken(ctx, c.token); !errors.Is(err, c.want) {
				t.Errorf("WorkspaceForToken returned %v, want %v", err, c.want)
			}
		}
		id, got, err := (acpV1Auth{acpTokenAuth{tokens: tokens, workspaces: &fakeACPWorkspaces{}}}).WorkspaceForToken(ctx, access(t))
		if err != nil || id != 7 || got != user {
			t.Errorf("WorkspaceForToken gave (%d, %q, %v), want (7, %q, nil)", id, got, err, user)
		}
	})
}

func TestCountACPV1(t *testing.T) {
	rec := &fakeACPCounter{}
	countACPV1(rec, false)(context.Background(), 70, monoflake.ID(7).String())
	countACPV1(rec, true)(context.Background(), 71, monoflake.ID(7).String())
	want := []entity.RecordACPV1Request{{UserID: 7, WorkspaceID: 70}, {UserID: 7, WorkspaceID: 71, Turn: true}}
	if len(rec.got) != 2 || rec.got[0] != want[0] || rec.got[1] != want[1] {
		t.Fatalf("counted %+v, want %+v", rec.got, want)
	}
}

func TestMountACP(t *testing.T) {
	tokens := auth.NewTokenService(auth.TokenConfig{JWTSecret: "acp-test-secret"})
	token, err := tokens.CreateMCPToken(monoflake.ID(5).String(), monoflake.ID(7).String(), "access")
	if err != nil {
		t.Fatalf("minting the token failed: %v", err)
	}
	serve := func(t *testing.T, enabled bool) (*httptest.Server, *acpv1.Hub, *fakeACPCounter) {
		t.Helper()
		hub, counter := acpv1.NewHub(), &fakeACPCounter{}
		mux := http.NewServeMux()
		mountACP(mux, acpDeps{
			enabled: enabled, hub: hub, counter: counter,
			auth:       acpTokenAuth{tokens: tokens, workspaces: &fakeACPWorkspaces{}},
			workspaces: mcp.NewManager(func(int64, string) *mcp.WorkspaceServer { return &mcp.WorkspaceServer{} }),
			mcpURL:     func(int64) string { return "https://agentrq.test/mcp/ws" },
			appVersion: "0.10.3",
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return srv, hub, counter
	}
	get := func(t *testing.T, url string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", url, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	t.Run("the endpoint is mounted on the mux and answers 426 to a plain GET", func(t *testing.T) {
		srv, _, _ := serve(t, true)
		if code := get(t, srv.URL+"/api/acp/v1"); code != http.StatusUpgradeRequired {
			t.Fatalf("GET /api/acp/v1 answered %d, want 426", code)
		}
	})

	t.Run("only the exact path is mounted", func(t *testing.T) {
		srv, _, _ := serve(t, true)
		if code := get(t, srv.URL+"/api/acp/v1/x"); code != http.StatusNotFound {
			t.Fatalf("GET /api/acp/v1/x answered %d, want 404", code)
		}
	})

	t.Run("a switched-off endpoint answers 404", func(t *testing.T) {
		srv, _, _ := serve(t, false)
		if code := get(t, srv.URL+"/api/acp/v1"); code != http.StatusNotFound {
			t.Fatalf("GET /api/acp/v1 answered %d, want 404", code)
		}
	})

	t.Run("a bridge is opened with initialize and told when the server shuts down", func(t *testing.T) {
		srv, hub, _ := serve(t, true)
		hdr := http.Header{}
		hdr.Set("Authorization", "Bearer "+token)
		hdr.Set("X-AgentRQ-Cwd", "/home/me/work")
		dialer := websocket.Dialer{Subprotocols: []string{"agentrq.bridge.1"}}
		ws, resp, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/acp/v1", hdr)
		if err != nil {
			t.Fatalf("dial failed: %v (%v)", err, resp)
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var sawInitialize bool
		for !sawInitialize {
			_, f, err := ws.ReadMessage()
			if err != nil {
				t.Fatalf("the socket failed with %v before initialize", err)
			}
			sawInitialize = strings.Contains(string(f), `"method":"initialize"`) && strings.Contains(string(f), `"version":"0.10.3"`)
		}

		a := &App{server: &stubServer{}, acpV1Hub: hub, cancel: func() {}}
		if err := a.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown returned %v", err)
		}
		for {
			_, _, err := ws.ReadMessage()
			if err == nil {
				continue
			}
			var ce *websocket.CloseError
			if !errors.As(err, &ce) || ce.Code != websocket.CloseGoingAway {
				t.Fatalf("the socket ended with %v, want close 1001", err)
			}
			return
		}
	})
}

// The switch reaches acp.v1.enabled through the real base.yaml, off unless set.
func TestACPSwitchFromEnv(t *testing.T) {
	for _, tt := range []struct {
		env  string
		set  bool
		want bool
	}{
		{"", false, false},
		{"true", true, true},
		{"false", true, false},
	} {
		t.Run("AGENTRQ_ACP_V1_ENABLED="+tt.env, func(t *testing.T) {
			t.Setenv("ENV", "development")
			if tt.set {
				t.Setenv("AGENTRQ_ACP_V1_ENABLED", tt.env)
			}
			t.Chdir("../../cmd/server")
			svc, err := config.New()
			if err != nil {
				t.Fatal(err)
			}
			var cfg Config
			if err := svc.Populate("acp", &cfg.ACP); err != nil {
				t.Fatal(err)
			}
			if cfg.ACP.V1.Enabled != tt.want {
				t.Errorf("acp.v1.enabled was %v, want %v", cfg.ACP.V1.Enabled, tt.want)
			}
		})
	}

	t.Run("a value that is not a boolean stops startup", func(t *testing.T) {
		t.Setenv("ENV", "development")
		t.Setenv("AGENTRQ_ACP_V1_ENABLED", "sometimes")
		t.Chdir("../../cmd/server")
		svc, err := config.New()
		if err != nil {
			t.Fatal(err)
		}
		var cfg Config
		if err := svc.Populate("acp", &cfg.ACP); err == nil {
			t.Fatalf("acp.v1.enabled took %q as %v, want an error", "sometimes", cfg.ACP.V1.Enabled)
		}
	})
}

// newTestApp builds the whole app the way main does, on a fresh SQLite file,
// with the ACP v1 switch set to enabled.
func newTestApp(t *testing.T, enabled string) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "acp-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(dir, "agentrq.db"))
	t.Setenv("AGENTRQ_STORAGE_DIR", dir)
	t.Setenv("AGENTRQ_ACP_V1_ENABLED", enabled)
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	for key, dst := range map[string]any{"app": &cfg.App, "auth": &cfg.Auth, "smtp": &cfg.SMTP, "ssl": &cfg.SSL,
		"slack": &cfg.Slack, "webPush": &cfg.WebPush, "storage": &cfg.Storage, "memory": &cfg.Memory,
		"skills": &cfg.Skills, "pano": &cfg.Pano, "acp": &cfg.ACP, "idgen": &cfg.Idgen} {
		if err := svc.Populate(key, dst); err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
	}
	cfg.ConfigSvc = svc
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown returned %v", err)
		}
	})
	return a
}

// The real router, Fiber's fallback behind it, serves the endpoint only while
// the switch is on.
func TestTheAppServesACPV1(t *testing.T) {
	for _, tt := range []struct {
		enabled string
		want    int
		body    string
	}{
		// On, the endpoint itself answers: a bridge with no token is asked for one.
		{"true", http.StatusUnauthorized, "a workspace access token is required"},
		{"false", http.StatusNotFound, "404 page not found"},
	} {
		t.Run("AGENTRQ_ACP_V1_ENABLED="+tt.enabled, func(t *testing.T) {
			a := newTestApp(t, tt.enabled)
			if a.acpV1Hub == nil {
				t.Fatal("the app keeps no ACP v1 hub to close on shutdown")
			}
			req := httptest.NewRequest(http.MethodGet, "http://localhost/api/acp/v1", nil)
			rr := httptest.NewRecorder()
			a.server.(http.Handler).ServeHTTP(rr, req)
			if body := strings.TrimSpace(rr.Body.String()); rr.Code != tt.want || body != tt.body {
				t.Fatalf("GET /api/acp/v1 answered %d %q, want %d %q", rr.Code, body, tt.want, tt.body)
			}
		})
	}
}
