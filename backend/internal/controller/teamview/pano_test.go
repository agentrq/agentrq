// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
)

// panoOwnerID owns the shared doc 500 the fakes answer; any other doc is
// not shared with the team.
const panoOwnerID int64 = 1

func (f *fake) ListTeamPanoDocs(context.Context, int64, time.Time) ([]base.TeamPanoDocRow, error) {
	return []base.TeamPanoDocRow{{
		PanoDoc:   model.PanoDoc{ID: 500, UserID: panoOwnerID, Title: "Plan", Excerpt: "steps", Color: 2, ChecklistDone: 1, ChecklistTotal: 3, Version: 4},
		OwnerName: "Alice", OwnerPicture: "a.png",
	}}, f.err("ListTeamPanoDocs")
}

func (f *fake) PanoDocOwnerInTeam(_ context.Context, _, _, docID int64) (base.PanoDocOwner, error) {
	if err := f.err("PanoDocOwnerInTeam"); err != nil {
		return base.PanoDocOwner{}, err
	}
	if docID != 500 {
		return base.PanoDocOwner{}, base.ErrNotFound
	}
	return base.PanoDocOwner{UserID: panoOwnerID, Name: "Alice", Picture: "a.png"}, nil
}

func (f *fake) GetPanoDoc(_ context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	f.panoReq = req.UserID
	if !req.IncludeAttachments || req.IncludeComments {
		return nil, errors.New("a teammate reads attachments and no comments")
	}
	return &entity.GetPanoDocResponse{Doc: entity.PanoDoc{
		ID: 500, Title: "Plan", Markdown: "![chart](chart.png)", CreatedByWorkspaceID: 7, UpdatedByWorkspaceID: 8,
		Files:       []entity.PanoFile{{ID: 600, WorkspaceID: 7, Filename: "chart.png", URL: "/storage/public"}},
		Attachments: []entity.PanoFile{{ID: 601, WorkspaceID: 7, Filename: "notes.txt", URL: "/storage/public2"}},
		Tasks:       []entity.PanoDocTask{{TaskID: 9, WorkspaceID: 7, Status: "ongoing"}},
	}}, f.err("GetPanoDoc")
}

func (f *fake) GetPanoFile(_ context.Context, req entity.GetPanoFileRequest) (*entity.GetAttachmentResponse, error) {
	f.panoReq = req.UserID
	return &entity.GetAttachmentResponse{Data: []byte("png"), Filename: "chart.png", MimeType: "image/png"}, f.err("GetPanoFile")
}

func TestListPanoDocs(t *testing.T) {
	_, c := newFake()
	docs, err := c.ListPanoDocs(context.Background(), 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %+v", docs)
	}
	d := docs[0]
	if d.ID != 500 || d.Title != "Plan" || d.Excerpt != "steps" || d.Color != entity.PanoColor(2) ||
		d.ChecklistDone != 1 || d.ChecklistTotal != 3 || d.Version != 4 {
		t.Errorf("doc = %+v", d.PanoDoc)
	}
	if d.OwnerID != panoOwnerID || d.OwnerName != "Alice" || d.OwnerPicture != "a.png" {
		t.Errorf("owner = %d %q %q", d.OwnerID, d.OwnerName, d.OwnerPicture)
	}
}

// A shared doc is read as its owner, with every file on the team's route
// and nothing that names the owner's workspaces.
func TestGetPanoDoc(t *testing.T) {
	f, c := newFake()
	d, err := c.GetPanoDoc(context.Background(), 2, 10, 500)
	if err != nil {
		t.Fatal(err)
	}
	if f.panoReq != monoflake.ID(panoOwnerID).String() {
		t.Errorf("read as %q, want the owner", f.panoReq)
	}
	if d.OwnerID != panoOwnerID || d.OwnerName != "Alice" || d.OwnerPicture != "a.png" || d.Markdown != "![chart](chart.png)" {
		t.Errorf("doc = %+v", d)
	}
	if d.Tasks != nil || d.CreatedByWorkspaceID != 0 || d.UpdatedByWorkspaceID != 0 {
		t.Errorf("the owner's workspaces leak: tasks %v, by %d/%d", d.Tasks, d.CreatedByWorkspaceID, d.UpdatedByWorkspaceID)
	}
	if got, want := d.Files[0].URL, TeamPanoFileURL(10, 500, 600); got != want {
		t.Errorf("file url = %q, want %q", got, want)
	}
	if got, want := d.Attachments[0].URL, TeamPanoFileURL(10, 500, 601); got != want {
		t.Errorf("attachment url = %q, want %q", got, want)
	}
	if d.Files[0].WorkspaceID != 0 || d.Attachments[0].WorkspaceID != 0 {
		t.Error("a file names the owner's workspace")
	}
	if want := "/api/v1/teams/" + monoflake.ID(10).String() + "/pano/" + monoflake.ID(500).String() + "/files/" + monoflake.ID(600).String(); d.Files[0].URL != want {
		t.Errorf("file url = %q, want %q", d.Files[0].URL, want)
	}
}

func TestGetPanoFile(t *testing.T) {
	f, c := newFake()
	res, err := c.GetPanoFile(context.Background(), 2, 10, 500, 600)
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Data) != "png" || f.panoReq != monoflake.ID(panoOwnerID).String() {
		t.Errorf("res = %+v, read as %q", res, f.panoReq)
	}
}

// A doc not shared with the team, or a caller not in it, is not found, and
// the owner's doc is never read.
func TestPanoNotShared(t *testing.T) {
	ctx := context.Background()
	f, c := newFake()
	f.fail = "GetPanoDoc" // would surface if read
	if _, err := c.GetPanoDoc(ctx, 2, 10, 501); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("GetPanoDoc: %v", err)
	}
	f.fail = "GetPanoFile"
	if _, err := c.GetPanoFile(ctx, 2, 10, 501, 600); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("GetPanoFile: %v", err)
	}
	if f.panoReq != "" {
		t.Errorf("the owner's doc was read as %q", f.panoReq)
	}
}

func TestPanoFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		fail string
		call func(c Controller) error
	}{
		{"GetTeam", func(c Controller) error { _, err := c.ListPanoDocs(ctx, 2, 10); return err }},
		{"ListTeamPanoDocs", func(c Controller) error { _, err := c.ListPanoDocs(ctx, 2, 10); return err }},
		{"PanoDocOwnerInTeam", func(c Controller) error { _, err := c.GetPanoDoc(ctx, 2, 10, 500); return err }},
		{"GetPanoDoc", func(c Controller) error { _, err := c.GetPanoDoc(ctx, 2, 10, 500); return err }},
		{"PanoDocOwnerInTeam", func(c Controller) error { _, err := c.GetPanoFile(ctx, 2, 10, 500, 600); return err }},
		{"GetPanoFile", func(c Controller) error { _, err := c.GetPanoFile(ctx, 2, 10, 500, 600); return err }},
	} {
		f, c := newFake()
		f.fail = tc.fail
		if err := tc.call(c); !errors.Is(err, errBoom) {
			t.Errorf("with %s failing: err = %v, want it passed on", tc.fail, err)
		}
	}
}
