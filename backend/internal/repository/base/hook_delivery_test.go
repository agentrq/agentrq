// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var hookTables = []any{&model.Hook{}, &model.HookRegistration{}, &model.HookDelivery{}, &model.HookDeliveryKey{}}

// deliveryRepos is a repository on SQLite, and on Postgres too when
// AGENTRQ_TEST_POSTGRES_DSN names a throwaway database: the claim and the
// dedup insert are the queue's correctness, and they are dialect SQL.
func deliveryRepos(t *testing.T) map[string]*repository {
	t.Helper()
	open := func(d gorm.Dialector) *repository {
		db, err := gorm.Open(d, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		if err := db.Migrator().DropTable(hookTables...); err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(hookTables...); err != nil {
			t.Fatal(err)
		}
		return New(&mockDB{db: db}).(*repository)
	}
	repos := map[string]*repository{"sqlite": open(sqlite.Open("file::memory:"))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		repos["postgres"] = open(postgres.Open(dsn))
	}
	return repos
}

var t0 = time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC)

func delivery(id, regID int64, status int16, next time.Time) model.HookDelivery {
	return model.HookDelivery{ID: id, RegistrationID: regID, UserID: 7, ReceivedAt: t0, Status: status, NextAttemptAt: next,
		Request: []byte(`{"method":"POST"}`), BodySHA256: "0000000000000000000000000000000000000000000000000000000000000000", EventNames: "[]"}
}

func TestHookDeliveryQueue(t *testing.T) {
	ctx := context.Background()
	for name, r := range deliveryRepos(t) {
		t.Run(name, func(t *testing.T) {
			if err := r.CreateHookRegistration(ctx, model.HookRegistration{ID: 1, UserID: 7, Name: "n"}); err != nil {
				t.Fatal(err)
			}
			if reg, err := r.GetHookRegistrationByID(ctx, 1); err != nil || reg.UserID != 7 {
				t.Fatalf("by id = %+v, %v", reg, err)
			}
			if _, err := r.GetHookRegistrationByID(ctx, 2); !errors.Is(err, ErrNotFound) {
				t.Errorf("none = %v", err)
			}

			for _, d := range []model.HookDelivery{
				delivery(10, 1, model.HookDeliveryQueued, t0),
				delivery(11, 1, model.HookDeliveryQueued, t0.Add(time.Hour)), // a retry, not yet due
				delivery(12, 1, model.HookDeliveryProcessing, t0.Add(-time.Second)),
				delivery(13, 1, model.HookDeliveryRouted, t0.Add(-2*time.Second)),
				delivery(14, 1, model.HookDeliveryPublished, t0.Add(-3*time.Second)),
				delivery(15, 2, model.HookDeliveryQueued, t0),
			} {
				if err := r.CreateHookDelivery(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := r.CountQueuedHookDeliveries(ctx, 1); err != nil || n != 2 {
				t.Errorf("queued = %d, %v", n, err)
			}

			due, err := r.ListDueHookDeliveries(ctx, t0, 10)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, d := range due {
				ids = append(ids, d.ID)
			}
			if len(ids) != 4 || ids[0] != 13 || ids[1] != 12 || due[0].Status != model.HookDeliveryRouted {
				t.Errorf("due = %v", ids)
			}
			if due, _ := r.ListDueHookDeliveries(ctx, t0, 1); len(due) != 1 {
				t.Errorf("limit: %d", len(due))
			}

			lease := t0.Add(time.Minute)
			for _, tc := range []struct {
				id   int64
				want bool
			}{{10, true}, {10, false}, {11, false}, {12, true}, {13, false}, {14, false}} {
				if ok, err := r.ClaimHookDelivery(ctx, tc.id, t0, lease); err != nil || ok != tc.want {
					t.Errorf("claim %d = %v, %v", tc.id, ok, err)
				}
			}
			d, err := r.GetHookDelivery(ctx, 10)
			if err != nil || d.Status != model.HookDeliveryProcessing || d.Attempts != 1 || !d.NextAttemptAt.Equal(lease) || string(d.Request) != `{"method":"POST"}` {
				t.Errorf("claimed = %+v, %v", d, err)
			}
			if _, err := r.GetHookDelivery(ctx, 99); !errors.Is(err, ErrNotFound) {
				t.Errorf("none = %v", err)
			}
			// A lease ended is claimed again, as another attempt.
			if ok, _ := r.ClaimHookDelivery(ctx, 10, lease, lease.Add(time.Minute)); !ok {
				t.Error("an ended lease was not claimed")
			}
			if d, _ := r.GetHookDelivery(ctx, 10); d.Attempts != 2 {
				t.Errorf("attempts = %d", d.Attempts)
			}

			for _, tc := range []struct {
				id   int64
				want bool
			}{{13, true}, {13, false}, {10, false}} {
				if ok, err := r.ClaimRoutedHookDelivery(ctx, tc.id, t0, lease); err != nil || ok != tc.want {
					t.Errorf("claim routed %d = %v, %v", tc.id, ok, err)
				}
			}

			// A retry: queued again, later, with the error.
			d.Status, d.NextAttemptAt, d.Error, d.DurationMs = model.HookDeliveryQueued, t0.Add(time.Hour), "timeout", 250
			if err := r.UpdateHookDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			got, _ := r.GetHookDelivery(ctx, 10)
			if got.Status != model.HookDeliveryQueued || got.Error != "timeout" || got.DurationMs != 250 || got.Request == nil || got.Attempts != 2 {
				t.Errorf("updated = %+v", got)
			}
			// Final: the request cleared.
			got.Status, got.Request, got.Error = model.HookDeliveryRejected, nil, "bad signature"
			if err := r.UpdateHookDelivery(ctx, got); err != nil {
				t.Fatal(err)
			}
			if got, _ = r.GetHookDelivery(ctx, 10); got.Request != nil || got.Status != model.HookDeliveryRejected {
				t.Errorf("final = %+v", got)
			}
		})
	}
}

func TestRouteHookDelivery(t *testing.T) {
	ctx := context.Background()
	for name, r := range deliveryRepos(t) {
		t.Run(name, func(t *testing.T) {
			for _, d := range []model.HookDelivery{delivery(20, 1, model.HookDeliveryProcessing, t0), delivery(21, 1, model.HookDeliveryProcessing, t0), delivery(22, 2, model.HookDeliveryProcessing, t0)} {
				if err := r.CreateHookDelivery(ctx, d); err != nil {
					t.Fatal(err)
				}
			}
			events := []model.HookPendingEvent{
				{HookEvent: "ci.failed", EventID: 5, DedupKey: "run-1", Payload: "p1", FAQ: []model.HookPendingFAQ{{Q: "q", A: "a"}}},
				{HookEvent: "push", EventID: 6, Payload: "p2"},
			}
			d, _ := r.GetHookDelivery(ctx, 20)
			kept, err := r.RouteHookDelivery(ctx, d, events)
			if err != nil || len(kept) != 2 {
				t.Fatalf("route = %+v, %v", kept, err)
			}
			got, _ := r.GetHookDelivery(ctx, 20)
			var pending []model.HookPendingEvent
			_ = json.Unmarshal(got.Pending, &pending)
			if got.Status != model.HookDeliveryRouted || got.Request != nil || got.EventNames != `["ci.failed","push"]` || len(pending) != 2 || pending[0].FAQ[0].A != "a" {
				t.Errorf("routed = %+v", got)
			}

			// The provider's retry: the keyed event is a duplicate, the
			// unkeyed one is not.
			d, _ = r.GetHookDelivery(ctx, 21)
			if kept, err = r.RouteHookDelivery(ctx, d, events); err != nil || len(kept) != 1 || kept[0].HookEvent != "push" {
				t.Errorf("retry = %+v, %v", kept, err)
			}
			// Only the keyed one: nothing kept, a duplicate.
			if kept, err = r.RouteHookDelivery(ctx, d, events[:1]); err != nil || len(kept) != 0 {
				t.Errorf("duplicate = %+v, %v", kept, err)
			}
			if got, _ = r.GetHookDelivery(ctx, 21); got.Status != model.HookDeliveryDuplicate || got.Pending != nil || got.EventNames != "[]" {
				t.Errorf("duplicate = %+v", got)
			}
			// Another registration's key of the same text is its own.
			d, _ = r.GetHookDelivery(ctx, 22)
			if kept, err = r.RouteHookDelivery(ctx, d, events[:1]); err != nil || len(kept) != 1 {
				t.Errorf("another registration = %+v, %v", kept, err)
			}
		})
	}
}

func TestRouteHookDeliveryFails(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"hook_delivery_keys", "hook_deliveries"} {
		r := deliveryRepos(t)["sqlite"]
		d := delivery(30, 1, model.HookDeliveryProcessing, t0)
		if err := r.CreateHookDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
		if err := r.db.Conn(ctx).Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
		if _, err := r.RouteHookDelivery(ctx, d, []model.HookPendingEvent{{HookEvent: "e", DedupKey: "k"}}); err == nil {
			t.Errorf("without %s: no error", table)
		}
	}
}

func TestListAndReplayHookDeliveries(t *testing.T) {
	ctx := context.Background()
	r := deliveryRepos(t)["sqlite"]
	for i, status := range []int16{model.HookDeliveryPublished, model.HookDeliveryFailed, model.HookDeliveryRouted, model.HookDeliveryRejected} {
		d := delivery(int64(40+i), 1, status, t0)
		d.Pending = []byte("[]")
		if err := r.CreateHookDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	other := delivery(50, 1, model.HookDeliveryFailed, t0)
	other.UserID = 8
	if err := r.CreateHookDelivery(ctx, other); err != nil {
		t.Fatal(err)
	}

	ds, err := r.ListHookDeliveries(ctx, 1, 7, 0, 2)
	if err != nil || len(ds) != 2 || ds[0].ID != 43 || ds[1].ID != 42 || ds[0].Request != nil || ds[0].Pending != nil {
		t.Errorf("first page = %+v, %v", ds, err)
	}
	if ds, err = r.ListHookDeliveries(ctx, 1, 7, 42, 10); err != nil || len(ds) != 2 || ds[0].ID != 41 {
		t.Errorf("next page = %+v, %v", ds, err)
	}

	if err := r.ReplayHookDelivery(ctx, 41, 1, 7, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.GetHookDelivery(ctx, 41); d.Status != model.HookDeliveryQueued || d.Attempts != 0 || !d.NextAttemptAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("replayed = %+v", d)
	}
	for _, tc := range []struct{ id, reg, user int64 }{{41, 1, 7}, {40, 1, 7}, {42, 1, 7}, {43, 1, 7}, {50, 1, 7}, {41, 2, 7}} {
		if err := r.ReplayHookDelivery(ctx, tc.id, tc.reg, tc.user, t0); !errors.Is(err, ErrNotFound) {
			t.Errorf("replay %+v = %v", tc, err)
		}
	}
}

func TestDeleteHookDeliveriesBefore(t *testing.T) {
	ctx := context.Background()
	r := deliveryRepos(t)["sqlite"]
	db := r.db.Conn(ctx)
	old, recent := delivery(60, 1, model.HookDeliveryFailed, t0), delivery(61, 1, model.HookDeliveryFailed, t0)
	old.ReceivedAt, recent.ReceivedAt = t0.Add(-48*time.Hour), t0
	for _, d := range []model.HookDelivery{old, recent} {
		if err := r.CreateHookDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&model.HookDeliveryKey{RegistrationID: 1, DedupKey: "old", CreatedAt: t0.Add(-48 * time.Hour)})
	db.Create(&model.HookDeliveryKey{RegistrationID: 1, DedupKey: "new", CreatedAt: t0})

	if err := r.DeleteHookDeliveriesBefore(ctx, t0.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var deliveries, keys int64
	db.Model(&model.HookDelivery{}).Count(&deliveries)
	db.Model(&model.HookDeliveryKey{}).Count(&keys)
	if deliveries != 1 || keys != 1 {
		t.Errorf("left %d deliveries and %d keys", deliveries, keys)
	}

	if err := db.Migrator().DropTable("hook_delivery_keys"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHookDeliveriesBefore(ctx, t0); err == nil {
		t.Error("without the keys table: no error")
	}
	if err := db.Migrator().DropTable("hook_deliveries"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHookDeliveriesBefore(ctx, t0); err == nil {
		t.Error("without the deliveries table: no error")
	}
}
