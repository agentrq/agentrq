// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	rsUser  = int64(482467435371298817)
	rsOld   = int64(710000000000000001)
	rsNew   = int64(710000000000000099)
	rsFork  = int64(710000000000000002)
	rsOther = int64(710000000000000003)
)

// resetDB is a database with every table a reset touches, holding a
// workspace with one row in each of them, a fork, and another workspace.
func resetDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	tables := append([]any{&model.Workspace{}, &model.Message{}, &model.SkillShare{}, &model.PanoDoc{}}, workspaceIDTables...)
	if err := db.AutoMigrate(tables...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	past := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, w := range []model.Workspace{
		{ID: rsOld, UserID: rsUser, Name: "app", CreatedAt: past, UpdatedAt: past},
		{ID: rsFork, UserID: rsUser, Name: "app-fork", ForkOfID: rsOld, CreatedAt: past, UpdatedAt: past},
		{ID: rsOther, UserID: rsUser, Name: "other", CreatedAt: past, UpdatedAt: past},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	// One row naming the workspace in each table, the task row 1 among them.
	for i, m := range workspaceIDTables {
		row := reflect.New(reflect.TypeOf(m).Elem())
		row.Elem().FieldByName("WorkspaceID").SetInt(rsOld)
		if id := row.Elem().FieldByName("ID"); id.IsValid() && id.Kind() == reflect.Int64 {
			id.SetInt(int64(i + 1))
		}
		if err := db.Create(row.Interface()).Error; err != nil {
			t.Fatalf("seed %T: %v", m, err)
		}
	}
	rows := []any{
		&model.SkillShare{ID: 1, SkillID: 1, TargetWorkspaceID: rsOld},
		&model.PanoDoc{ID: 1, CreatedByWorkspaceID: rsOld, UpdatedByWorkspaceID: rsOld},
		&model.PanoDoc{ID: 2, CreatedByWorkspaceID: rsOther, UpdatedByWorkspaceID: rsOld},
	}
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("seed %T: %v", r, err)
		}
	}
	return db, New(&mockDB{db: db})
}

// attachments is an attachment list whose link names the file's key.
func attachments(workspaceID int64) datatypes.JSON {
	return datatypes.JSON(`[{"id":"a1","filename":"a.png","mimeType":"image/png","url":"https://files.example/` + storage.AttachmentKey(workspaceID, 7, "a1") + `"}]`)
}

func TestResetWorkspaceID_MovesEveryRowNamingTheWorkspace(t *testing.T) {
	db, repo := resetDB(t)
	past := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.Model(&model.Task{}).Where("id = ?", 1).UpdateColumns(map[string]any{"attachments": attachments(rsOld), "updated_at": past}).Error; err != nil {
		t.Fatal(err)
	}
	// A fork's files are kept under its parent; another workspace's are not.
	for _, task := range []model.Task{
		{ID: 500, WorkspaceID: rsFork, UserID: rsUser, Attachments: attachments(rsOld)},
		{ID: 600, WorkspaceID: rsOther, UserID: rsUser, Attachments: attachments(rsOther)},
		// Another workspace's task that this one delegated to it.
		{ID: 700, WorkspaceID: rsOther, UserID: rsUser, CreatedByWorkspaceID: rsOld},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, msg := range []model.Message{
		{ID: 1, TaskID: 1, Attachments: attachments(rsOld)},
		{ID: 2, TaskID: 500, Attachments: attachments(rsOld)},
		{ID: 3, TaskID: 600, Attachments: attachments(rsOther)},
	} {
		if err := db.Create(&msg).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.ResetWorkspaceID(context.Background(), rsOld, rsNew, rsUser); err != nil {
		t.Fatalf("reset: %v", err)
	}

	var w model.Workspace
	if err := db.First(&w, rsNew).Error; err != nil {
		t.Fatalf("the workspace is not under its new id: %v", err)
	}
	if w.Name != "app" || !w.UpdatedAt.Equal(past) {
		t.Errorf("the workspace under the new id is %q updated at %v, want app updated at %v", w.Name, w.UpdatedAt, past)
	}
	var n int64
	db.Model(&model.Workspace{}).Where("id = ?", rsOld).Count(&n)
	if n != 0 {
		t.Errorf("%d workspaces are still under the old id, want 0", n)
	}
	var fork model.Workspace
	db.First(&fork, rsFork)
	if fork.ForkOfID != rsNew {
		t.Errorf("the fork is a fork of %d, want the new id %d", fork.ForkOfID, rsNew)
	}
	for _, m := range workspaceIDTables {
		if c := countIn(t, db, m, rsNew); c != 1 {
			t.Errorf("%d rows of %T name the new id, want 1", c, m)
		}
		if c := countIn(t, db, m, rsOld); c != 0 {
			t.Errorf("%d rows of %T still name the old id, want 0", c, m)
		}
	}
	var share model.SkillShare
	db.First(&share, 1)
	if share.TargetWorkspaceID != rsNew {
		t.Errorf("the skill share targets %d, want the new id %d", share.TargetWorkspaceID, rsNew)
	}
	var docs []model.PanoDoc
	db.Order("id").Find(&docs)
	if docs[0].CreatedByWorkspaceID != rsNew || docs[0].UpdatedByWorkspaceID != rsNew || docs[1].CreatedByWorkspaceID != rsOther || docs[1].UpdatedByWorkspaceID != rsNew {
		t.Errorf("the Pano docs were written by %d/%d and %d/%d, want %d/%d and %d/%d",
			docs[0].CreatedByWorkspaceID, docs[0].UpdatedByWorkspaceID, docs[1].CreatedByWorkspaceID, docs[1].UpdatedByWorkspaceID,
			rsNew, rsNew, rsOther, rsNew)
	}

	var delegated model.Task
	db.First(&delegated, 700)
	if delegated.CreatedByWorkspaceID != rsNew {
		t.Errorf("the delegated task names %d as its delegator, want the new id %d", delegated.CreatedByWorkspaceID, rsNew)
	}

	var task model.Task
	db.First(&task, 1)
	if !task.UpdatedAt.Equal(past) {
		t.Errorf("the task was updated at %v, want it left at %v: a reset must not reorder tasks", task.UpdatedAt, past)
	}
	for id, want := range map[int64]datatypes.JSON{1: attachments(rsNew), 500: attachments(rsNew), 600: attachments(rsOther)} {
		var tk model.Task
		db.First(&tk, id)
		if string(tk.Attachments) != string(want) {
			t.Errorf("task %d's attachments are %s, want %s", id, tk.Attachments, want)
		}
	}
	for id, want := range map[int64]datatypes.JSON{1: attachments(rsNew), 2: attachments(rsNew), 3: attachments(rsOther)} {
		var msg model.Message
		db.First(&msg, id)
		if string(msg.Attachments) != string(want) {
			t.Errorf("message %d's attachments are %s, want %s", id, msg.Attachments, want)
		}
	}
}

func TestResetWorkspaceID_AnotherAccountsWorkspaceIsNotFound(t *testing.T) {
	db, repo := resetDB(t)
	err := repo.ResetWorkspaceID(context.Background(), rsOld, rsNew, rsUser+1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("a reset by another account returned %v, want ErrNotFound", err)
	}
	if c := countIn(t, db, &model.Task{}, rsOld); c != 1 {
		t.Errorf("%d tasks are under the old id, want the 1 left where it was", c)
	}
}

func TestResetWorkspaceID_AFailureAtAnyStepChangesNothing(t *testing.T) {
	errDB := errors.New("database unavailable")
	for step := 1; ; step++ {
		db, repo := resetDB(t)
		seen := 0
		if err := db.Callback().Update().Before("gorm:update").Register("fail_nth_update", func(tx *gorm.DB) {
			if seen++; seen == step {
				_ = tx.AddError(errDB)
			}
		}); err != nil {
			t.Fatal(err)
		}
		err := repo.ResetWorkspaceID(context.Background(), rsOld, rsNew, rsUser)
		if seen < step {
			if err != nil {
				t.Fatalf("a reset with no failure returned %v", err)
			}
			if step < 3 {
				t.Fatalf("the reset ran only %d updates", seen)
			}
			return
		}
		if !errors.Is(err, errDB) {
			t.Fatalf("a reset failing at update %d returned %v, want %v", step, err, errDB)
		}
		if c := countIn(t, db, &model.Memory{}, rsOld); c != 1 {
			t.Fatalf("after a failure at update %d, %d memories are under the old id, want 1: the transaction must roll back", step, c)
		}
		var n int64
		db.Model(&model.Workspace{}).Where("id = ?", rsOld).Count(&n)
		if n != 1 {
			t.Fatalf("after a failure at update %d the workspace is gone from its old id", step)
		}
	}
}

// Every column naming a workspace must move with it, or a reset leaves the
// row behind under an id nothing has any more.
func TestResetWorkspaceID_CoversEveryColumnNamingAWorkspace(t *testing.T) {
	_, imp := sourceTypes()
	pkg, err := imp.Import("github.com/agentrq/agentrq/backend/internal/data/model")
	if err != nil {
		t.Fatalf("load the model package: %v", err)
	}
	var found []string
	for _, name := range pkg.Scope().Names() {
		st, ok := pkg.Scope().Lookup(name).Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i).Name(); strings.HasSuffix(f, "WorkspaceID") || f == "ForkOfID" {
				found = append(found, name+"."+f)
			}
		}
	}
	covered := map[string]bool{
		"Workspace.ForkOfID":           true,
		"SkillShare.TargetWorkspaceID": true,
		"PanoDoc.CreatedByWorkspaceID": true,
		"PanoDoc.UpdatedByWorkspaceID": true,
		"Task.CreatedByWorkspaceID":    true,
	}
	for _, m := range workspaceIDTables {
		covered[reflect.TypeOf(m).Elem().Name()+".WorkspaceID"] = true
	}
	var missing []string
	for _, f := range found {
		if !covered[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("ResetWorkspaceID does not move %v; add the model to workspaceIDTables", missing)
	}
	if len(found) != len(covered) {
		t.Errorf("the model package has %d columns naming a workspace and the reset moves %d", len(found), len(covered))
	}
}
