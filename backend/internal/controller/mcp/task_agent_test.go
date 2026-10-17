// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// toolCallOn is a tool call arriving on the given session.
func toolCallOn(t *testing.T, ps *WorkspaceServer, sessionID string) *mcp.CallToolRequest {
	t.Helper()
	for sess := range ps.mcpServer.Sessions() {
		if sess.ID() == sessionID {
			return &mcp.CallToolRequest{Session: sess}
		}
	}
	t.Fatalf("no connected session has ID %q", sessionID)
	return nil
}

func TestCallingAgent(t *testing.T) {
	t.Run("names a client that is itself the agent", func(t *testing.T) {
		ps, sessionID, _ := connectedIdentityServer(t, "claude-code")
		got := ps.callingAgent(toolCallOn(t, ps, sessionID))
		if got != (entity.TaskAgent{Name: "claude-code"}) {
			t.Errorf("callingAgent() = %+v, want claude-code with no model", got)
		}
	})

	t.Run("names nobody for a call without a session", func(t *testing.T) {
		ps := &WorkspaceServer{}
		if got := ps.callingAgent(nil); got != (entity.TaskAgent{}) {
			t.Errorf("callingAgent(nil) = %+v, want an empty agent", got)
		}
		if got := ps.callingAgent(&mcp.CallToolRequest{}); got != (entity.TaskAgent{}) {
			t.Errorf("callingAgent(no session) = %+v, want an empty agent", got)
		}
	})
}

func TestAgentModelsSnapshotCurrentModelName(t *testing.T) {
	cases := []struct {
		name     string
		snapshot agentlink.ModelsSnapshot
		want     string
	}{
		{"nothing selected", agentlink.ModelsSnapshot{Models: []agentlink.Model{{ID: "a", Name: "A"}}}, ""},
		{"the list's name for it", agentlink.ModelsSnapshot{CurrentModel: "a", Models: []agentlink.Model{{ID: "a", Name: "A"}}}, "A"},
		{"the ID when the list gives no name", agentlink.ModelsSnapshot{CurrentModel: "a", Models: []agentlink.Model{{ID: "a", Name: " "}}}, "a"},
		{"the ID when the list does not have it", agentlink.ModelsSnapshot{CurrentModel: "b", Models: []agentlink.Model{{ID: "a", Name: "A"}}}, "b"},
	}
	for _, c := range cases {
		if got := c.snapshot.CurrentModelName(); got != c.want {
			t.Errorf("%s: CurrentModelName() = %q, want %q", c.name, got, c.want)
		}
	}
}

// A status change registers the calling agent and hands it, with its IDs, to
// the update that records the change.
func TestHandleUpdateTaskStatus_RecordsTheCallingAgent(t *testing.T) {
	updateStatusAs := func(t *testing.T, client string, register RegisterTaskAgentFunc) entity.TaskAgent {
		t.Helper()
		ps, sessionID, _ := connectedIdentityServer(t, client)
		ps.registerTaskAgent = register
		var got entity.TaskAgent
		ps.updateStatus = func(ctx context.Context, taskID int64, status string) (model.Task, error) {
			got = entity.GetTaskAgent(ctx)
			return model.Task{ID: taskID, Status: status}, nil
		}
		res, _, err := ps.handleUpdateTaskStatus(context.Background(), toolCallOn(t, ps, sessionID), UpdateTaskStatusParams{
			TaskID: monoflake.ID(42).String(),
			Status: "ongoing",
		})
		if err != nil || res.IsError {
			t.Fatalf("handleUpdateTaskStatus: error %v, result %v; want the status changed", err, res)
		}
		return got
	}

	t.Run("registers the agent and records it", func(t *testing.T) {
		var asked entity.TaskAgent
		got := updateStatusAs(t, "claude-code", func(_ context.Context, a entity.TaskAgent) entity.TaskAgent {
			asked = a
			return entity.TaskAgent{Name: a.Name, ID: 7}
		})
		if asked.Name != "claude-code" || got != (entity.TaskAgent{Name: "claude-code", ID: 7}) {
			t.Errorf("registered %+v and recorded %+v, want claude-code registered and recorded with ID 7", asked, got)
		}
	})

	t.Run("records nobody when the server has no registrar", func(t *testing.T) {
		if got := updateStatusAs(t, "claude-code", nil); got != (entity.TaskAgent{}) {
			t.Errorf("recorded %+v, want an empty agent", got)
		}
	})
}
