// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	ssUser   = int64(482467435371298817)
	ssOther  = int64(482467435371298818)
	ssWS     = int64(498041479541817345)
	ssWS2    = int64(498041479541817346)
	ssOrigin = "https://github.com"
)

func siteShareDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.SiteShare{}, &model.ForkFolder{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func share(id, userID, workspaceID int64, origin string) model.SiteShare {
	return model.SiteShare{
		ID: id, UserID: userID, WorkspaceID: workspaceID, Origin: origin,
		BrowserID: "browser-1", InstanceID: "instance-1",
		LastURL: origin + "/", Tools: `[{"name":"search"}]`,
	}
}

func upsert(t *testing.T, r Repository, s model.SiteShare) model.SiteShare {
	t.Helper()
	got, err := r.UpsertSiteShare(context.Background(), s)
	if err != nil {
		t.Fatalf("UpsertSiteShare: %v", err)
	}
	return got
}

func TestUpsertSiteShare_NewRowDefaultsToEmptyArrays(t *testing.T) {
	r := New(&mockDB{db: siteShareDB(t)})
	got := upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
	if got.ID != 1 || got.AlwaysAllow != "[]" || got.Tools != `[{"name":"search"}]` {
		t.Fatalf("got %+v", got)
	}
	bare := share(2, ssUser, ssWS, "https://example.com")
	bare.Tools = ""
	if got := upsert(t, r, bare); got.Tools != "[]" || got.AlwaysAllow != "[]" {
		t.Fatalf("got %+v", got)
	}
}

// Sharing a site again updates the one row for (user, origin). A move to
// another workspace forgets what the old one was always allowed to call.
func TestUpsertSiteShare_OneRowPerUserOrigin(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		workspaceID int64
		wantAllow   string
	}{
		{"same workspace keeps always-allow", ssWS, `["search"]`},
		{"new workspace resets always-allow", ssWS2, "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := siteShareDB(t)
			r := New(&mockDB{db: db})
			first := upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
			if err := r.SetSiteShareAlwaysAllow(ctx, first.ID, []string{"search"}); err != nil {
				t.Fatalf("SetSiteShareAlwaysAllow: %v", err)
			}

			next := share(2, ssUser, tc.workspaceID, ssOrigin)
			next.BrowserID, next.InstanceID, next.LastURL, next.Tools = "browser-2", "instance-2", ssOrigin+"/x", "[]"
			got := upsert(t, r, next)

			var n int64
			if err := db.Model(&model.SiteShare{}).Count(&n).Error; err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 1 {
				t.Fatalf("%d rows, want 1", n)
			}
			if got.ID != first.ID || got.WorkspaceID != tc.workspaceID || got.BrowserID != "browser-2" ||
				got.InstanceID != "instance-2" || got.LastURL != ssOrigin+"/x" || got.Tools != "[]" {
				t.Errorf("got %+v", got)
			}
			if got.AlwaysAllow != tc.wantAllow {
				t.Errorf("AlwaysAllow = %s, want %s", got.AlwaysAllow, tc.wantAllow)
			}
		})
	}
}

func TestSiteShares_OtherUsersRowsAreNeverListed(t *testing.T) {
	ctx := context.Background()
	r := New(&mockDB{db: siteShareDB(t)})
	upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
	upsert(t, r, share(2, ssUser, ssWS, "https://example.com"))
	upsert(t, r, share(3, ssUser, ssWS2, "https://news.ycombinator.com"))
	upsert(t, r, share(4, ssOther, ssWS, ssOrigin))

	mine, err := r.ListSiteSharesForWorkspace(ctx, ssWS, ssUser)
	if err != nil {
		t.Fatalf("ListSiteSharesForWorkspace: %v", err)
	}
	if len(mine) != 2 || mine[0].Origin != "https://example.com" || mine[1].Origin != ssOrigin {
		t.Errorf("workspace list: %+v", mine)
	}
	all, err := r.ListSiteSharesForUser(ctx, ssUser)
	if err != nil {
		t.Fatalf("ListSiteSharesForUser: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("user list: %d rows, want 3", len(all))
	}
	for _, s := range append(mine, all...) {
		if s.UserID != ssUser {
			t.Errorf("listed another user's row: %+v", s)
		}
	}

	got, err := r.GetSiteShare(ctx, ssWS, ssOther, ssOrigin)
	if err != nil || got.ID != 4 {
		t.Errorf("GetSiteShare = %+v, %v", got, err)
	}
	if _, err := r.GetSiteShare(ctx, ssWS2, ssUser, ssOrigin); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("GetSiteShare in the wrong workspace: %v, want ErrRecordNotFound", err)
	}
}

func TestDeleteSiteShare(t *testing.T) {
	ctx := context.Background()
	r := New(&mockDB{db: siteShareDB(t)})
	upsert(t, r, share(1, ssUser, ssWS, ssOrigin))

	if ok, err := r.DeleteSiteShare(ctx, ssOther, ssOrigin); err != nil || ok {
		t.Errorf("another user's delete = %v, %v; want false", ok, err)
	}
	if ok, err := r.DeleteSiteShare(ctx, ssUser, ssOrigin); err != nil || !ok {
		t.Errorf("delete = %v, %v; want true", ok, err)
	}
	if ok, err := r.DeleteSiteShare(ctx, ssUser, ssOrigin); err != nil || ok {
		t.Errorf("delete of a missing row = %v, %v; want false", ok, err)
	}
}

func TestSetSiteShareAlwaysAllow_EmptyIsAnArray(t *testing.T) {
	ctx := context.Background()
	r := New(&mockDB{db: siteShareDB(t)})
	upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
	if err := r.SetSiteShareAlwaysAllow(ctx, 1, nil); err != nil {
		t.Fatalf("SetSiteShareAlwaysAllow: %v", err)
	}
	got, err := r.GetSiteShare(ctx, ssWS, ssUser, ssOrigin)
	if err != nil || got.AlwaysAllow != "[]" {
		t.Errorf("got %q, %v; want []", got.AlwaysAllow, err)
	}
}

// Every statement can fail, and each failure reaches the caller.
func TestSiteShareRepository_EveryStatementFailure(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(r Repository) error
	}{
		{"upsert", func(r Repository) error {
			_, err := r.UpsertSiteShare(ctx, share(2, ssUser, ssWS2, ssOrigin))
			return err
		}},
		{"delete", func(r Repository) error {
			_, err := r.DeleteSiteShare(ctx, ssUser, ssOrigin)
			return err
		}},
		{"list workspace", func(r Repository) error {
			_, err := r.ListSiteSharesForWorkspace(ctx, ssWS, ssUser)
			return err
		}},
		{"list user", func(r Repository) error {
			_, err := r.ListSiteSharesForUser(ctx, ssUser)
			return err
		}},
		{"get", func(r Repository) error {
			_, err := r.GetSiteShare(ctx, ssWS, ssUser, ssOrigin)
			return err
		}},
		{"always allow", func(r Repository) error {
			return r.SetSiteShareAlwaysAllow(ctx, 1, []string{"search"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for n := 1; ; n++ {
				db := siteShareDB(t)
				r := New(&mockDB{db: db})
				upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
				ran := failNth(db, n)
				err := tc.call(r)
				if *ran < n {
					if err != nil {
						t.Fatalf("with no failure injected: %v", err)
					}
					if n == 1 {
						t.Fatal("the call ran no statements")
					}
					return
				}
				if !errors.Is(err, errInjected) {
					t.Fatalf("failing statement %d: got %v, want the injected failure", n, err)
				}
			}
		})
	}
}

// A deleted workspace takes its site shares with it, and nobody else's.
func TestDeleteWorkspace_DeletesItsSiteShares(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	for _, id := range []int64{ssWS, ssWS2} {
		if err := db.Create(&model.Workspace{ID: id, CreatedAt: now, UpdatedAt: now, UserID: ssUser, Name: "w"}).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	r := New(&mockDB{db: db})
	upsert(t, r, share(1, ssUser, ssWS, ssOrigin))
	upsert(t, r, share(2, ssUser, ssWS2, "https://example.com"))

	if err := r.DeleteWorkspace(context.Background(), ssWS, ssUser); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	left, err := r.ListSiteSharesForUser(context.Background(), ssUser)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 1 || left[0].WorkspaceID != ssWS2 {
		t.Errorf("left behind: %+v", left)
	}
}
