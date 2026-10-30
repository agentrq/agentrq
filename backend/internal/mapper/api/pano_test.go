// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func TestFromHTTPRequestToPanoDocIDRequestEntities(t *testing.T) {
	doc := "/pano/" + b62(40)
	checkRequest(t, "/pano/:id", FromHTTPRequestToGetPanoDocRequestEntity, []requestCase[entity.GetPanoDocRequest]{
		// The interface reads a doc whole.
		{"a doc", doc, ``, &entity.GetPanoDocRequest{ID: 40, IncludeComments: true, IncludeAttachments: true}},
		{"doc 0", "/pano/0", ``, nil},
	})
	checkRequest(t, "/pano/:id", FromHTTPRequestToDeletePanoDocRequestEntity, []requestCase[entity.DeletePanoDocRequest]{
		{"a doc", doc, ``, &entity.DeletePanoDocRequest{ID: 40}},
		{"doc 0", "/pano/0", ``, nil},
	})
	checkRequest(t, "/pano/:id/comments", FromHTTPRequestToAddPanoCommentRequestEntity, []requestCase[entity.AddPanoCommentRequest]{
		{"a comment", doc + "/comments", `{"body":"Looks right"}`, &entity.AddPanoCommentRequest{ID: 40, Body: "Looks right"}},
		{"doc 0", "/pano/0/comments", `{"body":"x"}`, nil},
		{"not JSON", doc + "/comments", `{`, nil},
	})
	checkRequest(t, "/pano/:id/attachments", FromHTTPRequestToAttachPanoFilesRequestEntity, []requestCase[entity.AttachPanoFilesRequest]{
		{"two files", doc + "/attachments", `{"files":[{"filename":"a.png","mimeType":"image/png","data":"AA=="},{"filename":"b.txt"}]}`,
			&entity.AttachPanoFilesRequest{ID: 40, Files: []entity.PanoUpload{{Filename: "a.png", MimeType: "image/png", Data: "AA=="}, {Filename: "b.txt"}}}},
		{"none", doc + "/attachments", `{}`, &entity.AttachPanoFilesRequest{ID: 40, Files: []entity.PanoUpload{}}},
		{"doc 0", "/pano/0/attachments", `{}`, nil},
		{"not JSON", doc + "/attachments", `{`, nil},
	})
}

func TestFromHTTPRequestToSavePanoDocRequestEntity(t *testing.T) {
	body := `{"version":3,"title":"Plan","markdown":"![c](chart.png)","ttlDays":14,"color":"blue","files":[{"filename":"chart.png","mimeType":"image/png","data":"AA=="}]}`
	files := []entity.PanoUpload{{Filename: "chart.png", MimeType: "image/png", Data: "AA=="}}
	// A new doc has no id in its path.
	checkRequest(t, "/pano", FromHTTPRequestToSavePanoDocRequestEntity, []requestCase[entity.SavePanoDocRequest]{
		{"a new doc", "/pano", body, &entity.SavePanoDocRequest{Version: 3, Title: "Plan", Markdown: "![c](chart.png)", TTLDays: 14, Color: "blue", Files: files}},
		{"not JSON", "/pano", `{`, nil},
	})
	checkRequest(t, "/pano/:id", FromHTTPRequestToSavePanoDocRequestEntity, []requestCase[entity.SavePanoDocRequest]{
		{"a new version", "/pano/" + b62(40), body,
			&entity.SavePanoDocRequest{ID: 40, Version: 3, Title: "Plan", Markdown: "![c](chart.png)", TTLDays: 14, Color: "blue", Files: files}},
		{"an empty doc keeps no files", "/pano/" + b62(40), `{}`, &entity.SavePanoDocRequest{ID: 40, Files: []entity.PanoUpload{}}},
		{"doc 0", "/pano/0", body, nil},
	})
}

func TestPanoResponses(t *testing.T) {
	expires := at.AddDate(0, 0, 14)
	byPerson := entity.PanoFile{ID: 50, CreatedAt: at, Filename: "chart.png", MimeType: "image/png", SizeBytes: 9, URL: "https://cdn/pano/50"}
	byAgent := entity.PanoFile{ID: 51, CreatedAt: at, WorkspaceID: 20, Filename: "log.txt", MimeType: "text/plain", SizeBytes: 3}
	wantByPerson := view.PanoFile{ID: b62(50), CreatedAt: at, Filename: "chart.png", MimeType: "image/png", Size: 9, URL: "https://cdn/pano/50"}
	wantByAgent := view.PanoFile{ID: b62(51), CreatedAt: at, WorkspaceID: b62(20), Filename: "log.txt", MimeType: "text/plain", Size: 3}

	full := entity.PanoDoc{ID: 40, CreatedAt: at, UpdatedAt: at.Add(1), Title: "Plan", Excerpt: "e", Markdown: "m",
		CreatedByWorkspaceID: 20, UpdatedByWorkspaceID: 0, TTLDays: 14, ExpiresAt: expires, Color: entity.PanoColorBlue,
		ChecklistDone: 1, ChecklistTotal: 3, Version: 4,
		Files: []entity.PanoFile{byPerson}, Attachments: []entity.PanoFile{byAgent},
		Comments: []entity.PanoComment{{ID: 60, CreatedAt: at, Body: "mine"}, {ID: 61, CreatedAt: at, WorkspaceID: 20, Body: "agent's"}},
		Tasks:    []entity.PanoDocTask{{TaskID: 70, WorkspaceID: 20, Status: "ongoing"}}}
	// A blank workspace id is the person.
	wantFull := view.PanoDoc{ID: b62(40), CreatedAt: at, UpdatedAt: at.Add(1), Title: "Plan", Excerpt: "e", Markdown: "m",
		CreatedByWorkspaceID: b62(20), TTLDays: 14, ExpiresAt: expires, Color: "blue",
		ChecklistDone: 1, ChecklistTotal: 3, Version: 4,
		Files: []view.PanoFile{wantByPerson}, Attachments: []view.PanoFile{wantByAgent},
		Comments: []view.PanoComment{{ID: b62(60), CreatedAt: at, Body: "mine"}, {ID: b62(61), CreatedAt: at, WorkspaceID: b62(20), Body: "agent's"}},
		Tasks:    []view.PanoDocTask{{TaskID: b62(70), WorkspaceID: b62(20), Status: "ongoing"}}}
	// A listed doc has none of its parts, and an unknown color reads as yellow.
	listed := entity.PanoDoc{ID: 41, CreatedAt: at, UpdatedAt: at, Title: "Note", UpdatedByWorkspaceID: 21, ExpiresAt: expires, Color: entity.PanoColor(200)}
	wantListed := view.PanoDoc{ID: b62(41), CreatedAt: at, UpdatedAt: at, Title: "Note", UpdatedByWorkspaceID: b62(21), ExpiresAt: expires, Color: "yellow"}

	checkResponse(t, FromPanoDocEntityToHTTPResponse(full), view.PanoDocResponse{Doc: wantFull})
	checkResponse(t, FromListPanoDocsResponseEntityToHTTPResponse(&entity.ListPanoDocsResponse{Docs: []entity.PanoDoc{listed, full}}),
		view.ListPanoDocsResponse{Docs: []view.PanoDoc{wantListed, wantFull}})
	if got := string(FromListPanoDocsResponseEntityToHTTPResponse(&entity.ListPanoDocsResponse{})); got != `{"docs":[]}` {
		t.Errorf("no docs = %s, want an empty list", got)
	}

	checkResponse(t, FromAddPanoCommentResponseEntityToHTTPResponse(&entity.AddPanoCommentResponse{Comment: entity.PanoComment{ID: 61, CreatedAt: at, WorkspaceID: 20, Body: "b"}}),
		view.AddPanoCommentResponse{Comment: view.PanoComment{ID: b62(61), CreatedAt: at, WorkspaceID: b62(20), Body: "b"}})

	checkResponse(t, FromAttachPanoFilesResponseEntityToHTTPResponse(&entity.AttachPanoFilesResponse{Attachments: []entity.PanoFile{byPerson, byAgent}}),
		view.AttachPanoFilesResponse{Attachments: []view.PanoFile{wantByPerson, wantByAgent}})
}
