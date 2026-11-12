// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func hookRepo(t *testing.T) Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Hook{}, &model.HookRegistration{}, &model.HookDelivery{}, &model.HookDeliveryKey{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(&mockDB{db: db})
}

func testHook(id, userID int64, name string) model.Hook {
	return model.Hook{
		ID: id, UserID: userID, Name: name, Version: "1.0.0", BlobKey: "k", SHA256: "s",
		SizeBytes: 10, ABIVersion: 1, Manifest: "{}", Enabled: true,
	}
}

func TestCreateHookRefusesASecondOfTheSameName(t *testing.T) {
	r, ctx := hookRepo(t), context.Background()
	for _, tc := range []struct {
		h    model.Hook
		want bool
	}{
		{testHook(1, 0, "github"), true},
		{testHook(2, 0, "github"), false},
		{testHook(3, 7, "github"), true}, // another account's
		{testHook(4, 0, "generic"), true},
	} {
		if ok, err := r.CreateHook(ctx, tc.h); err != nil || ok != tc.want {
			t.Fatalf("CreateHook(%d) = %v, %v; want %v", tc.h.ID, ok, err, tc.want)
		}
	}
	hooks, err := r.ListHooks(ctx, 0)
	if err != nil || len(hooks) != 2 || hooks[0].Name != "generic" || hooks[1].ID != 1 {
		t.Fatalf("ListHooks = %+v, %v", hooks, err)
	}
	if _, err := r.GetHook(ctx, 3, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's hook: err = %v", err)
	}
	if h, err := r.GetHook(ctx, 3, 7); err != nil || h.Name != "github" {
		t.Errorf("GetHook = %+v, %v", h, err)
	}
}

func TestReplaceHookModuleAndSetHookEnabled(t *testing.T) {
	r, ctx := hookRepo(t), context.Background()
	if _, err := r.CreateHook(ctx, testHook(1, 0, "github")); err != nil {
		t.Fatal(err)
	}
	before, _ := r.GetHook(ctx, 1, 0)
	up := model.Hook{ID: 1, UserID: 0, Version: "2.0.0", BlobKey: "k2", SHA256: "s2", SizeBytes: 20, ABIVersion: 1, Manifest: `{"a":1}`}
	if err := r.ReplaceHookModule(ctx, up); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHookEnabled(ctx, 1, 0, false); err != nil {
		t.Fatal(err)
	}
	h, _ := r.GetHook(ctx, 1, 0)
	if h.Version != "2.0.0" || h.BlobKey != "k2" || h.SHA256 != "s2" || h.SizeBytes != 20 || h.Manifest != `{"a":1}` ||
		h.UpdatedAt.Before(before.UpdatedAt) || h.Enabled || h.Name != "github" {
		t.Errorf("hook = %+v", h)
	}
	up.UserID = 7
	if err := r.ReplaceHookModule(ctx, up); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReplaceHookModule of another account's: err = %v", err)
	}
	if err := r.SetHookEnabled(ctx, 1, 7, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetHookEnabled of another account's: err = %v", err)
	}
}

func TestDeleteHookOnlyWhileNothingRegistersIt(t *testing.T) {
	r, ctx := hookRepo(t), context.Background()
	db := r.(*repository).db.Conn(ctx)
	for _, h := range []model.Hook{testHook(1, 0, "github"), testHook(2, 0, "generic")} {
		if _, err := r.CreateHook(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	regs := []model.HookRegistration{
		{ID: 11, UserID: 7, HookID: 1, Name: "a", TokenHash: "t", Inputs: "", Routes: `{"ci.failed":5}`},
		{ID: 10, UserID: 8, HookID: 1, Name: "b", TokenHash: "t", Inputs: "", Routes: `{"push":6}`},
	}
	if err := db.Create(&regs).Error; err != nil {
		t.Fatal(err)
	}
	routes, err := r.ListHookRegistrationRoutes(ctx, 1)
	if err != nil || !slices.Equal(routes, []string{`{"push":6}`, `{"ci.failed":5}`}) {
		t.Fatalf("routes = %q, %v", routes, err)
	}
	if routes, _ := r.ListHookRegistrationRoutes(ctx, 2); len(routes) != 0 {
		t.Errorf("an unregistered hook's routes = %q", routes)
	}

	if ok, err := r.DeleteHook(ctx, 1, 0); err != nil || ok {
		t.Fatalf("DeleteHook of a registered hook = %v, %v", ok, err)
	}
	if ok, err := r.DeleteHook(ctx, 2, 7); err != nil || ok {
		t.Fatalf("DeleteHook of another account's = %v, %v", ok, err)
	}
	if ok, err := r.DeleteHook(ctx, 2, 0); err != nil || !ok {
		t.Fatalf("DeleteHook = %v, %v", ok, err)
	}
	if _, err := r.GetHook(ctx, 1, 0); err != nil {
		t.Errorf("the registered hook is gone: %v", err)
	}
}

func TestHookRegistrations(t *testing.T) {
	r, ctx := hookRepo(t), context.Background()
	db := r.(*repository).db.Conn(ctx)
	for _, reg := range []model.HookRegistration{
		{ID: 1, UserID: 7, HookID: 3, Name: "ci", Enabled: true, TokenHash: "h1", Inputs: "i", Routes: "{}"},
		{ID: 2, UserID: 7, HookID: 3, Name: "prs", Enabled: true, TokenHash: "h2", Inputs: "i", Routes: "{}"},
		{ID: 3, UserID: 8, HookID: 3, Name: "other", TokenHash: "h3", Inputs: "i", Routes: "{}"},
	} {
		if err := r.CreateHookRegistration(ctx, reg); err != nil {
			t.Fatal(err)
		}
	}
	regs, err := r.ListHookRegistrations(ctx, 7)
	if err != nil || len(regs) != 2 || regs[0].ID != 2 {
		t.Fatalf("list = %+v, %v", regs, err)
	}
	if _, err := r.GetHookRegistration(ctx, 3, 7); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's: err = %v", err)
	}

	up := model.HookRegistration{ID: 1, UserID: 7, Name: "ci2", Enabled: false, Inputs: "i2", Routes: `{"push":5}`, TokenHash: "ignored"}
	if err := r.UpdateHookRegistration(ctx, up); err != nil {
		t.Fatal(err)
	}
	if err := r.SetHookRegistrationTokenHash(ctx, 1, 7, "h9"); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetHookRegistration(ctx, 1, 7)
	if err != nil || got.Name != "ci2" || got.Enabled || got.Inputs != "i2" || got.Routes != `{"push":5}` || got.TokenHash != "h9" || got.HookID != 3 {
		t.Errorf("updated = %+v, %v", got, err)
	}
	up.UserID = 8
	if err := r.UpdateHookRegistration(ctx, up); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of another account's: err = %v", err)
	}
	if err := r.SetHookRegistrationTokenHash(ctx, 1, 8, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token of another account's: err = %v", err)
	}

	// Deleting takes the deliveries and dedup keys with it, and only its own.
	for _, d := range []model.HookDelivery{{ID: 1, RegistrationID: 1}, {ID: 2, RegistrationID: 2}} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []model.HookDeliveryKey{{RegistrationID: 1, DedupKey: "a"}, {RegistrationID: 2, DedupKey: "a"}} {
		if err := db.Create(&k).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DeleteHookRegistration(ctx, 1, 8); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete of another account's: err = %v", err)
	}
	if err := r.DeleteHookRegistration(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	var deliveries, keys int64
	db.Model(&model.HookDelivery{}).Count(&deliveries)
	db.Model(&model.HookDeliveryKey{}).Count(&keys)
	if deliveries != 1 || keys != 1 {
		t.Errorf("left %d deliveries and %d keys, want 1 and 1", deliveries, keys)
	}
}

func TestDeleteHookRegistrationFails(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"hook_deliveries", "hook_delivery_keys"} {
		r := hookRepo(t)
		db := r.(*repository).db.Conn(ctx)
		if err := r.CreateHookRegistration(ctx, model.HookRegistration{ID: 1, UserID: 7, Name: "n"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteHookRegistration(ctx, 1, 7); err == nil {
			t.Errorf("without %s: no error", table)
		}
		if _, err := r.GetHookRegistration(ctx, 1, 7); err != nil {
			t.Errorf("without %s: the registration went anyway: %v", table, err)
		}
	}
	r := hookRepo(t)
	if err := r.(*repository).db.Conn(ctx).Migrator().DropTable("hook_registrations"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteHookRegistration(ctx, 1, 7); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("without the table: err = %v", err)
	}
}
