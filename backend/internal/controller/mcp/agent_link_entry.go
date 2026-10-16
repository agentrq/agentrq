// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// The entry points a native link calls. An MCP agent reaches the same
// behaviour through its tools; these are the same paths without a tool call
// around them.

// ErrWorkspaceArchived is what a native agent is told when it writes to an
// archived workspace — the same words the MCP tools answer with.
var ErrWorkspaceArchived = errors.New("workspace is archived and read-only")

// ErrBadQuestion is a question that cannot be put to a human as asked.
var ErrBadQuestion = errors.New("a question needs a message, and a form schema or a url")

// HandlePermissionRequest relays a native agent's permission request through
// the same path a gateway's permission_request notification takes.
func (ps *WorkspaceServer) HandlePermissionRequest(ctx context.Context, linkID string, p agentlink.PermissionRequest) {
	ps.relayPermissionRequest(ctx, linkID, p)
}

// ClosePermissionRequest marks a request the agent withdrew, or one nobody
// answered in time, as cancelled — the card and its tool call — and forgets
// it, as a Stop does for the requests a stopped task was waiting on.
func (ps *WorkspaceServer) ClosePermissionRequest(ctx context.Context, requestID string) {
	ps.requestTaskIDsMu.RLock()
	taskID := ps.requestTaskIDs[requestID]
	ps.requestTaskIDsMu.RUnlock()
	ps.closeRequest(ctx, taskID, requestID)
}

// RecordToolCall records a call a native agent ran without asking, as its
// tool_calls row; 0 when it was not recorded.
func (ps *WorkspaceServer) RecordToolCall(ctx context.Context, taskID int64, c agentlink.ToolCall) int64 {
	return ps.persistToolCall(ctx, taskID, agentlink.PermissionRequest{
		ToolName: c.ToolName, Description: c.Description, InputPreview: c.InputPreview}, c.Status)
}

// SetToolCallStatus moves a recorded call on. Best-effort, like the record.
func (ps *WorkspaceServer) SetToolCallStatus(ctx context.Context, id int64, status string) {
	if ps.updateToolCallStatus == nil {
		return
	}
	if err := ps.updateToolCallStatus(ctx, id, status); err != nil {
		zlog.Error().Err(err).Int64("tool_call_id", id).Str("status", status).Msg("failed to update tool call status")
	}
}

// PostAgentReply writes the agent's answer for a task, attributed to the agent
// behind the link.
func (ps *WorkspaceServer) PostAgentReply(ctx context.Context, linkID string, taskID int64, text string) (int64, error) {
	if ps.isArchived() {
		return 0, ErrWorkspaceArchived
	}
	return ps.reply(ps.linkTaskAgentContext(ctx, linkID), monoflake.ID(taskID).String(), text, nil, nil)
}

// AskHuman puts a question to the human in a task and waits for the answer,
// with the elicit tool's card, validation and limits.
func (ps *WorkspaceServer) AskHuman(ctx context.Context, taskID int64, q agentlink.Question) (agentlink.Answer, error) {
	if q.Message == "" {
		return agentlink.Answer{}, ErrBadQuestion
	}
	metadata := map[string]any{"type": "elicitation_request", "message": q.Message, "mode": q.Mode, "status": "pending"}
	switch q.Mode {
	case "form":
		if len(q.RequestedSchema) == 0 {
			return agentlink.Answer{}, ErrBadQuestion
		}
		if err := validateElicitRequestedSchema(q.RequestedSchema); err != nil {
			return agentlink.Answer{}, fmt.Errorf("invalid requestedSchema: %w", err)
		}
		metadata["requestedSchema"] = q.RequestedSchema
	case "url":
		if q.URL == "" {
			return agentlink.Answer{}, ErrBadQuestion
		}
		metadata["url"] = q.URL
	default:
		return agentlink.Answer{}, ErrBadQuestion
	}
	resp, err := ps.askHuman(ctx, taskID, q.Message, metadata, questionTimeout(q.Timeout))
	if err != nil {
		return agentlink.Answer{}, err
	}
	return agentlink.Answer{Action: resp.Action, Content: resp.Content}, nil
}

// questionTimeout is how long a question waits: the elicit tool's default for
// none, and never longer than its cap.
func questionTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return elicitDefaultTimeout
	}
	return min(d, elicitMaxTimeout)
}

// maxHumanTaskTitle is the gateway's limit for a title made from a question.
const maxHumanTaskTitle = 80

// CreateHumanTask opens a task for the human to answer a question that came
// with no task — a login asked for before any session existed.
func (ps *WorkspaceServer) CreateHumanTask(ctx context.Context, title, body string) (int64, error) {
	if ps.isArchived() {
		return 0, ErrWorkspaceArchived
	}
	if utf8.RuneCountInString(title) > maxHumanTaskTitle {
		title = string([]rune(title)[:maxHumanTaskTitle-3]) + "..."
	}
	now := time.Now()
	created, err := ps.createTask(ctx, model.Task{
		ID:          ps.idgen.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		WorkspaceID: ps.workspaceID,
		UserID:      monoflake.IDFromBase62(ps.userID).Int64(),
		CreatedBy:   "agent",
		Assignee:    "human",
		Status:      "ongoing",
		Title:       title,
		Body:        body,
	})
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

// MarkTaskOngoing records that a native agent has started a task: the moment
// its prompt is sent. Anything already past notstarted is left alone.
func (ps *WorkspaceServer) MarkTaskOngoing(ctx context.Context, linkID string, taskID int64) error {
	t, err := ps.getTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status != "notstarted" {
		return nil
	}
	_, err = ps.updateStatus(ps.linkTaskAgentContext(ctx, linkID), taskID, "ongoing")
	return err
}

// OfferNextTask runs one poller tick now, for a link that has just freed a
// slot, and reports the task it offered (0 for none). The rotation, the limit
// and the clear-before-push rules are the poller's own.
//
// Never call it from inside a link method: the tick holds pollMu while it
// pushes, and the push calls the link.
func (ps *WorkspaceServer) OfferNextTask(ctx context.Context) int64 {
	ps.pollMu.Lock()
	repo := ps.pollRepo
	ps.pollMu.Unlock()
	if repo == nil {
		return 0
	}
	return ps.pollOnce(repo)
}

// ToolNames is what this workspace's tools/list answers. Read on first use
// rather than at construction, which runs under the manager's lock, and kept
// only once a listing succeeds: an empty one is a failed read, and keeping it
// would send every call to the workspace's own tools to the human.
func (ps *WorkspaceServer) ToolNames() map[string]struct{} {
	ps.toolNamesMu.Lock()
	defer ps.toolNamesMu.Unlock()
	if ps.toolNames == nil {
		if names := listToolNames(ps.mcpServer); len(names) > 0 {
			ps.toolNames = names
		} else {
			return names
		}
	}
	return ps.toolNames
}

// toolNameTransports connects listToolNames to the server; a var so a test
// can hand it a transport that fails.
var toolNameTransports = func() (mcp.Transport, mcp.Transport) {
	c, s := mcp.NewInMemoryTransports()
	return c, s
}

// listToolNames asks a server for its tools through an in-memory client. The
// session it opens never streams, so nothing that counts live agents sees it.
func listToolNames(srv *mcp.Server) map[string]struct{} {
	names := map[string]struct{}{}
	if srv == nil {
		return names
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientT, serverT := toolNameTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return names
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "agentrq-tool-names", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		return names
	}
	defer cs.Close()
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			break
		}
		names[tool.Name] = struct{}{}
	}
	return names
}

func (ps *WorkspaceServer) isArchived() bool {
	ps.metadataMu.RLock()
	defer ps.metadataMu.RUnlock()
	return ps.archivedAt != nil
}
