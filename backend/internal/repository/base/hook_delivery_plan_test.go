// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// Every request to a hook URL counts the registration's queued deliveries,
// so the count must reach them by an index on both the registration and the
// status: by the registration alone it reads every delivery the retention
// keeps. The plan of the statement the repository sends is checked, on a
// registration whose log is mostly processed deliveries.
func TestCountQueuedHookDeliveriesUsesTheRegistrationStatusIndex(t *testing.T) {
	ctx := context.Background()
	for name, r := range deliveryRepos(t) {
		t.Run(name, func(t *testing.T) {
			db := r.db.Conn(ctx)
			var ds []model.HookDelivery
			for i := int64(1); i <= 3000; i++ {
				status, reg := model.HookDeliveryPublished, int64(1)
				if i%1000 == 0 {
					status = model.HookDeliveryQueued
				}
				if i%3 == 0 {
					reg = 2
				}
				ds = append(ds, delivery(i, reg, status, t0))
			}
			if err := db.CreateInBatches(ds, 500).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("ANALYZE").Error; err != nil {
				t.Fatal(err)
			}

			var sent []string
			capture := func(tx *gorm.DB) {
				sent = append(sent, tx.Dialector.Explain(tx.Statement.SQL.String(), tx.Statement.Vars...))
			}
			if err := db.Callback().Query().After("gorm:query").Register("test:plan", capture); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove("test:plan") })
			if n, err := r.CountQueuedHookDeliveries(ctx, 1); err != nil || n != 2 {
				t.Fatalf("queued = %d, %v", n, err)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d statements, want 1", len(sent))
			}

			explain, want := "EXPLAIN ", "idx_hook_deliveries_registration_id_status"
			if name == "sqlite" {
				// SQLite names the index's columns it searches on.
				explain, want = "EXPLAIN QUERY PLAN ", want+" (registration_id=? AND status=?)"
			}
			rows, err := db.Raw(explain + sent[0]).Rows()
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cols, _ := rows.Columns()
			var plan []string
			for rows.Next() {
				cells := make([]any, len(cols))
				for i := range cells {
					cells[i] = new(any)
				}
				if err := rows.Scan(cells...); err != nil {
					t.Fatal(err)
				}
				// The step is the last column: SQLite's detail, Postgres's line.
				step := *(cells[len(cells)-1].(*any))
				if b, ok := step.([]byte); ok {
					step = string(b)
				}
				plan = append(plan, step.(string))
			}
			if got := strings.Join(plan, "\n"); !strings.Contains(got, want) {
				t.Errorf("the count's plan does not use %q:\n%s\nfor %s", want, got, sent[0])
			}
		})
	}
}
