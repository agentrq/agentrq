// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	tsBusy  int64 = 7101
	tsQuiet int64 = 7102
	tsOther int64 = 7103
)

// seedSummaryTask adds a task, and a message with the metadata given when
// there is one.
func seedSummaryTask(t *testing.T, db *gorm.DB, ws, id int64, status, assignee string, updated time.Time, metadata string) {
	t.Helper()
	task := model.Task{ID: id, CreatedAt: updated, UpdatedAt: updated, WorkspaceID: ws, UserID: wfUser, Status: status, Assignee: assignee, Title: "task " + status}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if metadata == "" {
		return
	}
	msg := model.Message{ID: id + 1000, CreatedAt: updated, TaskID: id, UserID: wfUser, Sender: "agent", Metadata: datatypes.JSON(metadata)}
	if err := db.Create(&msg).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

func TestSummarizeTasks(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			db := d.db
			repo := New(&mockDB{db: db})
			now := time.Now().UTC().Truncate(time.Second)
			const asking = `{"type":"permission_request","status":"pending"}`
			const answered = `{"type":"permission_request","status":"allow"}`
			seedSummaryTask(t, db, tsBusy, 1, "ongoing", "agent", now.Add(-time.Hour), "")
			seedSummaryTask(t, db, tsBusy, 2, "ongoing", "agent", now, asking)
			seedSummaryTask(t, db, tsBusy, 3, "ongoing", "agent", now.Add(-2*time.Hour), answered)
			seedSummaryTask(t, db, tsBusy, 4, "blocked", "agent", now, "")
			seedSummaryTask(t, db, tsBusy, 5, "notstarted", "human", now, "")
			seedSummaryTask(t, db, tsBusy, 6, "notstarted", "agent", now, "")
			seedSummaryTask(t, db, tsBusy, 7, "completed", "agent", now, asking)
			seedSummaryTask(t, db, tsBusy, 8, "rejected", "agent", now, "")
			seedSummaryTask(t, db, tsBusy, 9, "cron", "agent", now, "")
			seedSummaryTask(t, db, tsOther, 10, "ongoing", "agent", now.Add(time.Hour), asking)

			got, err := repo.SummarizeTasks(context.Background(), []int64{tsBusy, tsQuiet})
			if err != nil {
				t.Fatal(err)
			}
			want := entity.WorkspaceTaskSummary{Ongoing: 3, NeedsYou: 3, Done: 2, Total: 8, Current: &entity.TaskRef{ID: 2, Title: "task ongoing"}}
			busy := got[tsBusy]
			if busy.Current == nil || *busy.Current != *want.Current {
				t.Fatalf("the current task is %+v, want %+v, the ongoing one updated last", busy.Current, want.Current)
			}
			busy.Current, want.Current = nil, nil
			if busy != want {
				t.Errorf("the busy workspace sums up as %+v, want %+v", busy, want)
			}
			if _, ok := got[tsQuiet]; ok || len(got) != 1 {
				t.Errorf("the summaries are %v, want only the busy workspace", got)
			}

			got, err = repo.SummarizeTasks(context.Background(), nil)
			if err != nil || len(got) != 0 {
				t.Errorf("summing up no workspaces returned %v and error %v, want nothing and no error", got, err)
			}
			got, err = repo.SummarizeTasks(context.Background(), []int64{tsQuiet})
			if err != nil || len(got) != 0 {
				t.Errorf("a workspace with no tasks returned %v and error %v, want nothing and no error", got, err)
			}
		})
	}
}

func TestSummarizeTasks_EachStatementCanFail(t *testing.T) {
	for _, tc := range []struct{ kind, table string }{
		{"row", "tasks"},
		{"query", "tasks"},
		{"query", "messages"},
	} {
		t.Run(tc.kind+" "+tc.table, func(t *testing.T) {
			db := historyDBs(t)[0].db
			seedSummaryTask(t, db, tsBusy, 1, "ongoing", "agent", time.Now(), "")
			failOn(t, db, tc.kind, tc.table)
			if _, err := New(&mockDB{db: db}).SummarizeTasks(context.Background(), []int64{tsBusy}); !errors.Is(err, errInjected) {
				t.Fatalf("SummarizeTasks returned %v, want the injected failure", err)
			}
		})
	}
}
