// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	crudctrl "github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	slacksvc "github.com/agentrq/agentrq/backend/internal/service/slack"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	slackapi "github.com/slack-go/slack"
	"gorm.io/datatypes"
)

const tokenKey32 = "0123456789abcdef0123456789abcdef"

var errChannelNotFound = errors.New("slack: post message to C1: channel_not_found")

// fakeSlack is a slacksvc.Service whose answers each test sets, and which
// records what it was asked to do.
type fakeSlack struct {
	mu       sync.Mutex
	disabled bool

	postTS  string
	postErr error

	// replyTS and replyErr answer PostThreadReply call by call; past their
	// end the reply succeeds with an empty ts.
	replyTS  []string
	replyErr []error

	updateErr error

	createID  string
	createErr error
	inviteErr error

	exchangeErr error

	posts   []string
	replies []string
	updates []string
	creates []string
	invites []string
}

func (f *fakeSlack) IsEnabled() bool  { return !f.disabled }
func (f *fakeSlack) ClientID() string { return "client-id" }

func (f *fakeSlack) CreatePrivateChannel(ctx context.Context, token, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates = append(f.creates, token+" "+name)
	return f.createID, f.createErr
}

func (f *fakeSlack) InviteUsersToChannel(ctx context.Context, token, channelID string, userIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invites = append(f.invites, channelID+" "+strings.Join(userIDs, ","))
	return f.inviteErr
}

func (f *fakeSlack) PostMessage(ctx context.Context, token, channelID string, blocks []slackapi.Block) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, channelID+" "+blocksText(blocks))
	return f.postTS, f.postErr
}

func (f *fakeSlack) PostThreadReply(ctx context.Context, token, channelID, threadTS string, blocks []slackapi.Block) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := len(f.replies)
	f.replies = append(f.replies, channelID+"/"+threadTS+" "+blocksText(blocks))
	var ts string
	var err error
	if i < len(f.replyTS) {
		ts = f.replyTS[i]
	}
	if i < len(f.replyErr) {
		err = f.replyErr[i]
	}
	return ts, err
}

func (f *fakeSlack) UpdateMessage(ctx context.Context, token, channelID, ts string, blocks []slackapi.Block) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, channelID+"/"+ts+" "+blocksText(blocks))
	return f.updateErr
}

func (f *fakeSlack) ExchangeCode(ctx context.Context, code, redirectURI string) (string, string, string, string, error) {
	if f.exchangeErr != nil {
		return "", "", "", "", f.exchangeErr
	}
	return "xoxb-installed", "T1", "B1", "U_INSTALLER", nil
}

func (f *fakeSlack) VerifyRequest(r *http.Request, body []byte) error { return nil }

// blocksText flattens blocks to their text and action ids, for assertions.
func blocksText(blocks []slackapi.Block) string {
	var parts []string
	for _, b := range blocks {
		switch v := b.(type) {
		case *slackapi.SectionBlock:
			parts = append(parts, v.Text.Text)
		case *slackapi.ActionBlock:
			for _, el := range v.Elements.ElementSet {
				if btn, ok := el.(*slackapi.ButtonBlockElement); ok {
					parts = append(parts, btn.ActionID)
				}
			}
		}
	}
	return strings.Join(parts, " | ")
}

// accessCRUD answers CheckWorkspaceAccess as told.
type accessCRUD struct {
	mockCRUD
	ok  bool
	err error
}

func (a *accessCRUD) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	return a.ok, a.err
}

// verdictMCP is an MCPManager whose verdicts fail with verdictErr.
type verdictMCP struct {
	mockMCP
	verdictErr error
	notified   []entity.Attachment
}

func (m *verdictMCP) SendPermissionVerdict(ctx context.Context, workspaceID int64, userID string, taskID int64, requestID, behavior string) error {
	return m.verdictErr
}

func (m *verdictMCP) SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string, attachments ...entity.Attachment) {
	m.pushed = content
	m.notified = attachments
}

type failingStateTokenSvc struct{ mockTokenSvc }

func (*failingStateTokenSvc) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	return "", errors.New("signing key unavailable")
}

type flowFixture struct {
	repo   *mock_repo.MockRepository
	pubsub *mock_pubsub.MockService
	slack  *fakeSlack
	crud   *mockCRUD
	mcp    *verdictMCP
	params Params
}

func newFlowFixture(t *testing.T) *flowFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &flowFixture{
		repo:   mock_repo.NewMockRepository(ctrl),
		pubsub: mock_pubsub.NewMockService(ctrl),
		slack:  &fakeSlack{},
		crud:   &mockCRUD{},
		mcp:    &verdictMCP{},
	}
	f.params = Params{
		Repository: f.repo,
		SlackSvc:   f.slack,
		Crud:       f.crud,
		MCPManager: f.mcp,
		PubSub:     f.pubsub,
		TokenSvc:   &mockTokenSvc{},
		TokenKey:   tokenKey32,
		BaseURL:    "https://app.agentrq.com",
	}
	return f
}

func (f *flowFixture) controller() *controller { return New(f.params).(*controller) }

// authorizedLink is a workspace link whose bot token decrypts with tokenKey32.
func authorizedLink(t *testing.T, workspaceID int64) model.SlackWorkspaceLink {
	t.Helper()
	enc, nonce, err := security.Encrypt("xoxb-bot", tokenKey32)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return model.SlackWorkspaceLink{
		WorkspaceID:      workspaceID,
		AccessToken:      enc,
		TokenNonce:       nonce,
		SlackChannelID:   "C1",
		SlackChannelName: "agentrq-demo",
	}
}

// undecryptableLink has a token that cannot be decrypted.
func undecryptableLink(workspaceID int64) model.SlackWorkspaceLink {
	return model.SlackWorkspaceLink{WorkspaceID: workspaceID, AccessToken: "not-hex", TokenNonce: "nope", SlackChannelID: "C1"}
}

var errNotFound = errors.New("record not found")

// ─── OnWorkspaceCreated / OnTaskCreated ────────────────────────────────────────

func TestOnWorkspaceCreatedDoesNothing(t *testing.T) {
	f := newFlowFixture(t)
	if err := f.controller().OnWorkspaceCreated(context.Background(), entity.Workspace{ID: 1}); err != nil {
		t.Fatalf("OnWorkspaceCreated: %v", err)
	}
}

func TestOnTaskCreatedSkipsWithoutAnAuthorizedLink(t *testing.T) {
	task := entity.Task{ID: 1, WorkspaceID: 42}

	t.Run("slack disabled", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.disabled = true
		if err := f.controller().OnTaskCreated(context.Background(), task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no link", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		if err := f.controller().OnTaskCreated(context.Background(), task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("link without a token", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{SlackChannelID: "C1"}, nil)
		if err := f.controller().OnTaskCreated(context.Background(), task); err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(f.slack.posts) != 0 {
			t.Errorf("posted %v", f.slack.posts)
		}
	})
}

func TestOnTaskCreatedFailsOnAnUndecryptableToken(t *testing.T) {
	f := newFlowFixture(t)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(undecryptableLink(42), nil)
	if err := f.controller().OnTaskCreated(context.Background(), entity.Task{ID: 1, WorkspaceID: 42}); err == nil {
		t.Fatal("expected a decryption error")
	}
	if len(f.slack.posts) != 0 {
		t.Errorf("posted %v", f.slack.posts)
	}
}

func TestOnTaskCreatedOffersApprovalForAnAgentsOwnTask(t *testing.T) {
	f := newFlowFixture(t)
	f.slack.postTS = "100.1"
	link := authorizedLink(t, 42)
	link.BotUserID = "B_BOT"
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(link, nil)
	f.repo.EXPECT().UpsertSlackTaskThread(gomock.Any(), model.SlackTaskThread{TaskID: 7, WorkspaceID: 42, SlackChannelID: "C1", ThreadTS: "100.1"}).Return(nil)

	task := entity.Task{ID: 7, WorkspaceID: 42, Title: "Deploy", Body: "now", Assignee: "agent", Status: "notstarted", CreatedBy: "agent"}
	if err := f.controller().OnTaskCreated(context.Background(), task); err != nil {
		t.Fatalf("OnTaskCreated: %v", err)
	}

	approve := "task_respond:" + monoflake.ID(42).String() + ":" + monoflake.ID(7).String() + ":allow"
	if len(f.slack.posts) != 1 || !strings.Contains(f.slack.posts[0], approve) {
		t.Errorf("posts = %v, want the task with approval buttons", f.slack.posts)
	}
	if len(f.slack.replies) != 1 || !strings.Contains(f.slack.replies[0], "<@B_BOT> <message>") || !strings.HasPrefix(f.slack.replies[0], "C1/100.1 ") {
		t.Errorf("replies = %v, want the instruction naming the bot in the thread", f.slack.replies)
	}
}

func TestOnTaskCreatedStillSucceedsWhenTheThreadIsNotSaved(t *testing.T) {
	f := newFlowFixture(t)
	f.slack.postTS = "100.1"
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
	f.repo.EXPECT().UpsertSlackTaskThread(gomock.Any(), gomock.Any()).Return(errors.New("database unavailable"))

	if err := f.controller().OnTaskCreated(context.Background(), entity.Task{ID: 7, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnTaskCreated: %v", err)
	}
	if len(f.slack.replies) != 0 {
		t.Errorf("replies = %v, want no instruction for an unsaved thread", f.slack.replies)
	}
}

func TestOnTaskCreatedUnlinksAWorkspaceWhoseChannelIsGone(t *testing.T) {
	f := newFlowFixture(t)
	f.slack.postTS = "100.1"
	f.slack.replyErr = []error{errChannelNotFound}
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
	f.repo.EXPECT().UpsertSlackTaskThread(gomock.Any(), gomock.Any()).Return(nil)
	// The unlink failing too is only logged.
	f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(errors.New("database unavailable"))

	if err := f.controller().OnTaskCreated(context.Background(), entity.Task{ID: 7, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnTaskCreated: %v", err)
	}
	if len(f.slack.replies) != 1 || !strings.Contains(f.slack.replies[0], "`@agentrq <message>`") {
		t.Errorf("replies = %v, want the generic instruction", f.slack.replies)
	}
}

// ─── OnMessageCreated ──────────────────────────────────────────────────────────

func TestOnMessageCreatedSkipsWithoutAThreadOrToken(t *testing.T) {
	msg := entity.Message{ID: 1, Sender: "agent", Text: "hi"}
	task := entity.Task{ID: 9, WorkspaceID: 42}

	t.Run("slack disabled", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.disabled = true
		if err := f.controller().OnMessageCreated(context.Background(), msg, task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no thread", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackTaskThreadByTask(gomock.Any(), int64(9)).Return(model.SlackTaskThread{}, errNotFound)
		if err := f.controller().OnMessageCreated(context.Background(), msg, task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no link", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackTaskThreadByTask(gomock.Any(), int64(9)).Return(model.SlackTaskThread{SlackChannelID: "C1", ThreadTS: "1.1"}, nil)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		if err := f.controller().OnMessageCreated(context.Background(), msg, task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("undecryptable token", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackTaskThreadByTask(gomock.Any(), int64(9)).Return(model.SlackTaskThread{SlackChannelID: "C1", ThreadTS: "1.1"}, nil)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(undecryptableLink(42), nil)
		if err := f.controller().OnMessageCreated(context.Background(), msg, task); err == nil {
			t.Fatal("expected a decryption error")
		}
	})
}

func (f *flowFixture) expectThreadAndLink(t *testing.T) {
	t.Helper()
	f.repo.EXPECT().GetSlackTaskThreadByTask(gomock.Any(), int64(9)).
		Return(model.SlackTaskThread{TaskID: 9, WorkspaceID: 42, SlackChannelID: "C1", ThreadTS: "1.1"}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
}

func TestOnMessageCreatedUnlinksWhenTheReplyFindsNoChannel(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	f.slack.replyErr = []error{errChannelNotFound}
	f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(nil)

	msg := entity.Message{ID: 1, Sender: "human", UserID: 5, Text: "hi"}
	f.repo.EXPECT().SystemGetUser(gomock.Any(), int64(5)).Return(model.User{}, errNotFound)
	if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if len(f.slack.replies) != 1 || !strings.Contains(f.slack.replies[0], "*Human:* hi") {
		t.Errorf("replies = %v, want the plain human label when the user is unknown", f.slack.replies)
	}
}

func TestOnMessageCreatedPostsButtonsForAPendingPermissionRequest(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	f.slack.replyTS = []string{"2.1", "2.2"}

	var saved map[string]any
	f.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(9), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ int64, b []byte) error {
			return json.Unmarshal(b, &saved)
		})

	msg := entity.Message{ID: 1, Sender: "agent", Text: "may I?", Metadata: map[string]any{
		"type": "permission_request", "status": "pending", "requestId": "R1", "tool": "bash", "args": "ls -la",
	}}
	if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}

	if len(f.slack.replies) != 2 {
		t.Fatalf("replies = %v, want the text and the buttons", f.slack.replies)
	}
	allow := "task_permission:" + monoflake.ID(42).String() + ":" + monoflake.ID(9).String() + ":R1:allow"
	if !strings.Contains(f.slack.replies[1], "bash ls -la") || !strings.Contains(f.slack.replies[1], allow) {
		t.Errorf("buttons = %q", f.slack.replies[1])
	}
	if saved["slack_channel_id"] != "C1" || saved["slack_message_ts"] != "2.2" || saved["requestId"] != "R1" {
		t.Errorf("saved metadata = %v, want the buttons' coordinates added", saved)
	}
}

// structMetadata is permission-request metadata that is not a map, as a
// caller holding a typed value would pass it.
type structMetadata struct {
	Type      string `json:"type"`
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	ToolName  string `json:"tool_name"`
	Desc      string `json:"description"`
}

func TestOnMessageCreatedSavesCoordinatesForNonMapMetadata(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	f.slack.replyTS = []string{"2.1", "2.2"}

	var saved map[string]any
	f.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(9), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ int64, b []byte) error {
			_ = json.Unmarshal(b, &saved)
			return errors.New("database unavailable") // only logged
		})

	msg := entity.Message{ID: 1, Sender: "agent", Metadata: structMetadata{
		Type: "permission_request", Status: "pending", RequestID: "R2", ToolName: "edit", Desc: "main.go",
	}}
	if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if !strings.Contains(f.slack.replies[1], "edit main.go") {
		t.Errorf("buttons = %q, want the tool and its description", f.slack.replies[1])
	}
	if len(saved) != 2 || saved["slack_message_ts"] != "2.2" {
		t.Errorf("saved metadata = %v, want only the coordinates", saved)
	}
}

func TestOnMessageCreatedPermissionRequestEdgeCases(t *testing.T) {
	pending := func(extra map[string]any) map[string]any {
		m := map[string]any{"type": "permission_request", "status": "pending"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	t.Run("no request id posts no buttons", func(t *testing.T) {
		f := newFlowFixture(t)
		f.expectThreadAndLink(t)
		msg := entity.Message{ID: 1, Sender: "agent", Metadata: pending(nil)}
		if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(f.slack.replies) != 1 {
			t.Errorf("replies = %v, want the text only", f.slack.replies)
		}
	})
	t.Run("buttons not posted", func(t *testing.T) {
		f := newFlowFixture(t)
		f.expectThreadAndLink(t)
		f.slack.replyErr = []error{nil, errChannelNotFound}
		f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(nil)
		msg := entity.Message{ID: 1, Sender: "agent", Metadata: pending(map[string]any{"requestId": "R1"})}
		if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("buttons posted without a timestamp save nothing", func(t *testing.T) {
		f := newFlowFixture(t)
		f.expectThreadAndLink(t)
		msg := entity.Message{ID: 1, Sender: "agent", Metadata: pending(map[string]any{"requestId": "R1"})}
		if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(f.slack.replies) != 2 {
			t.Errorf("replies = %v, want text and buttons", f.slack.replies)
		}
	})
}

// ─── OnMessageUpdated ──────────────────────────────────────────────────────────

func TestOnMessageUpdatedIgnoresMessagesItHasNothingToDoFor(t *testing.T) {
	coords := map[string]any{"slack_channel_id": "C1", "slack_message_ts": "2.2"}
	with := func(m map[string]any) map[string]any {
		for k, v := range coords {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name     string
		metadata any
	}{
		{"no metadata", nil},
		{"metadata is not a map", "permission_request"},
		{"not a permission request", with(map[string]any{"type": "chat", "status": "allow"})},
		{"still pending", with(map[string]any{"type": "permission_request", "status": "pending"})},
		{"no slack coordinates", map[string]any{"type": "permission_request", "status": "deny"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t) // no repository call is expected
			msg := entity.Message{ID: 1, Metadata: tt.metadata}
			if err := f.controller().OnMessageUpdated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(f.slack.updates) != 0 {
				t.Errorf("updates = %v", f.slack.updates)
			}
		})
	}

	t.Run("slack disabled", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.disabled = true
		msg := entity.Message{ID: 1, Metadata: with(map[string]any{"type": "permission_request", "status": "deny"})}
		if err := f.controller().OnMessageUpdated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestOnMessageUpdatedNeedsAUsableLink(t *testing.T) {
	msg := entity.Message{ID: 1, Metadata: map[string]any{
		"type": "permission_request", "status": "deny", "slack_channel_id": "C1", "slack_message_ts": "2.2",
	}}

	t.Run("no link", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		if err := f.controller().OnMessageUpdated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("undecryptable token", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(undecryptableLink(42), nil)
		if err := f.controller().OnMessageUpdated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err == nil {
			t.Fatal("expected a decryption error")
		}
	})
	t.Run("denied, and the update fails", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.updateErr = errors.New("message_not_found")
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
		if err := f.controller().OnMessageUpdated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err == nil {
			t.Fatal("expected the update error")
		}
		if len(f.slack.updates) != 1 || !strings.Contains(f.slack.updates[0], "Denied by operator") {
			t.Errorf("updates = %v", f.slack.updates)
		}
	})
}

// ─── channel assignment ────────────────────────────────────────────────────────

func TestSetWorkspaceChannel(t *testing.T) {
	req := entity.SetWorkspaceSlackChannelRequest{WorkspaceID: 42, ChannelID: "C9", ChannelName: "ops", AutoCreated: false, UserID: "u"}

	t.Run("access check fails", func(t *testing.T) {
		f := newFlowFixture(t)
		f.params.Crud = &accessCRUD{err: errors.New("database unavailable")}
		if err := f.controller().SetWorkspaceChannel(context.Background(), req); err == nil || err.Error() != "access denied" {
			t.Fatalf("err = %v, want access denied", err)
		}
	})
	t.Run("no access", func(t *testing.T) {
		f := newFlowFixture(t)
		f.params.Crud = &accessCRUD{ok: false}
		if err := f.controller().SetWorkspaceChannel(context.Background(), req); err == nil || err.Error() != "access denied" {
			t.Fatalf("err = %v, want access denied", err)
		}
	})
	t.Run("not authorized yet", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		if err := f.controller().SetWorkspaceChannel(context.Background(), req); err == nil || !strings.Contains(err.Error(), "authorized first") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("assigned", func(t *testing.T) {
		f := newFlowFixture(t)
		link := authorizedLink(t, 42)
		link.AutoCreated = true
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(link, nil)
		want := link
		want.SlackChannelID, want.SlackChannelName, want.AutoCreated = "C9", "ops", false
		f.repo.EXPECT().UpsertSlackWorkspaceLink(gomock.Any(), want).Return(nil)
		if err := f.controller().SetWorkspaceChannel(context.Background(), req); err != nil {
			t.Fatalf("SetWorkspaceChannel: %v", err)
		}
	})
}

func TestRemoveWorkspaceChannel(t *testing.T) {
	req := entity.RemoveWorkspaceSlackChannelRequest{WorkspaceID: 42, UserID: "u"}

	t.Run("no access", func(t *testing.T) {
		f := newFlowFixture(t)
		f.params.Crud = &accessCRUD{ok: false}
		if err := f.controller().RemoveWorkspaceChannel(context.Background(), req); err == nil || err.Error() != "access denied" {
			t.Fatalf("err = %v, want access denied", err)
		}
	})
	t.Run("says goodbye in the channel, then unlinks", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
		f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(nil)
		if err := f.controller().RemoveWorkspaceChannel(context.Background(), req); err != nil {
			t.Fatalf("RemoveWorkspaceChannel: %v", err)
		}
		if len(f.slack.posts) != 1 || !strings.Contains(f.slack.posts[0], "unlinked from this channel") {
			t.Errorf("posts = %v", f.slack.posts)
		}
	})
	t.Run("unlinks even when the goodbye fails", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.postErr = errChannelNotFound
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
		f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(nil)
		if err := f.controller().RemoveWorkspaceChannel(context.Background(), req); err != nil {
			t.Fatalf("RemoveWorkspaceChannel: %v", err)
		}
	})
	t.Run("unlinks without a goodbye when the token is unreadable", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(undecryptableLink(42), nil)
		f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(errors.New("database unavailable"))
		if err := f.controller().RemoveWorkspaceChannel(context.Background(), req); err == nil {
			t.Fatal("expected the delete error")
		}
		if len(f.slack.posts) != 0 {
			t.Errorf("posts = %v", f.slack.posts)
		}
	})
}

func TestGetWorkspaceSlackConfig(t *testing.T) {
	t.Run("slack disabled", func(t *testing.T) {
		f := newFlowFixture(t)
		f.slack.disabled = true
		cfg, err := f.controller().GetWorkspaceSlackConfig(context.Background(), 42)
		if err != nil || cfg.Enabled {
			t.Fatalf("cfg = %+v, err = %v; want disabled", cfg, err)
		}
	})
	t.Run("installed", func(t *testing.T) {
		f := newFlowFixture(t)
		link := authorizedLink(t, 42)
		link.AutoCreated = true
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(link, nil)
		cfg, err := f.controller().GetWorkspaceSlackConfig(context.Background(), 42)
		if err != nil {
			t.Fatalf("GetWorkspaceSlackConfig: %v", err)
		}
		if !cfg.Enabled || !cfg.Installed || cfg.ChannelID != "C1" || cfg.ChannelName != "agentrq-demo" || !cfg.AutoCreated || cfg.ClientID != "client-id" {
			t.Errorf("cfg = %+v", cfg)
		}
		// mockTokenSvc signs the workspace id as the state.
		wantState := "state=" + monoflake.ID(42).String()
		if !strings.HasPrefix(cfg.AuthURL, "https://slack.com/oauth/v2/authorize?client_id=client-id&") ||
			!strings.Contains(cfg.AuthURL, "redirect_uri=https%3A%2F%2Fapp.agentrq.com%2Fslack%2Foauth%2Fcallback") ||
			!strings.HasSuffix(cfg.AuthURL, wantState) {
			t.Errorf("AuthURL = %q", cfg.AuthURL)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		f := newFlowFixture(t)
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{SlackChannelID: "C1"}, nil)
		cfg, err := f.controller().GetWorkspaceSlackConfig(context.Background(), 42)
		if err != nil {
			t.Fatalf("GetWorkspaceSlackConfig: %v", err)
		}
		if cfg.Installed || cfg.ChannelID != "" || cfg.AuthURL == "" {
			t.Errorf("cfg = %+v, want an install link and no channel", cfg)
		}
	})
	t.Run("state cannot be signed", func(t *testing.T) {
		f := newFlowFixture(t)
		f.params.TokenSvc = &failingStateTokenSvc{}
		f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		if _, err := f.controller().GetWorkspaceSlackConfig(context.Background(), 42); err == nil {
			t.Fatal("expected an error")
		}
	})
}

// ─── HandleOAuthCallback ───────────────────────────────────────────────────────

func TestHandleOAuthCallbackFailures(t *testing.T) {
	ws62 := monoflake.ID(42).String()
	tests := []struct {
		name    string
		ws62    string
		setup   func(f *flowFixture)
		wantErr string
	}{
		{"no workspace in the state", "", nil, "invalid workspace ID"},
		{"workspace gone", ws62, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{}, errNotFound)
		}, "workspace not found"},
		{"code refused", ws62, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
			f.slack.exchangeErr = errors.New("invalid_code")
		}, "oauth exchange failed"},
		{"token cannot be encrypted", ws62, func(f *flowFixture) {
			f.params.TokenKey = "short"
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
		}, "failed to encrypt token"},
		{"channel cannot be created", ws62, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
			f.slack.createErr = errors.New("restricted_action")
		}, "failed to auto-provision channel"},
		{"link cannot be saved", ws62, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
			f.repo.EXPECT().UpsertSlackWorkspaceLink(gomock.Any(), gomock.Any()).Return(errors.New("database unavailable"))
		}, "failed to save workspace link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			err := f.controller().HandleOAuthCallback(context.Background(), tt.ws62, "code", "https://app/cb")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleOAuthCallbackProvisionsAChannelAndInvitesTheInstaller(t *testing.T) {
	for _, inviteErr := range []error{nil, errors.New("user_not_found")} {
		t.Run(fmt.Sprintf("invite error %v", inviteErr), func(t *testing.T) {
			f := newFlowFixture(t)
			f.slack.createID = "C_NEW"
			f.slack.inviteErr = inviteErr
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo Space"}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)

			var saved model.SlackWorkspaceLink
			f.repo.EXPECT().UpsertSlackWorkspaceLink(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, l model.SlackWorkspaceLink) error { saved = l; return nil })

			if err := f.controller().HandleOAuthCallback(context.Background(), monoflake.ID(42).String(), "code", "https://app/cb"); err != nil {
				t.Fatalf("HandleOAuthCallback: %v", err)
			}

			channel := slacksvc.BuildChannelNameFromWorkspace("Demo Space", 42)
			if len(f.slack.creates) != 1 || f.slack.creates[0] != "xoxb-installed "+channel {
				t.Errorf("creates = %v, want %q with the plain installed token", f.slack.creates, channel)
			}
			if len(f.slack.invites) != 1 || f.slack.invites[0] != "C_NEW U_INSTALLER" {
				t.Errorf("invites = %v", f.slack.invites)
			}
			if saved.WorkspaceID != 42 || saved.SlackChannelID != "C_NEW" || saved.SlackChannelName != channel ||
				!saved.AutoCreated || saved.TeamID != "T1" || saved.BotUserID != "B1" {
				t.Errorf("saved = %+v", saved)
			}
			token, err := security.Decrypt(saved.AccessToken, tokenKey32, saved.TokenNonce)
			if err != nil || token != "xoxb-installed" {
				t.Errorf("saved token decrypts to %q, %v", token, err)
			}
		})
	}
}

type noInstallerSlack struct{ *fakeSlack }

func (s noInstallerSlack) ExchangeCode(ctx context.Context, code, redirectURI string) (string, string, string, string, error) {
	return "xoxb-installed", "T1", "B1", "", nil
}

func TestHandleOAuthCallbackInvitesNobodyWithoutAnInstaller(t *testing.T) {
	f := newFlowFixture(t)
	f.slack.createID = "C_NEW"
	f.params.SlackSvc = noInstallerSlack{f.slack}
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
	f.repo.EXPECT().UpsertSlackWorkspaceLink(gomock.Any(), gomock.Any()).Return(nil)

	if err := f.controller().HandleOAuthCallback(context.Background(), monoflake.ID(42).String(), "code", "https://app/cb"); err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if len(f.slack.invites) != 0 {
		t.Errorf("invites = %v", f.slack.invites)
	}
}

func TestHandleOAuthCallbackKeepsAnAssignedChannel(t *testing.T) {
	f := newFlowFixture(t)
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, Name: "Demo"}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
	var saved model.SlackWorkspaceLink
	f.repo.EXPECT().UpsertSlackWorkspaceLink(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, l model.SlackWorkspaceLink) error { saved = l; return nil })

	if err := f.controller().HandleOAuthCallback(context.Background(), monoflake.ID(42).String(), "code", "https://app/cb"); err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if len(f.slack.creates) != 0 || saved.SlackChannelID != "C1" || saved.BotUserID != "B1" {
		t.Errorf("creates = %v, saved = %+v; want the existing channel kept", f.slack.creates, saved)
	}
}

// ─── HandleSlackEvent ──────────────────────────────────────────────────────────

func mention(text, threadTS string, files ...SlackFile) SlackEventPayload {
	var p SlackEventPayload
	p.Event.Type = "app_mention"
	p.Event.Channel = "C1"
	p.Event.ThreadTS = threadTS
	p.Event.User = "U_USER"
	p.Event.Text = text
	p.Event.Files = files
	return p
}

func TestHandleSlackEventIgnoresWhatIsNotAThreadMention(t *testing.T) {
	f := newFlowFixture(t)
	var other SlackEventPayload
	other.Event.Type = "message"
	if err := f.controller().HandleSlackEvent(context.Background(), other); err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := f.controller().HandleSlackEvent(context.Background(), mention("hi", "")); err != nil {
		t.Fatalf("err = %v", err)
	}

	f.repo.EXPECT().GetSlackTaskThreadByChannel(gomock.Any(), "C1", "1.1").Return(model.SlackTaskThread{}, errNotFound)
	if err := f.controller().HandleSlackEvent(context.Background(), mention("hi", "1.1")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func (f *flowFixture) expectMentionThread(link model.SlackWorkspaceLink, linkErr error) {
	f.repo.EXPECT().GetSlackTaskThreadByChannel(gomock.Any(), "C1", "1.1").Return(model.SlackTaskThread{TaskID: 9, WorkspaceID: 42}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(link, linkErr)
}

func TestHandleSlackEventIgnoresAnEmptyMention(t *testing.T) {
	f := newFlowFixture(t)
	link := authorizedLink(t, 42)
	link.BotUserID = "B_BOT"
	f.expectMentionThread(link, nil)
	if err := f.controller().HandleSlackEvent(context.Background(), mention("  <@B_BOT>  ", "1.1")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleSlackEventRepliesWithoutAttachmentsItCannotFetch(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer missing.Close()

	f := newFlowFixture(t)
	var got entity.ReplyToTaskRequest
	f.crud.replyToTaskFunc = func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
		got = req
		return &entity.ReplyToTaskResponse{}, nil
	}
	f.expectMentionThread(authorizedLink(t, 42), nil)
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, UserID: 100}, nil)

	err := f.controller().HandleSlackEvent(context.Background(), mention("see files", "1.1",
		SlackFile{ID: "F0", Name: "inline.txt"}, // no download URL
		SlackFile{ID: "F1", Name: "gone.txt", URLPrivateDownload: missing.URL},
	))
	if err != nil {
		t.Fatalf("HandleSlackEvent: %v", err)
	}
	if got.Text != "see files" || len(got.Attachments) != 0 || got.SlackUser != "U_USER" || got.UserID != monoflake.ID(100).String() {
		t.Errorf("reply = %+v", got)
	}
	want := "[Reply to task " + monoflake.ID(9).String() + "] see files"
	if f.mcp.pushed != want {
		t.Errorf("pushed %q, want %q", f.mcp.pushed, want)
	}
}

func TestHandleSlackEventTellsTheAgentAboutTheFilesItSaved(t *testing.T) {
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("png"))
	}))
	defer files.Close()

	f := newFlowFixture(t)
	ids := mock_idgen.NewMockService(gomock.NewController(t))
	ids.EXPECT().NextID().Return(int64(62))
	store, err := storage.NewNestedPublic(t.TempDir(), "https://agentrq.example/storage/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	// Saves the files as the real ReplyToTask does.
	f.crud.replyToTaskFunc = func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
		crudctrl.SaveAttachments(store, ids, req.WorkspaceID, req.TaskID, req.Attachments)
		return &entity.ReplyToTaskResponse{}, nil
	}
	f.expectMentionThread(authorizedLink(t, 42), nil)
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, UserID: 100}, nil)

	err = f.controller().HandleSlackEvent(context.Background(), mention("see the shot", "1.1",
		SlackFile{ID: "F1", Name: "shot.png", MimeType: "image/png", URLPrivateDownload: files.URL}))
	if err != nil {
		t.Fatalf("HandleSlackEvent: %v", err)
	}
	id := monoflake.ID(62).String()
	want := "[Reply to task " + monoflake.ID(9).String() + "] see the shot\nAttachments:\n  - id=" + id + " name=shot.png type=image/png"
	if f.mcp.pushed != want {
		t.Errorf("pushed %q, want %q", f.mcp.pushed, want)
	}
	if len(f.mcp.notified) != 1 || f.mcp.notified[0].ID != id {
		t.Errorf("attachments handed over = %+v", f.mcp.notified)
	}
}

func TestHandleSlackEventSkipsDownloadsWithAnUndecryptableToken(t *testing.T) {
	f := newFlowFixture(t)
	f.params.MCPManager = nil
	var got entity.ReplyToTaskRequest
	f.crud.replyToTaskFunc = func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
		got = req
		return &entity.ReplyToTaskResponse{}, nil
	}
	link := undecryptableLink(42)
	link.BotUserID = "B_BOT"
	f.expectMentionThread(link, nil)
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, UserID: 100}, nil)

	err := f.controller().HandleSlackEvent(context.Background(), mention("<@B_BOT> hello", "1.1",
		SlackFile{ID: "F1", Name: "a.txt", URLPrivateDownload: "http://127.0.0.1:1/never"}))
	if err != nil {
		t.Fatalf("HandleSlackEvent: %v", err)
	}
	if got.Text != "hello" || len(got.Attachments) != 0 {
		t.Errorf("reply = %+v", got)
	}
}

func TestHandleSlackEventFailures(t *testing.T) {
	t.Run("workspace gone", func(t *testing.T) {
		f := newFlowFixture(t)
		f.expectMentionThread(model.SlackWorkspaceLink{}, errNotFound)
		f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{}, errNotFound)
		if err := f.controller().HandleSlackEvent(context.Background(), mention("hi", "1.1")); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("reply refused", func(t *testing.T) {
		f := newFlowFixture(t)
		f.crud.replyToTaskFunc = func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
			return nil, errors.New("task is completed")
		}
		f.expectMentionThread(model.SlackWorkspaceLink{}, errNotFound)
		f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, UserID: 100}, nil)
		if err := f.controller().HandleSlackEvent(context.Background(), mention("hi", "1.1")); err == nil || !strings.Contains(err.Error(), "task is completed") {
			t.Fatalf("err = %v", err)
		}
		if f.mcp.pushed != "" {
			t.Errorf("the agent was notified of a refused reply: %q", f.mcp.pushed)
		}
	})
}

// ─── HandleSlashCommand ────────────────────────────────────────────────────────

func (f *flowFixture) expectSlashWorkspace() {
	f.repo.EXPECT().GetSlackWorkspaceLinkByChannel(gomock.Any(), "C1").Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{ID: 42, UserID: 100}, nil)
}

func TestHandleSlashCommandRefusesAnEmptyDescription(t *testing.T) {
	f := newFlowFixture(t)
	f.repo.EXPECT().GetSlackWorkspaceLinkByChannel(gomock.Any(), "C1").Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)
	msg, ephemeral, err := f.controller().HandleSlashCommand(context.Background(), "C1", "   ")
	if err != nil || !ephemeral || !strings.Contains(msg, "cannot be empty") {
		t.Fatalf("got (%q, %v, %v)", msg, ephemeral, err)
	}
}

func TestHandleSlashCommandReportsACreateFailure(t *testing.T) {
	f := newFlowFixture(t)
	f.expectSlashWorkspace()
	f.crud.createTaskFunc = func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
		return nil, errors.New("quota exceeded")
	}
	msg, ephemeral, err := f.controller().HandleSlashCommand(context.Background(), "C1", "do it")
	if err == nil || !ephemeral || msg != "⚠️ Failed to create task: quota exceeded" {
		t.Fatalf("got (%q, %v, %v)", msg, ephemeral, err)
	}
}

func TestHandleSlashCommandKeepsAnUnclosedQuoteAsText(t *testing.T) {
	f := newFlowFixture(t)
	var got entity.CreateTaskRequest
	f.crud.createTaskFunc = func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
		got = req
		return &entity.CreateTaskResponse{}, nil
	}
	for _, text := range []string{`"no closing quote`, `"only a title"`} {
		f.expectSlashWorkspace()
		f.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), int64(100)).Return(nil, nil)
		if _, _, err := f.controller().HandleSlashCommand(context.Background(), "C1", text); err != nil {
			t.Fatalf("HandleSlashCommand: %v", err)
		}
		if got.Task.Title != text || got.Task.Body != text {
			t.Errorf("%s: title %q body %q, want the text as both", text, got.Task.Title, got.Task.Body)
		}
	}
}

func TestHandleSlashCommandCutsALongTitleByCharacters(t *testing.T) {
	ascii := strings.Repeat("a", 59)
	tests := map[string]string{
		ascii + "é":   ascii + "é",
		ascii + "é🙂x": ascii + "é...",
		ascii + "🙂🙂":  ascii + "🙂...",
	}
	for text, want := range tests {
		t.Run(text[59:], func(t *testing.T) {
			f := newFlowFixture(t)
			f.expectSlashWorkspace()
			f.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), int64(100)).Return(nil, nil)
			var got entity.CreateTaskRequest
			f.crud.createTaskFunc = func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
				got = req
				return &entity.CreateTaskResponse{}, nil
			}
			if _, _, err := f.controller().HandleSlashCommand(context.Background(), "C1", text); err != nil {
				t.Fatalf("HandleSlashCommand: %v", err)
			}
			if got.Task.Title != want || !utf8.ValidString(got.Task.Title) {
				t.Errorf("title %q, want %q", got.Task.Title, want)
			}
			if got.Task.Body != text {
				t.Errorf("body %q, want the whole text", got.Task.Body)
			}
		})
	}
}

func TestHandleSlashCommandHoldsTheAgentWhileItIsBusy(t *testing.T) {
	// The new task is 7; a listed task 7 is the new one itself.
	const newID = int64(7)
	tests := []struct {
		name       string
		others     []model.Task
		listErr    error
		wantNotify bool
	}{
		{"another task is ongoing", []model.Task{{ID: 8, Status: "ongoing"}}, nil, false},
		{"another agent task is waiting", []model.Task{{ID: 8, Status: "notstarted", Assignee: "agent"}}, nil, false},
		{"only the new task and finished ones", []model.Task{{ID: newID, Status: "notstarted", Assignee: "agent"}, {ID: 8, Status: "completed"}, {ID: 6, Status: "notstarted", Assignee: "human"}}, nil, true},
		{"tasks cannot be listed", nil, errors.New("database unavailable"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			f.expectSlashWorkspace()
			f.repo.EXPECT().ListTasks(gomock.Any(), entity.ListTasksRequest{WorkspaceID: 42}, int64(100)).Return(tt.others, tt.listErr)
			f.crud.createTaskFunc = func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
				if entity.GetOrigin(ctx) != entity.OriginSlack {
					t.Errorf("origin = %v, want slack", entity.GetOrigin(ctx))
				}
				return &entity.CreateTaskResponse{Task: entity.Task{ID: newID, Body: req.Task.Body, Attachments: []entity.Attachment{
					{ID: "A1", Filename: "spec.md", MimeType: "text/markdown"},
					{Filename: "unsaved.txt"},
				}}}, nil
			}

			msg, ephemeral, err := f.controller().HandleSlashCommand(context.Background(), "C1", "ship it")
			if err != nil || ephemeral || !strings.Contains(msg, "ship it") {
				t.Fatalf("got (%q, %v, %v)", msg, ephemeral, err)
			}
			if !tt.wantNotify {
				if f.mcp.pushed != "" {
					t.Errorf("pushed %q while the agent is busy", f.mcp.pushed)
				}
				return
			}
			want := "[Task " + monoflake.ID(newID).String() + "]\nship it\nAttachments:\n  - id=A1 name=spec.md type=text/markdown"
			if f.mcp.pushed != want {
				t.Errorf("pushed %q, want %q", f.mcp.pushed, want)
			}
			if len(f.mcp.notified) != 2 {
				t.Errorf("attachments handed over = %d, want 2", len(f.mcp.notified))
			}
		})
	}
}

// ─── HandleTaskApproval ────────────────────────────────────────────────────────

func approvalAction(ws, task int64, verb string) SlackBlockAction {
	return SlackBlockAction{
		ActionID:  "task_respond:" + monoflake.ID(ws).String() + ":" + monoflake.ID(task).String() + ":" + verb,
		ChannelID: "C1",
		MessageTS: "5.5",
		UserID:    "U_REVIEWER",
	}
}

func TestHandleTaskApprovalRejectsAnActionWithoutIDs(t *testing.T) {
	f := newFlowFixture(t)
	err := f.controller().HandleTaskApproval(context.Background(), SlackBlockAction{ActionID: "task_respond:::allow"})
	if err == nil || !strings.Contains(err.Error(), "invalid IDs") {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleTaskApprovalNeedsUsableCredentials(t *testing.T) {
	tests := []struct {
		name    string
		link    model.SlackWorkspaceLink
		linkErr error
		wantErr string
	}{
		{"no link", model.SlackWorkspaceLink{}, errNotFound, "slack workspace credentials not found: record not found"},
		{"link without a token", model.SlackWorkspaceLink{WorkspaceID: 42, SlackChannelID: "C1"}, nil, "slack workspace credentials not found: the link has no bot token"},
		{"undecryptable token", undecryptableLink(42), nil, "slack decryption failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			f.crud.respondToTaskFunc = func(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
				t.Error("the approval was applied without usable credentials")
				return nil, nil
			}
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(tt.link, tt.linkErr)
			err := f.controller().HandleTaskApproval(context.Background(), approvalAction(42, 9, "allow"))
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandleTaskApprovalShowsAFailedResponseInSlack(t *testing.T) {
	f := newFlowFixture(t)
	f.crud.respondToTaskFunc = func(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
		return nil, errors.New("task already started")
	}
	f.slack.updateErr = errChannelNotFound
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
	f.repo.EXPECT().DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).Return(nil)

	err := f.controller().HandleTaskApproval(context.Background(), approvalAction(42, 9, "allow"))
	if err == nil || err.Error() != "task already started" {
		t.Fatalf("err = %v, want the respond error", err)
	}
	if len(f.slack.updates) != 1 || !strings.Contains(f.slack.updates[0], "⚠️ Action failed: task already started") {
		t.Errorf("updates = %v", f.slack.updates)
	}
}

func TestHandleTaskApprovalLabelsTheOutcome(t *testing.T) {
	tests := map[string]string{
		"allow_all": "🚀 Approved (Allow All Commands) by <@U_REVIEWER>",
		"reject":    "❌ Rejected by <@U_REVIEWER>",
		"snooze":    "✅ Done by <@U_REVIEWER>",
	}
	for verb, want := range tests {
		t.Run(verb, func(t *testing.T) {
			f := newFlowFixture(t)
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
			if err := f.controller().HandleTaskApproval(context.Background(), approvalAction(42, 9, verb)); err != nil {
				t.Fatalf("HandleTaskApproval: %v", err)
			}
			if len(f.slack.updates) != 1 || f.slack.updates[0] != "C1/5.5 "+want {
				t.Errorf("updates = %v, want %q", f.slack.updates, want)
			}
		})
	}
}

// ─── HandleMCPPermission ───────────────────────────────────────────────────────

func permissionAction(ws int64, behavior string) SlackBlockAction {
	return SlackBlockAction{
		ActionID:  "task_permission:" + monoflake.ID(ws).String() + ":" + monoflake.ID(9).String() + ":R1:" + behavior,
		ChannelID: "C1",
		MessageTS: "5.5",
		UserID:    "U_REVIEWER",
		UserName:  "ada",
	}
}

func TestHandleMCPPermissionEarlyFailures(t *testing.T) {
	tests := []struct {
		name    string
		action  SlackBlockAction
		setup   func(f *flowFixture)
		wantErr string
	}{
		{"no workspace", SlackBlockAction{ActionID: "task_permission::T:R1:allow"}, nil, "invalid workspaceID"},
		{"workspace gone", permissionAction(42, "allow"), func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{}, errNotFound)
		}, "workspace not found"},
		{"no link", permissionAction(42, "allow"), func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{}, errNotFound)
		}, "credentials not found"},
		{"link without a token", permissionAction(42, "allow"), func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)
		}, "slack workspace credentials not found: the link has no bot token"},
		{"undecryptable token", permissionAction(42, "allow"), func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
			f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(undecryptableLink(42), nil)
		}, "decryption failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			err := f.controller().HandleMCPPermission(context.Background(), tt.action)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func (f *flowFixture) expectPermissionLookups(t *testing.T, messages []model.Message, listErr error) {
	t.Helper()
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{UserID: 100}, nil)
	f.repo.EXPECT().GetSlackWorkspaceLink(gomock.Any(), int64(42)).Return(authorizedLink(t, 42), nil)
	f.repo.EXPECT().ListMessages(gomock.Any(), int64(9)).Return(messages, listErr)
}

func metaJSON(m map[string]any) datatypes.JSON {
	b, _ := json.Marshal(m)
	return datatypes.JSON(b)
}

func TestHandleMCPPermissionMarksTheCamelCaseRequest(t *testing.T) {
	f := newFlowFixture(t)
	f.expectPermissionLookups(t, []model.Message{
		{ID: 1}, // no metadata
		{ID: 2, Metadata: metaJSON(map[string]any{"type": "chat"})},
		{ID: 3, Metadata: metaJSON(map[string]any{"type": "permission_request", "requestId": "R0"})},
		{ID: 4, Metadata: metaJSON(map[string]any{"type": "permission_request", "requestId": "R1"})},
		{ID: 5, Metadata: metaJSON(map[string]any{"type": "permission_request", "requestId": "R1"})},
	}, nil)
	f.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(9), int64(4), gomock.Any()).Return(nil)

	if err := f.controller().HandleMCPPermission(context.Background(), permissionAction(42, "deny")); err != nil {
		t.Fatalf("HandleMCPPermission: %v", err)
	}
	if len(f.slack.updates) != 1 || f.slack.updates[0] != "C1/5.5 ❌ Denied by <@U_REVIEWER>" {
		t.Errorf("updates = %v", f.slack.updates)
	}
}

func TestHandleMCPPermissionFailedVerdicts(t *testing.T) {
	tests := []struct {
		name       string
		verdictErr error
		wantText   string
	}{
		{"expired", errors.New("permission request expired"), "⚠️ This permission request has expired (agent was restarted)."},
		{"other", errors.New("agent offline"), "⚠️ Failed: agent offline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			f.mcp.verdictErr = tt.verdictErr
			f.expectPermissionLookups(t, nil, errors.New("database unavailable"))

			err := f.controller().HandleMCPPermission(context.Background(), permissionAction(42, "allow"))
			if !errors.Is(err, tt.verdictErr) {
				t.Fatalf("err = %v, want %v", err, tt.verdictErr)
			}
			if len(f.slack.updates) != 1 || f.slack.updates[0] != "C1/5.5 "+tt.wantText {
				t.Errorf("updates = %v, want %q", f.slack.updates, tt.wantText)
			}
		})
	}
}

func TestHandleMCPPermissionLabelsAnUnknownBehaviorDone(t *testing.T) {
	f := newFlowFixture(t)
	f.expectPermissionLookups(t, nil, nil)
	if err := f.controller().HandleMCPPermission(context.Background(), permissionAction(42, "allow_always")); err != nil {
		t.Fatalf("HandleMCPPermission: %v", err)
	}
	if len(f.slack.updates) != 1 || f.slack.updates[0] != "C1/5.5 Done" {
		t.Errorf("updates = %v", f.slack.updates)
	}
}

// ─── helpers ───────────────────────────────────────────────────────────────────

func TestDownloadSlackFileFailures(t *testing.T) {
	t.Run("bad url", func(t *testing.T) {
		if _, err := downloadSlackFile(context.Background(), "tok", "://nope"); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		if _, err := downloadSlackFile(context.Background(), "tok", srv.URL); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("not ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		if _, err := downloadSlackFile(context.Background(), "tok", srv.URL); err == nil || !strings.Contains(err.Error(), "403") {
			t.Fatalf("err = %v, want the status", err)
		}
	})
	t.Run("cut short", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("short"))
		}))
		defer srv.Close()
		if _, err := downloadSlackFile(context.Background(), "tok", srv.URL); err == nil {
			t.Fatal("expected a read error")
		}
	})
}

func TestPermissionMetadataThatCannotBeRead(t *testing.T) {
	for name, meta := range map[string]any{
		"not marshalable": make(chan int),
		"not an object":   "permission_request",
		"absent":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			msg := entity.Message{Metadata: meta}
			if isPermissionRequest(msg) {
				t.Error("isPermissionRequest = true")
			}
			if meta == nil {
				return
			}
			if id, desc := extractPermissionRequestFields(msg); id != "" || desc != "" {
				t.Errorf("extractPermissionRequestFields = (%q, %q), want empty", id, desc)
			}
		})
	}
}

func TestFormatSlackAttachmentsListsOnlySavedOnes(t *testing.T) {
	if got := formatSlackAttachments(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := formatSlackAttachments([]entity.Attachment{{Filename: "x"}}); got != "" {
		t.Errorf("unsaved only = %q", got)
	}
	got := formatSlackAttachments([]entity.Attachment{{ID: "A1", Filename: "a.png", MimeType: "image/png"}, {Filename: "x"}})
	if got != "Attachments:\n  - id=A1 name=a.png type=image/png" {
		t.Errorf("got %q", got)
	}
}

func TestModelConversions(t *testing.T) {
	c := newFlowFixture(t).controller()
	now := time.Now()

	ws := c.fromModelWorkspaceToEntity(model.Workspace{ID: 1, UserID: 2, Name: "n", Description: "d", Icon: "i", CreatedAt: now})
	if ws.ID != 1 || ws.UserID != 2 || ws.Name != "n" || ws.Description != "d" || ws.Icon != "i" || !ws.CreatedAt.Equal(now) {
		t.Errorf("workspace = %+v", ws)
	}

	task := c.fromModelTaskToEntity(model.Task{ID: 3, WorkspaceID: 1, UserID: 2, CreatedBy: "human", Assignee: "agent", Status: "ongoing", Title: "t", Body: "b", CronSchedule: "* * * * *", AllowAllCommands: true})
	if task.ID != 3 || task.WorkspaceID != 1 || task.Assignee != "agent" || task.Status != "ongoing" || task.Title != "t" || !task.AllowAllCommands || task.CronSchedule != "* * * * *" {
		t.Errorf("task = %+v", task)
	}

	msg := c.fromModelMessageToEntity(model.Message{ID: 4, TaskID: 3, UserID: 2, Sender: "agent", Text: "x", Metadata: metaJSON(map[string]any{"type": "permission_request"})})
	if meta, _ := msg.Metadata.(map[string]any); msg.ID != 4 || msg.Text != "x" || meta["type"] != "permission_request" {
		t.Errorf("message = %+v", msg)
	}
	for name, raw := range map[string]datatypes.JSON{"none": nil, "broken": datatypes.JSON("{")} {
		m := c.fromModelMessageToEntity(model.Message{Metadata: raw})
		if meta, ok := m.Metadata.(map[string]any); !ok || len(meta) != 0 {
			t.Errorf("%s metadata = %#v, want an empty map", name, m.Metadata)
		}
	}
}

// ─── Start / processEvent ──────────────────────────────────────────────────────

func TestStartDoesNothingWhenSlackIsDisabled(t *testing.T) {
	f := newFlowFixture(t)
	f.slack.disabled = true
	if err := f.controller().Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestStartFailsWhenItCannotSubscribe(t *testing.T) {
	f := newFlowFixture(t)
	f.pubsub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).Return(nil, errors.New("closed"))
	if err := f.controller().Start(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStartProcessesEventsUntilTheChannelCloses(t *testing.T) {
	f := newFlowFixture(t)
	events := make(chan any)
	f.pubsub.EXPECT().Subscribe(gomock.Any(), gomock.Any()).Return(&pubsub.SubscribeResponse{Events: events}, nil)

	looked := make(chan struct{})
	f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).DoAndReturn(func(context.Context, int64) (model.Workspace, error) {
		close(looked)
		return model.Workspace{ID: 42}, nil
	})

	if err := f.controller().Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	events <- "not an event" // skipped
	events <- entity.CRUDEvent{Action: entity.ActionWorkspaceCreate, ResourceType: entity.ResourceWorkspace, ResourceID: 42}
	select {
	case <-looked:
	case <-time.After(5 * time.Second):
		t.Fatal("the workspace event was not processed")
	}
	close(events)
}

func TestStartStopsWithItsContext(t *testing.T) {
	f := newFlowFixture(t)
	events := make(chan any)
	f.pubsub.EXPECT().Subscribe(gomock.Any(), gomock.Any()).Return(&pubsub.SubscribeResponse{Events: events}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	if err := f.controller().Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	// Once stopped, nothing reads the channel.
	select {
	case events <- "late":
		t.Error("the loop still reads events after its context ended")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestProcessEventLoadsWhatEachHookNeeds(t *testing.T) {
	// Slack disabled: each hook the event starts returns at once, so only
	// the lookups processEvent itself makes are expected.
	msgModel := model.Message{ID: 5, TaskID: 9}
	tests := []struct {
		name  string
		event entity.CRUDEvent
		setup func(f *flowFixture)
	}{
		{"task created", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceTask, ResourceID: 9}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{ID: 9}, nil)
		}},
		{"scheduled task created", entity.CRUDEvent{Action: entity.ActionTaskFromScheduled, ResourceType: entity.ResourceTask, ResourceID: 9}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{}, errNotFound)
		}},
		{"task updated", entity.CRUDEvent{Action: entity.ActionMessageUpdate, ResourceType: entity.ResourceTask, ResourceID: 9}, nil},
		{"workspace created", entity.CRUDEvent{Action: entity.ActionWorkspaceCreate, ResourceType: entity.ResourceWorkspace, ResourceID: 42}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(42)).Return(model.Workspace{}, errNotFound)
		}},
		{"workspace other", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceWorkspace, ResourceID: 42}, nil},
		{"message created", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(msgModel, nil)
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{ID: 9}, nil)
		}},
		{"message created, task gone", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(msgModel, nil)
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{}, errNotFound)
		}},
		{"message created, message gone", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(model.Message{}, errNotFound)
		}},
		{"message updated", entity.CRUDEvent{Action: entity.ActionMessageUpdate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(msgModel, nil)
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{ID: 9}, nil)
		}},
		{"message updated, task gone", entity.CRUDEvent{Action: entity.ActionMessageUpdate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(msgModel, nil)
			f.repo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{}, errNotFound)
		}},
		{"message updated, message gone", entity.CRUDEvent{Action: entity.ActionMessageUpdate, ResourceType: entity.ResourceMessage, ResourceID: 5}, func(f *flowFixture) {
			f.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(5)).Return(model.Message{}, errNotFound)
		}},
		{"message deleted", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceMessage, ResourceID: 5}, nil},
		{"user created", entity.CRUDEvent{Action: entity.ActionUserCreate, ResourceType: entity.ResourceUser, ResourceID: 1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlowFixture(t)
			f.slack.disabled = true
			if tt.setup != nil {
				tt.setup(f)
			}
			f.controller().processEvent(context.Background(), tt.event)
		})
	}
}
