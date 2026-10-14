// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// Every read of a doc's task links runs on a status change or a page load, so
// none may scan a whole table: each is answered from an index. The query plan
// of each statement the repository sends is checked, not the statement as
// written here.
func TestPanoDocTaskQueriesUseAnIndex(t *testing.T) {
	ctx := context.Background()
	db, r := panoDB(t)
	type statement struct {
		sql  string
		vars []any
	}
	var sent []statement
	capture := func(tx *gorm.DB) {
		sent = append(sent, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:plan", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:plan", capture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"a doc's tasks, with their status", func() error { _, err := r.ListPanoDocTasks(ctx, []int64{1, 2}); return err }},
		{"the docs of a task", func() error { _, err := r.ListPanoDocIDsOfTask(ctx, 31); return err }},
		{"the statuses of an item's tasks", func() error { _, err := r.ListPanoTaskStatuses(ctx, skUser, []int64{31, 32}); return err }},
		{"a workspace's ongoing agent tasks", func() error { _, err := r.ListOngoingAgentTaskIDs(ctx, skUser, 7, 2); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent = nil
			if err := tc.call(); err != nil {
				t.Fatal(err)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d statements, want 1", len(sent))
			}
			var plan []string
			rows, err := db.Raw("EXPLAIN QUERY PLAN "+sent[0].sql, sent[0].vars...).Rows()
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			// A SEARCH reaches rows by an index's key; a SCAN reads all of a
			// table, or all of an index, which is no better.
			for _, step := range plan {
				if strings.HasPrefix(step, "SCAN ") {
					t.Errorf("the plan scans: %q (plan %v, for %s)", step, plan, sent[0].sql)
				}
			}
		})
	}
}
