// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// clearContext is taken from older supervisors but named nowhere: agents copy
// every field they are shown, and a workspace starts tasks on a clean slate
// by default.
func assertNoClearContext(t *testing.T, where, text string) {
	t.Helper()
	if strings.Contains(strings.ToLower(text), "clearcontext") {
		t.Errorf("%s names clearContext, which agents are not to be told about", where)
	}
}

func TestSupervisorServer_AdvertisesNoClearContext(t *testing.T) {
	wire, err := json.Marshal(listedTools(t))
	if err != nil {
		t.Fatal(err)
	}
	assertNoClearContext(t, "tools/list", string(wire))
	assertNoClearContext(t, "the server's instructions", serverInstructions)

	tools := (&WorkspaceServer{}).tools()
	help := func(topic string) {
		res := mcpevent.ToolsHelp(tools, topic)
		assertNoClearContext(t, "help "+topic, res.Content[0].(*mcp.TextContent).Text)
	}
	help("")
	for _, tool := range tools {
		help(tool.Name)
		for _, a := range tool.Actions {
			help(tool.Name + "." + a.Name)
		}
	}
	// A refusal ends with the action's fields and example.
	res := toolResult((&WorkspaceServer{}).handleTask(authedContext(), nil, TaskParams{Action: "create", Payload: map[string]any{"nope": 1}}))
	if !res.isError {
		t.Fatal("a bad create payload was taken")
	}
	assertNoClearContext(t, "create's refusal", res.text)

	err = fs.WalkDir(builtinSkillFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(builtinSkillFiles, path)
		assertNoClearContext(t, "skill file "+path, string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Older supervisors still send clearContext on create, through the task tool
// as they call it: it is taken, and honoured.
func TestTaskCreate_StillTakesClearContext(t *testing.T) {
	var got entity.CreateTaskRequest
	ctrl := &mockTaskCrud{createTask: func(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
		got = req
		return &entity.CreateTaskResponse{Task: entity.Task{ID: 7, WorkspaceID: testWorkspace}}, nil
	}}
	textOf(t, toolResult(taskServer(ctrl).handleTask(authedContext(), nil, TaskParams{Action: "create", Payload: map[string]any{
		"workspaceId": base62(testWorkspace), "title": "Wire up the gateway", "clearContext": true,
	}})))
	if !got.Task.ClearContext {
		t.Error("clearContext was taken but not honoured")
	}
}

// help once said followUp took clearContext, so supervisors send it: it is
// taken and ignored, not refused.
func TestTaskFollowUp_TakesClearContext(t *testing.T) {
	fake := &followUpCrud{status: "notstarted"}
	textOf(t, toolResult((&WorkspaceServer{crud: fake}).handleTask(authedContext(), nil, TaskParams{Action: "followUp", Payload: map[string]any{
		"workspaceId": base62(testWorkspace), "taskId": base62(42), "text": "Also cover the error path.", "clearContext": true,
	}})))
	if fake.req.TaskID != 42 {
		t.Errorf("request = %+v, want the follow-up sent", fake.req)
	}
}
