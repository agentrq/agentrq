// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
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

func (f *fake) SharedPanoDocOwner(_ context.Context, _, docID int64) (base.PanoDocOwner, error) {
	return f.PanoDocOwnerInTeam(context.Background(), 0, 0, docID)
}

func (f *fake) GetPanoDoc(_ context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	f.panoReq, f.panoFiles = req.UserID, req.IncludeAttachments
	if req.IncludeComments {
		return nil, errors.New("a teammate reads no comments")
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
	if f.panoReq != monoflake.ID(panoOwnerID).String() || !f.panoFiles {
		t.Errorf("read as %q, attachments %v: want the owner's, with them", f.panoReq, f.panoFiles)
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
	if _, err := c.GetSharedPanoDoc(ctx, 2, 501, true); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("GetSharedPanoDoc: %v", err)
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
		{"PanoDocOwnerInTeam", func(c Controller) error { _, err := c.GetSharedPanoDoc(ctx, 2, 500, true); return err }},
		{"GetPanoDoc", func(c Controller) error { _, err := c.GetSharedPanoDoc(ctx, 2, 500, true); return err }},
	} {
		f, c := newFake()
		f.fail = tc.fail
		if err := tc.call(c); !errors.Is(err, errBoom) {
			t.Errorf("with %s failing: err = %v, want it passed on", tc.fail, err)
		}
	}
}

// A doc shared with one of the user's teams is read as its owner, as asked
// for attachments, with no file links and nothing naming the owner's
// workspaces.
func TestGetSharedPanoDoc(t *testing.T) {
	for _, attachments := range []bool{true, false} {
		f, c := newFake()
		d, err := c.GetSharedPanoDoc(context.Background(), 2, 500, attachments)
		if err != nil {
			t.Fatal(err)
		}
		if f.panoReq != monoflake.ID(panoOwnerID).String() || f.panoFiles != attachments {
			t.Errorf("read as %q, attachments %v: want the owner's, attachments %v", f.panoReq, f.panoFiles, attachments)
		}
		if d.ID != 500 || d.Markdown != "![chart](chart.png)" {
			t.Errorf("doc = %+v", d)
		}
		if d.Tasks != nil || d.CreatedByWorkspaceID != 0 || d.UpdatedByWorkspaceID != 0 {
			t.Errorf("the owner's workspaces leak: tasks %v, by %d/%d", d.Tasks, d.CreatedByWorkspaceID, d.UpdatedByWorkspaceID)
		}
		for _, file := range append(d.Files, d.Attachments...) {
			if file.URL != "" || file.WorkspaceID != 0 {
				t.Errorf("file %s carries link %q, workspace %d", file.Filename, file.URL, file.WorkspaceID)
			}
		}
	}
}

// ownPano is an account's own Pano, which holds doc 7 only.
type ownPano struct {
	crud.PanoController
	err     error
	deleted int64
}

func (o *ownPano) GetPanoDoc(_ context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	if o.err != nil {
		return nil, o.err
	}
	if req.ID != 7 {
		return nil, base.ErrNotFound
	}
	return &entity.GetPanoDocResponse{Doc: entity.PanoDoc{ID: 7, Title: "own", CreatedByWorkspaceID: 9}}, nil
}

func (o *ownPano) DeletePanoDoc(_ context.Context, req entity.DeletePanoDocRequest) error {
	o.deleted = req.ID
	return base.ErrNotFound
}

// An agent reads its own doc as its account does, and a doc not its own
// only when shared with one of the account's teams; every write stays the
// account's own.
func TestAgentPano(t *testing.T) {
	ctx := context.Background()
	user := monoflake.ID(2).String()
	read := func(own *ownPano, f *fake, id int64) (*entity.GetPanoDocResponse, error) {
		return AgentPano(own, New(Params{Teams: f, Owner: f, Repository: f, PubSub: f})).
			GetPanoDoc(ctx, entity.GetPanoDocRequest{UserID: user, ID: id, IncludeComments: true, IncludeAttachments: true})
	}

	f, _ := newFake()
	f.fail = "PanoDocOwnerInTeam" // would surface if the shares were asked
	if rs, err := read(&ownPano{}, f, 7); err != nil || rs.Doc.Title != "own" || rs.Doc.CreatedByWorkspaceID != 9 {
		t.Errorf("own doc: %+v %v", rs, err)
	}
	if _, err := read(&ownPano{err: errBoom}, f, 500); !errors.Is(err, errBoom) {
		t.Errorf("a failed own read: %v, want it passed on, the shares never asked", err)
	}

	f, _ = newFake()
	rs, err := read(&ownPano{}, f, 500)
	if err != nil || rs.Doc.ID != 500 || rs.Doc.Files[0].URL != "" || rs.Doc.Comments != nil {
		t.Errorf("shared doc: %+v %v", rs, err)
	}
	if f.panoReq != monoflake.ID(panoOwnerID).String() || !f.panoFiles {
		t.Errorf("read as %q, attachments %v: want the owner's, with them", f.panoReq, f.panoFiles)
	}

	f, _ = newFake()
	if _, err := read(&ownPano{}, f, 501); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("a doc neither own nor shared: %v", err)
	}
	if f.panoReq != "" {
		t.Errorf("the owner's doc was read as %q", f.panoReq)
	}

	own := &ownPano{}
	if err := AgentPano(own, New(Params{Teams: f, Owner: f, Repository: f, PubSub: f})).DeletePanoDoc(ctx, entity.DeletePanoDocRequest{UserID: user, ID: 500}); !errors.Is(err, base.ErrNotFound) || own.deleted != 500 {
		t.Errorf("a delete went to %d with %v, want the account's own Pano, which refuses it", own.deleted, err)
	}
}
