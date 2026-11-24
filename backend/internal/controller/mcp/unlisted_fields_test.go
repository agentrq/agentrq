// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

// unadvertisedFields are what no agent is told about: agents copy every
// field they are shown, so these are taken from older callers and named
// nowhere. clearContext: a workspace starts tasks on a clean slate by default.
var unadvertisedFields = []string{"clearContext"}

// assertUnadvertised fails for each of unadvertisedFields that text, what
// where is, names.
func assertUnadvertised(t *testing.T, where, text string) {
	t.Helper()
	for _, field := range unadvertisedFields {
		if strings.Contains(strings.ToLower(text), strings.ToLower(field)) {
			t.Errorf("%s names %s, which agents are not to be told about", where, field)
		}
	}
}

// assertToolsUnadvertised sweeps tools' help: the list, each tool's, each
// action's, and the usage a refused payload ends with.
func assertToolsUnadvertised(t *testing.T, tools []ActionTool) {
	t.Helper()
	help := func(topic string) {
		res := ToolsHelp(tools, topic)
		assertUnadvertised(t, "help "+topic, res.Content[0].(*mcp.TextContent).Text)
	}
	help("")
	for _, tool := range tools {
		help(tool.Name)
		for _, a := range tool.Actions {
			help(tool.Name + "." + a.Name)
			assertUnadvertised(t, tool.Name+"."+a.Name+"'s refusal", a.usage())
		}
	}
}

// assertSkillsUnadvertised sweeps every file of the built-in skills in fsys.
func assertSkillsUnadvertised(t *testing.T, fsys fs.FS) {
	t.Helper()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, path)
		assertUnadvertised(t, "skill file "+path, string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceServer_AdvertisesNoUnlistedField(t *testing.T) {
	wire, err := json.Marshal(toolsOverTheWire(t))
	if err != nil {
		t.Fatal(err)
	}
	assertUnadvertised(t, "tools/list", string(wire))
	assertToolsUnadvertised(t, (&WorkspaceServer{}).tools())
	assertSkillsUnadvertised(t, builtinSkillFiles)
}

// An unlisted field is decoded, and left out of help and of a refusal.
func TestPayloadFields_LeaveOutUnlisted(t *testing.T) {
	type params struct {
		Name string `json:"name" jsonschema:"The name."`
		Old  bool   `json:"old,omitempty" mcp:"unlisted"`
	}
	fields := payloadFields(reflect.TypeFor[params]())
	if len(fields) != 1 || fields[0].name != "name" {
		t.Errorf("fields = %+v, want name only", fields)
	}
	var p params
	if err := decodePayload(map[string]any{"name": "a", "old": true}, &p); err != nil || !p.Old {
		t.Errorf("decoded %+v (%v), want old taken", p, err)
	}
}

// Older agents still send clearContext on task create: it is taken, and
// honoured, through the task tool as an agent calls it.
func TestTaskCreate_StillTakesClearContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockPS := mock_pubsub.NewMockService(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
	mockIdgen.EXPECT().NextID().Return(int64(123))
	var created model.Task
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		idgen:       mockIdgen,
		bus:         eventbus.New(),
		createTask: func(_ context.Context, task model.Task) (model.Task, error) {
			task.ID = 123
			created = task
			return task, nil
		},
	}

	res, _, err := ps.handleTask(context.Background(), nil, TaskParams{Action: "create", Payload: map[string]any{
		"title": "Rotate the keys", "body": "Before Friday.", "clearContext": true,
	}})
	if err != nil || res.IsError {
		t.Fatalf("create with clearContext: err=%v res=%v", err, res.Content[0].(*mcp.TextContent).Text)
	}
	if !created.ClearContext {
		t.Error("clearContext was taken but not honoured")
	}
}
