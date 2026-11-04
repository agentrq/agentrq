// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent/oauthconsenttest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tokens a grant is redeemed for are for the resource it was for: a
// read-only refresh token never comes back read-write, and a read-write one is
// unchanged.
func TestSupervisorToken_KeepsTheResourceOfTheGrant(t *testing.T) {
	h := &handler{tokenSvc: supervisorTokens}
	for _, resource := range []string{resourceSupervisor, resourceSupervisorReadOnly} {
		t.Run(resource, func(t *testing.T) {
			refresh := mintSupervisorToken(t, "user-1", resource, "refresh")
			w := supervisorTokenRequest(h, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}.Encode())
			if w.Code != http.StatusOK {
				t.Fatalf("refresh answered %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for _, kind := range []string{"access_token", "refresh_token"} {
				claims, err := supervisorTokens.ValidateToken(resp[kind].(string))
				if err != nil {
					t.Fatalf("%s: %v", kind, err)
				}
				if got := supervisorResource(claims.Audience); got != resource {
					t.Errorf("%s is for %q, want %q", kind, got, resource)
				}
			}
		})
	}
}

func supervisorToolCall(t *testing.T, h *handler, token, tool, args string) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/call answered %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// A read-only token opens the server, and every write it asks for, by its
// action or by a retired tool's name, is refused, while help still answers.
func TestSupervisorStreamable_AReadOnlyTokenIsRefusedWrites(t *testing.T) {
	h := supervisorStreamHandler()
	readOnly := mintSupervisorToken(t, "user-1", resourceSupervisorReadOnly, "access")

	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, supervisorStreamRequest(readOnly))
	if w.Code != http.StatusOK {
		t.Fatalf("initialize answered %d: %s", w.Code, w.Body.String())
	}

	calls := map[string]string{
		"task":       `{"action":"delete","payload":{"workspaceId":"0ZzhYQG2qtl","taskId":"0kGogHmaqvZ"}}`,
		"deleteTask": `{"workspaceId":"0ZzhYQG2qtl","taskId":"0kGogHmaqvZ"}`,
	}
	for tool, args := range calls {
		if body := supervisorToolCall(t, h, readOnly, tool, args); !strings.Contains(body, "read-only") {
			t.Errorf("%s on a read-only token was not refused: %s", tool, body)
		}
	}

	if body := supervisorToolCall(t, h, readOnly, "task", `{"action":"help"}`); strings.Contains(body, "read-only") || !strings.Contains(body, "listAll") {
		t.Errorf("help on a read-only token did not answer: %s", body)
	}
}

// The read-only resource can never be taken for a workspace's base62 ID.
func TestSupervisorResource_IsNeverAWorkspaceID(t *testing.T) {
	if !strings.Contains(resourceSupervisorReadOnly, ":") {
		t.Errorf("%q could decode as a workspace ID", resourceSupervisorReadOnly)
	}
	if got := supervisorResource([]string{"000000003D7", "access"}); got != "" {
		t.Errorf("a workspace token reads as the supervisor's %q", got)
	}
	if !slices.Contains([]string{resourceSupervisor, resourceSupervisorReadOnly}, supervisorResource([]string{"access", resourceSupervisorReadOnly})) {
		t.Error("a read-only supervisor token is not recognised")
	}
}

// Read only chosen on the page is a code, and then tokens, for the read-only
// resource, counted as a read-only allow. Read and write, chosen when the page
// opens, is the supervisor as before.
func TestSupervisorAuthorize_TheChosenLevelIsTheTokensResource(t *testing.T) {
	for _, tc := range []struct{ access, resource string }{
		{"", resourceSupervisor},
		{levelReadOnly, resourceSupervisorReadOnly},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			h := pkceHandler()
			crud := h.crud.(*consentRecorder)
			req := supervisorAuthorizeRequest()
			q := req.URL.Query()
			q.Set("code_challenge", pkceChallenge)
			q.Set("code_challenge_method", "S256")
			req.URL.RawQuery = q.Encode()

			w := oauthconsenttest.AllowAs(h.oauthAuthorizeHandler(), req, tc.access)
			if w.Code != http.StatusFound {
				t.Fatalf("authorize answered %d: %s", w.Code, w.Body.String())
			}
			loc, _ := url.Parse(w.Header().Get("Location"))
			tok := supervisorToken(h, loc.Query().Get("code"), pkceVerifier)
			var resp map[string]any
			if err := json.Unmarshal(tok.Body.Bytes(), &resp); err != nil || tok.Code != http.StatusOK {
				t.Fatalf("token answered %d: %s", tok.Code, tok.Body.String())
			}
			claims, err := h.tokenSvc.ValidateToken(resp["access_token"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if got := supervisorResource(claims.Audience); got != tc.resource {
				t.Errorf("the access token is for %q, want %q", got, tc.resource)
			}
			if len(crud.consents) != 1 || crud.consents[0].ReadOnly != (tc.resource == resourceSupervisorReadOnly) {
				t.Errorf("recorded %+v", crud.consents)
			}
		})
	}
}

func TestSupervisorAuthorize_ReadAndWriteIsChosenFirst(t *testing.T) {
	h, _ := consentHandler()
	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, supervisorAuthorizeRequest())
	if got := oauthconsenttest.Checked(w.Body.String()); got != levelReadWrite {
		t.Errorf("the page opens with %q chosen, want %q", got, levelReadWrite)
	}
	if !strings.Contains(w.Body.String(), "but change nothing") {
		t.Error("the page does not say what read only allows")
	}
}

func supervisorToolsList(t *testing.T, h *handler, token string) []*mcp.Tool {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, req)
	body := w.Body.String()
	if i := strings.Index(body, "data: "); i >= 0 {
		body = strings.TrimSpace(strings.SplitN(body[i+len("data: "):], "\n", 2)[0])
	}
	var resp struct {
		Result mcp.ListToolsResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || len(resp.Result.Tools) == 0 {
		t.Fatalf("tools/list answered %d: %s (%v)", w.Code, w.Body.String(), err)
	}
	return resp.Result.Tools
}

func actionEnum(t *testing.T, tool *mcp.Tool) []string {
	t.Helper()
	raw, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("%s's schema: %v", tool.Name, err)
	}
	return schema.Properties["action"].Enum
}

// A read-only connection is offered each tool's reads alone, every tool
// marked read-only; a read-write one is offered every action, as before.
func TestSupervisorToolsList_ReadOnlyListsOnlyReads(t *testing.T) {
	h := supervisorStreamHandler()
	for _, tool := range supervisorToolsList(t, h, mintSupervisorToken(t, "user-1", resourceSupervisorReadOnly, "access")) {
		if !tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("%s is listed as %+v, want read-only", tool.Name, tool.Annotations)
		}
		if tool.Name == "help" {
			continue
		}
		if got, want := actionEnum(t, tool), append(slices.Clone(readActions[tool.Name]), "help"); !slices.Equal(got, want) {
			t.Errorf("%s offers %v, want %v", tool.Name, got, want)
		}
	}

	for _, tool := range supervisorToolsList(t, h, mintSupervisorToken(t, "user-1", resourceSupervisor, "access")) {
		if tool.Name == "task" && (tool.Annotations.ReadOnlyHint || !slices.Contains(actionEnum(t, tool), "delete")) {
			t.Errorf("a read-write connection was offered task as %+v with %v", tool.Annotations, actionEnum(t, tool))
		}
	}
}

// A tool the server lists without annotations or a schema of its own is
// still listed, marked read-only, and anything else passes untouched.
func TestReadOnlyToolsMiddleware_ToolsWithoutHintsOrSchema(t *testing.T) {
	s := &WorkspaceServer{}
	next := func(_ context.Context, method string, _ mcp.Request) (mcp.Result, error) {
		if method != "tools/list" {
			return &mcp.ListPromptsResult{}, nil
		}
		return &mcp.ListToolsResult{Tools: []*mcp.Tool{{Name: "bare", InputSchema: map[string]any{"type": "object"}}}}, nil
	}
	mw := s.readOnlyToolsMiddleware(next)
	res, err := mw(mcpevent.WithReadOnly(context.Background()), "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := res.(*mcp.ListToolsResult).Tools[0]
	if !tool.Annotations.ReadOnlyHint || tool.Annotations.Title != "" {
		t.Errorf("a bare tool is listed as %+v", tool.Annotations)
	}
	if _, ok := tool.InputSchema.(map[string]any); !ok {
		t.Errorf("a bare tool's schema became %T", tool.InputSchema)
	}
	if res, _ := mw(mcpevent.WithReadOnly(context.Background()), "prompts/list", nil); res == nil {
		t.Error("another method was swallowed")
	}
}
