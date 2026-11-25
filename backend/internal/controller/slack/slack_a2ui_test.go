// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package slack

import (
	"context"
	"errors"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	slackapi "github.com/slack-go/slack"
)

// cardMessage is an agent's reply carrying a card that shows one Text and
// asks for a click.
func cardMessage() entity.Message {
	return entity.Message{ID: 1, Sender: "agent", Text: "PR opened", Metadata: map[string]any{
		"type": "a2ui", "version": "1.0", "surfaceId": "pr",
		"messages": []any{
			map[string]any{"version": "1.0", "createSurface": map[string]any{"surfaceId": "pr", "catalogId": "x"}},
			map[string]any{"version": "1.0", "updateComponents": map[string]any{"surfaceId": "pr", "components": []any{
				map[string]any{"id": "root", "component": "Column", "children": []any{"title", "ok"}},
				map[string]any{"id": "title", "component": "Text", "text": "Coverage 100%"},
				map[string]any{"id": "ok", "component": "Button", "child": "title"},
			}}},
		},
	}}
}

func TestOnMessageCreatedPostsACardAsBlocks(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	var published pubsub.PublishRequest
	f.pubsub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		published = req
		return nil, nil
	})

	task := entity.Task{ID: 9, WorkspaceID: 42, UserID: 7}
	if err := f.controller().OnMessageCreated(context.Background(), cardMessage(), task); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if len(f.slack.replies) != 1 {
		t.Fatalf("replies = %v, want the card alone", f.slack.replies)
	}
	if got := f.slack.replies[0]; !strings.Contains(got, "Coverage 100%") || !strings.Contains(got, "open_in_agentrq") || strings.Contains(got, "PR opened") {
		t.Errorf("reply = %q, want the card's text and the Open button, not the message text", got)
	}
	ev, _ := published.Event.(entity.CRUDEvent)
	if published.PubSubID != entity.PubSubTopicCRUD || ev.Action != entity.ActionA2UISlack || ev.ResourceID != 1 || ev.WorkspaceID != 42 || ev.UserID != 7 {
		t.Errorf("published = %+v, want a2ui_slack for the message", published)
	}
}

func TestOnMessageCreatedLinksTheCardToItsTask(t *testing.T) {
	task := entity.Task{ID: 9, WorkspaceID: 42}
	want := "https://app.agentrq.com/workspaces/" + monoflake.ID(42).String() + "/tasks/" + monoflake.ID(9).String()
	f := newFlowFixture(t)
	c := f.controller()
	f.pubsub.EXPECT().Publish(gomock.Any(), gomock.Any())
	var got string
	rec := &urlSlack{fakeSlack: f.slack, url: &got}
	c.slack = rec
	f.expectThreadAndLink(t)
	if err := c.OnMessageCreated(context.Background(), cardMessage(), task); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if got != want {
		t.Errorf("button url = %q, want %q", got, want)
	}
}

func TestOnMessageCreatedPostsTheTextWhenSlackRefusesTheCard(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	f.slack.replyErr = []error{errors.New("slack: invalid_blocks: downloading image failed")}

	if err := f.controller().OnMessageCreated(context.Background(), cardMessage(), entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if len(f.slack.replies) != 2 || !strings.Contains(f.slack.replies[1], "*Agent:* PR opened") {
		t.Errorf("replies = %v, want the refused card, then the text", f.slack.replies)
	}
}

func TestOnMessageCreatedPostsTheTextOfACardThatShowsNothing(t *testing.T) {
	f := newFlowFixture(t)
	f.expectThreadAndLink(t)
	msg := entity.Message{ID: 1, Sender: "agent", Text: "done", Metadata: map[string]any{
		"type": "a2ui", "messages": []any{
			map[string]any{"version": "1.0", "updateComponents": map[string]any{"components": []any{
				map[string]any{"id": "root", "component": "Icon", "name": "check"},
			}}},
		},
	}}
	if err := f.controller().OnMessageCreated(context.Background(), msg, entity.Task{ID: 9, WorkspaceID: 42}); err != nil {
		t.Fatalf("OnMessageCreated: %v", err)
	}
	if len(f.slack.replies) != 1 || !strings.Contains(f.slack.replies[0], "*Agent:* done") {
		t.Errorf("replies = %v, want the text", f.slack.replies)
	}
}

// urlSlack records the link of the button in the replies it posts.
type urlSlack struct {
	*fakeSlack
	url *string
}

func (u *urlSlack) PostThreadReply(ctx context.Context, token, channelID, threadTS string, blocks []slackapi.Block) (string, error) {
	for _, b := range blocks {
		if a, ok := b.(*slackapi.ActionBlock); ok {
			*u.url = a.Elements.ElementSet[0].(*slackapi.ButtonBlockElement).URL
		}
	}
	return u.fakeSlack.PostThreadReply(ctx, token, channelID, threadTS, blocks)
}
