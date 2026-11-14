// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errTeamDB = errors.New("database unavailable")

var teamT0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

var teamTables = []any{&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamMemory{}}

func openTeamDB(t *testing.T, d gorm.Dialector) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(d, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// teamDBs is a migrated SQLite file, with the pragmas the app opens it with,
// and a database of its own on the Postgres AGENTRQ_TEST_POSTGRES_DSN names,
// when it is set.
func teamDBs(t *testing.T) map[string]*gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentrq.db")
	dbs := map[string]*gorm.DB{"sqlite": openTeamDB(t, sqlite.Open("file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		admin := openTeamDB(t, postgres.Open(dsn))
		name := fmt.Sprintf("teams_repo_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
		dbs["postgres"] = openTeamDB(t, postgres.Open(dsn+" dbname="+name))
	}
	for name, db := range dbs {
		if err := db.AutoMigrate(teamTables...); err != nil {
			t.Fatalf("%s: migrate: %v", name, err)
		}
	}
	return dbs
}

// forEachTeamDB runs fn on each database, against a repository seeded with
// one team, 1, whose only owner is user 10.
func forEachTeamDB(t *testing.T, fn func(t *testing.T, db *gorm.DB, r Repository)) {
	for name, db := range teamDBs(t) {
		t.Run(name, func(t *testing.T) {
			r := New(&mockDB{db: db})
			owner := model.TeamMember{ID: 1, TeamID: 1, UserID: 10, Role: model.TeamRoleOwner, CreatedAt: teamT0}
			if err := r.CreateTeam(context.Background(), model.Team{ID: 1, Name: "core", CreatedAt: teamT0}, owner); err != nil {
				t.Fatal(err)
			}
			fn(t, db, r)
		})
	}
}

// failTeamStatement makes the statement of kind on table fail with
// errTeamDB, after letting the first `skip` of them through.
func failTeamStatement(t *testing.T, db *gorm.DB, kind, table string, skip int) {
	t.Helper()
	var seen atomic.Int64
	fail := func(tx *gorm.DB) {
		if tx.Statement.Table == table && seen.Add(1) > int64(skip) {
			_ = tx.AddError(errTeamDB)
		}
	}
	name := fmt.Sprintf("test:team-fail-%s-%s", kind, table)
	var err error
	switch kind {
	case "create":
		err = db.Callback().Create().Before("gorm:create").Register(name, fail)
	case "update":
		err = db.Callback().Update().Before("gorm:update").Register(name, fail)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, fail)
	case "query":
		err = db.Callback().Query().Before("gorm:query").Register(name, fail)
	case "row":
		err = db.Callback().Row().Before("gorm:row").Register(name, fail)
	case "raw":
		// Exec has no table: every one fails.
		err = db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) { _ = tx.AddError(errTeamDB) })
	}
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}

func invitation(id int64, email string, created time.Time) model.TeamInvitation {
	return model.TeamInvitation{
		ID: id, TeamID: 1, Email: email, Role: model.TeamRoleMember, InvitedBy: 10,
		TokenHash: fmt.Sprintf("%064d", id), ExpiresAt: created.Add(7 * 24 * time.Hour), CreatedAt: created,
	}
}

func TestTeamIndexesCoverTheirColumns(t *testing.T) {
	db := teamDBs(t)["sqlite"]
	for name, want := range map[string]string{
		"uk_team_members_team_id_user_id":            "(`team_id`,`user_id`)",
		"idx_team_members_user_id":                   "(`user_id`)",
		"idx_team_invitations_team_id_created_at":    "(`team_id`,`created_at`)",
		"idx_team_invitations_invited_by_created_at": "(`invited_by`,`created_at`)",
		"idx_team_invitations_email":                 "(`email`)",
		"uk_team_invitations_token_hash":             "(`token_hash`)",
		"uk_team_invitations_pending_email":          "(`team_id`,`email`) WHERE accepted_at IS NULL AND revoked_at IS NULL",
		"uk_workspace_shares_team_id_workspace_id":   "(`team_id`,`workspace_id`)",
		"idx_workspace_shares_team_id_user_id":       "(`team_id`,`user_id`)",
		"idx_workspace_shares_workspace_id":          "(`workspace_id`)",
		"uk_team_memories_team_id_name":              "(`team_id`,`name`)",
	} {
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&ddl).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(ddl, want) {
			t.Errorf("index %s is %q, want it on %s", name, ddl, want)
		}
	}
}

func TestTeamsListedForAMemberWithRoleAndCount(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		if err := r.CreateTeam(ctx, model.Team{ID: 2, Name: "alpha", CreatedAt: teamT0}, model.TeamMember{ID: 2, TeamID: 2, UserID: 11, Role: model.TeamRoleOwner}); err != nil {
			t.Fatal(err)
		}
		db.Create(&model.TeamMember{ID: 3, TeamID: 2, UserID: 10, Role: model.TeamRoleMember})
		teams, err := r.ListTeamsForUser(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, tm := range teams {
			got = append(got, fmt.Sprintf("%s/%s/%d", tm.Name, tm.Role, tm.MemberCount))
		}
		if strings.Join(got, " ") != "alpha/member/2 core/owner/1" {
			t.Errorf("user 10's teams are %v, want alpha as member of 2 then core as owner of 1", got)
		}
		if _, err := r.GetTeam(ctx, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetTeam of an unknown team returned %v, want ErrNotFound", err)
		}
		if err := r.RenameTeam(ctx, 99, "x"); !errors.Is(err, ErrNotFound) {
			t.Errorf("RenameTeam of an unknown team returned %v, want ErrNotFound", err)
		}
		if err := r.DeleteTeam(ctx, 99, 10); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteTeam of an unknown team returned %v, want ErrNotFound", err)
		}
	})
}

func TestDeleteTeamTakesEverythingOfTheTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, Role: model.TeamRoleMember})
		db.Create(&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 5, UserID: 10, GroupName: "g"})
		db.Create(&model.TeamMemory{ID: 1, TeamID: 1, Name: "memory.md", Content: "x", UpdatedBy: 10})
		if err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), teamT0.Add(-24*time.Hour), 10, 0); err != nil {
			t.Fatal(err)
		}
		// Another team's rows stay.
		db.Create(&model.TeamMember{ID: 9, TeamID: 2, UserID: 11, Role: model.TeamRoleOwner})
		if err := r.DeleteTeam(ctx, 1, 12); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleting a team the caller is not in returned %v, want ErrNotFound", err)
		}
		db.Model(&model.TeamMember{}).Where("id = 2").Update("role", model.TeamRoleAdmin)
		if err := r.DeleteTeam(ctx, 1, 11); !errors.Is(err, ErrTeamRoleForbidden) {
			t.Errorf("an admin deleting the team returned %v, want ErrTeamRoleForbidden", err)
		}
		if err := r.DeleteTeam(ctx, 1, 10); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetTeam(ctx, 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("the deleted team is still read (%v)", err)
		}
		for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamMemory{}} {
			var n int64
			db.Model(m).Where("team_id = 1").Count(&n)
			if n != 0 {
				t.Errorf("%T still has %d rows of the deleted team", m, n)
			}
		}
		var left int64
		db.Model(&model.TeamMember{}).Where("team_id = 2").Count(&left)
		if left != 1 {
			t.Errorf("another team has %d members after deleting team 1, want 1", left)
		}
	})
}

func TestMembersAndTheLastOwner(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.User{ID: 11, Email: "Bob@Example.com", Name: "Bob", Picture: "p"})
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, Role: model.TeamRoleMember, CreatedAt: teamT0.Add(time.Minute)})
		members, err := r.ListTeamMembers(ctx, 1)
		if err != nil || len(members) != 2 || members[1].Name != "Bob" || members[1].Email != "Bob@Example.com" || members[0].UserID != 10 {
			t.Fatalf("members are %+v (%v), want the owner then Bob with his account", members, err)
		}
		if in, err := r.TeamHasMemberWithEmail(ctx, 1, "bob@example.com"); err != nil || !in {
			t.Errorf("TeamHasMemberWithEmail(bob@example.com) = %v, %v; want true whatever the account's case", in, err)
		}
		if in, _ := r.TeamHasMemberWithEmail(ctx, 1, "eve@example.com"); in {
			t.Error("TeamHasMemberWithEmail found eve, who is in no team")
		}
		if err := r.SetTeamMemberRole(ctx, 1, 10, 10, model.TeamRoleAdmin); !errors.Is(err, ErrLastTeamOwner) {
			t.Errorf("demoting the only owner returned %v, want ErrLastTeamOwner", err)
		}
		if err := r.RemoveTeamMember(ctx, 1, 10, 10); !errors.Is(err, ErrLastTeamOwner) {
			t.Errorf("the only owner leaving returned %v, want ErrLastTeamOwner", err)
		}
		if err := r.SetTeamMemberRole(ctx, 1, 10, 10, model.TeamRoleOwner); err != nil {
			t.Errorf("keeping the only owner an owner returned %v", err)
		}
		if err := r.SetTeamMemberRole(ctx, 1, 10, 11, model.TeamRoleOwner); err != nil {
			t.Fatal(err)
		}
		if err := r.SetTeamMemberRole(ctx, 1, 10, 10, model.TeamRoleMember); err != nil {
			t.Errorf("demoting one of two owners returned %v", err)
		}
		if err := r.SetTeamMemberRole(ctx, 1, 11, 99, model.TeamRoleMember); !errors.Is(err, ErrNotFound) {
			t.Errorf("changing a stranger's role returned %v, want ErrNotFound", err)
		}
		if err := r.SetTeamMemberRole(ctx, 1, 99, 10, model.TeamRoleMember); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger changing a role returned %v, want ErrNotFound", err)
		}
		if err := r.SetTeamMemberRole(ctx, 99, 11, 10, model.TeamRoleMember); !errors.Is(err, ErrNotFound) {
			t.Errorf("changing a role in an unknown team returned %v, want ErrNotFound", err)
		}
		if err := r.RemoveTeamMember(ctx, 1, 11, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("removing a stranger returned %v, want ErrNotFound", err)
		}
		if err := r.RemoveTeamMember(ctx, 1, 99, 10); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger removing a member returned %v, want ErrNotFound", err)
		}
		// Removing a member takes their shares with this team, and only those.
		db.Create(&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 5, UserID: 10, GroupName: "g"})
		db.Create(&model.WorkspaceShare{ID: 2, TeamID: 2, WorkspaceID: 5, UserID: 10, GroupName: "g"})
		db.Create(&model.WorkspaceShare{ID: 3, TeamID: 1, WorkspaceID: 6, UserID: 11, GroupName: "g"})
		if err := r.RemoveTeamMember(ctx, 1, 11, 10); err != nil {
			t.Fatal(err)
		}
		var ids []int64
		db.Model(&model.WorkspaceShare{}).Order("id").Pluck("id", &ids)
		if fmt.Sprint(ids) != "[2 3]" {
			t.Errorf("shares left after removing user 10 from team 1 are %v, want [2 3]", ids)
		}
	})
}

// Two owners demoting each other at once: the team row's lock lets one see
// the other's change, so one owner remains.
func TestOwnersDemotingEachOtherAtOnceKeepOne(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, Role: model.TeamRoleOwner})
		for round := range 10 {
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, uid := range []int64{10, 11} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = r.SetTeamMemberRole(context.Background(), 1, uid, uid, model.TeamRoleMember)
				}()
			}
			wg.Wait()
			var owners int64
			db.Model(&model.TeamMember{}).Where("team_id = 1 AND role = ?", model.TeamRoleOwner).Count(&owners)
			if owners != 1 {
				t.Fatalf("round %d: %d owners left (errors %v), want 1", round, owners, errs)
			}
			lost := 0
			for _, err := range errs {
				if errors.Is(err, ErrLastTeamOwner) {
					lost++
				} else if err != nil {
					t.Fatalf("round %d: a demotion failed with %v, want only ErrLastTeamOwner", round, err)
				}
			}
			if lost != 1 {
				t.Fatalf("round %d: %d demotions were refused, want 1", round, lost)
			}
			db.Model(&model.TeamMember{}).Where("team_id = 1").Update("role", model.TeamRoleOwner)
		}
	})
}

func TestInvitationsPendingAcceptedAndRevoked(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		since := teamT0.Add(-24 * time.Hour)
		if err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(2, "a@x.io", teamT0), since, 10, 0); !errors.Is(err, ErrInvitationPending) {
			t.Errorf("a second pending invitation for a@x.io returned %v, want ErrInvitationPending", err)
		}
		// Once expired, the old one is revoked to make way.
		later := teamT0.Add(8 * 24 * time.Hour)
		if err := r.CreateTeamInvitation(ctx, invitation(3, "a@x.io", later), later.Add(-24*time.Hour), 10, 0); err != nil {
			t.Fatalf("inviting again after the first expired returned %v", err)
		}
		if old, _ := r.GetTeamInvitation(ctx, 1); old.RevokedAt == nil || !old.RevokedAt.Equal(later) {
			t.Errorf("the expired invitation's revoked_at is %v, want %v", old.RevokedAt, later)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(4, "b@x.io", later), later.Add(-24*time.Hour), 10, 0); err != nil {
			t.Fatal(err)
		}
		list, err := r.ListTeamInvitations(ctx, 1, later)
		if err != nil || len(list) != 2 || list[0].ID != 4 || list[1].ID != 3 {
			t.Errorf("open invitations are %+v (%v), want 4 then 3", list, err)
		}
		mine, err := r.ListInvitationsForEmail(ctx, "a@x.io", later)
		if err != nil || len(mine) != 1 || mine[0].ID != 3 {
			t.Errorf("a@x.io's open invitations are %+v (%v), want only 3", mine, err)
		}
		if got, err := r.GetTeamInvitationByTokenHash(ctx, invitation(3, "", later).TokenHash); err != nil || got.ID != 3 {
			t.Errorf("by token hash found %d (%v), want 3", got.ID, err)
		}
		if _, err := r.GetTeamInvitation(ctx, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetTeamInvitation(99) returned %v, want ErrNotFound", err)
		}

		member := model.TeamMember{ID: 20, TeamID: 1, UserID: 11, Role: model.TeamRoleMember, CreatedAt: later}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later.Add(8*24*time.Hour)); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting an expired invitation returned %v, want ErrInvitationNotPending", err)
		}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later); err != nil {
			t.Fatal(err)
		}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting twice returned %v, want ErrInvitationNotPending", err)
		}
		if got, _ := r.GetTeamInvitation(ctx, 3); got.AcceptedAt == nil || got.AcceptedBy != 11 {
			t.Errorf("the accepted invitation is %+v, want accepted by 11", got)
		}
		// A member accepting again keeps their role.
		db.Model(&model.TeamMember{}).Where("user_id = 11").Update("role", model.TeamRoleAdmin)
		if err := r.AcceptTeamInvitation(ctx, 4, model.TeamMember{ID: 21, TeamID: 1, UserID: 11, Role: model.TeamRoleMember}, later); err != nil {
			t.Fatal(err)
		}
		if m, _ := r.GetTeamMember(ctx, 1, 11); m.Role != model.TeamRoleAdmin {
			t.Errorf("an admin accepting another invitation became %q, want admin still", m.Role)
		}

		if err := r.CreateTeamInvitation(ctx, invitation(5, "c@x.io", later), later.Add(-24*time.Hour), 10, 0); err != nil {
			t.Fatal(err)
		}
		if err := r.RevokeTeamInvitation(ctx, 2, 5, later); !errors.Is(err, ErrNotFound) {
			t.Errorf("revoking through another team returned %v, want ErrNotFound", err)
		}
		if err := r.RevokeTeamInvitation(ctx, 1, 5, later); err != nil {
			t.Fatal(err)
		}
		if err := r.RevokeTeamInvitation(ctx, 1, 5, later); !errors.Is(err, ErrNotFound) {
			t.Errorf("revoking twice returned %v, want ErrNotFound", err)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(6, "a@x.io", later), later.Add(-24*time.Hour), 10, 0); err != nil {
			t.Errorf("inviting an address whose invitation was accepted returned %v", err)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(7, "a@x.io", later), later, 10, 0); err != nil && !errors.Is(err, ErrInvitationPending) {
			t.Errorf("a duplicate returned %v, want ErrInvitationPending", err)
		}
		unknownTeam := invitation(8, "z@x.io", later)
		unknownTeam.TeamID = 99
		if err := r.CreateTeamInvitation(ctx, unknownTeam, later, 10, 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("inviting to an unknown team returned %v, want ErrNotFound", err)
		}
	})
}

// The cap counts every invitation created in the window, revoked ones too,
// and only the team's own.
func TestInvitationCapCountsTheWindow(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.Team{ID: 2, Name: "other"})
		other := invitation(50, "o@x.io", teamT0)
		other.TeamID = 2
		db.Create(&other)
		since := teamT0.Add(-24 * time.Hour)
		// One just outside the window does not count.
		old := invitation(1, "old@x.io", since)
		db.Create(&old)
		for i := int64(2); i <= 4; i++ {
			if err := r.CreateTeamInvitation(ctx, invitation(i, fmt.Sprintf("u%d@x.io", i), teamT0), since, 3, 0); err != nil {
				t.Fatalf("invitation %d of 3 returned %v", i-1, err)
			}
		}
		if err := r.RevokeTeamInvitation(ctx, 1, 4, teamT0); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(5, "u5@x.io", teamT0), since, 3, 0); !errors.Is(err, ErrInvitationCapReached) {
			t.Errorf("a 4th invitation with a cap of 3 returned %v, want ErrInvitationCapReached", err)
		}
		if err := r.CreateTeamInvitation(ctx, invitation(5, "u5@x.io", teamT0), since, 4, 0); err != nil {
			t.Errorf("a 4th invitation with a cap of 4 returned %v", err)
		}
	})
}

// Requests at once never pass the cap together: the team row is locked from
// the count to the insert.
func TestInvitationCapHoldsUnderParallelRequests(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		const limit, requests = 5, 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = r.CreateTeamInvitation(context.Background(), invitation(int64(i+1), fmt.Sprintf("u%d@x.io", i), teamT0), teamT0.Add(-24*time.Hour), limit, 0)
			}()
		}
		wg.Wait()
		ok, capped := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrInvitationCapReached):
				capped++
			default:
				t.Errorf("a request failed with %v, want only success or ErrInvitationCapReached", err)
			}
		}
		var n int64
		db.Model(&model.TeamInvitation{}).Count(&n)
		if ok != limit || capped != requests-limit || n != limit {
			t.Errorf("%d created, %d refused, %d stored; want %d, %d, %d", ok, capped, n, limit, requests-limit, limit)
		}
	})
}

// inviter seeds user 10, who sends the invitations, and team 2, a second
// team for them to invite to.
func inviter(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.User{ID: 10, Name: "Ten", Email: "ten@x.io"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Team{ID: 2, Name: "other", CreatedAt: teamT0}).Error; err != nil {
		t.Fatal(err)
	}
}

func inTeam(inv model.TeamInvitation, teamID int64) model.TeamInvitation {
	inv.TeamID = teamID
	return inv
}

// The per-account cap counts the inviter's invitations in every team in the
// window, revoked ones too, and nobody else's; 0 is no such cap.
func TestUserInvitationCapCountsEveryTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		inviter(t, db)
		since := teamT0.Add(-24 * time.Hour)
		old := invitation(1, "old@x.io", since) // just outside the window
		db.Create(&old)
		someoneElse := invitation(2, "else@x.io", teamT0)
		someoneElse.InvitedBy = 11
		db.Create(&someoneElse)
		for i, team := range []int64{1, 2, 1} {
			id := int64(10 + i)
			if err := r.CreateTeamInvitation(ctx, inTeam(invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0), team), since, 10, 3); err != nil {
				t.Fatalf("invitation %d of 3 returned %v", i+1, err)
			}
		}
		if err := r.RevokeTeamInvitation(ctx, 2, 11, teamT0); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateTeamInvitation(ctx, inTeam(invitation(20, "u20@x.io", teamT0), 2), since, 10, 3); !errors.Is(err, ErrUserInvitationCapReached) {
			t.Errorf("a 4th invitation with a cap of 3 across teams returned %v, want ErrUserInvitationCapReached", err)
		}
		// The team's cap answers first when both are reached.
		if err := r.CreateTeamInvitation(ctx, invitation(20, "u20@x.io", teamT0), since, 3, 3); !errors.Is(err, ErrInvitationCapReached) {
			t.Errorf("both caps reached returned %v, want ErrInvitationCapReached", err)
		}
		if err := r.CreateTeamInvitation(ctx, inTeam(invitation(20, "u20@x.io", teamT0), 2), since, 10, 0); err != nil {
			t.Errorf("an invitation with no per-account cap returned %v", err)
		}
	})
}

// An invitation the inviter sent to another team, committed while this one
// waited for their lock, counts: the count runs under the inviter's lock.
func TestUserInvitationCapReadsUnderTheInvitersLock(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		inviter(t, db)
		since := teamT0.Add(-24 * time.Hour)
		tx := db.Begin()
		if err := tx.Exec("UPDATE users SET id = id WHERE id = ?", 10).Error; err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			done <- r.CreateTeamInvitation(context.Background(), invitation(2, "b@x.io", teamT0), since, 10, 1)
		}()
		time.Sleep(200 * time.Millisecond) // waiting for the lock by now
		other := inTeam(invitation(1, "a@x.io", teamT0), 2)
		if err := tx.Create(&other).Error; err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, ErrUserInvitationCapReached) {
			t.Errorf("an invitation past the cap once the other committed returned %v, want ErrUserInvitationCapReached", err)
		}
	})
}

// One account's requests at once, to two teams, never pass the per-account
// cap together.
func TestUserInvitationCapHoldsUnderParallelRequests(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		inviter(t, db)
		const limit, requests = 5, 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				inv := inTeam(invitation(int64(i+1), fmt.Sprintf("u%d@x.io", i), teamT0), int64(1+i%2))
				errs[i] = r.CreateTeamInvitation(context.Background(), inv, teamT0.Add(-24*time.Hour), 100, limit)
			}()
		}
		wg.Wait()
		ok, capped := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrUserInvitationCapReached):
				capped++
			default:
				t.Errorf("a request failed with %v, want only success or ErrUserInvitationCapReached", err)
			}
		}
		var n int64
		db.Model(&model.TeamInvitation{}).Count(&n)
		if ok != limit || capped != requests-limit || n != limit {
			t.Errorf("%d created, %d refused, %d stored; want %d, %d, %d", ok, capped, n, limit, requests-limit, limit)
		}
	})
}

func TestUserInvitationCapFailures(t *testing.T) {
	since := teamT0.Add(-24 * time.Hour)
	t.Run("an unknown inviter", func(t *testing.T) {
		forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
			if err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), since, 10, 5); !errors.Is(err, ErrNotFound) {
				t.Errorf("an inviter with no account returned %v, want ErrNotFound", err)
			}
		})
	})
	t.Run("locking the inviter", func(t *testing.T) {
		forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
			if err := db.Migrator().DropTable(&model.User{}); err != nil {
				t.Fatal(err)
			}
			if err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), since, 10, 5); err == nil {
				t.Error("an invitation with no users table to lock returned nil")
			}
		})
	})
	t.Run("counting the inviter's invitations", func(t *testing.T) {
		forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
			inviter(t, db)
			failTeamStatement(t, db, "query", "team_invitations", 1)
			if err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), since, 10, 5); !errors.Is(err, errTeamDB) {
				t.Errorf("a failed count returned %v, want %v", err, errTeamDB)
			}
		})
	})
}

func TestOnlyOneParallelAcceptWins(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		if err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), teamT0.Add(-24*time.Hour), 10, 0); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var won, lost atomic.Int64
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := model.TeamMember{ID: int64(100 + i), TeamID: 1, UserID: int64(20 + i), Role: model.TeamRoleMember}
				switch err := r.AcceptTeamInvitation(context.Background(), 1, m, teamT0); {
				case err == nil:
					won.Add(1)
				case errors.Is(err, ErrInvitationNotPending):
					lost.Add(1)
				default:
					t.Errorf("an accept failed with %v", err)
				}
			}()
		}
		wg.Wait()
		var members int64
		db.Model(&model.TeamMember{}).Count(&members)
		if won.Load() != 1 || lost.Load() != 7 || members != 2 {
			t.Errorf("%d accepts won, %d lost, %d members; want 1, 7 and 2", won.Load(), lost.Load(), members)
		}
	})
}

// seedTeamWorkspaces gives user 10 (Alice) a shared workspace 100 with a fork
// 101, an archived fork 102, an unshared workspace 103 and an archived shared
// one 104; user 11 (Bob), in the team, a shared workspace 200.
func seedTeamWorkspaces(t *testing.T, db *gorm.DB) {
	t.Helper()
	archived := teamT0
	db.Create(&model.User{ID: 10, Email: "alice@x.io", Name: "Alice", Picture: "a.png"})
	db.Create(&model.User{ID: 11, Email: "bob@x.io", Name: "Bob"})
	for _, w := range []model.Workspace{
		{ID: 100, UserID: 10, Name: "backend"},
		{ID: 101, UserID: 10, Name: "backend-fork", ForkOfID: 100},
		{ID: 102, UserID: 10, Name: "old-fork", ForkOfID: 100, ArchivedAt: &archived},
		{ID: 103, UserID: 10, Name: "private"},
		{ID: 104, UserID: 10, Name: "archived", ArchivedAt: &archived},
		{ID: 200, UserID: 11, Name: "docs"},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, Role: model.TeamRoleMember})
	for _, s := range []model.WorkspaceShare{
		{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: 10, GroupName: "Backend"},
		{ID: 2, TeamID: 1, WorkspaceID: 104, UserID: 10, GroupName: "Old"},
		{ID: 3, TeamID: 1, WorkspaceID: 200, UserID: 11, GroupName: "Docs"},
		// A share of a workspace since deleted.
		{ID: 4, TeamID: 1, WorkspaceID: 999, UserID: 11, GroupName: "Gone"},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanReadWorkspaceInTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		db.Create(&model.Team{ID: 2, Name: "other"})
		db.Create(&model.TeamMember{ID: 9, TeamID: 2, UserID: 12, Role: model.TeamRoleOwner})
		for _, c := range []struct {
			name                  string
			user, team, workspace int64
			want                  bool
		}{
			{"a member reads a workspace shared with the team", 11, 1, 100, true},
			{"its owner reads it through the team too", 10, 1, 100, true},
			{"a fork follows its shared parent", 11, 1, 101, true},
			{"an archived fork is not read", 11, 1, 102, false},
			{"an unshared workspace is not read", 11, 1, 103, false},
			{"an archived shared workspace is not read", 11, 1, 104, false},
			{"a deleted shared workspace is not read", 11, 1, 999, false},
			{"someone outside the team reads nothing", 12, 1, 100, false},
			{"a share with one team is not read through another", 12, 2, 100, false},
			{"an unknown team reads nothing", 11, 99, 100, false},
		} {
			got, err := r.CanReadWorkspaceInTeam(context.Background(), c.user, c.team, c.workspace)
			if err != nil || got != c.want {
				t.Errorf("%s: CanReadWorkspaceInTeam(user %d, team %d, workspace %d) = %v, %v; want %v", c.name, c.user, c.team, c.workspace, got, err, c.want)
			}
		}
		// The fork's parent archived: the fork goes with it.
		db.Model(&model.Workspace{}).Where("id = 100").Update("archived_at", teamT0)
		if got, _ := r.CanReadWorkspaceInTeam(context.Background(), 11, 1, 101); got {
			t.Error("a fork of an archived shared workspace is still read")
		}
	})
}

func TestListTeamWorkspacesByGroup(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		rows, err := r.ListTeamWorkspaces(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, w := range rows {
			got = append(got, fmt.Sprintf("%s:%d:%s:%d:%s:%s", w.GroupName, w.ID, w.Name, w.ForkOfID, w.OwnerName, w.OwnerPicture))
		}
		want := []string{"Backend:100:backend:0:Alice:a.png", "Backend:101:backend-fork:100:Alice:a.png", "Docs:200:docs:0:Bob:"}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("team workspaces are %v, want %v", got, want)
		}
	})
}

func TestWorkspaceShares(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.Team{ID: 2, Name: "alpha"})
		db.Create(&model.TeamMember{ID: 2, TeamID: 2, UserID: 10, Role: model.TeamRoleMember})
		s, err := r.SetWorkspaceShare(ctx, model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: 10, GroupName: "one", CreatedAt: teamT0, UpdatedAt: teamT0})
		if err != nil || s.GroupName != "one" || s.ID != 1 || s.TeamName != "core" {
			t.Fatalf("first share is %+v (%v), want share 1 in core", s, err)
		}
		if _, err := r.SetWorkspaceShare(ctx, model.WorkspaceShare{ID: 4, TeamID: 1, WorkspaceID: 101, UserID: 11, GroupName: "g"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("sharing with a team the sharer is not in returned %v, want ErrNotFound", err)
		}
		if _, err := r.SetWorkspaceShare(ctx, model.WorkspaceShare{ID: 5, TeamID: 99, WorkspaceID: 100, UserID: 10, GroupName: "g"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("sharing with an unknown team returned %v, want ErrNotFound", err)
		}
		later := teamT0.Add(time.Hour)
		s, err = r.SetWorkspaceShare(ctx, model.WorkspaceShare{ID: 2, TeamID: 1, WorkspaceID: 100, UserID: 10, GroupName: "two", CreatedAt: later, UpdatedAt: later})
		if err != nil || s.GroupName != "two" || s.ID != 1 || !s.CreatedAt.Equal(teamT0) || !s.UpdatedAt.Equal(later) {
			t.Fatalf("sharing again is %+v (%v), want share 1 with group two, created at %v and updated at %v", s, err, teamT0, later)
		}
		if _, err := r.SetWorkspaceShare(ctx, model.WorkspaceShare{ID: 3, TeamID: 2, WorkspaceID: 100, UserID: 10, GroupName: "g"}); err != nil {
			t.Fatal(err)
		}
		list, err := r.ListWorkspaceShares(ctx, 100, 10)
		if err != nil || len(list) != 2 || list[0].TeamName != "alpha" || list[1].TeamName != "core" || list[1].GroupName != "two" {
			t.Errorf("shares are %+v (%v), want alpha then core in group two", list, err)
		}
		if list, _ := r.ListWorkspaceShares(ctx, 100, 11); len(list) != 0 {
			t.Errorf("another account sees %d shares of workspace 100, want none", len(list))
		}
		if err := r.DeleteWorkspaceShare(ctx, 100, 1, 11); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsharing someone else's workspace returned %v, want ErrNotFound", err)
		}
		if err := r.DeleteWorkspaceShare(ctx, 100, 1, 10); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteWorkspaceShare(ctx, 100, 1, 10); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsharing twice returned %v, want ErrNotFound", err)
		}
	})
}

// Every statement's failure reaches the caller, and a failed transaction
// leaves nothing behind.
func TestTeamStatementFailures(t *testing.T) {
	ctx := context.Background()
	since := teamT0.Add(-24 * time.Hour)
	member := model.TeamMember{ID: 30, TeamID: 1, UserID: 11, Role: model.TeamRoleMember}
	share := model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: 10, GroupName: "g"}
	cases := []struct {
		name        string
		kind, table string
		skip        int
		setup       func(r Repository)
		call        func(r Repository) error
	}{
		{"creating a team", "create", "teams", 0, nil, func(r Repository) error {
			return r.CreateTeam(ctx, model.Team{ID: 2, Name: "x"}, model.TeamMember{ID: 2, TeamID: 2, UserID: 10, Role: model.TeamRoleOwner})
		}},
		{"creating a team's owner", "create", "team_members", 0, nil, func(r Repository) error {
			return r.CreateTeam(ctx, model.Team{ID: 2, Name: "x"}, model.TeamMember{ID: 2, TeamID: 2, UserID: 10, Role: model.TeamRoleOwner})
		}},
		{"listing teams", "row", "teams", 0, nil, func(r Repository) error { _, err := r.ListTeamsForUser(ctx, 10); return err }},
		{"reading a team", "query", "teams", 0, nil, func(r Repository) error { _, err := r.GetTeam(ctx, 1); return err }},
		{"counting members", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.CountTeamMembers(ctx, 1); return err }},
		{"renaming a team", "update", "teams", 0, nil, func(r Repository) error { return r.RenameTeam(ctx, 1, "y") }},
		{"reading the deleter", "query", "team_members", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10) }},
		{"deleting a team's rows", "delete", "team_members", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10) }},
		{"deleting a team", "delete", "teams", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10) }},
		{"listing members", "row", "team_members", 0, nil, func(r Repository) error { _, err := r.ListTeamMembers(ctx, 1); return err }},
		{"finding a member by email", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.TeamHasMemberWithEmail(ctx, 1, "a@x.io"); return err }},
		{"locking the team", "raw", "", 0, nil, func(r Repository) error { return r.SetTeamMemberRole(ctx, 1, 10, 10, model.TeamRoleOwner) }},
		{"reading the caller's role", "query", "team_members", 0, nil, func(r Repository) error {
			return r.SetTeamMemberRole(ctx, 1, 10, 11, model.TeamRoleAdmin)
		}},
		{"reading the member to change", "query", "team_members", 1, nil, func(r Repository) error {
			return r.SetTeamMemberRole(ctx, 1, 10, 11, model.TeamRoleAdmin)
		}},
		{"counting owners", "query", "team_members", 2, nil, func(r Repository) error {
			return r.SetTeamMemberRole(ctx, 1, 10, 10, model.TeamRoleAdmin)
		}},
		{"changing a role", "update", "team_members", 0, nil, func(r Repository) error {
			return r.SetTeamMemberRole(ctx, 1, 10, 11, model.TeamRoleAdmin)
		}},
		{"removing a member", "delete", "team_members", 0, nil, func(r Repository) error { return r.RemoveTeamMember(ctx, 1, 10, 11) }},
		{"removing a member's shares", "delete", "workspace_shares", 0, nil, func(r Repository) error { return r.RemoveTeamMember(ctx, 1, 10, 11) }},
		{"reading the remover's role", "query", "team_members", 0, nil, func(r Repository) error { return r.RemoveTeamMember(ctx, 1, 10, 11) }},
		{"reading the member to remove", "query", "team_members", 1, nil, func(r Repository) error { return r.RemoveTeamMember(ctx, 1, 10, 11) }},
		{"counting owners as one leaves", "query", "team_members", 1, nil, func(r Repository) error { return r.RemoveTeamMember(ctx, 1, 10, 10) }},
		{"counting invitations", "query", "team_invitations", 0, nil, func(r Repository) error {
			return r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}},
		{"revoking an expired invitation", "update", "team_invitations", 0, nil, func(r Repository) error {
			return r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}},
		{"storing an invitation", "create", "team_invitations", 0, nil, func(r Repository) error {
			return r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}},
		{"locking the team to accept", "raw", "", 0, nil, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0) }},
		{"reading the invitation to accept", "query", "team_invitations", 0, nil, func(r Repository) error {
			return r.AcceptTeamInvitation(ctx, 1, member, teamT0)
		}},
		{"reading the inviter", "query", "team_members", 0, func(r Repository) {
			_ = r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0) }},
		{"marking an invitation accepted", "update", "team_invitations", 0, func(r Repository) {
			_ = r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0) }},
		{"adding the accepted member", "create", "team_members", 0, func(r Repository) {
			_ = r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), since, 10, 0)
		}, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0) }},
		{"locking the team to share", "raw", "", 0, nil, func(r Repository) error { _, err := r.SetWorkspaceShare(ctx, share); return err }},
		{"reading the sharer's membership", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.SetWorkspaceShare(ctx, share); return err }},
		{"storing a share", "create", "workspace_shares", 0, nil, func(r Repository) error { _, err := r.SetWorkspaceShare(ctx, share); return err }},
		{"reading the share back", "row", "workspace_shares", 0, nil, func(r Repository) error { _, err := r.SetWorkspaceShare(ctx, share); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, Role: model.TeamRoleMember})
				db.Create(&model.WorkspaceShare{ID: 9, TeamID: 1, WorkspaceID: 5, UserID: 11, GroupName: "g"})
				if c.setup != nil {
					c.setup(r)
				}
				failTeamStatement(t, db, c.kind, c.table, c.skip)
				if err := c.call(r); !errors.Is(err, errTeamDB) {
					t.Errorf("returned %v, want %v", err, errTeamDB)
				}
			})
		})
	}
}

// The team rows sort by what the person reads first; a stable order keeps
// the lists the same on both databases.
func TestTeamListsAreOrdered(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		for i, name := range []string{"b", "a", "a"} {
			id := int64(10 + i)
			db.Create(&model.Team{ID: id, Name: name})
			db.Create(&model.TeamMember{ID: id, TeamID: id, UserID: 50, Role: model.TeamRoleMember})
		}
		teams, _ := r.ListTeamsForUser(context.Background(), 50)
		ids := []int64{}
		for _, tm := range teams {
			ids = append(ids, tm.ID)
		}
		if fmt.Sprint(ids) != "[11 12 10]" {
			t.Errorf("teams are listed as %v, want [11 12 10]: by name, then id", ids)
		}
	})
}
