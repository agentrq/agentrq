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
const PanoCLIHint = " Prefer the shell to save tokens: `npx -y @agentrq/agentrq-ws@latest pano save [id] --title T --markdown @doc.md --file chart.png` " +
	"prints only the id, and `pano read <id> --out DIR` writes the markdown and downloads its files there; see `pano --help`."

// PanoToolDescription is the pano tool's description, on both servers.
var PanoToolDescription = "Read and write the Pano: markdown docs shared by every workspace of this account. One action per call. " +
	"save: no id creates a doc and returns its id; with an id it replaces the doc's title and markdown. A title and markdown are required. " +
	"A markdown task list (- [ ] item, - [x] done) shows as a checklist with its progress. " +
	"An item ending → [@workspace](/workspaces/<id>/tasks/<id> \"status\") is assigned to that task: keep the link when you save, and the item follows the task's status, ticked once it is completed. " +
	"A doc is deleted ttlDays after its last save: 30 by default, or as long as it last asked for. color is its post-it's color, yellow by default. " +
	"files are linked files: each must be linked from the markdown by its filename, as [text](name) or ![alt](name), or the save is refused; " +
	"a linked file the markdown stops linking is dropped. " +
	"read: a doc by id, with its linked files; includeComments and includeAttachments add those. " +
	"comment: adds comment to the doc. attach: adds files listed with the doc that need no link. delete: deletes the doc. " +
	"Your workspace (its parent, for a fork) is recorded as the author. There is no search: share a doc's id with the agents that need it. " +
	fmt.Sprintf("Limits: title %d characters, markdown %d KiB, file %d MiB, %d files per doc.", crud.PanoMaxTitleChars, crud.PanoMaxMarkdownBytes>>10, crud.PanoMaxFileBytes>>20, crud.PanoMaxFiles)

// PanoParams is the input to the pano tool.
type PanoParams struct {
	Action             string          `json:"action" jsonschema:"enum: save, read, comment, attach, delete"`
	ID                 string          `json:"id,omitempty" jsonschema:"The doc's ID (base62). Leave it out on save to create a doc; required for every other action."`
	Title              string          `json:"title,omitempty" jsonschema:"save: the doc's title."`
	Markdown           string          `json:"markdown,omitempty" jsonschema:"save: the doc's full markdown, replacing what it had."`
	TTLDays            int             `json:"ttlDays,omitempty" jsonschema:"save: how many days the doc lives from this save before it is deleted. Leave it out to keep the doc's own, 30 for a new doc."`
	Color              string          `json:"color,omitempty" jsonschema:"enum: yellow, pink, green, blue, orange, purple, amber"`
	Files              []PanoFileParam `json:"files,omitempty" jsonschema:"save: linked files, each linked from the markdown by its filename. attach: the attachments."`
	Comment            string          `json:"comment,omitempty" jsonschema:"comment: the comment's text."`
	IncludeComments    bool            `json:"includeComments,omitempty" jsonschema:"read: include the doc's comments."`
	IncludeAttachments bool            `json:"includeAttachments,omitempty" jsonschema:"read: include the doc's attachments."`
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

// RunPano does one pano tool call for the account userID, as the workspace
// workspaceID: what both MCP servers' pano tools run.
func RunPano(ctx context.Context, pano crud.PanoController, userID string, workspaceID int64, p PanoParams) *mcp.CallToolResult {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	id := monoflake.IDFromBase62(p.ID).Int64()
	if p.Action != "save" && id == 0 {
		return toolError("%s needs the doc's id", p.Action)
	}
	switch p.Action {
	case "save":
		if p.ID != "" && id == 0 {
			return toolError("Pano doc not found")
		}
		rs, err := pano.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Title: p.Title, Markdown: p.Markdown, TTLDays: p.TTLDays, Color: p.Color, Files: panoUploads(p.Files)})
		if err != nil {
			return panoFailure(err, "save")
		}
		v := panoDocViewOf(rs.Doc)
		v.Markdown = ""
		return jsonResult(v)
	case "read":
		rs, err := pano.GetPanoDoc(ctx, entity.GetPanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, IncludeComments: p.IncludeComments, IncludeAttachments: p.IncludeAttachments})
		if err != nil {
			return panoFailure(err, "read")
		}
		return jsonResult(panoDocViewOf(rs.Doc))
	case "comment":
		rs, err := pano.AddPanoComment(ctx, entity.AddPanoCommentRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Body: p.Comment})
		if err != nil {
			return panoFailure(err, "comment on")
		}
		return jsonResult(panoCommentView{ID: monoflake.ID(rs.Comment.ID).String(), Author: panoAuthor(rs.Comment.WorkspaceID), Body: rs.Comment.Body, CreatedAt: rs.Comment.CreatedAt.UTC().Format(time.RFC3339)})
	case "attach":
		rs, err := pano.AttachPanoFiles(ctx, entity.AttachPanoFilesRequest{UserID: userID, WorkspaceID: workspaceID, ID: id, Files: panoUploads(p.Files)})
		if err != nil {
			return panoFailure(err, "attach to")
		}
		return jsonResult(map[string]any{"attachments": panoFileViews(rs.Attachments)})
	case "delete":
		if err := pano.DeletePanoDoc(ctx, entity.DeletePanoDocRequest{UserID: userID, WorkspaceID: workspaceID, ID: id}); err != nil {
			return panoFailure(err, "delete")
		}
		return textResult("Deleted Pano doc %s.", p.ID)
	}
	return toolError("unknown action %q: use save, read, comment, attach or delete", p.Action)
}

func (ps *WorkspaceServer) handlePano(ctx context.Context, req *mcp.CallToolRequest, params PanoParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "pano", clientIdentityFromRequest(req))
	if ps.pano == nil {
		return toolError("the Pano is not available on this server"), nil, nil
	}
	return RunPano(ctx, ps.pano, ps.userID, ps.contentID(), params), nil, nil
}
