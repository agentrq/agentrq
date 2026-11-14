// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// whileLocked holds team 1's row in a transaction of its own and starts op,
// which then waits for it; it makes change in that transaction, commits, and
// returns op's answer. A check that reads under the lock sees the change.
func whileLocked(t *testing.T, db *gorm.DB, change func(tx *gorm.DB) error, op func() error) error {
	t.Helper()
	tx := db.Begin()
	if err := tx.Exec("UPDATE teams SET id = id WHERE id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- op() }()
	time.Sleep(200 * time.Millisecond) // op is waiting for the lock by now
	if err := change(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	return <-done
}

func countTeamRows(db *gorm.DB, m any, where string, args ...any) int64 {
	var n int64
	db.Model(m).Where(where, args...).Count(&n)
	return n
}

// removeMember is a change taking the user out of team 1, with their shares.
func removeMember(userID int64) func(tx *gorm.DB) error {
	return func(tx *gorm.DB) error {
		if err := tx.Where("team_id = 1 AND user_id = ?", userID).Delete(&model.TeamMember{}).Error; err != nil {
			return err
		}
		return tx.Where("team_id = 1 AND user_id = ?", userID).Delete(&model.WorkspaceShare{}).Error
	}
}

// Every check reads the version and the memberships after the team's lock,
// so a change committed while it waited is the one it decides on.
func TestTeamChecksSeeWhatChangedWhileTheyWaitedForTheLock(t *testing.T) {
	ctx := context.Background()
	share := func(user int64) model.WorkspaceShare {
		return model.WorkspaceShare{ID: 500 + user, TeamID: 1, WorkspaceID: 600 + user, UserID: user, CreatedAt: teamT0, UpdatedAt: teamT0}
	}
	invite := func(id, by int64) func(t *testing.T, db *gorm.DB) {
		return func(t *testing.T, db *gorm.DB) {
			inv := invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0)
			inv.InvitedBy = by
			if err := db.Create(&inv).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	newcomer := model.TeamMember{ID: 300, TeamID: 1, UserID: 20, CreatedAt: teamT0}
	unchanged := func(t *testing.T, db *gorm.DB) {
		if v := teamVersion(db, 1); v != 1 {
			t.Errorf("the refused change left version %d, want 1", v)
		}
	}
	cases := []struct {
		name   string
		seed   func(t *testing.T, db *gorm.DB)
		change func(tx *gorm.DB) error
		op     func(r Repository) error
		want   error
		check  func(t *testing.T, db *gorm.DB)
	}{
		{
			name:   "a rename from a version replaced meanwhile",
			change: func(tx *gorm.DB) error { return tx.Exec("UPDATE teams SET version = version + 1 WHERE id = 1").Error },
			op: func(r Repository) error {
				_, err := r.RenameTeam(ctx, 1, 11, 1, "x", act())
				if !isConflict(err, 2) {
					return fmt.Errorf("%v, not a conflict at 2", err)
				}
				return nil
			},
			check: func(t *testing.T, db *gorm.DB) {
				if tm, _ := New(&mockDB{db: db}).GetTeam(ctx, 1); tm.Name != "core" || tm.Version != 2 {
					t.Errorf("the team is %+v, want core at version 2", tm)
				}
			},
		},
		{
			name:   "a member removed meanwhile renaming",
			change: removeMember(11),
			op:     func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 11, 1, "x", act()); return err },
			want:   ErrNotFound,
			check:  unchanged,
		},
		{
			name:   "a member removed meanwhile sharing a workspace",
			change: removeMember(11),
			op:     func(r Repository) error { _, _, err := r.SetWorkspaceShare(ctx, share(11), 1, act()); return err },
			want:   ErrNotFound,
			check: func(t *testing.T, db *gorm.DB) {
				if n := countTeamRows(db, &model.WorkspaceShare{}, "user_id = ?", 11); n != 0 {
					t.Errorf("the removed member has %d shares, want none", n)
				}
				unchanged(t, db)
			},
		},
		{
			name:   "accepting an invitation whose inviter was removed meanwhile",
			seed:   invite(1, 11),
			change: removeMember(11),
			op:     func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, newcomer, teamT0, act()) },
			want:   ErrInvitationNotPending,
			check: func(t *testing.T, db *gorm.DB) {
				if n := countTeamRows(db, &model.TeamMember{}, "user_id = ?", newcomer.UserID); n != 0 {
					t.Error("the invitee joined")
				}
				unchanged(t, db)
			},
		},
		{
			name: "accepting an invitation revoked meanwhile",
			seed: invite(1, 10),
			change: func(tx *gorm.DB) error {
				return tx.Model(&model.TeamInvitation{}).Where("id = 1").Update("revoked_at", teamT0).Error
			},
			op:   func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, newcomer, teamT0, act()) },
			want: ErrInvitationNotPending,
		},
		{
			name: "accepting into a team deleted meanwhile",
			seed: invite(1, 10),
			change: func(tx *gorm.DB) error {
				for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}} {
					if err := tx.Where("team_id = 1").Delete(m).Error; err != nil {
						return err
					}
				}
				return tx.Where("id = 1").Delete(&model.Team{}).Error
			},
			op:   func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, newcomer, teamT0, act()) },
			want: ErrNotFound,
			check: func(t *testing.T, db *gorm.DB) {
				if n := countTeamRows(db, &model.TeamMember{}, "team_id = 1"); n != 0 {
					t.Errorf("the deleted team has %d members, want none", n)
				}
			},
		},
		{
			name: "deleting a team joined and shared with meanwhile",
			change: func(tx *gorm.DB) error {
				m, s := newcomer, share(newcomer.UserID)
				if err := tx.Create(&m).Error; err != nil {
					return err
				}
				return tx.Create(&s).Error
			},
			op: func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) },
			check: func(t *testing.T, db *gorm.DB) {
				for _, m := range []any{&model.TeamMember{}, &model.WorkspaceShare{}} {
					if n := countTeamRows(db, m, "team_id = 1"); n != 0 {
						t.Errorf("%T has %d rows of the deleted team, want none", m, n)
					}
				}
			},
		},
		{
			// The count of members left is read under the lock: the team does
			// not outlive its last member.
			name:   "the last but one leaving while the other left meanwhile",
			change: removeMember(11),
			op: func(r Repository) error {
				_, deleted, err := r.RemoveTeamMember(ctx, 1, 10, 10, 1, act(), 0)
				if err == nil && !deleted {
					return errors.New("the team was kept with no member")
				}
				return err
			},
			check: func(t *testing.T, db *gorm.DB) {
				if n := countTeamRows(db, &model.Team{}, "id = 1"); n != 0 {
					t.Error("a team with no member is left")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				if err := db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, CreatedAt: teamT0}).Error; err != nil {
					t.Fatal(err)
				}
				if c.seed != nil {
					c.seed(t, db)
				}
				err := whileLocked(t, db, c.change, func() error { return c.op(r) })
				if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
					t.Errorf("returned %v, want %v", err, c.want)
				}
				if c.check != nil {
					c.check(t, db)
				}
			})
		})
	}
}

// An invitation is accepted only while its inviter is in the team, and only
// into its own team.
func TestAcceptingNeedsAnInviterStillInTheTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
		db.Create(&model.Team{ID: 2, Name: "other", Version: 1})
		for i, c := range []struct {
			name     string
			by       int64
			memberOf int64
			want     error
		}{
			{"the invitation of a member", 11, 1, nil},
			{"the invitation of someone who left", 99, 1, ErrInvitationNotPending},
			{"an invitation accepted into another team", 10, 2, ErrNotFound},
			{"an invitation accepted into a team that does not exist", 10, 77, ErrNotFound},
		} {
			id := int64(i + 1)
			inv := invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0)
			inv.InvitedBy = c.by
			if err := db.Create(&inv).Error; err != nil {
				t.Fatal(err)
			}
			m := model.TeamMember{ID: 200 + id, TeamID: c.memberOf, UserID: 200 + id}
			err := r.AcceptTeamInvitation(ctx, id, m, teamT0, act())
			if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Errorf("%s returned %v, want %v", c.name, err, c.want)
			}
			joined := countTeamRows(db, &model.TeamMember{}, "user_id = ?", m.UserID) == 1
			if joined != (c.want == nil) {
				t.Errorf("after %s the invitee joined: %v", c.name, joined)
			}
		}
	})
}

// A deleted workspace takes its team shares with it, and nobody else's.
func TestDeleteWorkspace_DeletesItsTeamShares(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.WorkspaceShare{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, id := range []int64{100, 101} {
		if err := db.Create(&model.Workspace{ID: id, UserID: 10, Name: "w"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.WorkspaceShare{ID: id, TeamID: 1, WorkspaceID: id, UserID: 10}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := New(&mockDB{db: db}).DeleteWorkspace(context.Background(), 100, 10); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	var left []int64
	db.Model(&model.WorkspaceShare{}).Order("workspace_id").Pluck("workspace_id", &left)
	if fmt.Sprint(left) != "[101]" {
		t.Errorf("shares left are of workspaces %v, want only [101]", left)
	}
}

// Accepting refuses an invitation revoked, or at the very moment it expires,
// whatever the caller checked before.
func TestAcceptingRefusesARevokedOrExpiredInvitation(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		revoked, expiring := invitation(1, "a@x.io", teamT0), invitation(2, "b@x.io", teamT0)
		db.Create(&revoked)
		db.Create(&expiring)
		if _, err := r.RevokeTeamInvitation(ctx, 1, 10, 1, 1, act()); err != nil {
			t.Fatal(err)
		}
		m := model.TeamMember{ID: 40, TeamID: 1, UserID: 40}
		if err := r.AcceptTeamInvitation(ctx, 1, m, teamT0, act()); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting a revoked invitation returned %v, want ErrInvitationNotPending", err)
		}
		if err := r.AcceptTeamInvitation(ctx, 2, m, expiring.ExpiresAt, act()); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting at the moment it expires returned %v, want ErrInvitationNotPending", err)
		}
		if err := r.AcceptTeamInvitation(ctx, 2, m, expiring.ExpiresAt.Add(-time.Second), act()); err != nil {
			t.Errorf("accepting a second before it expires returned %v", err)
		}
	})
}
