// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	slackctrl "github.com/agentrq/agentrq/backend/internal/controller/slack"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	slacksvc "github.com/agentrq/agentrq/backend/internal/service/slack"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

const baseURL = "https://app.agentrq.com"

// stubSlackService answers VerifyRequest with verifyErr; the handler calls
// nothing else on it.
type stubSlackService struct {
	slacksvc.Service
	verifyErr error
	verified  []string
}

func (s *stubSlackService) VerifyRequest(r *http.Request, body []byte) error {
	s.verified = append(s.verified, string(body))
	return s.verifyErr
}

// stubController records what the handler hands to the controller. The
// background calls report on channels so a test can wait for them.
type stubController struct {
	slackctrl.Controller

	events   chan slackctrl.SlackEventPayload
	eventErr error

	approvals   chan slackctrl.SlackBlockAction
	approvalErr error

	permissions   chan slackctrl.SlackBlockAction
	permissionErr error

	oauthErr  error
	oauthArgs []string

	slashMsg       string
	slashEphemeral bool
	slashErr       error
	slashArgs      []string
}

func newStubController() *stubController {
	return &stubController{
		events:      make(chan slackctrl.SlackEventPayload, 4),
		approvals:   make(chan slackctrl.SlackBlockAction, 4),
		permissions: make(chan slackctrl.SlackBlockAction, 4),
	}
}

func (c *stubController) HandleSlackEvent(ctx context.Context, payload slackctrl.SlackEventPayload) error {
	c.events <- payload
	return c.eventErr
}

func (c *stubController) HandleTaskApproval(ctx context.Context, action slackctrl.SlackBlockAction) error {
	c.approvals <- action
	return c.approvalErr
}

func (c *stubController) HandleMCPPermission(ctx context.Context, action slackctrl.SlackBlockAction) error {
	c.permissions <- action
	return c.permissionErr
}

func (c *stubController) HandleOAuthCallback(ctx context.Context, workspaceID62, code, redirectURI string) error {
	c.oauthArgs = []string{workspaceID62, code, redirectURI}
	return c.oauthErr
}

func (c *stubController) HandleSlashCommand(ctx context.Context, channelID, text string) (string, bool, error) {
	c.slashArgs = []string{channelID, text}
	return c.slashMsg, c.slashEphemeral, c.slashErr
}

type stubTokenService struct {
	auth.TokenService
	workspaceID62 string
	err           error
}

func (s *stubTokenService) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	if provider != "slack" {
		return "", errors.New("wrong provider " + provider)
	}
	return s.workspaceID62, s.err
}

type fixture struct {
	mux   *http.ServeMux
	svc   *stubSlackService
	ctrl  *stubController
	token *stubTokenService
}

func newFixture() *fixture {
	f := &fixture{
		mux:   http.NewServeMux(),
		svc:   &stubSlackService{},
		ctrl:  newStubController(),
		token: &stubTokenService{workspaceID62: "W62"},
	}
	New(Params{SlackCtrl: f.ctrl, SlackSvc: f.svc, TokenSvc: f.token, BaseURL: baseURL, Mux: f.mux})
	return f
}

func (f *fixture) serve(r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, r)
	return rr
}

// failingBody is a request body that cannot be read.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// logLines collects zerolog output so a test can wait for what a
// background goroutine logs.
type logLines chan string

func (l logLines) Write(p []byte) (int, error) {
	select {
	case l <- string(p):
	default:
	}
	return len(p), nil
}

func captureLogs(t *testing.T) logLines {
	t.Helper()
	lines := make(logLines, 64)
	old := zlog.Logger
	zlog.Logger = zerolog.New(lines)
	t.Cleanup(func() { zlog.Logger = old })
	return lines
}

func (l logLines) waitFor(t *testing.T, substr string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-l:
			if strings.Contains(line, substr) {
				return
			}
		case <-deadline:
			t.Fatalf("no log line containing %q", substr)
		}
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		var zero T
		t.Fatal("the controller was not called")
		return zero
	}
}

func formRequest(path string, form url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// ─── shared request checks ─────────────────────────────────────────────────────

func TestSignedEndpointsRejectBadRequestsBeforeTheController(t *testing.T) {
	for _, path := range []string{"/slack/events", "/slack/interactions", "/slack/commands"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture()
			if rr := f.serve(httptest.NewRequest(http.MethodGet, path, nil)); rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("GET = %d, want 405", rr.Code)
			}
			if rr := f.serve(httptest.NewRequest(http.MethodPost, path, failingBody{})); rr.Code != http.StatusBadRequest {
				t.Errorf("unreadable body = %d, want 400", rr.Code)
			}

			f.svc.verifyErr = errors.New("slack: invalid signature")
			rr := f.serve(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"type":"event_callback"}`)))
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("unsigned = %d, want 401", rr.Code)
			}
			if len(f.svc.verified) != 1 || f.svc.verified[0] != `{"type":"event_callback"}` {
				t.Errorf("verified bodies = %q, want the raw request body", f.svc.verified)
			}
		})
	}
}

// ─── /slack/events ─────────────────────────────────────────────────────────────

func TestEventsRejectsAPayloadThatIsNotJSON(t *testing.T) {
	f := newFixture()
	rr := f.serve(httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader("not json")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestEventsAnswersTheURLVerificationChallenge(t *testing.T) {
	f := newFixture()
	rr := f.serve(httptest.NewRequest(http.MethodPost, "/slack/events",
		strings.NewReader(`{"type":"url_verification","challenge":"c-123"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got["challenge"] != "c-123" {
		t.Errorf("body = %q, want the challenge echoed", rr.Body.String())
	}
	select {
	case <-f.ctrl.events:
		t.Error("a verification challenge reached the controller")
	default:
	}
}

func TestEventsAcknowledgesAndHandsTheEventToTheController(t *testing.T) {
	f := newFixture()
	rr := f.serve(httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(
		`{"type":"event_callback","event":{"type":"app_mention","user":"U1","text":"hi","channel":"C1","thread_ts":"1.1"}}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}

	got := receive(t, f.ctrl.events)
	if got.Event.Type != "app_mention" || got.Event.Channel != "C1" || got.Event.ThreadTS != "1.1" || got.Event.Text != "hi" {
		t.Errorf("event = %+v", got.Event)
	}
}

func TestEventsLogsAControllerFailure(t *testing.T) {
	logs := captureLogs(t)
	f := newFixture()
	f.ctrl.eventErr = errors.New("database unavailable")

	rr := f.serve(httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(`{"type":"event_callback"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 even when handling fails later", rr.Code)
	}
	receive(t, f.ctrl.events)
	logs.waitFor(t, "HandleSlackEvent error")
}

// ─── /slack/interactions ───────────────────────────────────────────────────────

func TestInteractionsRejectsAFormItCannotParse(t *testing.T) {
	f := newFixture()
	r := httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader("payload=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rr := f.serve(r); rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestInteractionsRejectsAPayloadThatIsNotJSON(t *testing.T) {
	f := newFixture()
	rr := f.serve(formRequest("/slack/interactions", url.Values{"payload": {"{not json"}}))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestInteractionsIgnoresAnythingButBlockActions(t *testing.T) {
	f := newFixture()
	rr := f.serve(formRequest("/slack/interactions", url.Values{"payload": {
		`{"type":"view_submission","actions":[{"action_id":"task_respond:W:T:allow"}]}`,
	}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	// Dispatch happens before the handler returns, so nothing can arrive later.
	select {
	case <-f.ctrl.approvals:
		t.Error("a view_submission was dispatched as an approval")
	default:
	}
}

func TestInteractionsDispatchesEachButtonByItsPrefix(t *testing.T) {
	logs := captureLogs(t)
	f := newFixture()
	payload := `{
		"type":"block_actions",
		"user":{"id":"U1","username":"ada"},
		"channel":{"id":"C1"},
		"message":{"ts":"111.222"},
		"actions":[
			{"action_id":"task_respond:W:T:allow"},
			{"action_id":"task_permission:W:T:R:deny"},
			{"action_id":"something_else"}
		]}`
	rr := f.serve(formRequest("/slack/interactions", url.Values{"payload": {payload}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}

	approval := receive(t, f.ctrl.approvals)
	want := slackctrl.SlackBlockAction{ActionID: "task_respond:W:T:allow", ChannelID: "C1", MessageTS: "111.222", UserID: "U1", UserName: "ada"}
	if approval != want {
		t.Errorf("approval = %+v, want %+v", approval, want)
	}
	permission := receive(t, f.ctrl.permissions)
	want.ActionID = "task_permission:W:T:R:deny"
	if permission != want {
		t.Errorf("permission = %+v, want %+v", permission, want)
	}
	logs.waitFor(t, "unknown action_id prefix")
}

func TestInteractionsAcceptsARawJSONBody(t *testing.T) {
	f := newFixture()
	rr := f.serve(httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader(
		`{"type":"block_actions","actions":[{"action_id":"task_respond:W:T:reject"}]}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	if got := receive(t, f.ctrl.approvals); got.ActionID != "task_respond:W:T:reject" {
		t.Errorf("approval = %+v", got)
	}
}

func TestInteractionsLogsAControllerFailure(t *testing.T) {
	logs := captureLogs(t)
	f := newFixture()
	f.ctrl.permissionErr = errors.New("verdict refused")

	rr := f.serve(formRequest("/slack/interactions", url.Values{"payload": {
		`{"type":"block_actions","actions":[{"action_id":"task_permission:W:T:R:allow"}]}`,
	}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	receive(t, f.ctrl.permissions)
	logs.waitFor(t, "[slack/interactions] handler error")
}

// ─── /slack/oauth/callback ─────────────────────────────────────────────────────

func TestOAuthCallbackRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		query    string
		stateErr error
		want     int
	}{
		{"not a GET", http.MethodPost, "?code=c&state=s", nil, http.StatusMethodNotAllowed},
		{"no code", http.MethodGet, "?state=s", nil, http.StatusBadRequest},
		{"no state", http.MethodGet, "?code=c", nil, http.StatusBadRequest},
		{"state does not validate", http.MethodGet, "?code=c&state=s", errors.New("expired"), http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.token.err = tt.stateErr
			rr := f.serve(httptest.NewRequest(tt.method, "/slack/oauth/callback"+tt.query, nil))
			if rr.Code != tt.want {
				t.Errorf("code = %d, want %d", rr.Code, tt.want)
			}
			if f.ctrl.oauthArgs != nil {
				t.Errorf("controller was called with %v", f.ctrl.oauthArgs)
			}
		})
	}
}

func TestOAuthCallbackCompletesAndReturnsToSettings(t *testing.T) {
	f := newFixture()
	rr := f.serve(httptest.NewRequest(http.MethodGet, "/slack/oauth/callback?code=the-code&state=jwt", nil))
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("code = %d, want 307", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != baseURL+"/workspaces/W62/settings?tab=slack" {
		t.Errorf("Location = %q", loc)
	}
	want := []string{"W62", "the-code", baseURL + "/slack/oauth/callback"}
	if strings.Join(f.ctrl.oauthArgs, " ") != strings.Join(want, " ") {
		t.Errorf("controller args = %v, want %v", f.ctrl.oauthArgs, want)
	}
}

func TestOAuthCallbackReturnsToSettingsWithAGenericError(t *testing.T) {
	f := newFixture()
	f.ctrl.oauthErr = errors.New("slack: failed to encrypt token: key is secret-ish detail")
	rr := f.serve(httptest.NewRequest(http.MethodGet, "/slack/oauth/callback?code=c&state=jwt", nil))
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("code = %d, want 307", rr.Code)
	}
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if loc.Path != "/workspaces/W62/settings" || loc.Query().Get("tab") != "slack" {
		t.Errorf("Location = %q", loc)
	}
	if got := loc.Query().Get("slack_error"); got != "failed to complete slack authorization" {
		t.Errorf("slack_error = %q, want the generic message only", got)
	}
}

// ─── /slack/commands ───────────────────────────────────────────────────────────

func decodeCommandResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rr.Body.String(), err)
	}
	return got
}

func TestCommandsRejectsAFormItCannotParse(t *testing.T) {
	f := newFixture()
	r := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader("command=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rr := f.serve(r); rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rr.Code)
	}
}

func TestCommandsAnswersAnUnknownCommandPrivately(t *testing.T) {
	f := newFixture()
	rr := f.serve(formRequest("/slack/commands", url.Values{"command": {"/other"}, "text": {"x"}, "channel_id": {"C1"}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	got := decodeCommandResponse(t, rr)
	if got["response_type"] != "ephemeral" || !strings.Contains(got["text"], "Unknown command") {
		t.Errorf("response = %v", got)
	}
	if f.ctrl.slashArgs != nil {
		t.Errorf("controller was called with %v", f.ctrl.slashArgs)
	}
}

func TestCommandsRelaysTheControllerAnswer(t *testing.T) {
	tests := []struct {
		name      string
		msg       string
		ephemeral bool
		err       error
		wantType  string
	}{
		{"shown to the channel", "🚀 *Task created successfully:* x", false, nil, "in_channel"},
		{"shown only to the caller", "⚠️ Task description cannot be empty.", true, nil, "ephemeral"},
		{"a failure is still answered", "⚠️ Failed to create task: boom", true, errors.New("boom"), "ephemeral"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			f := newFixture()
			f.ctrl.slashMsg, f.ctrl.slashEphemeral, f.ctrl.slashErr = tt.msg, tt.ephemeral, tt.err

			rr := f.serve(formRequest("/slack/commands", url.Values{"command": {"/t"}, "text": {"write tests"}, "channel_id": {"C1"}}))
			if rr.Code != http.StatusOK {
				t.Fatalf("code = %d, want 200", rr.Code)
			}
			got := decodeCommandResponse(t, rr)
			if got["response_type"] != tt.wantType || got["text"] != tt.msg {
				t.Errorf("response = %v, want %s %q", got, tt.wantType, tt.msg)
			}
			if strings.Join(f.ctrl.slashArgs, "|") != "C1|write tests" {
				t.Errorf("controller args = %v", f.ctrl.slashArgs)
			}
			if tt.err != nil {
				logs.waitFor(t, "HandleSlashCommand error")
			}
		})
	}
}

func TestNewRegistersEverySlackRoute(t *testing.T) {
	f := newFixture()
	var paths []string
	for _, p := range []string{"/slack/events", "/slack/interactions", "/slack/oauth/callback", "/slack/commands"} {
		if _, pattern := f.mux.Handler(httptest.NewRequest(http.MethodGet, p, nil)); pattern == p {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if len(paths) != 4 {
		t.Errorf("registered = %v, want all four Slack routes", paths)
	}
}
