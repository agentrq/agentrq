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
	"prints only the id; `pano read <id> --out DIR` writes the markdown and its files there; see `pano --help`."

// PanoToolDescription is the pano tool's description, on both servers. With
// PanoCLIHint it must stay within the 2048 bytes Claude Code keeps.
var PanoToolDescription = "Read and write the Pano: markdown docs shared by every workspace of this account. " +
	"Inputs: action, and payload, its fields; help with payload {\"topic\": \"<action>\"} gives an action's payload. " +
	"save: no id creates a doc, returning its id and version; with an id it replaces title and markdown, both required. " +
	"Every write changes a doc's version; save or edit needs the version you read, and if refused, read again, redo your change, retry with the new version. " +
	"edit: id, version and ops, all or none: {op: append, text} adds at the end; {op: replace, oldString, newString} swaps oldString, which must occur once. Tick an item by replacing \"- [ ] x\" with \"- [x] x\". " +
	"A checklist item ending → [@workspace](/workspaces/<id>/tasks/<id> \"status\") is assigned to that task: keep the link. " +
	"A read shows the task's status now, and the box ticks when it completes; unticked by hand, it stays so until the status changes. " +
	"With manual after the status (\"completed manual\") it never ticks itself: tick it when the work is really done, such as its PR merged. " +
	"Tick your own steps as plain items; assign an item only for a whole task. " +
	"A doc is deleted ttlDays after its last save (30, or the last ttlDays given). " +
	"files must each be linked from the markdown by filename, [text](name) or ![alt](name), or the save is refused; one no longer linked is dropped. " +
	"read: a doc by id, with its version and linked files; includeComments and includeAttachments add those. Also comment, attach (files that need no link) and delete. " +
	"Your workspace (a fork's parent) is the author. No search: share a doc's id with the agents that need it. " +
	fmt.Sprintf("Limits: title %d characters, markdown %d KiB, file %d MiB, %d files per doc.", crud.PanoMaxTitleChars, crud.PanoMaxMarkdownBytes>>10, crud.PanoMaxFileBytes>>20, crud.PanoMaxFiles)

// PanoParams is the input to the pano tool.
type PanoParams struct {
	Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
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
