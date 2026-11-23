// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package team

import (
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// panoDoc stores a Pano doc of the account's that lives a day past t0.
func (f *fixture) panoDoc(id, userID int64, title string) {
	f.t.Helper()
	if err := f.db.AutoMigrate(&model.PanoDoc{}, &model.PanoFile{}, &model.PanoComment{}, &model.PanoDocTask{}, &model.PanoDocShare{}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.db.Create(&model.PanoDoc{ID: id, UserID: userID, Title: title, Markdown: "# " + title, CreatedAt: t0, UpdatedAt: t0, ExpiresAt: t0.Add(24 * time.Hour)}).Error; err != nil {
		f.t.Fatal(err)
	}
}

func TestSharingAPanoDoc(t *testing.T) {
	each(t, func(f *fixture) {
		f.panoDoc(500, alice, "Plan")
		f.panoDoc(501, carol, "Carol's")
		f.panoDoc(502, alice, "Old")
		f.db.Model(&model.PanoDoc{}).Where("id = ?", 502).Update("expires_at", t0.Add(-time.Hour))
		other := must(f.c.CreateTeam(f.ctx, dave, "ops"))

		s := must(f.c.SharePanoDoc(f.ctx, f.ch(alice), 500))
		if s.TeamName != "core" || s.TeamID != f.team || s.DocID != 500 || s.Version != 6 || !s.CreatedAt.Equal(t0) {
			t.Errorf("sharing gave %+v, want Plan in core at version 6", s)
		}
		// Sharing again changes nothing and records nothing.
		if s := must(f.c.SharePanoDoc(f.ctx, f.ch(alice), 500)); s.DocID != 500 || s.Version != 7 {
			t.Errorf("sharing again gave %+v", s)
		}
		expectConflict(t, "sharing from the version before", errOf(f.c.SharePanoDoc(f.ctx, f.stale(alice), 500)), 7)
		expectNotFound(t, "Alice sharing Carol's doc", errOf(f.c.SharePanoDoc(f.ctx, f.ch(alice), 501)))
		expectNotFound(t, "Alice sharing with a team she is not in", errOf(f.c.SharePanoDoc(f.ctx, f.chOf(alice, other.ID), 500)))
		expectNotFound(t, "sharing an unknown doc", errOf(f.c.SharePanoDoc(f.ctx, f.ch(alice), 424242)))
		expectNotFound(t, "sharing an expired doc", errOf(f.c.SharePanoDoc(f.ctx, f.ch(alice), 502)))
		expectNotFound(t, "sharing with an unknown team", errOf(f.c.SharePanoDoc(f.ctx, entity.TeamChange{UserID: alice, TeamID: 424242, Version: 1}, 500)))

		shares := must(f.c.ListPanoShares(f.ctx, alice, 500))
		if len(shares) != 1 || shares[0].TeamName != "core" || shares[0].DocID != 500 || shares[0].Version != 0 {
			t.Errorf("Alice's shares of Plan are %+v", shares)
		}
		expectNotFound(t, "Carol listing Alice's shares", errOf(f.c.ListPanoShares(f.ctx, carol, 500)))
		if acts := strings.Join(f.events.actions(), " "); !strings.Contains(acts, "pano_share:0") {
			t.Errorf("events are %s, want a pano_share with no workspace", acts)
		}

		expectNotFound(t, "Carol unsharing Alice's doc", errOf(f.c.UnsharePanoDoc(f.ctx, f.ch(carol), 500)))
		expectNotFound(t, "unsharing from a team she is not in", errOf(f.c.UnsharePanoDoc(f.ctx, f.chOf(alice, other.ID), 500)))
		expectConflict(t, "unsharing from the version before", errOf(f.c.UnsharePanoDoc(f.ctx, f.stale(alice), 500)), 7)
		f.db.Model(&model.PanoDoc{}).Where("id = ?", 500).Update("title", "Plan v2")
		if v, err := f.c.UnsharePanoDoc(f.ctx, f.ch(alice), 500); err != nil || v != 8 {
			t.Errorf("Alice unsharing returned %d, %v; want version 8", v, err)
		}
		expectNotFound(t, "unsharing it twice", errOf(f.c.UnsharePanoDoc(f.ctx, f.ch(alice), 500)))
		if shares := must(f.c.ListPanoShares(f.ctx, alice, 500)); len(shares) != 0 {
			t.Errorf("shares after unsharing are %+v", shares)
		}
		as := must(f.c.ListActivities(f.ctx, entity.ListTeamActivitiesRequest{UserID: bob, TeamID: f.team, Limit: 2}))
		if len(as.Activities) != 2 {
			t.Fatalf("the activity is %+v", as)
		}
		un, sh := as.Activities[0], as.Activities[1]
		if un.Action != model.TeamActivityPanoUnshared || un.Details["docTitle"] != "Plan v2" || un.Actor.UserID != alice ||
			sh.Action != model.TeamActivityPanoShared || sh.Details["docTitle"] != "Plan" {
			t.Errorf("the share and unshare are recorded as %+v and %+v", sh, un)
		}
	})
}

// A teammate reads a shared doc while its owner is in the team and the doc
// lives; every way out of the team, and the doc's own end, stops it.
func TestASharedPanoDocIsReadOnlyWhileShared(t *testing.T) {
	each(t, func(f *fixture) {
		f.panoDoc(500, alice, "Plan")
		f.panoDoc(501, bob, "Bob's")
		f.panoDoc(502, carol, "Carol's")
		must(f.c.SharePanoDoc(f.ctx, f.ch(alice), 500))
		must(f.c.SharePanoDoc(f.ctx, f.ch(bob), 501))
		must(f.c.SharePanoDoc(f.ctx, f.ch(carol), 502))

		owner, err := f.repo.PanoDocOwnerInTeam(f.ctx, bob, f.team, 500)
		if err != nil || owner.UserID != alice || owner.Name != "Alice" || owner.Picture != "https://img/alice" {
			t.Errorf("Bob reading Plan: owner %d, %v", owner.UserID, err)
		}
		if _, err := f.repo.PanoDocOwnerInTeam(f.ctx, dave, f.team, 500); err != base.ErrNotFound {
			t.Errorf("Dave, outside, reading Plan: %v", err)
		}
		docs := must(f.repo.ListTeamPanoDocs(f.ctx, f.team, t0))
		if len(docs) != 3 || docs[0].Markdown != "" {
			t.Fatalf("the team's docs are %+v", docs)
		}
		names := map[int64]string{}
		for _, d := range docs {
			names[d.ID] = d.OwnerName
		}
		if names[500] != "Alice" || names[501] != "Bob" || names[502] != "Carol" {
			t.Errorf("owners are %v", names)
		}
		if docs := must(f.repo.ListTeamPanoDocs(f.ctx, f.team, t0.Add(48*time.Hour))); len(docs) != 0 {
			t.Errorf("expired docs are listed: %+v", docs)
		}

		expectOK(t, "Carol leaving", f.c.LeaveTeam(f.ctx, f.ch(carol)))
		expectOK(t, "Alice removing Bob", errOf(f.c.RemoveMember(f.ctx, f.ch(alice), bob)))
		if shares := must(f.c.ListPanoShares(f.ctx, carol, 502)); len(shares) != 0 {
			t.Errorf("Carol's doc is still shared after she left: %+v", shares)
		}
		if shares := must(f.c.ListPanoShares(f.ctx, bob, 501)); len(shares) != 0 {
			t.Errorf("Bob's doc is still shared after his removal: %+v", shares)
		}

		if _, err := f.repo.DeletePanoDoc(f.ctx, alice, 500); err != nil {
			t.Fatal(err)
		}
		var n int64
		f.db.Model(&model.PanoDocShare{}).Count(&n)
		if n != 0 {
			t.Errorf("%d shares outlive their doc or their owner", n)
		}
	})
}

func TestDeletingATeamUnsharesItsPanoDocs(t *testing.T) {
	each(t, func(f *fixture) {
		f.panoDoc(500, alice, "Plan")
		must(f.c.SharePanoDoc(f.ctx, f.ch(alice), 500))
		expectOK(t, "Alice deleting the team", f.c.DeleteTeam(f.ctx, f.ch(alice)))
		var n int64
		f.db.Model(&model.PanoDocShare{}).Count(&n)
		if n != 0 {
			t.Errorf("%d shares outlive the team", n)
		}
	})
}
