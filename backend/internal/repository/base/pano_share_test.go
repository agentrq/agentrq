// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A Pano doc's share lives from its owner's share to its unshare, and a
// teammate reaches it only through a team they are in that it is shared
// with.
func TestPanoDocShares(t *testing.T) {
	ctx := context.Background()
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		db.Create(&model.User{ID: 10, Name: "Alice", Email: "a@x.io", Picture: "a.png"})
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
		db.Create(&model.PanoDoc{ID: 7, UserID: 10, Title: "plan", Excerpt: "steps", UpdatedAt: teamT0, ExpiresAt: teamT0.Add(time.Hour)})
		db.Create(&model.PanoDoc{ID: 8, UserID: 11, Title: "bob's", ExpiresAt: teamT0.Add(time.Hour)})

		if _, _, err := r.SetPanoDocShare(ctx, model.PanoDocShare{ID: 1, TeamID: 1, DocID: 8, UserID: 10, CreatedAt: teamT0}, 1, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("sharing another account's doc: %v", err)
		}
		s, next, err := r.SetPanoDocShare(ctx, model.PanoDocShare{ID: 1, TeamID: 1, DocID: 7, UserID: 10, CreatedAt: teamT0}, 1, act())
		if err != nil || next != 2 || s.TeamName != "core" || s.DocID != 7 {
			t.Fatalf("sharing: %+v %d %v", s, next, err)
		}
		if shares, err := r.ListPanoDocShares(ctx, 7, 10); err != nil || len(shares) != 1 || shares[0].TeamName != "core" {
			t.Errorf("shares: %+v %v", shares, err)
		}
		if shares, _ := r.ListPanoDocShares(ctx, 7, 11); len(shares) != 0 {
			t.Errorf("another account sees the shares: %+v", shares)
		}
		docs, err := r.ListTeamPanoDocs(ctx, 1, teamT0)
		if err != nil || len(docs) != 1 || docs[0].Title != "plan" || docs[0].Excerpt != "steps" || docs[0].OwnerName != "Alice" || docs[0].OwnerPicture != "a.png" {
			t.Errorf("the team's docs: %+v %v", docs, err)
		}
		if o, err := r.PanoDocOwnerInTeam(ctx, 11, 1, 7); err != nil || o.UserID != 10 || o.Name != "Alice" {
			t.Errorf("a teammate reading: %+v %v", o, err)
		}
		for name, ids := range map[string][3]int64{"an outsider": {12, 1, 7}, "another team": {11, 2, 7}, "an unshared doc": {11, 1, 8}} {
			if _, err := r.PanoDocOwnerInTeam(ctx, ids[0], ids[1], ids[2]); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s: %v", name, err)
			}
		}
		if _, err := r.DeletePanoDocShare(ctx, 7, 1, 11, 2, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("a teammate unsharing the owner's doc: %v", err)
		}
		if next, err := r.DeletePanoDocShare(ctx, 7, 1, 10, 2, act()); err != nil || next != 3 {
			t.Errorf("unsharing: %d %v", next, err)
		}
		if docs, _ := r.ListTeamPanoDocs(ctx, 1, teamT0); len(docs) != 0 {
			t.Errorf("the team still lists %+v", docs)
		}

		failTeamStatement(t, db, "row", "pano_doc_shares", 0)
		if _, err := r.PanoDocOwnerInTeam(ctx, 11, 1, 7); !errors.Is(err, errTeamDB) {
			t.Errorf("a failed read: %v", err)
		}
	})
}
