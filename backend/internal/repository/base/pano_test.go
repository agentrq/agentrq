// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

var panoNow = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)

func panoDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.PanoDoc{}, &model.PanoFile{}, &model.PanoComment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

// seedPano stores doc 1 of skUser with a linked file and a comment, living
// until a day after panoNow, and doc 2, which expired an hour before it.
func seedPano(t *testing.T, r Repository) {
	t.Helper()
	ctx := context.Background()
	live := model.PanoDoc{ID: 1, UserID: skUser, Title: "live", Markdown: "live markdown", CreatedAt: panoNow, UpdatedAt: panoNow, TTLDays: 1, ExpiresAt: panoNow.Add(24 * time.Hour)}
	if err := r.SavePanoDoc(ctx, live, true, []model.PanoFile{{ID: 11, DocID: 1, Linked: true, Filename: "a.png", StorageID: "file-11", CreatedAt: panoNow}}, nil); err != nil {
		t.Fatalf("seed live doc: %v", err)
	}
	if err := r.AddPanoComment(ctx, skUser, model.PanoComment{ID: 21, DocID: 1, Body: "first", CreatedAt: panoNow}); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	expired := model.PanoDoc{ID: 2, UserID: skUser, Title: "expired", Markdown: "expired", CreatedAt: panoNow, UpdatedAt: panoNow.Add(-2 * time.Hour), ExpiresAt: panoNow.Add(-time.Hour)}
	if err := r.SavePanoDoc(ctx, expired, true, nil, nil); err != nil {
		t.Fatalf("seed expired doc: %v", err)
	}
}

func TestPanoRepository_ReadsOnlyTheAccountsLiveDocs(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)

	docs, err := r.ListPanoDocs(ctx, skUser, panoNow)
	if err != nil || len(docs) != 1 || docs[0].ID != 1 || docs[0].Markdown != "" {
		t.Fatalf("ListPanoDocs returned %+v, %v, want only the live doc, without its markdown", docs, err)
	}
	if docs, err := r.ListPanoDocs(ctx, skOther, panoNow); err != nil || len(docs) != 0 {
		t.Errorf("ListPanoDocs for another account returned %+v, %v, want none", docs, err)
	}
	if d, err := r.GetPanoDoc(ctx, skUser, 1, panoNow); err != nil || d.Title != "live" || d.Markdown != "live markdown" {
		t.Errorf("GetPanoDoc returned %+v, %v, want the live doc with its markdown", d, err)
	}
	for _, tc := range []struct {
		name     string
		user, id int64
	}{
		{"an expired doc is not found", skUser, 2},
		{"another account's doc is not found", skOther, 1},
		{"a doc that never was is not found", skUser, 9},
	} {
		if _, err := r.GetPanoDoc(ctx, tc.user, tc.id, panoNow); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: GetPanoDoc returned %v, want ErrNotFound", tc.name, err)
		}
	}
	// A doc is gone the moment it expires.
	if _, err := r.GetPanoDoc(ctx, skUser, 1, panoNow.Add(24*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPanoDoc at its expiry returned %v, want ErrNotFound", err)
	}
}

// The person's docs lead the board, newest first; the agents' follow, newest first.
func TestPanoRepository_ListsThePersonsDocsFirst(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	for _, d := range []struct {
		id, ws int64
		age    time.Duration
	}{{1, 0, 3 * time.Hour}, {2, 5, time.Hour}, {3, 0, 2 * time.Hour}, {4, 6, 0}} {
		doc := model.PanoDoc{ID: d.id, UserID: skUser, CreatedByWorkspaceID: d.ws, Markdown: "m", CreatedAt: panoNow, UpdatedAt: panoNow.Add(-d.age), ExpiresAt: panoNow.Add(time.Hour)}
		if err := r.SavePanoDoc(ctx, doc, true, nil, nil); err != nil {
			t.Fatalf("seed doc %d: %v", d.id, err)
		}
	}
	docs, err := r.ListPanoDocs(ctx, skUser, panoNow)
	var ids []int64
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	if err != nil || !reflect.DeepEqual(ids, []int64{3, 1, 4, 2}) {
		t.Errorf("ListPanoDocs returned %v, %v, want the person's 3, 1 then the agents' 4, 2", ids, err)
	}
}

func TestPanoRepository_UpdateReplacesFilesInOneGo(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)

	later := panoNow.Add(time.Hour)
	d := model.PanoDoc{ID: 1, Version: 1, UserID: skUser, Title: "renamed", Excerpt: "e", Markdown: "renamed markdown", SizeBytes: 9, UpdatedAt: later, UpdatedByWorkspaceID: skWS, TTLDays: 7, ExpiresAt: later.AddDate(0, 0, 7)}
	add := []model.PanoFile{{ID: 12, DocID: 1, Linked: true, Filename: "b.png", StorageID: "file-12", CreatedAt: later}}
	if err := r.SavePanoDoc(ctx, d, false, add, []int64{11}); err != nil {
		t.Fatalf("SavePanoDoc returned %v, want the doc updated", err)
	}
	got, err := r.GetPanoDoc(ctx, skUser, 1, later)
	if err != nil || got.Title != "renamed" || got.Markdown != "renamed markdown" || got.TTLDays != 7 || !got.ExpiresAt.Equal(d.ExpiresAt) || got.UpdatedByWorkspaceID != skWS || got.SizeBytes != 9 {
		t.Fatalf("after the update GetPanoDoc returned %+v, %v, want the new title, markdown, size, author and TTL", got, err)
	}
	files, err := r.ListPanoFiles(ctx, 1)
	if err != nil || len(files) != 1 || files[0].ID != 12 {
		t.Errorf("ListPanoFiles returned %+v, %v, want only the added file", files, err)
	}

	// An update of a doc that is gone, or expired, is not found.
	for _, gone := range []model.PanoDoc{
		{ID: 9, UserID: skUser, UpdatedAt: later},
		{ID: 2, UserID: skUser, UpdatedAt: later},
		{ID: 1, UserID: skOther, UpdatedAt: later},
	} {
		if err := r.SavePanoDoc(ctx, gone, false, nil, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("updating doc %d of user %d returned %v, want ErrNotFound", gone.ID, gone.UserID, err)
		}
	}
}

func TestPanoRepository_FilesAndCommentsNeedALiveDoc(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)

	if err := r.AddPanoFiles(ctx, skUser, 1, []model.PanoFile{{ID: 13, DocID: 1, Filename: "c.txt", CreatedAt: panoNow.Add(time.Second)}}); err != nil {
		t.Fatalf("AddPanoFiles returned %v, want the file added", err)
	}
	if err := r.AddPanoComment(ctx, skUser, model.PanoComment{ID: 22, DocID: 1, Body: "second", CreatedAt: panoNow.Add(time.Second)}); err != nil {
		t.Fatalf("AddPanoComment returned %v, want the comment added", err)
	}
	files, err := r.ListPanoFiles(ctx, 1)
	if err != nil || len(files) != 2 || files[0].ID != 11 || files[1].ID != 13 {
		t.Errorf("ListPanoFiles returned %+v, %v, want the linked file then the attachment", files, err)
	}
	comments, err := r.ListPanoComments(ctx, 1)
	if err != nil || len(comments) != 2 || comments[0].Body != "first" || comments[1].Body != "second" {
		t.Errorf("ListPanoComments returned %+v, %v, want both comments oldest first", comments, err)
	}

	for _, tc := range []struct {
		name     string
		user, id int64
	}{
		{"an expired doc", skUser, 2},
		{"another account's doc", skOther, 1},
	} {
		if err := r.AddPanoFiles(ctx, tc.user, tc.id, []model.PanoFile{{ID: 14, DocID: tc.id, CreatedAt: panoNow}}); !errors.Is(err, ErrNotFound) {
			t.Errorf("AddPanoFiles to %s returned %v, want ErrNotFound", tc.name, err)
		}
		if err := r.AddPanoComment(ctx, tc.user, model.PanoComment{ID: 23, DocID: tc.id, CreatedAt: panoNow}); !errors.Is(err, ErrNotFound) {
			t.Errorf("AddPanoComment to %s returned %v, want ErrNotFound", tc.name, err)
		}
	}
}

func TestPanoRepository_DeleteReturnsEveryFileToPurge(t *testing.T) {
	ctx := context.Background()
	db, r := panoDB(t)
	seedPano(t, r)

	if _, err := r.DeletePanoDoc(ctx, skOther, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePanoDoc of another account's doc returned %v, want ErrNotFound", err)
	}
	ids, err := r.DeletePanoDoc(ctx, skUser, 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"file-11"}) {
		t.Fatalf("DeletePanoDoc returned %v, %v, want its file's blob", ids, err)
	}
	for _, m := range []any{&model.PanoDoc{}, &model.PanoFile{}, &model.PanoComment{}} {
		var n int64
		db.Model(m).Where("id IN ?", []int64{1, 11, 21}).Count(&n)
		if n != 0 {
			t.Errorf("%T rows of the deleted doc are left: %d, want none", m, n)
		}
	}
	// An expired doc is still deleted: the sweep deletes it this way.
	if ids, err := r.DeletePanoDoc(ctx, skUser, 2); err != nil || len(ids) != 0 {
		t.Errorf("DeletePanoDoc of the expired doc returned %v, %v, want it deleted, with no file to purge", ids, err)
	}
	var left int64
	db.Model(&model.PanoDoc{}).Where("id = 2").Count(&left)
	if left != 0 {
		t.Error("the expired doc is still there")
	}
}

func TestPanoRepository_ListExpiredPanoDocs(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)
	older := model.PanoDoc{ID: 3, UserID: skOther, Markdown: "older", UpdatedAt: panoNow, ExpiresAt: panoNow.Add(-2 * time.Hour)}
	if err := r.SavePanoDoc(ctx, older, true, nil, nil); err != nil {
		t.Fatal(err)
	}

	docs, err := r.ListExpiredPanoDocs(ctx, panoNow, 10)
	if err != nil || len(docs) != 2 || docs[0].ID != 3 || docs[1].ID != 2 {
		t.Fatalf("ListExpiredPanoDocs returned %+v, %v, want docs 3 then 2 of every account, the first to expire first", docs, err)
	}
	if docs, err := r.ListExpiredPanoDocs(ctx, panoNow, 1); err != nil || len(docs) != 1 {
		t.Errorf("ListExpiredPanoDocs with a limit of 1 returned %+v, %v, want one doc", docs, err)
	}
}

// Every statement a Pano write runs can fail, and each failure reaches the
// caller rather than being swallowed.
func TestPanoRepository_EveryStatementFailure(t *testing.T) {
	ctx := context.Background()
	later := panoNow.Add(time.Hour)
	for _, tc := range []struct {
		name string
		call func(r Repository) error
	}{
		{"list", func(r Repository) error { _, err := r.ListPanoDocs(ctx, skUser, panoNow); return err }},
		{"get", func(r Repository) error { _, err := r.GetPanoDoc(ctx, skUser, 1, panoNow); return err }},
		{"create", func(r Repository) error {
			return r.SavePanoDoc(ctx, model.PanoDoc{ID: 5, UserID: skUser, ExpiresAt: later}, true, []model.PanoFile{{ID: 51, DocID: 5}}, nil)
		}},
		{"update", func(r Repository) error {
			return r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: 1, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, []model.PanoFile{{ID: 12, DocID: 1}}, []int64{11})
		}},
		{"list files", func(r Repository) error { _, err := r.ListPanoFiles(ctx, 1); return err }},
		{"attach", func(r Repository) error {
			return r.AddPanoFiles(ctx, skUser, 1, []model.PanoFile{{ID: 13, DocID: 1, CreatedAt: panoNow}})
		}},
		{"list comments", func(r Repository) error { _, err := r.ListPanoComments(ctx, 1); return err }},
		{"comment", func(r Repository) error {
			return r.AddPanoComment(ctx, skUser, model.PanoComment{ID: 22, DocID: 1, CreatedAt: panoNow})
		}},
		{"delete", func(r Repository) error { _, err := r.DeletePanoDoc(ctx, skUser, 1); return err }},
		{"list expired", func(r Repository) error { _, err := r.ListExpiredPanoDocs(ctx, panoNow, 10); return err }},
	} {
		t.Run(tc.name+" passes on a failure of any of its statements", func(t *testing.T) {
			for n := 1; ; n++ {
				db, r := panoDB(t)
				seedPano(t, r)
				ran := failNth(db, n)
				err := tc.call(r)
				if *ran < n {
					if err != nil {
						t.Fatalf("with no failure injected the call returned %v, want none", err)
					}
					if n == 1 {
						t.Fatal("the call ran no statements")
					}
					return
				}
				if !errors.Is(err, errInjected) {
					t.Fatalf("failing statement %d returned %v, want the injected failure", n, err)
				}
			}
		})
	}
}

// Adding no files touches nothing, not even to look for the doc.
func TestPanoRepository_AddNoFiles(t *testing.T) {
	_, r := panoDB(t)
	if err := r.AddPanoFiles(context.Background(), skUser, 9, nil); err != nil {
		t.Errorf("AddPanoFiles with no files returned %v, want nil", err)
	}
}

// The docs linking a task are the account's live ones whose markdown holds
// its path, and a sync writes their checklist without saving them.
func TestPanoRepository_SyncsTheDocsLinkingATask(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	const path = "/workspaces/a/tasks/b"
	for _, d := range []model.PanoDoc{
		{ID: 1, UserID: skUser, Title: "linking", Markdown: "- [ ] x → [@w](" + path + ")", UpdatedAt: panoNow, ExpiresAt: panoNow.Add(time.Hour)},
		{ID: 2, UserID: skUser, Title: "expired", Markdown: "- [ ] x → [@w](" + path + ")", UpdatedAt: panoNow, ExpiresAt: panoNow.Add(-time.Hour)},
		{ID: 3, UserID: skUser + 1, Title: "theirs", Markdown: "- [ ] x → [@w](" + path + ")", UpdatedAt: panoNow, ExpiresAt: panoNow.Add(time.Hour)},
		{ID: 4, UserID: skUser, Title: "unlinked", Markdown: "- [ ] x", UpdatedAt: panoNow, ExpiresAt: panoNow.Add(time.Hour)},
	} {
		if err := r.SavePanoDoc(ctx, d, true, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := r.ListPanoDocsLinking(ctx, skUser, path, panoNow)
	if err != nil || len(docs) != 1 || docs[0].ID != 1 || docs[0].Markdown == "" {
		t.Fatalf("ListPanoDocsLinking = %+v, %v, want doc 1 with its markdown", docs, err)
	}
	d := docs[0]
	d.Markdown, d.Excerpt, d.SizeBytes, d.ChecklistDone, d.ChecklistTotal = "- [x] done", "- [x] done", 10, 1, 1
	d.UpdatedAt, d.Title = panoNow.Add(time.Minute), "ignored"
	if err := r.SyncPanoChecklist(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetPanoDoc(ctx, skUser, 1, panoNow)
	if err != nil || got.Markdown != "- [x] done" || got.ChecklistDone != 1 || got.SizeBytes != 10 || got.Title != "linking" || !got.UpdatedAt.Equal(panoNow) {
		t.Errorf("after the sync the doc is %+v, %v, want the checklist written and the rest kept", got, err)
	}
}

// A write is made only on the version its writer read, and moves it on; the
// checklist sync moves it too, as it changes the markdown.
func TestPanoRepository_VersionGuardsAnUpdate(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)
	later := panoNow.Add(time.Hour)
	update := func(version int64) error {
		return r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: version, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, nil, nil)
	}
	if d, err := r.GetPanoDoc(ctx, skUser, 1, panoNow); err != nil || d.Version != 1 {
		t.Fatalf("a seeded doc is at version %d, %v, want 1", d.Version, err)
	}
	if err := update(1); err != nil {
		t.Fatalf("an update on version 1 returned %v, want it written", err)
	}
	if err := update(1); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("a second update on version 1 returned %v, want ErrVersionConflict", err)
	}
	if err := r.SavePanoDoc(ctx, model.PanoDoc{ID: 99, Version: 1, UserID: skUser, UpdatedAt: later}, false, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("an update of a doc that is gone returned %v, want ErrNotFound", err)
	}
	if err := r.SyncPanoChecklist(ctx, model.PanoDoc{ID: 1, UserID: skUser, Markdown: "synced"}); err != nil {
		t.Fatal(err)
	}
	if d, err := r.GetPanoDoc(ctx, skUser, 1, later); err != nil || d.Version != 3 || d.Markdown != "synced" {
		t.Errorf("after an update and a sync the doc is %q at %d, %v, want version 3", d.Markdown, d.Version, err)
	}
}
