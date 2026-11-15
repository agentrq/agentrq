// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

func agentNoteOf(t *testing.T, db *gorm.DB, taskID int64) int64 {
	t.Helper()
	var task model.Task
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("read task %d: %v", taskID, err)
	}
	return task.AgentNoteID
}

func countAgentNotes(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.AgentNote{}).Count(&n).Error; err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return n
}

// A note is filed once per text, and each task points at the one it was
// handed last.
func TestRecordAgentNote(t *testing.T) {
	db, repo := forkDB(t)
	ctx := context.Background()
	seedForkTask(t, db, wfParent, 10, "notstarted")
	seedForkTask(t, db, wfParent, 20, "notstarted")

	for _, id := range []int64{10, 20} {
		if err := repo.RecordAgentNote(ctx, wfUser, id, "Keep me posted."); err != nil {
			t.Fatalf("record for %d: %v", id, err)
		}
	}
	first := model.AgentNoteID(wfUser, "Keep me posted.")
	if agentNoteOf(t, db, 10) != first || agentNoteOf(t, db, 20) != first {
		t.Errorf("tasks point at %d and %d, want both at %d", agentNoteOf(t, db, 10), agentNoteOf(t, db, 20), first)
	}
	if n := countAgentNotes(t, db); n != 1 {
		t.Errorf("one text handed twice filed %d notes, want 1", n)
	}

	// The note edited, task 10 handed over again: it points at the new one.
	if err := repo.RecordAgentNote(ctx, wfUser, 10, "Be concise."); err != nil {
		t.Fatal(err)
	}
	if got, want := agentNoteOf(t, db, 10), model.AgentNoteID(wfUser, "Be concise."); got != want {
		t.Errorf("task 10 points at %d, want %d", got, want)
	}
	note, err := repo.GetAgentNote(ctx, agentNoteOf(t, db, 10))
	if err != nil || note.Text != "Be concise." || note.UserID != wfUser {
		t.Errorf("GetAgentNote = %+v, %v; want the edited note", note, err)
	}

	// Someone else's task is left alone.
	if err := repo.RecordAgentNote(ctx, wfUser+1, 20, "Not yours."); err != nil {
		t.Fatal(err)
	}
	if agentNoteOf(t, db, 20) != first {
		t.Error("another account's hand-off moved this account's task")
	}
}

func TestGetAgentNote_MissingIsNotFound(t *testing.T) {
	_, repo := forkDB(t)
	if _, err := repo.GetAgentNote(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgentNote(404) = %v, want ErrNotFound", err)
	}
}

func TestAgentNote_StatementsCanFail(t *testing.T) {
	failing := errors.New("injected")
	fail := func(table string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(failing)
			}
		}
	}
	ctx := context.Background()

	db, repo := forkDB(t)
	_ = db.Callback().Create().Before("gorm:create").Register("fail", fail("agent_notes"))
	if err := repo.RecordAgentNote(ctx, wfUser, 10, "n"); !errors.Is(err, failing) {
		t.Errorf("with the note insert failing, RecordAgentNote = %v", err)
	}

	db, repo = forkDB(t)
	_ = db.Callback().Update().Before("gorm:update").Register("fail", fail("tasks"))
	if err := repo.RecordAgentNote(ctx, wfUser, 10, "n"); !errors.Is(err, failing) {
		t.Errorf("with the task update failing, RecordAgentNote = %v", err)
	}
	if n := countAgentNotes(t, db); n != 0 {
		t.Errorf("a failed hand-off left %d notes, want the insert rolled back", n)
	}

	db, repo = forkDB(t)
	_ = db.Callback().Query().Before("gorm:query").Register("fail", fail("agent_notes"))
	if _, err := repo.GetAgentNote(ctx, 1); !errors.Is(err, failing) {
		t.Errorf("with the read failing, GetAgentNote = %v", err)
	}
}

// Deleting a workspace deletes the notes only its tasks pointed at, and keeps
// the ones another workspace's task still does.
func TestDeleteWorkspace_DeletesItsUnusedAgentNotes(t *testing.T) {
	db, repo := forkDB(t)
	ctx := context.Background()
	seedForkTask(t, db, wfParent, 10, "completed")
	seedForkTask(t, db, wfParent, 20, "completed")
	seedForkTask(t, db, wfFork, 30, "completed")
	seedForkTask(t, db, wfFork, 40, "completed") // never handed over
	for id, note := range map[int64]string{10: "Shared.", 20: "Only the parent's.", 30: "Shared."} {
		if err := repo.RecordAgentNote(ctx, wfUser, id, note); err != nil {
			t.Fatal(err)
		}
	}
	// A row from before the column existed reads NULL, which must not stop
	// the clean-up from matching.
	if err := db.Exec("UPDATE tasks SET agent_note_id = NULL WHERE id = 40").Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteWorkspace(ctx, wfParent, wfUser); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	var left []model.AgentNote
	if err := db.Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Text != "Shared." {
		t.Errorf("notes left %+v, want only the one the fork's task points at", left)
	}
}
