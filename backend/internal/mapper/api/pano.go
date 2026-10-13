// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// panoDocID reads the doc id from the path; 0 when it is not one.
func panoDocID(c *fiber.Ctx) int64 {
	return monoflake.IDFromBase62(c.Params("id")).Int64()
}

func FromHTTPRequestToGetPanoDocRequestEntity(c *fiber.Ctx) *entity.GetPanoDocRequest {
	id := panoDocID(c)
	if id == 0 {
		return nil
	}
	return &entity.GetPanoDocRequest{ID: id, IncludeComments: true, IncludeAttachments: true}
}

// FromHTTPRequestToSavePanoDocRequestEntity reads a new doc, or with an id in
// the path a doc's new version.
func FromHTTPRequestToSavePanoDocRequestEntity(c *fiber.Ctx) *entity.SavePanoDocRequest {
	var id int64
	if c.Params("id") != "" {
		if id = panoDocID(c); id == 0 {
			return nil
		}
	}
	var body view.SavePanoDocRequest
	if err := c.BodyParser(&body); err != nil {
		return nil
	}
	return &entity.SavePanoDocRequest{ID: id, Title: body.Title, Markdown: body.Markdown, TTLDays: body.TTLDays, Color: body.Color, Files: fromPanoUploadViews(body.Files)}
}

func FromHTTPRequestToDeletePanoDocRequestEntity(c *fiber.Ctx) *entity.DeletePanoDocRequest {
	id := panoDocID(c)
	if id == 0 {
		return nil
	}
	return &entity.DeletePanoDocRequest{ID: id}
}

func FromHTTPRequestToAddPanoCommentRequestEntity(c *fiber.Ctx) *entity.AddPanoCommentRequest {
	id := panoDocID(c)
	if id == 0 {
		return nil
	}
	var body view.AddPanoCommentRequest
	if err := c.BodyParser(&body); err != nil {
		return nil
	}
	return &entity.AddPanoCommentRequest{ID: id, Body: body.Body}
}

func FromHTTPRequestToAttachPanoFilesRequestEntity(c *fiber.Ctx) *entity.AttachPanoFilesRequest {
	id := panoDocID(c)
	if id == 0 {
		return nil
	}
	var body view.AttachPanoFilesRequest
	if err := c.BodyParser(&body); err != nil {
		return nil
	}
	return &entity.AttachPanoFilesRequest{ID: id, Files: fromPanoUploadViews(body.Files)}
}

func FromListPanoDocsResponseEntityToHTTPResponse(rs *entity.ListPanoDocsResponse) []byte {
	docs := make([]view.PanoDoc, len(rs.Docs))
	for i, d := range rs.Docs {
		docs[i] = fromEntityPanoDocToView(d)
	}
	b, _ := json.Marshal(view.ListPanoDocsResponse{Docs: docs})
	return b
}

func FromPanoDocEntityToHTTPResponse(d entity.PanoDoc) []byte {
	b, _ := json.Marshal(view.PanoDocResponse{Doc: fromEntityPanoDocToView(d)})
	return b
}

func FromAddPanoCommentResponseEntityToHTTPResponse(rs *entity.AddPanoCommentResponse) []byte {
	b, _ := json.Marshal(view.AddPanoCommentResponse{Comment: fromEntityPanoCommentToView(rs.Comment)})
	return b
}

func FromAttachPanoFilesResponseEntityToHTTPResponse(rs *entity.AttachPanoFilesResponse) []byte {
	b, _ := json.Marshal(view.AttachPanoFilesResponse{Attachments: fromEntityPanoFilesToViews(rs.Attachments)})
	return b
}

func fromPanoUploadViews(in []view.PanoUpload) []entity.PanoUpload {
	out := make([]entity.PanoUpload, len(in))
	for i, u := range in {
		out[i] = entity.PanoUpload{Filename: u.Filename, MimeType: u.MimeType, Data: u.Data}
	}
	return out
}

// panoWorkspaceID is a workspace id as the API writes it; blank for the person.
func panoWorkspaceID(id int64) string {
	if id == 0 {
		return ""
	}
	return monoflake.ID(id).String()
}

func fromEntityPanoDocToView(d entity.PanoDoc) view.PanoDoc {
	v := view.PanoDoc{
		ID:                   monoflake.ID(d.ID).String(),
		Title:                d.Title,
		Excerpt:              d.Excerpt,
		Markdown:             d.Markdown,
		CreatedByWorkspaceID: panoWorkspaceID(d.CreatedByWorkspaceID),
		UpdatedByWorkspaceID: panoWorkspaceID(d.UpdatedByWorkspaceID),
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
		TTLDays:              d.TTLDays,
		ExpiresAt:            d.ExpiresAt,
		Color:                d.Color.String(),
		ChecklistDone:        d.ChecklistDone,
		ChecklistTotal:       d.ChecklistTotal,
		Files:                fromEntityPanoFilesToViews(d.Files),
		Attachments:          fromEntityPanoFilesToViews(d.Attachments),
	}
	for _, c := range d.Comments {
		v.Comments = append(v.Comments, fromEntityPanoCommentToView(c))
	}
	return v
}

func fromEntityPanoFilesToViews(files []entity.PanoFile) []view.PanoFile {
	if files == nil {
		return nil
	}
	out := make([]view.PanoFile, len(files))
	for i, f := range files {
		out[i] = view.PanoFile{
			ID:          monoflake.ID(f.ID).String(),
			Filename:    f.Filename,
			MimeType:    f.MimeType,
			Size:        f.SizeBytes,
			URL:         f.URL,
			WorkspaceID: panoWorkspaceID(f.WorkspaceID),
			CreatedAt:   f.CreatedAt,
		}
	}
	return out
}

func fromEntityPanoCommentToView(c entity.PanoComment) view.PanoComment {
	return view.PanoComment{
		ID:          monoflake.ID(c.ID).String(),
		WorkspaceID: panoWorkspaceID(c.WorkspaceID),
		Body:        c.Body,
		CreatedAt:   c.CreatedAt,
	}
}
