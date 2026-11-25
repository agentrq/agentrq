// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	mock_a2ui "github.com/agentrq/agentrq/backend/internal/service/mocks/a2ui"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const a2uiTestCard = `[
 {"version":"1.0","createSurface":{"surfaceId":"pr-1","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},
 {"version":"1.0","updateComponents":{"surfaceId":"pr-1","components":[
  {"id":"root","component":"Card","child":"col"},
  {"id":"col","component":"Column","children":["title","chart"]},
  {"id":"title","component":"Text","text":"PR opened"},
  {"id":"chart","component":"Image","url":"attachment:chart.png"}
 ]}}
]`

type a2uiReplyEnv struct {
	ps       *WorkspaceServer
	store    storage.Service
	replies  int
	metadata any
	events   []entity.CRUDEvent
	replyErr error
}

func newA2UIReplyEnv(t *testing.T) *a2uiReplyEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	store, err := storage.NewNestedPublic(t.TempDir(), "https://files.test/storage")
	if err != nil {
		t.Fatal(err)
	}
	ids := mock_idgen.NewMockService(ctrl)
	ids.EXPECT().NextID().Return(int64(900)).AnyTimes()
	bus := mock_pubsub.NewMockService(ctrl)
	e := &a2uiReplyEnv{store: store}
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if ev, ok := req.Event.(entity.CRUDEvent); ok {
			e.events = append(e.events, ev)
		}
		return &pubsub.PublishResponse{}, nil
	})
	e.ps = &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(7).String(),
		pubsub:      bus,
		idgen:       ids,
		storage:     store,
		reply: func(_ context.Context, _ string, _ string, _ []entity.Attachment, metadata any) (int64, error) {
			if e.replyErr != nil {
				return 0, e.replyErr
			}
			e.replies++
			e.metadata = metadata
			return 555, nil
		},
	}
	return e
}

func a2uiReplyParams(ui string) ReplyParams {
	return ReplyParams{
		TaskID:      monoflake.ID(42).String(),
		Text:        "PR opened",
		Attachments: []AttachmentParam{{Filename: "chart.png", MimeType: "image/png", Data: "iVBORw0K"}},
		UI:          json.RawMessage(ui),
	}
}

func TestHandleReply_ACardIsStoredWithItsMediaResolved(t *testing.T) {
	e := newA2UIReplyEnv(t)
	res, _, err := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard))
	if err != nil || res.IsError {
		t.Fatalf("reply = %v, %v", res, err)
	}
	card, ok := e.metadata.(*A2UIMetadata)
	if !ok {
		t.Fatalf("metadata = %#v, want the card", e.metadata)
	}
	b, _ := json.Marshal(card)
	var stored map[string]any
	_ = json.Unmarshal(b, &stored)
	if stored["type"] != "a2ui" || stored["version"] != "1.0" || stored["surfaceId"] != "pr-1" || len(card.Messages) != 2 {
		t.Errorf("stored metadata = %s", b)
	}
	wantURL := "https://files.test/storage/" + storage.AttachmentKey(100, 42, monoflake.ID(900).String())
	if !strings.Contains(string(card.Messages[1]), `"url":"`+wantURL+`"`) {
		t.Errorf("the image was not resolved to %s: %s", wantURL, card.Messages[1])
	}
	if len(e.events) != 1 || e.events[0].Action != entity.ActionA2UIReply || e.events[0].ResourceID != 555 ||
		e.events[0].WorkspaceID != 100 || e.events[0].UserID != 7 || e.events[0].Actor != entity.ActorAgent {
		t.Errorf("events = %+v, want one a2ui_reply for message 555", e.events)
	}
}

func TestHandleReply_AnInvalidCardIsRefusedWithTheValidatorsWords(t *testing.T) {
	e := newA2UIReplyEnv(t)
	bad := strings.Replace(a2uiTestCard, "attachment:chart.png", "https://example.com/x.png", 1)
	res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(bad))
	if !res.IsError {
		t.Fatal("an invalid card was accepted")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.HasPrefix(text, "ui is not valid A2UI v1.0: ") || !strings.Contains(text, "media cannot come from a link") {
		t.Errorf("refusal = %q, want the validator's message", text)
	}
	if e.replies != 0 || len(e.events) != 0 {
		t.Errorf("a refused card was posted (%d) or counted (%v)", e.replies, e.events)
	}
	if _, err := storage.LoadAttachment(e.store, 100, 42, monoflake.ID(900).String()); err == nil {
		t.Error("the refused reply's file is still stored")
	}
}

func TestHandleReply_ACardNeedsTheValidator(t *testing.T) {
	shared := sharedA2UI
	t.Cleanup(func() { sharedA2UI = shared })
	sharedA2UI = func() (a2ui.Service, error) { return nil, errors.New("schemas missing") }
	e := newA2UIReplyEnv(t)
	res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard))
	if !res.IsError || res.Content[0].(*mcp.TextContent).Text != "ui is not available on this server (schemas missing): send the reply without it" {
		t.Errorf("reply = %v, want it refused", res)
	}
	if e.replies != 0 {
		t.Error("a card nobody could validate was posted")
	}
}

func TestHandleReply_AServersOwnValidatorIsUsed(t *testing.T) {
	ctrl := gomock.NewController(t)
	v := mock_a2ui.NewMockService(ctrl)
	v.EXPECT().Validate(gomock.Any(), gomock.Any(), gomock.Any()).Return(&a2ui.Surface{SurfaceID: "own"}, nil)
	e := newA2UIReplyEnv(t)
	e.ps.a2ui = v
	if res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard)); res.IsError {
		t.Fatalf("reply = %v", res)
	}
	if card := e.metadata.(*A2UIMetadata); card.SurfaceID != "own" {
		t.Errorf("surface = %q, want the server's validator's", card.SurfaceID)
	}
}

func TestHandleReply_AFailedPostDeletesItsFiles(t *testing.T) {
	e := newA2UIReplyEnv(t)
	e.replyErr = errors.New("task not found")
	res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard))
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "failed to deliver reply: task not found") {
		t.Errorf("reply = %v", res)
	}
	if _, err := storage.LoadAttachment(e.store, 100, 42, monoflake.ID(900).String()); err == nil {
		t.Error("the failed reply's file is still stored")
	}
	if len(e.events) != 0 {
		t.Errorf("a failed reply was counted: %v", e.events)
	}
}

func TestHandleReply_AnInvalidTaskIDIsRefusedBeforeAnyFileIsSaved(t *testing.T) {
	e := newA2UIReplyEnv(t)
	p := a2uiReplyParams(a2uiTestCard)
	p.TaskID = "!"
	res, _, _ := e.ps.handleReply(context.Background(), nil, p)
	if !res.IsError || res.Content[0].(*mcp.TextContent).Text != "invalid taskId format" {
		t.Errorf("reply = %v", res)
	}
}

func TestAttachmentURLs(t *testing.T) {
	ps := &WorkspaceServer{workspaceID: 100}
	got := ps.attachmentURLs(42, []entity.Attachment{
		{ID: "a1", Filename: "public.png", URL: "https://files.test/a1"},
		{ID: "a2", Filename: "private.png"},
		{Filename: "unsaved.png"},
		{ID: "a4"},
	})
	want := map[string]string{
		"public.png":  "https://files.test/a1",
		"private.png": "/api/v1/workspaces/" + monoflake.ID(100).String() + "/tasks/" + monoflake.ID(42).String() + "/attachments/a2",
	}
	if len(got) != len(want) || got["public.png"] != want["public.png"] || got["private.png"] != want["private.png"] {
		t.Errorf("urls = %v, want %v", got, want)
	}
}

func TestEmitA2UI_WithoutPubSubIsQuiet(t *testing.T) {
	(&WorkspaceServer{}).emitA2UI(context.Background(), entity.ActionA2UIReply, 1)
}

func TestBuildConversationJSON_ACardIsFlaggedNotSent(t *testing.T) {
	task := model.Task{ID: 42, Messages: []model.Message{
		{ID: 1, Sender: "agent", Text: "PR opened", Metadata: []byte(`{"type":"a2ui","version":"1.0","surfaceId":"pr-1","messages":[{"big":"json"}]}`)},
		{ID: 2, Sender: "human", Text: "thanks", Metadata: []byte(`{"type":"other"}`)},
	}}
	out := buildConversationJSON(task, 0, 10)
	if strings.Contains(out, "big") {
		t.Errorf("the card's JSON reached the agent: %s", out)
	}
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Messages) != 2 {
		t.Fatalf("conversation = %s, %v", out, err)
	}
	if got.Messages[0]["ui"] != true || got.Messages[0]["metadata"] != "" || got.Messages[0]["text"] != "PR opened" {
		t.Errorf("card message = %v", got.Messages[0])
	}
	if _, ok := got.Messages[1]["ui"]; ok || got.Messages[1]["metadata"] != `{"type":"other"}` {
		t.Errorf("plain message = %v", got.Messages[1])
	}
}

// Every card an agent is shown, in help task.reply and the skill's
// references, is one the server posts: the agent who copies it is not
// refused.
func TestA2UIExamples_EveryCardShownToAnAgentIsPosted(t *testing.T) {
	var payloads []map[string]any
	for _, a := range (&WorkspaceServer{}).taskActions() {
		if a.Name == "reply" {
			var p map[string]any
			if err := json.Unmarshal([]byte(a.Example), &p); err != nil {
				t.Fatal(err)
			}
			payloads = append(payloads, p)
		}
	}
	files, err := fs.Glob(builtinSkillFiles, "skills/agentrq/references/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		body, err := builtinSkillFiles.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range exampleCalls(t, file, body) {
			if c.Action == "reply" {
				payloads = append(payloads, c.Payload)
			}
		}
	}
	cards := 0
	for _, p := range payloads {
		if p["ui"] == nil {
			continue
		}
		cards++
		var params ReplyParams
		if err := decodePayload(p, &params); err != nil {
			t.Fatal(err)
		}
		e := newA2UIReplyEnv(t)
		res, _, _ := e.ps.handleReply(context.Background(), nil, params)
		if res.IsError || e.metadata == nil {
			t.Errorf("the card in %q is not posted: %s", p["text"], res.Content[0].(*mcp.TextContent).Text)
		}
	}
	if cards < 2 {
		t.Errorf("%d cards shown to an agent; want the help example and the a2ui reference's", cards)
	}
}

func TestHandleReply_ACardMayCarryAButton(t *testing.T) {
	e := newA2UIReplyEnv(t)
	card := `[
 {"version":"1.0","createSurface":{"surfaceId":"deploy","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},
 {"version":"1.0","updateComponents":{"surfaceId":"deploy","components":[
  {"id":"root","component":"Button","child":"label","action":{"event":{"name":"approve"}}},
  {"id":"label","component":"Text","text":"Approve"}
 ]}}
]`
	res, _, err := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(card))
	if err != nil || res.IsError {
		t.Fatalf("reply = %v, %v", res, err)
	}
	if _, ok := e.metadata.(*A2UIMetadata); !ok {
		t.Fatalf("metadata = %#v, want the card", e.metadata)
	}

	nameless := strings.Replace(card, `{"name":"approve"}`, `{"userMessage":"Approve"}`, 1)
	res, _, err = e.ps.handleReply(context.Background(), nil, a2uiReplyParams(nameless))
	if err != nil || !res.IsError {
		t.Fatalf("a Button whose event has no name = %v, %v; want it refused", res, err)
	}
}

const a2uiTestQuestion = `[
 {"version":"1.0","createSurface":{"surfaceId":"release","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},
 {"version":"1.0","updateComponents":{"surfaceId":"release","components":[
  {"id":"root","component":"Column","children":["q","go"]},
  {"id":"q","component":"Text","text":"Release from main?"},
  {"id":"go","component":"Button","child":"label","action":{"event":{"name":"release","context":{"branch":"main"}}}},
  {"id":"label","component":"Text","text":"Release"}
 ]}}
]`

func a2uiElicitParams(ui string) ElicitParams {
	return ElicitParams{TaskID: monoflake.ID(42).String(), Message: "Release from main?", Mode: "a2ui", UI: json.RawMessage(ui), TimeoutSeconds: 60}
}

// a2uiElicitEnv is newA2UIReplyEnv that also records how the question was
// closed.
func a2uiElicitEnv(t *testing.T) (*a2uiReplyEnv, *map[string]any) {
	e := newA2UIReplyEnv(t)
	var closed map[string]any
	e.ps.updateMessageMetadata = func(_ context.Context, taskID, msgID int64, metadata any) error {
		if taskID != 42 || msgID != 555 {
			t.Errorf("closed message %d of task %d, want 555 of 42", msgID, taskID)
		}
		closed = metadata.(map[string]any)
		return nil
	}
	return e, &closed
}

// answer sends the human's answer to the question as soon as it is asked.
func answer(t *testing.T, ps *WorkspaceServer, action string, content map[string]any) {
	go func() {
		requestID := monoflake.ID(900).String()
		for range 500 {
			if ps.RespondToElicitation(requestID, action, content) == nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Error("the question was never asked")
	}()
}

func TestHandleElicit_ACardQuestionIsAnsweredByAClick(t *testing.T) {
	e, closed := a2uiElicitEnv(t)
	content := map[string]any{"name": "release", "context": map[string]any{"branch": "main"}, "dataModel": map[string]any{"notes": "ship it"}}
	answer(t, e.ps, "accept", content)

	res, _, err := e.ps.handleElicit(context.Background(), nil, a2uiElicitParams(a2uiTestQuestion))
	if err != nil || res.IsError {
		t.Fatalf("elicit = %v, %v", res, err)
	}
	var got elicitationResponse
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "accept" || got.Content["name"] != "release" || got.Content["dataModel"].(map[string]any)["notes"] != "ship it" {
		t.Errorf("answer = %+v, want accept with the click's content", got)
	}

	meta := e.metadata.(map[string]any)
	if meta["type"] != "elicitation_request" || meta["mode"] != "a2ui" || meta["status"] != "pending" ||
		meta["version"] != "1.0" || meta["surfaceId"] != "release" || meta["message"] != "Release from main?" {
		t.Errorf("stored question = %v", meta)
	}
	if msgs, _ := meta["messages"].([]json.RawMessage); len(msgs) != 2 {
		t.Errorf("stored messages = %v, want the validated surface", meta["messages"])
	}
	if _, ok := meta["requestedSchema"]; ok {
		t.Errorf("a card question stored a schema: %v", meta)
	}
	if (*closed)["status"] != "accept" || (*closed)["content"].(map[string]any)["name"] != "release" {
		t.Errorf("the question was closed with %v, want the answer kept", *closed)
	}
	if len(e.events) != 1 || e.events[0].Action != entity.ActionA2UIElicit || e.events[0].ResourceID != 555 || e.events[0].Actor != entity.ActorAgent {
		t.Errorf("events = %+v, want one a2ui_elicit for message 555", e.events)
	}
}

func TestHandleElicit_ACardQuestionMayBeDeclined(t *testing.T) {
	e, closed := a2uiElicitEnv(t)
	answer(t, e.ps, "decline", nil)
	res, _, _ := e.ps.handleElicit(context.Background(), nil, a2uiElicitParams(a2uiTestQuestion))
	if res.IsError || res.Content[0].(*mcp.TextContent).Text != `{"action":"decline"}` {
		t.Errorf("elicit = %v, want decline", res.Content[0])
	}
	if (*closed)["status"] != "decline" {
		t.Errorf("closed = %v", *closed)
	}
}

func TestHandleElicit_AnUnansweredCardQuestionIsCancelled(t *testing.T) {
	e, closed := a2uiElicitEnv(t)
	p := a2uiElicitParams(a2uiTestQuestion)
	p.TimeoutSeconds = 1
	res, _, _ := e.ps.handleElicit(context.Background(), nil, p)
	if res.IsError || res.Content[0].(*mcp.TextContent).Text != `{"action":"cancel"}` {
		t.Errorf("elicit = %v, want cancel", res.Content[0])
	}
	if (*closed)["status"] != "cancel" {
		t.Errorf("closed = %v", *closed)
	}
}

func TestHandleElicit_CardQuestionRefusals(t *testing.T) {
	media := `[
 {"version":"1.0","createSurface":{"surfaceId":"m","catalogId":"https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}},
 {"version":"1.0","updateComponents":{"surfaceId":"m","components":[{"id":"root","component":"Image","url":"attachment:chart.png"}]}}
]`
	schema := map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}}
	cases := []struct {
		name   string
		edit   func(p *ElicitParams)
		prefix string
	}{
		{"no ui", func(p *ElicitParams) { p.UI = nil }, "ui is required when mode is 'a2ui'"},
		{"a schema too", func(p *ElicitParams) { p.RequestedSchema = schema }, "mode 'a2ui' takes ui, not requestedSchema or url"},
		{"a url too", func(p *ElicitParams) { p.URL = "https://example.com" }, "mode 'a2ui' takes ui, not requestedSchema or url"},
		{"ui on a form", func(p *ElicitParams) { p.Mode, p.RequestedSchema = "form", schema }, "ui needs mode 'a2ui'"},
		{"ui on a link", func(p *ElicitParams) { p.Mode, p.URL = "url", "https://example.com" }, "ui needs mode 'a2ui'"},
		{"media", func(p *ElicitParams) { p.UI = json.RawMessage(media) }, "ui is not valid A2UI v1.0: "},
		{"an unknown mode", func(p *ElicitParams) { p.Mode, p.UI = "card", nil }, "mode must be 'form', 'url' or 'a2ui'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newA2UIReplyEnv(t)
			p := a2uiElicitParams(a2uiTestQuestion)
			c.edit(&p)
			res, _, err := e.ps.handleElicit(context.Background(), nil, p)
			if err != nil || !res.IsError || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, c.prefix) {
				t.Errorf("elicit = %v, %v; want refused with %q", res.Content[0], err, c.prefix)
			}
			if e.replies != 0 || len(e.events) != 0 {
				t.Errorf("a refused question was posted (%d) or counted (%v)", e.replies, e.events)
			}
		})
	}
}

func TestHandleElicit_ACardQuestionNeedsTheValidator(t *testing.T) {
	shared := sharedA2UI
	t.Cleanup(func() { sharedA2UI = shared })
	sharedA2UI = func() (a2ui.Service, error) { return nil, errors.New("schemas missing") }
	e := newA2UIReplyEnv(t)
	res, _, _ := e.ps.handleElicit(context.Background(), nil, a2uiElicitParams(a2uiTestQuestion))
	if !res.IsError || res.Content[0].(*mcp.TextContent).Text != "ui is not available on this server (schemas missing): ask with mode 'form' instead" {
		t.Errorf("elicit = %v, want it refused", res.Content[0])
	}
}

func TestBuildConversationJSON_ACardQuestionKeepsItsAnswerNotItsCard(t *testing.T) {
	task := model.Task{ID: 42, Messages: []model.Message{
		{ID: 1, Sender: "agent", Text: "Release?", Metadata: []byte(`{"type":"elicitation_request","mode":"a2ui","status":"accept","surfaceId":"release","messages":[{"big":"json"}],"content":{"name":"release"}}`)},
		{ID: 2, Sender: "agent", Text: "Which?", Metadata: []byte(`{"type":"elicitation_request","mode":"form","status":"pending"}`)},
		{ID: 3, Sender: "human", Text: "hi"},
	}}
	out := buildConversationJSON(task, 0, 10)
	if strings.Contains(out, "big") {
		t.Errorf("the card's JSON reached the agent: %s", out)
	}
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Messages) != 3 {
		t.Fatalf("conversation = %s, %v", out, err)
	}
	var meta map[string]any
	_ = json.Unmarshal([]byte(got.Messages[0]["metadata"].(string)), &meta)
	if got.Messages[0]["ui"] != true || meta["status"] != "accept" || meta["content"].(map[string]any)["name"] != "release" || meta["surfaceId"] != "release" {
		t.Errorf("card question = %v", got.Messages[0])
	}
	if _, ok := got.Messages[1]["ui"]; ok {
		t.Errorf("a form question is flagged as a card: %v", got.Messages[1])
	}
	if _, ok := got.Messages[2]["ui"]; ok || got.Messages[2]["metadata"] != "" {
		t.Errorf("plain message = %v", got.Messages[2])
	}
}

// Every question card an agent is shown is one elicit accepts.
func TestA2UIExamples_EveryQuestionCardShownToAnAgentIsValid(t *testing.T) {
	files, err := fs.Glob(builtinSkillFiles, "skills/agentrq/references/*.md")
	if err != nil {
		t.Fatal(err)
	}
	cards := 0
	for _, file := range files {
		body, err := builtinSkillFiles.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range exampleCalls(t, file, body) {
			if c.Action != "elicit" || c.Payload["ui"] == nil {
				continue
			}
			cards++
			var p ElicitParams
			if err := decodePayload(c.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if p.Mode != "a2ui" || p.Message == "" {
				t.Errorf("question card in %s: mode %q, message %q", file, p.Mode, p.Message)
			}
			if _, err := (&WorkspaceServer{}).validateCard(context.Background(), p.UI, nil, ""); err != nil {
				t.Errorf("the question card in %s is refused: %v", file, err)
			}
		}
	}
	if cards < 1 {
		t.Error("no question card is shown to an agent; want the a2ui reference's")
	}
}
