// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	slackapi "github.com/slack-go/slack"
)

// enabledCfg is a configuration that IsEnabled accepts.
var enabledCfg = Config{
	Enabled:       true,
	SigningSecret: "signing-secret",
	ClientID:      "client-id",
	ClientSecret:  "client-secret",
}

// redirectTransport sends every request to the fake Slack API instead of
// slack.com. slack-go builds its clients with a zero http.Client, which uses
// http.DefaultTransport, so swapping that is the only way in.
type redirectTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	req.Host = rt.target.Host
	return rt.base.RoundTrip(req)
}

// fakeSlackAPI is a stand-in for https://slack.com/api/. Each method answers
// with the JSON its handler returns, and every call is recorded.
type fakeSlackAPI struct {
	mu       sync.Mutex
	handlers map[string]func(form url.Values) string
	calls    []string
	forms    []url.Values
	server   *httptest.Server
}

func newFakeSlackAPI(t *testing.T, handlers map[string]func(form url.Values) string) *fakeSlackAPI {
	t.Helper()
	f := &fakeSlackAPI{handlers: handlers}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		f.mu.Lock()
		f.calls = append(f.calls, method)
		f.forms = append(f.forms, r.Form)
		h := f.handlers[method]
		f.mu.Unlock()
		if h == nil {
			http.Error(w, "unexpected method "+method, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(h(r.Form)))
	}))
	t.Cleanup(f.server.Close)

	target, _ := url.Parse(f.server.URL)
	orig := http.DefaultTransport
	http.DefaultTransport = redirectTransport{target: target, base: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
	return f
}

func (f *fakeSlackAPI) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func reply(s string) func(url.Values) string { return func(url.Values) string { return s } }

func TestIsEnabledNeedsTheFlagAndBothClientCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set", enabledCfg, true},
		{"flag off", Config{ClientID: "id", ClientSecret: "secret"}, false},
		{"no client id", Config{Enabled: true, ClientSecret: "secret"}, false},
		{"no client secret", Config{Enabled: true, ClientID: "id"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.cfg).IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientIDIsTheConfiguredOne(t *testing.T) {
	if got := New(enabledCfg).ClientID(); got != "client-id" {
		t.Errorf("ClientID() = %q, want client-id", got)
	}
}

func TestADisabledServiceRefusesEveryCall(t *testing.T) {
	// No fake API: a disabled service must not reach the network at all.
	svc := New(Config{})
	ctx := context.Background()

	if _, err := svc.CreatePrivateChannel(ctx, "tok", "name"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("CreatePrivateChannel err = %v, want disabled", err)
	}
	if err := svc.InviteUsersToChannel(ctx, "tok", "C1", []string{"U1"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("InviteUsersToChannel err = %v, want disabled", err)
	}
	if _, err := svc.PostMessage(ctx, "tok", "C1", nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("PostMessage err = %v, want disabled", err)
	}
	if _, err := svc.PostThreadReply(ctx, "tok", "C1", "1.1", nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("PostThreadReply err = %v, want disabled", err)
	}
	if err := svc.UpdateMessage(ctx, "tok", "C1", "1.1", nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("UpdateMessage err = %v, want disabled", err)
	}
	if _, _, _, _, err := svc.ExchangeCode(ctx, "code", "https://x/cb"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("ExchangeCode err = %v, want disabled", err)
	}
}

func TestCreatePrivateChannelReturnsTheNewChannel(t *testing.T) {
	api := newFakeSlackAPI(t, map[string]func(url.Values) string{
		"conversations.create": func(form url.Values) string {
			if form.Get("name") != "agentrq-demo" || form.Get("is_private") != "true" {
				return `{"ok":false,"error":"unexpected_params"}`
			}
			return `{"ok":true,"channel":{"id":"C_NEW","name":"agentrq-demo"}}`
		},
	})

	id, err := New(enabledCfg).CreatePrivateChannel(context.Background(), "xoxb", "agentrq-demo")
	if err != nil {
		t.Fatalf("CreatePrivateChannel: %v", err)
	}
	if id != "C_NEW" {
		t.Errorf("channel = %q, want C_NEW", id)
	}
	if got := api.methods(); len(got) != 1 {
		t.Errorf("calls = %v, want a single conversations.create", got)
	}
}

func TestCreatePrivateChannelFailsOnAnErrorOtherThanNameTaken(t *testing.T) {
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"conversations.create": reply(`{"ok":false,"error":"restricted_action"}`),
	})

	_, err := New(enabledCfg).CreatePrivateChannel(context.Background(), "xoxb", "agentrq-demo")
	if err == nil || !strings.Contains(err.Error(), "restricted_action") {
		t.Fatalf("err = %v, want restricted_action", err)
	}
}

func TestCreatePrivateChannelReusesATakenNameFoundOnALaterPage(t *testing.T) {
	api := newFakeSlackAPI(t, map[string]func(url.Values) string{
		"conversations.create": reply(`{"ok":false,"error":"name_taken"}`),
		"conversations.list": func(form url.Values) string {
			if form.Get("cursor") == "" {
				return `{"ok":true,"channels":[{"id":"C_OTHER","name":"other"}],"response_metadata":{"next_cursor":"page2"}}`
			}
			return `{"ok":true,"channels":[{"id":"C_OLD","name":"agentrq-demo"}],"response_metadata":{"next_cursor":""}}`
		},
	})

	id, err := New(enabledCfg).CreatePrivateChannel(context.Background(), "xoxb", "agentrq-demo")
	if err != nil {
		t.Fatalf("CreatePrivateChannel: %v", err)
	}
	if id != "C_OLD" {
		t.Errorf("channel = %q, want the existing C_OLD", id)
	}
	want := []string{"conversations.create", "conversations.list", "conversations.list"}
	if got := api.methods(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestCreatePrivateChannelFallsBackToAUniqueName(t *testing.T) {
	tests := []struct {
		name string
		list string
	}{
		{"the taken channel is not visible", `{"ok":true,"channels":[{"id":"C_OTHER","name":"other"}],"response_metadata":{"next_cursor":""}}`},
		{"listing is not allowed", `{"ok":false,"error":"missing_scope"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fallbackName string
			newFakeSlackAPI(t, map[string]func(url.Values) string{
				"conversations.create": func(form url.Values) string {
					if form.Get("name") == "agentrq-demo" {
						return `{"ok":false,"error":"name_taken"}`
					}
					fallbackName = form.Get("name")
					return `{"ok":true,"channel":{"id":"C_UNIQUE"}}`
				},
				"conversations.list": reply(tt.list),
			})

			id, err := New(enabledCfg).CreatePrivateChannel(context.Background(), "xoxb", "agentrq-demo")
			if err != nil {
				t.Fatalf("CreatePrivateChannel: %v", err)
			}
			if id != "C_UNIQUE" {
				t.Errorf("channel = %q, want C_UNIQUE", id)
			}
			suffix := strings.TrimPrefix(fallbackName, "agentrq-demo-")
			if suffix == fallbackName {
				t.Fatalf("fallback name %q does not extend the taken name", fallbackName)
			}
			if _, err := strconv.Atoi(suffix); err != nil {
				t.Errorf("fallback suffix %q is not numeric", suffix)
			}
		})
	}
}

func TestCreatePrivateChannelFailsWhenTheFallbackFailsToo(t *testing.T) {
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"conversations.create": func(form url.Values) string {
			if form.Get("name") == "agentrq-demo" {
				return `{"ok":false,"error":"name_taken"}`
			}
			return `{"ok":false,"error":"too_many_channels"}`
		},
		"conversations.list": reply(`{"ok":false,"error":"missing_scope"}`),
	})

	_, err := New(enabledCfg).CreatePrivateChannel(context.Background(), "xoxb", "agentrq-demo")
	if err == nil || !strings.Contains(err.Error(), "fallback") || !strings.Contains(err.Error(), "too_many_channels") {
		t.Fatalf("err = %v, want the fallback failure", err)
	}
}

func TestInviteUsersToChannel(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		wantErr bool
	}{
		{"invited", `{"ok":true,"channel":{"id":"C1"}}`, false},
		{"already in the channel is not an error", `{"ok":false,"error":"already_in_channel"}`, false},
		{"any other failure is", `{"ok":false,"error":"user_not_found"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var users string
			newFakeSlackAPI(t, map[string]func(url.Values) string{
				"conversations.invite": func(form url.Values) string {
					users = form.Get("users")
					return tt.answer
				},
			})

			err := New(enabledCfg).InviteUsersToChannel(context.Background(), "xoxb", "C1", []string{"U1", "U2"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if users != "U1,U2" {
				t.Errorf("users = %q, want U1,U2", users)
			}
		})
	}
}

func TestPostMessageReturnsTheTimestamp(t *testing.T) {
	var form url.Values
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"chat.postMessage": func(f url.Values) string {
			form = f
			return `{"ok":true,"channel":"C1","ts":"111.222"}`
		},
	})

	ts, err := New(enabledCfg).PostMessage(context.Background(), "xoxb", "C1", BuildResultBlocks("hello"))
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "111.222" {
		t.Errorf("ts = %q, want 111.222", ts)
	}
	if form.Get("channel") != "C1" || !strings.Contains(form.Get("blocks"), "hello") {
		t.Errorf("posted form = %v, want channel C1 and the blocks", form)
	}
	if form.Get("thread_ts") != "" {
		t.Errorf("a top-level message carried thread_ts %q", form.Get("thread_ts"))
	}
}

func TestPostMessageReportsTheSlackError(t *testing.T) {
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"chat.postMessage": reply(`{"ok":false,"error":"channel_not_found"}`),
	})

	_, err := New(enabledCfg).PostMessage(context.Background(), "xoxb", "C1", BuildResultBlocks("hello"))
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("err = %v, want channel_not_found", err)
	}
}

func TestPostThreadReplyPostsIntoTheThread(t *testing.T) {
	var form url.Values
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"chat.postMessage": func(f url.Values) string {
			form = f
			return `{"ok":true,"channel":"C1","ts":"333.444"}`
		},
	})

	ts, err := New(enabledCfg).PostThreadReply(context.Background(), "xoxb", "C1", "111.222", BuildResultBlocks("reply"))
	if err != nil {
		t.Fatalf("PostThreadReply: %v", err)
	}
	if ts != "333.444" {
		t.Errorf("ts = %q, want 333.444", ts)
	}
	if form.Get("thread_ts") != "111.222" {
		t.Errorf("thread_ts = %q, want 111.222", form.Get("thread_ts"))
	}
}

func TestPostThreadReplyReportsTheSlackError(t *testing.T) {
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"chat.postMessage": reply(`{"ok":false,"error":"thread_not_found"}`),
	})

	_, err := New(enabledCfg).PostThreadReply(context.Background(), "xoxb", "C1", "111.222", BuildResultBlocks("reply"))
	if err == nil || !strings.Contains(err.Error(), "thread_not_found") {
		t.Fatalf("err = %v, want thread_not_found", err)
	}
}

func TestUpdateMessage(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		wantErr bool
	}{
		{"updated", `{"ok":true,"channel":"C1","ts":"111.222","text":""}`, false},
		{"refused", `{"ok":false,"error":"message_not_found"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var form url.Values
			newFakeSlackAPI(t, map[string]func(url.Values) string{
				"chat.update": func(f url.Values) string {
					form = f
					return tt.answer
				},
			})

			err := New(enabledCfg).UpdateMessage(context.Background(), "xoxb", "C1", "111.222", BuildResultBlocks("done"))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if form.Get("channel") != "C1" || form.Get("ts") != "111.222" {
				t.Errorf("update form = %v, want channel C1 ts 111.222", form)
			}
		})
	}
}

func TestExchangeCodeReturnsTheInstallation(t *testing.T) {
	var form url.Values
	newFakeSlackAPI(t, map[string]func(url.Values) string{
		"oauth.v2.access": func(f url.Values) string {
			form = f
			return `{"ok":true,"access_token":"xoxb-new","bot_user_id":"B1","team":{"id":"T1"},"authed_user":{"id":"U1"}}`
		},
	})

	token, team, bot, user, err := New(enabledCfg).ExchangeCode(context.Background(), "the-code", "https://app/slack/oauth/callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token != "xoxb-new" || team != "T1" || bot != "B1" || user != "U1" {
		t.Errorf("got (%q, %q, %q, %q), want (xoxb-new, T1, B1, U1)", token, team, bot, user)
	}
	if form.Get("code") != "the-code" || form.Get("client_id") != "client-id" ||
		form.Get("client_secret") != "client-secret" || form.Get("redirect_uri") != "https://app/slack/oauth/callback" {
		t.Errorf("exchange form = %v", form)
	}
}

func TestExchangeCodeFailures(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		wantErr string
	}{
		{"slack names the error", `{"ok":false,"error":"invalid_code"}`, "oauth exchange failed"},
		{"slack says no without a reason", `{"ok":false}`, "oauth exchange response error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newFakeSlackAPI(t, map[string]func(url.Values) string{
				"oauth.v2.access": reply(tt.answer),
			})

			_, _, _, _, err := New(enabledCfg).ExchangeCode(context.Background(), "code", "https://app/cb")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestExchangeCodeFailsWhenSlackIsUnreachable(t *testing.T) {
	api := newFakeSlackAPI(t, nil)
	api.server.Close()

	_, _, _, _, err := New(enabledCfg).ExchangeCode(context.Background(), "code", "https://app/cb")
	if err == nil || !strings.Contains(err.Error(), "oauth exchange failed") {
		t.Fatalf("err = %v, want oauth exchange failed", err)
	}
}

func signedRequest(secret, ts, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	if ts != "" {
		r.Header.Set("X-Slack-Request-Timestamp", ts)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	r.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func TestVerifyRequest(t *testing.T) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	body := `{"type":"event_callback"}`

	tests := []struct {
		name    string
		cfg     Config
		req     *http.Request
		wantErr string
	}{
		{"valid", enabledCfg, signedRequest("signing-secret", now, body), ""},
		{"no signing secret configured", Config{}, signedRequest("signing-secret", now, body), "signing secret not configured"},
		{"no timestamp", enabledCfg, signedRequest("signing-secret", "", body), "missing X-Slack-Request-Timestamp"},
		{"timestamp is not a number", enabledCfg, signedRequest("signing-secret", "yesterday", body), "invalid timestamp"},
		{"replayed request", enabledCfg, signedRequest("signing-secret", old, body), "too old"},
		{"signed with another secret", enabledCfg, signedRequest("other-secret", now, body), "invalid signature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.cfg).VerifyRequest(tt.req, []byte(body))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("VerifyRequest: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyRequestRejectsATamperedBody(t *testing.T) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	r := signedRequest("signing-secret", now, `{"a":1}`)
	if err := New(enabledCfg).VerifyRequest(r, []byte(`{"a":2}`)); err == nil {
		t.Fatal("a body other than the signed one was accepted")
	}
}

func sectionText(t *testing.T, b slackapi.Block) string {
	t.Helper()
	s, ok := b.(*slackapi.SectionBlock)
	if !ok {
		t.Fatalf("block is %T, want *SectionBlock", b)
	}
	return s.Text.Text
}

func actionIDs(t *testing.T, b slackapi.Block) []string {
	t.Helper()
	a, ok := b.(*slackapi.ActionBlock)
	if !ok {
		t.Fatalf("block is %T, want *ActionBlock", b)
	}
	var ids []string
	for _, el := range a.Elements.ElementSet {
		ids = append(ids, el.(*slackapi.ButtonBlockElement).ActionID)
	}
	return ids
}

func TestBuildTaskBlocks(t *testing.T) {
	plain := BuildTaskBlocks("W", "T", "Fix <b>", "a & b", false)
	if len(plain) != 2 {
		t.Fatalf("blocks = %d, want header and divider only", len(plain))
	}
	if got := sectionText(t, plain[0]); !strings.Contains(got, "Fix &lt;b&gt;") || !strings.Contains(got, "a &amp; b") {
		t.Errorf("header = %q, want the title and body escaped", got)
	}

	withButtons := BuildTaskBlocks("W", "T", "title", "body", true)
	if len(withButtons) != 3 {
		t.Fatalf("blocks = %d, want header, divider and buttons", len(withButtons))
	}
	want := "task_respond:W:T:allow,task_respond:W:T:allow_all,task_respond:W:T:reject"
	if got := strings.Join(actionIDs(t, withButtons[2]), ","); got != want {
		t.Errorf("action ids = %q, want %q", got, want)
	}
}

func TestBuildMessageBlocksNamesTheSender(t *testing.T) {
	tests := []struct {
		sender string
		want   string
	}{
		{"agent", "🤖 *Agent:* hi &lt;there&gt;"},
		{"human", "👤 *Human:* hi &lt;there&gt;"},
		{"Ada", "👤 *Ada:* hi &lt;there&gt;"},
	}
	for _, tt := range tests {
		blocks := BuildMessageBlocks(tt.sender, "hi <there>")
		if got := sectionText(t, blocks[0]); got != tt.want {
			t.Errorf("sender %q: text = %q, want %q", tt.sender, got, tt.want)
		}
	}
}

func TestBuildSystemAndResultBlocksKeepMentions(t *testing.T) {
	if got := sectionText(t, BuildSystemMessageBlocks("ping <@U1>")[0]); got != "ping <@U1>" {
		t.Errorf("system text = %q, want the mention unescaped", got)
	}
	if got := sectionText(t, BuildResultBlocks("done by <@U1>")[0]); got != "done by <@U1>" {
		t.Errorf("result text = %q, want the mention unescaped", got)
	}
}

func TestBuildPermissionRequestBlocks(t *testing.T) {
	blocks := BuildPermissionRequestBlocks("W", "T", "R", "bash <ls>")
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want text and buttons", len(blocks))
	}
	if got := sectionText(t, blocks[0]); !strings.Contains(got, "bash &lt;ls&gt;") {
		t.Errorf("text = %q, want the tool escaped", got)
	}
	want := "task_permission:W:T:R:allow,task_permission:W:T:R:deny"
	if got := strings.Join(actionIDs(t, blocks[1]), ","); got != want {
		t.Errorf("action ids = %q, want %q", got, want)
	}
	if _, err := json.Marshal(blocks); err != nil {
		t.Errorf("blocks do not marshal: %v", err)
	}
}

func TestBuildChannelNameFromWorkspace(t *testing.T) {
	tests := []struct {
		name string
		in   string
		id   int64
		want string
	}{
		{"spaces and punctuation", "My Cool_Project! (v2)", 35, "agentrq-my-cool-project-v2-z"},
		{"edges trimmed", "--Hello--", 36, "agentrq-hello-10"},
		{"long name cut to sixty without a trailing hyphen", strings.Repeat("a", 59) + " bcd", 1, "agentrq-" + strings.Repeat("a", 59) + "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildChannelNameFromWorkspace(tt.in, tt.id); got != tt.want {
				t.Errorf("BuildChannelNameFromWorkspace(%q, %d) = %q, want %q", tt.in, tt.id, got, tt.want)
			}
		})
	}
}
