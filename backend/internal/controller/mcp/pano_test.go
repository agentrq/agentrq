// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// fakePano answers every Pano call with err, or with a fixed response, and
// records the request it was given and the origin it came with.
type fakePano struct {
	crud.PanoController
	err    error
	saw    any
	origin entity.Origin
}

var panoAt = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)

func (f *fakePano) SavePanoDoc(ctx context.Context, rq entity.SavePanoDocRequest) (*entity.SavePanoDocResponse, error) {
	f.saw, f.origin = rq, entity.GetOrigin(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &entity.SavePanoDocResponse{Doc: entity.PanoDoc{
		ID: 7, Title: rq.Title, Markdown: rq.Markdown, CreatedAt: panoAt, UpdatedAt: panoAt, TTLDays: 30, ExpiresAt: panoAt.AddDate(0, 0, 30), Color: entity.PanoColorGreen,
		CreatedByWorkspaceID: rq.WorkspaceID, UpdatedByWorkspaceID: rq.WorkspaceID,
		Files: []entity.PanoFile{{ID: 11, Filename: "chart.png", MimeType: "image/png", SizeBytes: 3, URL: "https://l/c", WorkspaceID: rq.WorkspaceID, CreatedAt: panoAt}},
	}}, nil
}

func (f *fakePano) GetPanoDoc(ctx context.Context, rq entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	f.saw, f.origin = rq, entity.GetOrigin(ctx)
	if f.err != nil {
		return nil, f.err
	}
	d := entity.PanoDoc{ID: 7, Title: "Plan", Markdown: "# Plan", CreatedAt: panoAt, UpdatedAt: panoAt, CreatedByWorkspaceID: 100, Files: []entity.PanoFile{}}
	if rq.IncludeAttachments {
		d.Attachments = []entity.PanoFile{{ID: 12, Filename: "notes.txt", MimeType: "text/plain", SizeBytes: 5, CreatedAt: panoAt}}
	}
	if rq.IncludeComments {
		d.Comments = []entity.PanoComment{{ID: 13, Body: "Agreed.", CreatedAt: panoAt}, {ID: 14, WorkspaceID: 100, Body: "Done.", CreatedAt: panoAt}}
	}
	return &entity.GetPanoDocResponse{Doc: d}, nil
}

func (f *fakePano) AddPanoComment(ctx context.Context, rq entity.AddPanoCommentRequest) (*entity.AddPanoCommentResponse, error) {
	f.saw, f.origin = rq, entity.GetOrigin(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &entity.AddPanoCommentResponse{Comment: entity.PanoComment{ID: 13, WorkspaceID: rq.WorkspaceID, Body: rq.Body, CreatedAt: panoAt}}, nil
}

func (f *fakePano) AttachPanoFiles(ctx context.Context, rq entity.AttachPanoFilesRequest) (*entity.AttachPanoFilesResponse, error) {
	f.saw, f.origin = rq, entity.GetOrigin(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &entity.AttachPanoFilesResponse{Attachments: []entity.PanoFile{{ID: 12, Filename: "notes.txt", MimeType: "text/plain", SizeBytes: 5, WorkspaceID: rq.WorkspaceID, CreatedAt: panoAt}}}, nil
}

func (f *fakePano) DeletePanoDoc(ctx context.Context, rq entity.DeletePanoDocRequest) error {
	f.saw, f.origin = rq, entity.GetOrigin(ctx)
	return f.err
}

var docID = monoflake.ID(7).String()

func TestRunPano_EveryAction(t *testing.T) {
	const user, ws = "user-1", int64(100)
	wsID := monoflake.ID(ws).String()
	for _, tc := range []struct {
		name     string
		params   PanoParams
		want     any
		contains []string
		lacks    []string
	}{
		{"save with no id creates a doc and answers without its markdown",
			PanoParams{Action: "save", Payload: map[string]any{"title": "Plan", "markdown": "![c](chart.png)", "ttlDays": 30, "color": "green", "files": []PanoFileParam{{Filename: "chart.png", MimeType: "image/png", Data: "YWJj"}}}},
			entity.SavePanoDocRequest{UserID: user, WorkspaceID: ws, Title: "Plan", Markdown: "![c](chart.png)", TTLDays: 30, Color: "green", Files: []entity.PanoUpload{{Filename: "chart.png", MimeType: "image/png", Data: "YWJj"}}},
			[]string{`"id":"` + docID + `"`, `"createdBy":"` + wsID + `"`, `"ttlDays":30`, `"expiresAt":"2026-11-12T08:00:00Z"`, `"color":"green"`,
				`"files":[{"id":"` + monoflake.ID(11).String() + `","filename":"chart.png","mimeType":"image/png","size":3,"url":"https://l/c","addedBy":"` + wsID + `","createdAt":"2026-10-13T08:00:00Z"}]`},
			[]string{`"markdown"`, `"attachments"`, `"comments"`}},
		{"save with an id replaces the doc",
			PanoParams{Action: "save", Payload: map[string]any{"id": docID, "title": "Plan", "markdown": "text"}},
			entity.SavePanoDocRequest{UserID: user, WorkspaceID: ws, ID: 7, Title: "Plan", Markdown: "text", Files: []entity.PanoUpload{}},
			[]string{`"title":"Plan"`}, nil},
		{"read answers the markdown and linked files only",
			PanoParams{Action: "read", Payload: map[string]any{"id": docID}},
			entity.GetPanoDocRequest{UserID: user, WorkspaceID: ws, ID: 7},
			[]string{`"markdown":"# Plan"`, `"files":[]`, `"updatedBy":"human"`, `"color":"yellow"`},
			[]string{`"attachments"`, `"comments"`}},
		{"read with comments and attachments adds both, naming their authors",
			PanoParams{Action: "read", Payload: map[string]any{"id": docID, "includeComments": true, "includeAttachments": true}},
			entity.GetPanoDocRequest{UserID: user, WorkspaceID: ws, ID: 7, IncludeComments: true, IncludeAttachments: true},
			[]string{`"attachments":[{"id":"` + monoflake.ID(12).String() + `","filename":"notes.txt","mimeType":"text/plain","size":5,"addedBy":"human"`,
				`"comments":[{"id":"` + monoflake.ID(13).String() + `","author":"human","body":"Agreed."`, `"author":"` + wsID + `","body":"Done."`},
			nil},
		{"comment adds a comment as the workspace",
			PanoParams{Action: "comment", Payload: map[string]any{"id": docID, "comment": "Agreed."}},
			entity.AddPanoCommentRequest{UserID: user, WorkspaceID: ws, ID: 7, Body: "Agreed."},
			[]string{`{"id":"` + monoflake.ID(13).String() + `","author":"` + wsID + `","body":"Agreed.","createdAt":"2026-10-13T08:00:00Z"}`}, nil},
		{"attach adds attachments",
			PanoParams{Action: "attach", Payload: map[string]any{"id": docID, "files": []PanoFileParam{{Filename: "notes.txt", Data: "YWJj"}}}},
			entity.AttachPanoFilesRequest{UserID: user, WorkspaceID: ws, ID: 7, Files: []entity.PanoUpload{{Filename: "notes.txt", Data: "YWJj"}}},
			[]string{`{"attachments":[{"id":"` + monoflake.ID(12).String() + `","filename":"notes.txt"`, `"addedBy":"` + wsID + `"`}, nil},
		{"delete deletes the doc",
			PanoParams{Action: "delete", Payload: map[string]any{"id": docID}},
			entity.DeletePanoDocRequest{UserID: user, WorkspaceID: ws, ID: 7},
			[]string{"Deleted Pano doc " + docID + "."}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakePano{}
			res := RunPano(context.Background(), f, user, ws, tc.params)
			text, isErr := call(res, nil, nil)
			if isErr {
				t.Fatalf("RunPano answered the error %q, want a result", text)
			}
			if !reflect.DeepEqual(f.saw, tc.want) {
				t.Errorf("the controller was asked %+v, want %+v", f.saw, tc.want)
			}
			if f.origin != entity.OriginMCP {
				t.Errorf("the call came with origin %v, want MCP", f.origin)
			}
			for _, s := range tc.contains {
				if !strings.Contains(text, s) {
					t.Errorf("the answer %s does not contain %s", text, s)
				}
			}
			for _, s := range tc.lacks {
				if strings.Contains(text, s) {
					t.Errorf("the answer %s contains %s, want it left out", text, s)
				}
			}
			if strings.HasPrefix(text, "{") && !json.Valid([]byte(text)) {
				t.Errorf("the answer %s is not valid JSON", text)
			}
		})
	}
}

func TestRunPano_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params PanoParams
		want   string
	}{
		{"read with no id is refused", PanoParams{Action: "read"}, "read: payload needs id"},
		{"delete with an id that is not one is not found", PanoParams{Action: "delete", Payload: map[string]any{"id": "00000000000"}}, "Pano doc not found"},
		{"read with an id that is not one is not found", PanoParams{Action: "read", Payload: map[string]any{"id": "00000000000"}}, "Pano doc not found"},
		{"comment with an id that is not one is not found", PanoParams{Action: "comment", Payload: map[string]any{"id": "00000000000", "comment": "c"}}, "Pano doc not found"},
		{"attach with an id that is not one is not found", PanoParams{Action: "attach", Payload: map[string]any{"id": "00000000000", "files": []any{}}}, "Pano doc not found"},
		{"save with an id that is not one is not found", PanoParams{Action: "save", Payload: map[string]any{"id": "00000000000", "title": "t", "markdown": "m"}}, "Pano doc not found"},
		{"an unknown action is refused naming the actions", PanoParams{Action: "list", Payload: map[string]any{"id": docID}}, `unknown action "list": use save, read, comment, attach, delete or help`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakePano{}
			text, isErr := call(RunPano(context.Background(), f, "user-1", 100, tc.params), nil, nil)
			if !isErr || text != tc.want {
				t.Errorf("RunPano answered %q (error %v), want the error %q", text, isErr, tc.want)
			}
			if f.saw != nil {
				t.Errorf("the controller was asked %+v, want no call", f.saw)
			}
		})
	}
}

// Every action passes on a refusal as written, a missing doc as such, and
// any other failure with what was being attempted.
func TestRunPano_PassesOnTheControllersError(t *testing.T) {
	for _, action := range []struct {
		name, verb string
		payload    map[string]any
	}{
		{"save", "save", map[string]any{"id": docID, "title": "t", "markdown": "m"}},
		{"read", "read", map[string]any{"id": docID}},
		{"comment", "comment on", map[string]any{"id": docID, "comment": "c"}},
		{"attach", "attach to", map[string]any{"id": docID, "files": []any{}}},
		{"delete", "delete", map[string]any{"id": docID}},
	} {
		t.Run(action.name+" passes on a refusal, a missing doc and a failure", func(t *testing.T) {
			for _, e := range []struct {
				err  error
				want string
			}{
				{&entity.PanoError{Message: "a Pano doc needs a title"}, "a Pano doc needs a title"},
				{base.ErrNotFound, "Pano doc not found"},
				{errors.New("database unavailable"), "failed to " + action.verb + " the Pano doc: database unavailable"},
			} {
				text, isErr := call(RunPano(context.Background(), &fakePano{err: e.err}, "user-1", 100, PanoParams{Action: action.name, Payload: action.payload}), nil, nil)
				if !isErr || text != e.want {
					t.Errorf("for %v RunPano answered %q (error %v), want the error %q", e.err, text, isErr, e.want)
				}
			}
		})
	}
}

func TestHandlePano(t *testing.T) {
	t.Run("the tool runs as the workspace whose content it reads, and is counted", func(t *testing.T) {
		ps, tools := skillServer(t, nil)
		f := &fakePano{}
		ps.pano, ps.userID, ps.contentWorkspaceID = f, "user-1", 99
		if text, isErr := call(ps.handlePano(context.Background(), nil, PanoParams{Action: "delete", Payload: map[string]any{"id": docID}})); isErr {
			t.Fatalf("handlePano answered the error %q, want the doc deleted", text)
		}
		if want := (entity.DeletePanoDocRequest{UserID: "user-1", WorkspaceID: 99, ID: 7}); f.saw != want {
			t.Errorf("the controller was asked %+v, want %+v", f.saw, want)
		}
		if strings.Join(*tools, ",") != "pano" {
			t.Errorf("telemetry counted %v, want one pano call", *tools)
		}
	})

	t.Run("a server with no Pano says so", func(t *testing.T) {
		ps, _ := skillServer(t, nil)
		text, isErr := call(ps.handlePano(context.Background(), nil, PanoParams{Action: "read", Payload: map[string]any{"id": docID}}))
		if !isErr || text != "the Pano is not available on this server" {
			t.Errorf("handlePano answered %q (error %v), want the Pano reported unavailable", text, isErr)
		}
	})
}

// The colors the tool's schema offers are the colors the Pano keeps: one
// added to either and not the other is refused, or never offered.
func TestPanoParams_ColorEnumIsThePanosColors(t *testing.T) {
	f, _ := reflect.TypeOf(PanoSavePayload{}).FieldByName("Color")
	tag, ok := strings.CutPrefix(f.Tag.Get("jsonschema"), "enum:")
	if !ok {
		t.Fatalf("PanoSavePayload.Color's jsonschema tag is %q, want an enum", f.Tag.Get("jsonschema"))
	}
	var got []string
	for _, v := range strings.Split(tag, ",") {
		got = append(got, strings.TrimSpace(v))
	}
	if !reflect.DeepEqual(got, entity.PanoColors) {
		t.Errorf("PanoSavePayload.Color offers %v, want the Pano's colors %v", got, entity.PanoColors)
	}
}
