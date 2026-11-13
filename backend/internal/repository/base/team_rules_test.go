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

// The rules cast of team 1: 10 and 13 own it, 11 is its admin, 12 a member.
const (
	ruleOwner   int64 = 10
	ruleAdmin   int64 = 11
	ruleMember  int64 = 12
	ruleOwner2  int64 = 13
	ruleOutside int64 = 99
)

// seedRuleCast makes team 1's members the rules cast, whatever they were.
func seedRuleCast(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Where("team_id = 1").Delete(&model.TeamMember{}).Error; err != nil {
		t.Fatal(err)
	}
	for i, m := range []struct {
		user int64
		role string
	}{{ruleOwner, model.TeamRoleOwner}, {ruleAdmin, model.TeamRoleAdmin}, {ruleMember, model.TeamRoleMember}, {ruleOwner2, model.TeamRoleOwner}} {
		if err := db.Create(&model.TeamMember{ID: int64(100 + i), TeamID: 1, UserID: m.user, Role: m.role, CreatedAt: teamT0}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func roleOf(db *gorm.DB, teamID, userID int64) string {
	var roles []string
	db.Model(&model.TeamMember{}).Where("team_id = ? AND user_id = ?", teamID, userID).Pluck("role", &roles)
	if len(roles) == 0 {
		return "gone"
	}
	return roles[0]
}

func TestCanGrantTeamRole(t *testing.T) {
	for _, c := range []struct {
		role, granted string
		want          bool
	}{
		{model.TeamRoleOwner, model.TeamRoleOwner, true},
		{model.TeamRoleOwner, model.TeamRoleAdmin, true},
		{model.TeamRoleOwner, model.TeamRoleMember, true},
		{model.TeamRoleAdmin, model.TeamRoleOwner, false},
		{model.TeamRoleAdmin, model.TeamRoleAdmin, true},
		{model.TeamRoleAdmin, model.TeamRoleMember, true},
		{model.TeamRoleMember, model.TeamRoleAdmin, false},
		{model.TeamRoleMember, model.TeamRoleMember, false},
		{"", model.TeamRoleMember, false},
	} {
		if got := CanGrantTeamRole(c.role, c.granted); got != c.want {
			t.Errorf("CanGrantTeamRole(%q, %q) = %v, want %v", c.role, c.granted, got, c.want)
		}
	}
}

// Who may change whose role, and remove whom, decided from the roles in the
// database.
func TestTeamRoleRules(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		for _, c := range []struct {
			name            string
			caller, target  int64
			role            string // "" removes the target
			want            error
			targetRoleAfter string
		}{
			{"a member changing an admin's role", ruleMember, ruleAdmin, model.TeamRoleMember, ErrTeamRoleForbidden, model.TeamRoleAdmin},
			{"an admin making a member an admin", ruleAdmin, ruleMember, model.TeamRoleAdmin, nil, model.TeamRoleAdmin},
			{"an admin making a member an owner", ruleAdmin, ruleMember, model.TeamRoleOwner, ErrTeamRoleForbidden, model.TeamRoleMember},
			{"an admin demoting an owner", ruleAdmin, ruleOwner2, model.TeamRoleMember, ErrTeamRoleForbidden, model.TeamRoleOwner},
			{"an owner making a member an owner", ruleOwner, ruleMember, model.TeamRoleOwner, nil, model.TeamRoleOwner},
			{"an owner demoting another owner", ruleOwner, ruleOwner2, model.TeamRoleMember, nil, model.TeamRoleMember},
			{"a member removing an admin", ruleMember, ruleAdmin, "", ErrTeamRoleForbidden, model.TeamRoleAdmin},
			{"an admin removing an owner", ruleAdmin, ruleOwner2, "", ErrTeamRoleForbidden, model.TeamRoleOwner},
			{"an admin removing a member", ruleAdmin, ruleMember, "", nil, "gone"},
			{"an owner removing another owner", ruleOwner, ruleOwner2, "", nil, "gone"},
			{"a member leaving", ruleMember, ruleMember, "", nil, "gone"},
			{"an owner of two leaving", ruleOwner2, ruleOwner2, "", nil, "gone"},
		} {
			seedRuleCast(t, db)
			var err error
			if c.role == "" {
				err = r.RemoveTeamMember(ctx, 1, c.caller, c.target)
			} else {
				err = r.SetTeamMemberRole(ctx, 1, c.caller, c.target, c.role)
			}
			if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Errorf("%s returned %v, want %v", c.name, err, c.want)
			}
			if got := roleOf(db, 1, c.target); got != c.targetRoleAfter {
				t.Errorf("after %s the target is %s, want %s", c.name, got, c.targetRoleAfter)
			}
		}
	})
}

// whileLocked holds team 1's lock in a transaction of its own and starts op,
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

func setRole(userID int64, role string) func(tx *gorm.DB) error {
	return func(tx *gorm.DB) error {
		return tx.Model(&model.TeamMember{}).Where("team_id = 1 AND user_id = ?", userID).Update("role", role).Error
	}
}

func countTeamRows(db *gorm.DB, m any, where string, args ...any) int64 {
	var n int64
	db.Model(m).Where(where, args...).Count(&n)
	return n
}

// Every check reads the roles and memberships after the team's lock, so a
// change committed while it waited is the one it decides on.
func TestTeamChecksSeeWhatChangedWhileTheyWaitedForTheLock(t *testing.T) {
	ctx := context.Background()
	share := func(user int64) model.WorkspaceShare {
		return model.WorkspaceShare{ID: 500 + user, TeamID: 1, WorkspaceID: 600 + user, UserID: user, GroupName: "g", CreatedAt: teamT0, UpdatedAt: teamT0}
	}
	invite := func(id, by int64, role string) model.TeamInvitation {
		inv := invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0)
		inv.InvitedBy, inv.Role = by, role
		return inv
	}
	newcomer := model.TeamMember{ID: 300, TeamID: 1, UserID: 20, Role: model.TeamRoleMember, CreatedAt: teamT0}
	cases := []struct {
		name   string
		seed   func(t *testing.T, db *gorm.DB)
		change func(tx *gorm.DB) error
		op     func(r Repository) error
		want   error
		check  func(t *testing.T, db *gorm.DB)
	}{
		{
			name:   "an admin demoting a member made an owner meanwhile",
			change: setRole(ruleMember, model.TeamRoleOwner),
			op: func(r Repository) error {
				return r.SetTeamMemberRole(ctx, 1, ruleAdmin, ruleMember, model.TeamRoleMember)
			},
			want: ErrTeamRoleForbidden,
			check: func(t *testing.T, db *gorm.DB) {
				if got := roleOf(db, 1, ruleMember); got != model.TeamRoleOwner {
					t.Errorf("the new owner is %s, want owner", got)
				}
			},
		},
		{
			name:   "an admin removing a member made an owner meanwhile",
			change: setRole(ruleMember, model.TeamRoleOwner),
			op:     func(r Repository) error { return r.RemoveTeamMember(ctx, 1, ruleAdmin, ruleMember) },
			want:   ErrTeamRoleForbidden,
			check: func(t *testing.T, db *gorm.DB) {
				if got := roleOf(db, 1, ruleMember); got != model.TeamRoleOwner {
					t.Errorf("the new owner is %s, want owner", got)
				}
			},
		},
		{
			name:   "an admin made a member meanwhile removing a member",
			change: setRole(ruleAdmin, model.TeamRoleMember),
			op:     func(r Repository) error { return r.RemoveTeamMember(ctx, 1, ruleAdmin, ruleMember) },
			want:   ErrTeamRoleForbidden,
		},
		{
			name:   "an admin made a member meanwhile changing a role",
			change: setRole(ruleAdmin, model.TeamRoleMember),
			op: func(r Repository) error {
				return r.SetTeamMemberRole(ctx, 1, ruleAdmin, ruleMember, model.TeamRoleAdmin)
			},
			want: ErrTeamRoleForbidden,
		},
		{
			name:   "an owner made an admin meanwhile deleting the team",
			change: setRole(ruleOwner, model.TeamRoleAdmin),
			op:     func(r Repository) error { return r.DeleteTeam(ctx, 1, ruleOwner) },
			want:   ErrTeamRoleForbidden,
		},
		{
			name: "a member removed meanwhile sharing a workspace",
			change: func(tx *gorm.DB) error {
				if err := tx.Where("team_id = 1 AND user_id = ?", ruleMember).Delete(&model.TeamMember{}).Error; err != nil {
					return err
				}
				return tx.Where("team_id = 1 AND user_id = ?", ruleMember).Delete(&model.WorkspaceShare{}).Error
			},
			op:   func(r Repository) error { _, err := r.SetWorkspaceShare(ctx, share(ruleMember)); return err },
			want: ErrNotFound,
			check: func(t *testing.T, db *gorm.DB) {
				if n := countTeamRows(db, &model.WorkspaceShare{}, "user_id = ?", ruleMember); n != 0 {
					t.Errorf("the removed member has %d shares, want none", n)
				}
			},
		},
		{
			name: "accepting an invitation of an admin made a member meanwhile",
			seed: func(t *testing.T, db *gorm.DB) {
				inv := invite(1, ruleAdmin, model.TeamRoleAdmin)
				db.Create(&inv)
			},
			change: setRole(ruleAdmin, model.TeamRoleMember),
			op:     func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, newcomer, teamT0) },
			want:   ErrInvitationNotPending,
			check: func(t *testing.T, db *gorm.DB) {
				if got := roleOf(db, 1, newcomer.UserID); got != "gone" {
					t.Errorf("the invitee joined as %s, want not at all", got)
				}
			},
		},
		{
			name: "accepting into a team deleted meanwhile",
			seed: func(t *testing.T, db *gorm.DB) {
				inv := invite(1, ruleOwner, model.TeamRoleMember)
				db.Create(&inv)
			},
			change: func(tx *gorm.DB) error {
				for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}} {
					if err := tx.Where("team_id = 1").Delete(m).Error; err != nil {
						return err
					}
				}
				return tx.Where("id = 1").Delete(&model.Team{}).Error
			},
			op:   func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, newcomer, teamT0) },
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
			op:   func(r Repository) error { return r.DeleteTeam(ctx, 1, ruleOwner) },
			want: nil,
			check: func(t *testing.T, db *gorm.DB) {
				for _, m := range []any{&model.TeamMember{}, &model.WorkspaceShare{}} {
					if n := countTeamRows(db, m, "team_id = 1"); n != 0 {
						t.Errorf("%T has %d rows of the deleted team, want none", m, n)
					}
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				seedRuleCast(t, db)
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

// An invitation is accepted only while its inviter is in the team with a
// role that may grant the invited one, and only into its own team.
func TestAcceptingNeedsAnInviterWhoMayStillGrantTheRole(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedRuleCast(t, db)
		db.Create(&model.Team{ID: 2, Name: "other"})
		for i, c := range []struct {
			name     string
			by       int64
			role     string
			memberOf int64
			want     error
		}{
			{"an admin's invitation of an admin", ruleAdmin, model.TeamRoleAdmin, 1, nil},
			{"an owner's invitation of an owner", ruleOwner, model.TeamRoleOwner, 1, nil},
			{"an admin's invitation of an owner", ruleAdmin, model.TeamRoleOwner, 1, ErrInvitationNotPending},
			{"a member's invitation", ruleMember, model.TeamRoleMember, 1, ErrInvitationNotPending},
			{"the invitation of someone no longer in the team", ruleOutside, model.TeamRoleMember, 1, ErrInvitationNotPending},
			{"an invitation accepted into another team", ruleOwner, model.TeamRoleMember, 2, ErrNotFound},
			{"an invitation accepted into a team that does not exist", ruleOwner, model.TeamRoleMember, 77, ErrNotFound},
		} {
			id := int64(i + 1)
			inv := invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0)
			inv.InvitedBy, inv.Role = c.by, c.role
			if err := db.Create(&inv).Error; err != nil {
				t.Fatal(err)
			}
			m := model.TeamMember{ID: 200 + id, TeamID: c.memberOf, UserID: 200 + id, Role: c.role}
			err := r.AcceptTeamInvitation(ctx, id, m, teamT0)
			if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Errorf("%s returned %v, want %v", c.name, err, c.want)
			}
			joined := roleOf(db, c.memberOf, m.UserID) != "gone"
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
		if err := db.Create(&model.WorkspaceShare{ID: id, TeamID: 1, WorkspaceID: id, UserID: 10, GroupName: "g"}).Error; err != nil {
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
		if err := r.RevokeTeamInvitation(ctx, 1, 1, teamT0); err != nil {
			t.Fatal(err)
		}
		m := model.TeamMember{ID: 40, TeamID: 1, UserID: 40, Role: model.TeamRoleMember}
		if err := r.AcceptTeamInvitation(ctx, 1, m, teamT0); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting a revoked invitation returned %v, want ErrInvitationNotPending", err)
		}
		if err := r.AcceptTeamInvitation(ctx, 2, m, expiring.ExpiresAt); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting at the moment it expires returned %v, want ErrInvitationNotPending", err)
		}
		if err := r.AcceptTeamInvitation(ctx, 2, m, expiring.ExpiresAt.Add(-time.Second)); err != nil {
			t.Errorf("accepting a second before it expires returned %v", err)
		}
	})
}
