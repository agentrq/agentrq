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
	if err := db.AutoMigrate(&model.PanoDoc{}, &model.PanoFile{}, &model.PanoComment{}, &model.PanoDocTask{}, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}); err != nil {
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
	if err := r.SavePanoDoc(ctx, live, true, []model.PanoFile{{ID: 11, DocID: 1, Linked: true, Filename: "a.png", StorageID: "file-11", CreatedAt: panoNow}}, nil, nil); err != nil {
		t.Fatalf("seed live doc: %v", err)
	}
	if err := r.AddPanoComment(ctx, skUser, model.PanoComment{ID: 21, DocID: 1, Body: "first", CreatedAt: panoNow}); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	expired := model.PanoDoc{ID: 2, UserID: skUser, Title: "expired", Markdown: "expired", CreatedAt: panoNow, UpdatedAt: panoNow.Add(-2 * time.Hour), ExpiresAt: panoNow.Add(-time.Hour)}
	if err := r.SavePanoDoc(ctx, expired, true, nil, nil, nil); err != nil {
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
		if err := r.SavePanoDoc(ctx, doc, true, nil, nil, nil); err != nil {
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
	if err := r.SavePanoDoc(ctx, d, false, add, []int64{11}, nil); err != nil {
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
		if err := r.SavePanoDoc(ctx, gone, false, nil, nil, nil); !errors.Is(err, ErrNotFound) {
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
	if err := r.SavePanoDoc(ctx, older, true, nil, nil, nil); err != nil {
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
			return r.SavePanoDoc(ctx, model.PanoDoc{ID: 5, UserID: skUser, ExpiresAt: later}, true, []model.PanoFile{{ID: 51, DocID: 5}}, nil, nil)
		}},
		{"update", func(r Repository) error {
			return r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: 1, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, []model.PanoFile{{ID: 12, DocID: 1}}, []int64{11}, nil)
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
		{"update with links", func(r Repository) error {
			return r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: 1, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, nil, nil, []model.PanoDocTask{{ID: 71, DocID: 1, TaskID: 81}})
		}},
		{"link", func(r Repository) error {
			_, err := r.LinkPanoDocTasks(ctx, []model.PanoDocTask{{ID: 71, DocID: 1, TaskID: 81}})
			return err
		}},
		{"list doc tasks", func(r Repository) error { _, err := r.ListPanoDocTasks(ctx, skUser, []int64{1}); return err }},
		{"list task statuses", func(r Repository) error { _, err := r.ListPanoTaskStatuses(ctx, skUser, []int64{81}); return err }},
		{"list docs of a task", func(r Repository) error { _, err := r.ListPanoDocIDsOfTask(ctx, skUser, 81); return err }},
		{"list ongoing tasks", func(r Repository) error { _, err := r.ListOngoingAgentTaskIDs(ctx, skUser, 7, 2); return err }},
		{"set the count", func(r Repository) error { return r.SetPanoChecklistDone(ctx, skUser, 1, 1) }},
		{"list docs naming tasks", func(r Repository) error { _, err := r.ListPanoDocsNamingTasks(ctx, 0, 10); return err }},
		{"create a task for a doc", func(r Repository) error {
			_, err := r.CreateTaskLinkingPanoDoc(ctx, model.Task{ID: 81, UserID: skUser, WorkspaceID: 7, Status: "notstarted"}, model.PanoDocTask{ID: 71, UserID: skUser, DocID: 1, TaskID: 81}, panoNow)
			return err
		}},
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

// A doc's links name each task once, read back with the task's status while
// the task exists, and a task's docs are found by the task, within one account.
func TestPanoRepository_DocTaskLinks(t *testing.T) {
	ctx := context.Background()
	db, r := panoDB(t)
	seedPano(t, r)
	for _, task := range []model.Task{
		{ID: 31, UserID: skUser, WorkspaceID: 7, Status: "ongoing", Assignee: "agent"},
		{ID: 32, UserID: skUser, WorkspaceID: 7, Status: "completed", Assignee: "agent"},
		{ID: 33, UserID: skUser, WorkspaceID: 7, Status: "ongoing", Assignee: "human"},
		{ID: 34, UserID: skUser, WorkspaceID: 8, Status: "ongoing", Assignee: "agent"},
		{ID: 35, UserID: skUser + 1, WorkspaceID: 7, Status: "ongoing", Assignee: "agent"},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	link := func(id, doc, task int64, at time.Time) model.PanoDocTask {
		return model.PanoDocTask{ID: id, CreatedAt: at, UserID: skUser, DocID: doc, TaskID: task, WorkspaceID: 7}
	}
	later := panoNow.Add(time.Hour)
	if err := r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: 1, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, nil, nil, []model.PanoDocTask{link(41, 1, 32, later)}); err != nil {
		t.Fatal(err)
	}
	added, err := r.LinkPanoDocTasks(ctx, []model.PanoDocTask{link(42, 1, 31, later.Add(time.Minute)), link(43, 1, 32, later), link(44, 2, 31, later)})
	if err != nil || added != 2 {
		t.Fatalf("LinkPanoDocTasks = %d, %v, want the two new links added and the repeated one skipped", added, err)
	}
	if n, err := r.LinkPanoDocTasks(ctx, nil); err != nil || n != 0 {
		t.Errorf("linking nothing = %d, %v", n, err)
	}
	// A link of another account's, on the same doc id, is never read as this
	// account's, though its task is that account's own.
	if err := db.Create(&model.PanoDocTask{ID: 45, CreatedAt: panoNow, UserID: skUser + 1, DocID: 1, TaskID: 35, WorkspaceID: 7}).Error; err != nil {
		t.Fatal(err)
	}
	if ids, err := r.ListPanoDocIDsOfTask(ctx, skUser, 35); err != nil || len(ids) != 0 {
		t.Errorf("ListPanoDocIDsOfTask of another account's task = %v, %v, want none", ids, err)
	}
	got, err := r.ListPanoDocTasks(ctx, skUser, []int64{2, 1})
	want := []PanoTaskStatus{{DocID: 1, TaskID: 32, WorkspaceID: 7, Status: "completed"}, {DocID: 1, TaskID: 31, WorkspaceID: 7, Status: "ongoing"}, {DocID: 2, TaskID: 31, WorkspaceID: 7, Status: "ongoing"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ListPanoDocTasks = %+v, %v, want %+v, by doc, the oldest link first", got, err, want)
	}
	if got, err := r.ListPanoDocTasks(ctx, skUser, nil); err != nil || len(got) != 0 {
		t.Errorf("ListPanoDocTasks of no doc = %+v, %v", got, err)
	}
	if ids, err := r.ListPanoDocIDsOfTask(ctx, skUser, 31); err != nil || !reflect.DeepEqual(ids, []int64{1, 2}) {
		t.Errorf("ListPanoDocIDsOfTask = %v, %v, want docs 1 and 2", ids, err)
	}
	statuses, err := r.ListPanoTaskStatuses(ctx, skUser, []int64{31, 35, 99})
	if err != nil || len(statuses) != 1 || statuses[0] != (PanoTaskStatus{TaskID: 31, WorkspaceID: 7, Status: "ongoing"}) {
		t.Errorf("ListPanoTaskStatuses = %+v, %v, want only the account's task 31", statuses, err)
	}
	if statuses, err := r.ListPanoTaskStatuses(ctx, skUser, nil); err != nil || len(statuses) != 0 {
		t.Errorf("ListPanoTaskStatuses of nothing = %+v, %v", statuses, err)
	}
	if ids, err := r.ListOngoingAgentTaskIDs(ctx, skUser, 7, 2); err != nil || !reflect.DeepEqual(ids, []int64{31}) {
		t.Errorf("ListOngoingAgentTaskIDs = %v, %v, want the workspace's one ongoing agent task", ids, err)
	}

	// A deleted task drops out of the doc's tasks, and its links go with it.
	if err := r.DeleteTask(ctx, 7, 32, skUser); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListPanoDocTasks(ctx, skUser, []int64{1}); len(got) != 1 || got[0].TaskID != 31 {
		t.Errorf("after deleting task 32 the doc's tasks are %+v, want only 31", got)
	}
	var left int64
	db.Model(&model.PanoDocTask{}).Where("task_id = ?", 32).Count(&left)
	if left != 0 {
		t.Errorf("%d links to the deleted task are left, want none", left)
	}
}

// The count of ticked items is written alone: the markdown, version and
// updated_at stay.
func TestPanoRepository_SetChecklistDone(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)
	if err := r.SetPanoChecklistDone(ctx, skUser, 1, 3); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetPanoDoc(ctx, skUser, 1, panoNow)
	if err != nil || got.ChecklistDone != 3 || got.Version != 1 || got.Markdown != "live markdown" || !got.UpdatedAt.Equal(panoNow) {
		t.Errorf("after setting the count the doc is %+v, %v, want 3 done and the rest kept", got, err)
	}
}

// The backfill reads docs of any account that name a task, by id, a page at
// a time.
func TestPanoRepository_ListPanoDocsNamingTasks(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	for _, d := range []model.PanoDoc{
		{ID: 1, UserID: skUser, Markdown: "- [ ] x → [@w](/workspaces/a/tasks/b)", ExpiresAt: panoNow},
		{ID: 2, UserID: skUser, Markdown: "nothing", ExpiresAt: panoNow},
		{ID: 3, UserID: skUser + 1, Markdown: "see /workspaces/a/tasks/c", ExpiresAt: panoNow},
		{ID: 4, UserID: skUser, Markdown: "- [ ] /tasks/d", ExpiresAt: panoNow},
	} {
		if err := r.SavePanoDoc(ctx, d, true, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.ListPanoDocsNamingTasks(ctx, 0, 2)
	if err != nil || len(page) != 2 || page[0].ID != 1 || page[1].ID != 3 || page[0].Markdown == "" {
		t.Fatalf("the first page = %+v, %v, want docs 1 and 3 with their markdown", page, err)
	}
	if page, err := r.ListPanoDocsNamingTasks(ctx, 3, 2); err != nil || len(page) != 1 || page[0].ID != 4 {
		t.Errorf("the next page = %+v, %v, want doc 4", page, err)
	}
}

// A task made for a doc is created with its link, in one transaction: for a
// doc that is gone, neither is written.
func TestPanoRepository_CreateTaskLinkingPanoDoc(t *testing.T) {
	ctx := context.Background()
	db, r := panoDB(t)
	seedPano(t, r)
	task := model.Task{ID: 51, UserID: skUser, WorkspaceID: 7, Status: "notstarted", CreatedAt: panoNow}
	link := model.PanoDocTask{ID: 61, UserID: skUser, DocID: 1, TaskID: 51, WorkspaceID: 7}
	created, err := r.CreateTaskLinkingPanoDoc(ctx, task, link, panoNow)
	if err != nil || created.ID != 51 {
		t.Fatalf("CreateTaskLinkingPanoDoc = %+v, %v", created, err)
	}
	if got, _ := r.ListPanoDocTasks(ctx, skUser, []int64{1}); len(got) != 1 || got[0].TaskID != 51 {
		t.Errorf("the doc's tasks are %+v, want the new task", got)
	}
	for _, docID := range []int64{2, 99} {
		task.ID, link.ID, link.DocID, link.TaskID = 52, 62, docID, 52
		if _, err := r.CreateTaskLinkingPanoDoc(ctx, task, link, panoNow); !errors.Is(err, ErrNotFound) {
			t.Errorf("for doc %d CreateTaskLinkingPanoDoc returned %v, want ErrNotFound", docID, err)
		}
	}
	var n int64
	db.Model(&model.Task{}).Where("id = ?", 52).Count(&n)
	if n != 0 {
		t.Error("a task was created for a doc that is gone")
	}
}

// A write is made only on the version its writer read, and moves it on.
func TestPanoRepository_VersionGuardsAnUpdate(t *testing.T) {
	ctx := context.Background()
	_, r := panoDB(t)
	seedPano(t, r)
	later := panoNow.Add(time.Hour)
	update := func(version int64) error {
		return r.SavePanoDoc(ctx, model.PanoDoc{ID: 1, Version: version, UserID: skUser, UpdatedAt: later, ExpiresAt: later.Add(time.Hour)}, false, nil, nil, nil)
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
	if err := r.SavePanoDoc(ctx, model.PanoDoc{ID: 99, Version: 1, UserID: skUser, UpdatedAt: later}, false, nil, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("an update of a doc that is gone returned %v, want ErrNotFound", err)
	}
	if d, err := r.GetPanoDoc(ctx, skUser, 1, later); err != nil || d.Version != 2 {
		t.Errorf("after an update the doc is at %d, %v, want version 2", d.Version, err)
	}
}
