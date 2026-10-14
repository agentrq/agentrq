// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// PanoCLIHint points a workspace's agent at agentrq-ws, which reads the doc's
// markdown and files from disk and writes them back there, so neither the
// markdown nor any base64 passes through the agent's context. It is for the
// workspace server only: agentrq-ws reads a workspace's .mcp.json.
const PanoCLIHint = " Prefer the shell to save tokens: `npx -y @agentrq/agentrq-ws@latest pano save [id --version N] --title T --markdown @doc.md --file chart.png` " +
	"prints only the id, and `pano read <id> --out DIR` writes the markdown and downloads its files there; see `pano --help`."

// PanoToolDescription is the pano tool's description, on both servers.
var PanoToolDescription = "Read and write the Pano: markdown docs shared by every workspace of this account. Two inputs: action, and payload, the action's fields. " +
	"Run action help for the actions, and help with payload {\"topic\": \"<action>\"} for one's payload. " +
	"save: no id creates a doc and returns its id and version; with an id it replaces the doc's title and markdown. A title and markdown are required. " +
	"Every doc has a version that changes on every write: updating a doc (save or edit) needs the version you read, and is refused if the doc changed since: read it again, redo your change on the new content, save again with the new version. " +
	"edit: changes part of a doc without sending it whole, from id, version and ops: {op: append, text} adds text at the end, {op: replace, oldString, newString} swaps oldString, which must occur exactly once. The ops apply all or none; ticking an item is a replace of \"- [ ] x\" with \"- [x] x\". " +
	"A markdown task list (- [ ] item, - [x] done) shows as a checklist with its progress. " +
	"An item ending → [@workspace](/workspaces/<id>/tasks/<id> \"status\") is assigned to that task: keep the link when you save, and the item follows the task's status, ticked once it is completed. " +
	"Tick the steps of your own work as plain items; assign an item only when it stands for a whole task. A doc you write while on a task is linked to that task. " +
	"A doc is deleted ttlDays after its last save: 30 by default, or as long as it last asked for. color is its post-it's color, yellow by default. " +
	"files are linked files: each must be linked from the markdown by its filename, as [text](name) or ![alt](name), or the save is refused; " +
	"a linked file the markdown stops linking is dropped. " +
	"read: a doc by id, with its version and linked files; includeComments and includeAttachments add those. " +
	"comment: adds comment to the doc. attach: adds files listed with the doc that need no link. delete: deletes the doc. " +
	"Your workspace (its parent, for a fork) is recorded as the author. There is no search: share a doc's id with the agents that need it. " +
	fmt.Sprintf("Limits: title %d characters, markdown %d KiB, file %d MiB, %d files per doc.", crud.PanoMaxTitleChars, crud.PanoMaxMarkdownBytes>>10, crud.PanoMaxFileBytes>>20, crud.PanoMaxFiles)

// PanoParams is the input to the pano tool.
type PanoParams struct {
	Action  string         `json:"action" jsonschema:"enum: save, edit, read, comment, attach, delete, help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
}

// The pano tool's payloads, one per action.
type (
	PanoSavePayload struct {
		ID       string          `json:"id,omitempty" jsonschema:"The doc to replace (base62). Leave it out to create a doc."`
		Version  int64           `json:"version,omitempty" jsonschema:"The doc's version, as you last read it. Required with an id, refused if the doc has changed since; not needed to create."`
		Title    string          `json:"title" jsonschema:"The doc's title."`
		Markdown string          `json:"markdown" jsonschema:"The doc's full markdown, replacing what it had."`
		TTLDays  int             `json:"ttlDays,omitempty" jsonschema:"How many days the doc lives from this save before it is deleted. Leave it out to keep the doc's own, 30 for a new doc."`
		Color    string          `json:"color,omitempty" jsonschema:"enum: yellow, pink, green, blue, orange, purple, amber"`
		Files    []PanoFileParam `json:"files,omitempty" jsonschema:"Linked files, each linked from the markdown by its filename."`
	}
	PanoEditPayload struct {
		ID      string       `json:"id" jsonschema:"The doc's ID (base62)."`
		Version int64        `json:"version" jsonschema:"The doc's version, as you last read it. Refused if the doc has changed since."`
		Ops     []PanoEditOp `json:"ops" jsonschema:"The changes, applied in order, all or none."`
	}
	PanoReadPayload struct {
		ID                 string `json:"id" jsonschema:"The doc's ID (base62)."`
		IncludeComments    bool   `json:"includeComments,omitempty" jsonschema:"Include the doc's comments."`
		IncludeAttachments bool   `json:"includeAttachments,omitempty" jsonschema:"Include the doc's attachments."`
	}
	PanoCommentPayload struct {
		ID      string `json:"id" jsonschema:"The doc's ID (base62)."`
		Comment string `json:"comment" jsonschema:"The comment's text."`
	}
	PanoAttachPayload struct {
		ID    string          `json:"id" jsonschema:"The doc's ID (base62)."`
		Files []PanoFileParam `json:"files" jsonschema:"The attachments: {filename, mimeType, data (base64)}."`
	}
	PanoDeletePayload struct {
		ID string `json:"id" jsonschema:"The doc's ID (base62)."`
	}
)

// PanoEditOp is one change of an edit.
type PanoEditOp struct {
	Op        string `json:"op" jsonschema:"enum: append, replace"`
	Text      string `json:"text,omitempty" jsonschema:"append: the text added at the end of the markdown, as is: start it with a newline to begin a new line."`
	OldString string `json:"oldString,omitempty" jsonschema:"replace: the text to replace. It must occur exactly once in the markdown."`
	NewString string `json:"newString,omitempty" jsonschema:"replace: what it becomes. Empty deletes it."`
}

// PanoFileParam is a file sent to the Pano.
type PanoFileParam struct {
	Filename string `json:"filename"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data" jsonschema:"The file's content, base64."`
}

// panoFileView, panoCommentView and panoDocView are what the tool answers with.
type panoFileView struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mimeType"`
	Size      int    `json:"size"`
	URL       string `json:"url,omitempty"`
	AddedBy   string `json:"addedBy"`
	CreatedAt string `json:"createdAt"`
}

type panoCommentView struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type panoDocView struct {
	ID        string `json:"id"`
	Version   int64  `json:"version"`
	Title     string `json:"title"`
	Markdown  string `json:"markdown,omitempty"`
	CreatedBy string `json:"createdBy"`
	UpdatedBy string `json:"updatedBy"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	TTLDays   int    `json:"ttlDays"`
	ExpiresAt string `json:"expiresAt"`
	Color     string `json:"color"`
	// The markdown's task list: how many of its items are checked.
	ChecklistDone  int               `json:"checklistDone,omitempty"`
	ChecklistTotal int               `json:"checklistTotal,omitempty"`
	Files          []panoFileView    `json:"files"`
	Attachments    []panoFileView    `json:"attachments,omitempty"`
	Comments       []panoCommentView `json:"comments,omitempty"`
}

// panoAuthor names who wrote something: a workspace's id, or "human".
func panoAuthor(workspaceID int64) string {
	if workspaceID == 0 {
		return "human"
	}
	return monoflake.ID(workspaceID).String()
}

func panoFileViews(files []entity.PanoFile) []panoFileView {
	out := make([]panoFileView, len(files))
	for i, f := range files {
		out[i] = panoFileView{
			ID:        monoflake.ID(f.ID).String(),
			Filename:  f.Filename,
			MimeType:  f.MimeType,
			Size:      f.SizeBytes,
			URL:       f.URL,
			AddedBy:   panoAuthor(f.WorkspaceID),
			CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	return out
}

func panoDocViewOf(d entity.PanoDoc) panoDocView {
	v := panoDocView{
		ID:             monoflake.ID(d.ID).String(),
		Version:        d.Version,
		Title:          d.Title,
		Markdown:       d.Markdown,
		CreatedBy:      panoAuthor(d.CreatedByWorkspaceID),
		UpdatedBy:      panoAuthor(d.UpdatedByWorkspaceID),
		CreatedAt:      d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      d.UpdatedAt.UTC().Format(time.RFC3339),
		TTLDays:        d.TTLDays,
		ExpiresAt:      d.ExpiresAt.UTC().Format(time.RFC3339),
		Color:          d.Color.String(),
		ChecklistDone:  d.ChecklistDone,
		ChecklistTotal: d.ChecklistTotal,
		Files:          panoFileViews(d.Files),
	}
	if d.Attachments != nil {
		v.Attachments = panoFileViews(d.Attachments)
	}
	if d.Comments != nil {
		v.Comments = make([]panoCommentView, len(d.Comments))
		for i, c := range d.Comments {
			v.Comments[i] = panoCommentView{
				ID:        monoflake.ID(c.ID).String(),
				Author:    panoAuthor(c.WorkspaceID),
				Body:      c.Body,
				CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
			}
		}
	}
	return v
}

func panoUploads(files []PanoFileParam) []entity.PanoUpload {
	out := make([]entity.PanoUpload, len(files))
	for i, f := range files {
		out[i] = entity.PanoUpload{Filename: f.Filename, MimeType: f.MimeType, Data: f.Data}
	}
	return out
}

func jsonResult(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// panoFailure is a refusal passed on as written, a missing doc as such, or
// any other error prefixed with what was being attempted.
func panoFailure(err error, action string) *mcp.CallToolResult {
	var refusal *entity.PanoError
	switch {
	case errors.As(err, &refusal):
		return toolError("%s", refusal.Message)
	case errors.Is(err, base.ErrNotFound):
		return toolError("Pano doc not found")
	}
	return toolError("failed to %s the Pano doc: %v", action, err)
}

// PanoActions are the pano tool's actions, for the account userID writing as
// the workspace workspaceID.
func PanoActions(pano crud.PanoController, userID string, workspaceID int64) []ToolAction {
	// docID reads a doc's id; one that is not base62 names no doc.
	docID := func(id string) (int64, *mcp.CallToolResult) {
		if n := monoflake.IDFromBase62(id).Int64(); n != 0 {
			return n, nil
		}
		return 0, toolError("Pano doc not found")
	}
	return []ToolAction{
		NewToolAction("save", "Create a doc (no id) or replace one's title and markdown; replacing needs the version you read. Returns the doc, with its new version, without its markdown.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoSavePayload) (*mcp.CallToolResult, any, error) {
			var id int64
			if p.ID != "" {
				var bad *mcp.CallToolResult
				if id, bad = docID(p.ID); bad != nil {
					return bad, nil, nil
				}
			}
			rs, err := pano.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Version: p.Version, Title: p.Title, Markdown: p.Markdown, TTLDays: p.TTLDays, Color: p.Color, Files: panoUploads(p.Files)})
			if err != nil {
				return panoFailure(err, "save"), nil, nil
			}
			v := panoDocViewOf(rs.Doc)
			v.Markdown = ""
			return jsonResult(v), nil, nil
		}).WithExample(`{"title": "Release plan", "markdown": "# Release plan\n\n- [ ] Tag v0.9.20", "color": "green"}`),
		NewToolAction("edit", "Change part of a doc without sending it whole: append text, or replace a text that occurs exactly once. Needs the version you read. Returns the doc without its markdown.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoEditPayload) (*mcp.CallToolResult, any, error) {
			id, bad := docID(p.ID)
			if bad != nil {
				return bad, nil, nil
			}
			ops := make([]entity.PanoEditOp, len(p.Ops))
			for i, o := range p.Ops {
				ops[i] = entity.PanoEditOp{Op: o.Op, Text: o.Text, OldString: o.OldString, NewString: o.NewString}
			}
			rs, err := pano.EditPanoDoc(ctx, entity.EditPanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Version: p.Version, Ops: ops})
			if err != nil {
				return panoFailure(err, "edit"), nil, nil
			}
			v := panoDocViewOf(rs.Doc)
			v.Markdown = ""
			return jsonResult(v), nil, nil
		}).WithExample(`{"id": "0kGpezGeuxt", "version": 3, "ops": [{"op": "replace", "oldString": "- [ ] Tag v0.9.20", "newString": "- [x] Tag v0.9.20"}, {"op": "append", "text": "\n- [ ] Announce"}]}`),
		NewToolAction("read", "Read a doc, with its linked files.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoReadPayload) (*mcp.CallToolResult, any, error) {
			id, bad := docID(p.ID)
			if bad != nil {
				return bad, nil, nil
			}
			rs, err := pano.GetPanoDoc(ctx, entity.GetPanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, IncludeComments: p.IncludeComments, IncludeAttachments: p.IncludeAttachments})
			if err != nil {
				return panoFailure(err, "read"), nil, nil
			}
			return jsonResult(panoDocViewOf(rs.Doc)), nil, nil
		}).WithExample(`{"id": "0kGpezGeuxt", "includeComments": true}`),
		NewToolAction("comment", "Add a comment to a doc.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoCommentPayload) (*mcp.CallToolResult, any, error) {
			id, bad := docID(p.ID)
			if bad != nil {
				return bad, nil, nil
			}
			rs, err := pano.AddPanoComment(ctx, entity.AddPanoCommentRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Body: p.Comment})
			if err != nil {
				return panoFailure(err, "comment on"), nil, nil
			}
			return jsonResult(panoCommentView{ID: monoflake.ID(rs.Comment.ID).String(), Author: panoAuthor(rs.Comment.WorkspaceID), Body: rs.Comment.Body, CreatedAt: rs.Comment.CreatedAt.UTC().Format(time.RFC3339)}), nil, nil
		}).WithExample(`{"id": "0kGpezGeuxt", "comment": "Done on our side."}`),
		NewToolAction("attach", "Add files to a doc that its markdown need not link.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoAttachPayload) (*mcp.CallToolResult, any, error) {
			id, bad := docID(p.ID)
			if bad != nil {
				return bad, nil, nil
			}
			rs, err := pano.AttachPanoFiles(ctx, entity.AttachPanoFilesRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Files: panoUploads(p.Files)})
			if err != nil {
				return panoFailure(err, "attach to"), nil, nil
			}
			return jsonResult(map[string]any{"attachments": panoFileViews(rs.Attachments)}), nil, nil
		}).WithExample(`{"id": "0kGpezGeuxt", "files": [{"filename": "notes.txt", "mimeType": "text/plain", "data": "aGVsbG8="}]}`),
		NewToolAction("delete", "Delete a doc.", func(ctx context.Context, _ *mcp.CallToolRequest, p PanoDeletePayload) (*mcp.CallToolResult, any, error) {
			id, bad := docID(p.ID)
			if bad != nil {
				return bad, nil, nil
			}
			if err := pano.DeletePanoDoc(ctx, entity.DeletePanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id}); err != nil {
				return panoFailure(err, "delete"), nil, nil
			}
			return textResult("Deleted Pano doc %s.", p.ID), nil, nil
		}).WithExample(`{"id": "0kGpezGeuxt"}`),
	}
}

// RunPano does one pano tool call for the account userID, as the workspace
// workspaceID: what both MCP servers' pano tools run.
func RunPano(ctx context.Context, pano crud.PanoController, userID string, workspaceID int64, p PanoParams) *mcp.CallToolResult {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	res, _, _ := RunAction(ctx, nil, "pano", PanoActions(pano, userID, workspaceID), p.Action, p.Payload)
	return res
}

func (ps *WorkspaceServer) handlePano(ctx context.Context, req *mcp.CallToolRequest, params PanoParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "pano", clientIdentityFromRequest(req))
	if ps.pano == nil {
		return toolError("the Pano is not available on this server"), nil, nil
	}
	// A doc this agent writes is linked to its own task: the fork's, for a
	// fork, though the doc's author is the parent.
	ctx = entity.WithAgentWorkspace(ctx, ps.workspaceID)
	return RunPano(ctx, ps.pano, ps.userID, ps.contentID(), params), nil, nil
}
