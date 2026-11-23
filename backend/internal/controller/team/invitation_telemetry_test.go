// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// sent is the events of action, as "user team".
func (e *events) sent(action entity.Action) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []string{}
	for _, ev := range e.seen {
		if ev.Action == action {
			out = append(out, fmt.Sprintf("%d %d", ev.UserID, ev.ResourceID))
			if ev.WorkspaceID != 0 || ev.ResourceType != entity.ResourceTeam {
				panic(fmt.Sprintf("%s carries workspace %d, resource %s", action, ev.WorkspaceID, ev.ResourceType))
			}
		}
	}
	return out
}

func expectSent(t *testing.T, f *fixture, action entity.Action, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := f.events.sent(action); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s events are %v, want %v", action, got, want)
	}
}

func TestAnInvitationEmailIsCountedSentOrFailed(t *testing.T) {
	m := &mailbox{}
	each(t, func(f *fixture) {
		m.err = nil
		// The fixture's own invitations were emailed, by Alice.
		expectSent(t, f, entity.ActionTeamInviteEmailSent, fmt.Sprintf("%d %d", alice, f.team), fmt.Sprintf("%d %d", alice, f.team))
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		m.err = errors.New("network unreachable")
		must(f.c.CreateInvitation(f.ctx, f.ch(carol), "eve@example.com"))
		m.err = nil
		expectSent(t, f, entity.ActionTeamInviteEmailSent, fmt.Sprintf("%d %d", alice, f.team), fmt.Sprintf("%d %d", alice, f.team), fmt.Sprintf("%d %d", bob, f.team))
		expectSent(t, f, entity.ActionTeamInviteEmailFailed, fmt.Sprintf("%d %d", carol, f.team))
	}, withMailer(m))
}

func TestNoEmailIsCountedWithoutSMTP(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		expectSent(t, f, entity.ActionTeamInviteEmailSent)
		expectSent(t, f, entity.ActionTeamInviteEmailFailed)
	})
}

func TestARevokeIsCountedOnceDone(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		_, _ = f.c.RevokeInvitation(f.ctx, f.stale(carol), rs.Invitation.ID)
		_, _ = f.c.RevokeInvitation(f.ctx, f.ch(dave), rs.Invitation.ID)
		expectSent(t, f, entity.ActionTeamInviteRevoke)
		must(f.c.RevokeInvitation(f.ctx, f.ch(carol), rs.Invitation.ID))
		_, _ = f.c.RevokeInvitation(f.ctx, f.ch(carol), rs.Invitation.ID)
		expectSent(t, f, entity.ActionTeamInviteRevoke, fmt.Sprintf("%d %d", carol, f.team))
	})
}

func TestADeclineIsCountedOnceDone(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		_ = f.c.DeclineInvitation(f.ctx, eve, rs.Invitation.ID)
		expectSent(t, f, entity.ActionTeamInviteDecline)
		expectOK(t, "Dave declining", f.c.DeclineInvitation(f.ctx, dave, rs.Invitation.ID))
		_ = f.c.DeclineInvitation(f.ctx, dave, rs.Invitation.ID)
		expectSent(t, f, entity.ActionTeamInviteDecline, fmt.Sprintf("%d %d", dave, f.team))

		// The repository failing is no decline.
		rs = must(f.c.CreateInvitation(f.ctx, f.ch(bob), "eve@example.com"))
		f.c.(*controller).repo = failing{f.repo, "DeclineTeamInvitation"}
		if err := f.c.DeclineInvitation(f.ctx, eve, rs.Invitation.ID); !errors.Is(err, errDB) {
			t.Errorf("declining with the database down returned %v", err)
		}
		expectSent(t, f, entity.ActionTeamInviteDecline, fmt.Sprintf("%d %d", dave, f.team))
	})
}

func TestTheSweepCountsEachExpiredInvitationOnce(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		accepted := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "eve@example.com"))
		must(f.c.AcceptInvitationByID(f.ctx, eve, accepted.Invitation.ID))
		revoked := must(f.c.CreateInvitation(f.ctx, f.ch(carol), "x@example.com"))
		must(f.c.RevokeInvitation(f.ctx, f.ch(carol), revoked.Invitation.ID))
		must(f.c.CreateInvitation(f.ctx, f.ch(carol), "y@example.com"))
		declinedLate := must(f.c.CreateInvitation(f.ctx, f.ch(alice), "dave2@example.com"))
		f.now = t0.Add(time.Hour)
		must(f.c.CreateInvitation(f.ctx, f.ch(alice), "z@example.com")) // expires an hour after the rest

		if n := must(f.c.CountExpiredInvitations(f.ctx)); n != 0 {
			t.Errorf("before any expired the sweep counted %d", n)
		}

		f.now = t0.Add(InvitationTTL)
		// Re-inviting revokes the expired invitation, after its expiry.
		must(f.c.CreateInvitation(f.ctx, f.ch(alice), "y@example.com"))
		f.db.Model(&model.TeamInvitation{}).Where("id = ?", declinedLate.Invitation.ID).Update("revoked_at", f.now.Add(time.Minute))
		if n := must(f.c.CountExpiredInvitations(f.ctx)); n != 3 {
			t.Errorf("the sweep at the expiry counted %d, want Dave's, the re-invited and the late declined", n)
		}
		expectSent(t, f, entity.ActionTeamInviteExpire,
			fmt.Sprintf("%d %d", bob, f.team), fmt.Sprintf("%d %d", carol, f.team), fmt.Sprintf("%d %d", alice, f.team))
		if n := must(f.c.CountExpiredInvitations(f.ctx)); n != 0 {
			t.Errorf("sweeping again counted %d", n)
		}

		f.now = t0.Add(InvitationTTL + time.Hour)
		if n := must(f.c.CountExpiredInvitations(f.ctx)); n != 1 {
			t.Errorf("the sweep an hour on counted %d, want the one made an hour later", n)
		}
	})
}

func TestTheSweepTakesEveryBatch(t *testing.T) {
	each(t, func(f *fixture) {
		rows := make([]model.TeamInvitation, expirySweepBatch+1)
		for i := range rows {
			rows[i] = model.TeamInvitation{
				ID: int64(500000 + i), TeamID: f.team, Email: fmt.Sprintf("u%d@example.com", i), InvitedBy: alice,
				TokenHash: fmt.Sprintf("%064d", i), ExpiresAt: t0, CreatedAt: t0.Add(-InvitationTTL),
			}
		}
		if err := f.db.CreateInBatches(rows, 100).Error; err != nil {
			t.Fatal(err)
		}
		f.now = t0
		if n := must(f.c.CountExpiredInvitations(f.ctx)); n != expirySweepBatch+1 {
			t.Errorf("the sweep counted %d, want %d", n, expirySweepBatch+1)
		}
		if n := len(f.events.sent(entity.ActionTeamInviteExpire)); n != expirySweepBatch+1 {
			t.Errorf("%d expiries were sent", n)
		}
	})
}

func TestTheSweepWithTheDatabaseDown(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		f.now = t0.Add(InvitationTTL)
		f.c.(*controller).repo = failing{f.repo, "MarkExpiredTeamInvitations"}
		if _, err := f.c.CountExpiredInvitations(f.ctx); !errors.Is(err, errDB) {
			t.Errorf("the sweep returned %v, want the database's error", err)
		}
		expectSent(t, f, entity.ActionTeamInviteExpire)
	})
}

func TestTheRepositorySweepFailing(t *testing.T) {
	each(t, func(f *fixture) {
		must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		late := f.now.Add(InvitationTTL)

		// Reading the candidates fails.
		broken := f.db.Session(&gorm.Session{NewDB: true})
		if err := broken.Callback().Query().Before("gorm:query").Register("fail_query", func(tx *gorm.DB) {
			if tx.Statement.Table == "team_invitations" {
				_ = tx.AddError(errDB)
			}
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.MarkExpiredTeamInvitations(f.ctx, late, 10); !errors.Is(err, errDB) {
			t.Errorf("a failed read returned %v", err)
		}
		_ = broken.Callback().Query().Remove("fail_query")

		// Marking one fails.
		if err := f.db.Callback().Update().Before("gorm:update").Register("fail_update", func(tx *gorm.DB) {
			if tx.Statement.Table == "team_invitations" {
				_ = tx.AddError(errDB)
			}
		}); err != nil {
			t.Fatal(err)
		}
		marked, err := f.repo.MarkExpiredTeamInvitations(f.ctx, late, 10)
		if !errors.Is(err, errDB) || len(marked) != 0 {
			t.Errorf("a failed mark returned %v, %v", marked, err)
		}
		_ = f.db.Callback().Update().Remove("fail_update")
		if marked := must(f.repo.MarkExpiredTeamInvitations(f.ctx, late, 10)); len(marked) != 1 {
			t.Errorf("after the failure the sweep marked %d, want the one left", len(marked))
		}
	})
}

// An instance that lost the race to mark an invitation does not count it.
func TestASweepRacingAnotherCountsNothingTwice(t *testing.T) {
	each(t, func(f *fixture) {
		rs := must(f.c.CreateInvitation(f.ctx, f.ch(bob), "dave@example.com"))
		late := f.now.Add(InvitationTTL)
		if err := f.db.Callback().Update().Before("gorm:update").Register("race", func(tx *gorm.DB) {
			if tx.Statement.Table == "team_invitations" {
				// The other instance marks it between this one's read and update.
				tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec("UPDATE team_invitations SET expiry_counted_at = ? WHERE id = ?", late, rs.Invitation.ID)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.db.Callback().Update().Remove("race") }()
		if marked := must(f.repo.MarkExpiredTeamInvitations(context.Background(), late, 10)); len(marked) != 0 {
			t.Errorf("the sweep that lost the race marked %v", marked)
		}
	})
}
