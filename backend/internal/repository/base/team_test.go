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

var teamTables = []any{&model.User{}, &model.Workspace{}, &model.Team{}, &model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamManagementActivity{}}

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

// actIDs numbers the activities the tests make, so none share an id.
var actIDs atomic.Int64

// act is a new activity row, made at teamT0, as the controller makes one.
func act() model.TeamManagementActivity {
	return model.TeamManagementActivity{ID: 1_000_000 + actIDs.Add(1), CreatedAt: teamT0}
}

// forEachTeamDB runs fn on each database, against a repository seeded with
// one team, 1 "core" at version 1, whose only member is user 10.
func forEachTeamDB(t *testing.T, fn func(t *testing.T, db *gorm.DB, r Repository)) {
	for name, db := range teamDBs(t) {
		t.Run(name, func(t *testing.T) {
			r := New(&mockDB{db: db})
			first := model.TeamMember{ID: 1, TeamID: 1, UserID: 10, CreatedAt: teamT0}
			a := act()
			a.ActorUserID = 10
			if err := r.CreateTeam(context.Background(), model.Team{ID: 1, Name: "core", CreatedAt: teamT0}, first, a); err != nil {
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
		// Exec has no table: every one after skip fails.
		err = db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
			if seen.Add(1) > int64(skip) {
				_ = tx.AddError(errTeamDB)
			}
		})
	}
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}

func invitation(id int64, email string, created time.Time) model.TeamInvitation {
	return model.TeamInvitation{
		ID: id, TeamID: 1, Email: email, InvitedBy: 10,
		TokenHash: fmt.Sprintf("%064d", id), ExpiresAt: created.Add(7 * 24 * time.Hour), CreatedAt: created,
	}
}

// teamVersion is the team's version now; 0 when it is gone.
func teamVersion(db *gorm.DB, teamID int64) int64 {
	var vs []int64
	db.Model(&model.Team{}).Where("id = ?", teamID).Pluck("version", &vs)
	if len(vs) == 0 {
		return 0
	}
	return vs[0]
}

// activities is the team's activity log, oldest first, one line each:
// "action actor [target user] [target email] [target workspace] details".
func activities(db *gorm.DB, teamID int64) []string {
	var rows []model.TeamManagementActivity
	db.Where("team_id = ?", teamID).Order("created_at ASC, id ASC").Find(&rows)
	out := []string{}
	for _, a := range rows {
		line := fmt.Sprintf("%s %d", a.Action, a.ActorUserID)
		if a.TargetUserID != nil {
			line += fmt.Sprintf(" user:%d", *a.TargetUserID)
		}
		if a.TargetEmail != nil {
			line += " email:" + *a.TargetEmail
		}
		if a.TargetWorkspaceID != nil {
			line += fmt.Sprintf(" ws:%d", *a.TargetWorkspaceID)
		}
		out = append(out, line+" "+a.Details)
	}
	return out
}

// lastActivity is the newest line of activities.
func lastActivity(db *gorm.DB, teamID int64) string {
	all := activities(db, teamID)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// isConflict reports whether err is a version conflict at current.
func isConflict(err error, current int64) bool {
	var vc *TeamVersionConflict
	return errors.As(err, &vc) && vc.Current == current
}

func TestTeamIndexesCoverTheirColumns(t *testing.T) {
	db := teamDBs(t)["sqlite"]
	for name, want := range map[string]string{
		"uk_team_members_team_id_user_id":                   "(`team_id`,`user_id`)",
		"idx_team_members_user_id":                          "(`user_id`)",
		"idx_team_invitations_team_id_created_at":           "(`team_id`,`created_at`)",
		"idx_team_invitations_invited_by_created_at":        "(`invited_by`,`created_at`)",
		"idx_team_invitations_email":                        "(`email`)",
		"uk_team_invitations_token_hash":                    "(`token_hash`)",
		"uk_team_invitations_pending_email":                 "(`team_id`,`email`) WHERE accepted_at IS NULL AND revoked_at IS NULL",
		"uk_workspace_shares_team_id_workspace_id":          "(`team_id`,`workspace_id`)",
		"idx_workspace_shares_team_id_user_id":              "(`team_id`,`user_id`)",
		"idx_workspace_shares_workspace_id":                 "(`workspace_id`)",
		"idx_team_management_activities_team_id_created_at": "(`team_id`,`created_at`)",
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

func TestTeamActivityDetailsIsAnObject(t *testing.T) {
	if got := TeamActivityDetails(nil); got != "{}" {
		t.Errorf("no details are %q, want {}", got)
	}
	if got := TeamActivityDetails(map[string]string{"from": "a", "to": "b"}); got != `{"from":"a","to":"b"}` {
		t.Errorf("details are %q", got)
	}
	if got := (&TeamVersionConflict{Current: 4}).Error(); got != "the team changed: it is at version 4" {
		t.Errorf("the conflict reads %q", got)
	}
}

// A new team starts at version 1 with its creator in it, and records that.
func TestCreateTeamStartsAtVersionOneAndRecordsIt(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		team, err := r.GetTeam(ctx, 1)
		if err != nil || team.Version != 1 || team.Name != "core" {
			t.Errorf("the new team is %+v (%v), want core at version 1", team, err)
		}
		if _, err := r.GetTeamMember(ctx, 1, 10); err != nil {
			t.Errorf("the creator is not in the team: %v", err)
		}
		if got := fmt.Sprint(activities(db, 1)); got != "[team.created 10 {}]" {
			t.Errorf("the log is %s, want the creation by 10", got)
		}
		if _, err := r.GetTeam(ctx, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetTeam of an unknown team returned %v, want ErrNotFound", err)
		}
		if _, err := r.GetTeamMember(ctx, 1, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetTeamMember of a stranger returned %v, want ErrNotFound", err)
		}
	})
}

func TestTeamsListedForAMemberWithVersionAndCount(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		if err := r.CreateTeam(ctx, model.Team{ID: 2, Name: "alpha", CreatedAt: teamT0}, model.TeamMember{ID: 2, TeamID: 2, UserID: 11}, act()); err != nil {
			t.Fatal(err)
		}
		db.Create(&model.TeamMember{ID: 3, TeamID: 2, UserID: 10})
		db.Model(&model.Team{}).Where("id = 2").Update("version", 7)
		teams, err := r.ListTeamsForUser(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, tm := range teams {
			got = append(got, fmt.Sprintf("%s/v%d/%d", tm.Name, tm.Version, tm.MemberCount))
		}
		if strings.Join(got, " ") != "alpha/v7/2 core/v1/1" {
			t.Errorf("user 10's teams are %v, want alpha at version 7 of 2, then core at version 1 of 1", got)
		}
		if n, err := r.CountTeamMembers(ctx, 2); err != nil || n != 2 {
			t.Errorf("team 2 has %d members (%v), want 2", n, err)
		}
	})
}

// Every versioned change: a stranger is not told the team exists, whatever
// version they send; a member on a replaced version is told the version
// now; a change moves the version by one.
func TestTeamChangesCheckTheVersion(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
		for _, c := range []struct {
			name    string
			team    int64
			caller  int64
			version int64
			want    func(error) bool
			wantStr string
		}{
			{"a stranger on the right version", 1, 99, 1, func(err error) bool { return errors.Is(err, ErrNotFound) }, "ErrNotFound"},
			{"a stranger on a wrong version", 1, 99, 5, func(err error) bool { return errors.Is(err, ErrNotFound) }, "ErrNotFound"},
			{"a member on a newer version than there is", 1, 11, 2, func(err error) bool { return isConflict(err, 1) }, "a conflict at 1"},
			{"a member on version 0", 1, 11, 0, func(err error) bool { return isConflict(err, 1) }, "a conflict at 1"},
			{"an unknown team", 99, 10, 1, func(err error) bool { return errors.Is(err, ErrNotFound) }, "ErrNotFound"},
		} {
			if _, err := r.RenameTeam(ctx, c.team, c.caller, c.version, "x", act()); !c.want(err) {
				t.Errorf("%s renaming returned %v, want %s", c.name, err, c.wantStr)
			}
		}
		if v := teamVersion(db, 1); v != 1 {
			t.Errorf("refused changes moved the version to %d", v)
		}
		if n := len(activities(db, 1)); n != 1 {
			t.Errorf("refused changes recorded %d activities, want only the creation", n)
		}

		next, err := r.RenameTeam(ctx, 1, 11, 1, "platform", act())
		if err != nil || next != 2 || teamVersion(db, 1) != 2 {
			t.Fatalf("a rename from version 1 returned %d, %v; the team is at %d; want 2", next, err, teamVersion(db, 1))
		}
		if tm, _ := r.GetTeam(ctx, 1); tm.Name != "platform" {
			t.Errorf("the team is called %q, want platform", tm.Name)
		}
		if got := lastActivity(db, 1); got != `team.renamed 11 {"from":"core","to":"platform"}` {
			t.Errorf("the rename recorded %q", got)
		}
		if _, err := r.RenameTeam(ctx, 1, 10, 1, "again", act()); !isConflict(err, 2) {
			t.Errorf("a second rename from version 1 returned %v, want a conflict at 2", err)
		}
	})
}

// A change that fails after the version moved leaves the team as it was:
// the same version, nothing recorded.
func TestAFailedChangeLeavesTheVersion(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		failTeamStatement(t, db, "create", "team_management_activities", 0)
		if _, err := r.RenameTeam(context.Background(), 1, 10, 1, "x", act()); !errors.Is(err, errTeamDB) {
			t.Fatalf("a rename whose record failed returned %v, want %v", err, errTeamDB)
		}
		if tm, _ := New(&mockDB{db: db}).GetTeam(context.Background(), 1); tm.Version != 1 || tm.Name != "core" {
			t.Errorf("after a failed rename the team is %+v, want core at version 1", tm)
		}
	})
}

// Many members changing the team from one version at once: one wins, every
// other is told the version it made.
func TestConcurrentChangesFromOneVersionOneWins(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		const writers = 8
		for i := range writers {
			db.Create(&model.TeamMember{ID: int64(100 + i), TeamID: 1, UserID: int64(100 + i)})
		}
		for round := range 3 {
			from := teamVersion(db, 1)
			var wg sync.WaitGroup
			errs := make([]error, writers)
			for i := range writers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[i] = r.RenameTeam(context.Background(), 1, int64(100+i), from, fmt.Sprintf("name %d", i), act())
				}()
			}
			wg.Wait()
			won, lost := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					won++
				case isConflict(err, from+1):
					lost++
				default:
					t.Fatalf("round %d: a rename failed with %v, want success or a conflict at %d", round, err, from+1)
				}
			}
			if won != 1 || lost != writers-1 || teamVersion(db, 1) != from+1 {
				t.Fatalf("round %d: %d won, %d lost, version %d; want 1, %d, %d", round, won, lost, teamVersion(db, 1), writers-1, from+1)
			}
		}
		if n := len(activities(db, 1)); n != 4 {
			t.Errorf("%d activities recorded, want the creation and 3 renames", n)
		}
	})
}

func TestDeleteTeamTakesEverythingOfTheTeamAndKeepsTheLog(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
		db.Create(&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 5, UserID: 10})
		v, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, teamT0.Add(-24*time.Hour), 10, 0, act())
		if err != nil {
			t.Fatal(err)
		}
		// Another team's rows stay.
		db.Create(&model.Team{ID: 2, Name: "other", Version: 1})
		db.Create(&model.TeamMember{ID: 9, TeamID: 2, UserID: 11})
		if err := r.DeleteTeam(ctx, 1, 12, v, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleting a team the caller is not in returned %v, want ErrNotFound", err)
		}
		if err := r.DeleteTeam(ctx, 1, 11, 1, act()); !isConflict(err, v) {
			t.Errorf("deleting from a replaced version returned %v, want a conflict at %d", err, v)
		}
		// Any member deletes it.
		if err := r.DeleteTeam(ctx, 1, 11, v, act()); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetTeam(ctx, 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("the deleted team is still read (%v)", err)
		}
		for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}} {
			var n int64
			db.Model(m).Where("team_id = 1").Count(&n)
			if n != 0 {
				t.Errorf("%T still has %d rows of the deleted team", m, n)
			}
		}
		if got := fmt.Sprint(activities(db, 1)); got != `[team.created 10 {} member.invited 10 email:a@x.io {} team.deleted 11 {"name":"core"}]` {
			t.Errorf("the deleted team's log is %s", got)
		}
		var left int64
		db.Model(&model.TeamMember{}).Where("team_id = 2").Count(&left)
		if left != 1 {
			t.Errorf("another team has %d members after deleting team 1, want 1", left)
		}
	})
}

func TestMembersRemovedAndLeaving(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.User{ID: 11, Email: "Bob@Example.com", Name: "Bob", Picture: "p"})
		db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11, CreatedAt: teamT0.Add(time.Minute)})
		db.Create(&model.TeamMember{ID: 3, TeamID: 1, UserID: 12, CreatedAt: teamT0.Add(2 * time.Minute)})
		members, err := r.ListTeamMembers(ctx, 1)
		if err != nil || len(members) != 3 || members[1].Name != "Bob" || members[1].Email != "Bob@Example.com" || members[0].UserID != 10 {
			t.Fatalf("members are %+v (%v), want the creator then Bob with his account", members, err)
		}
		if in, err := r.TeamHasMemberWithEmail(ctx, 1, "bob@example.com"); err != nil || !in {
			t.Errorf("TeamHasMemberWithEmail(bob@example.com) = %v, %v; want true whatever the account's case", in, err)
		}
		if in, _ := r.TeamHasMemberWithEmail(ctx, 1, "eve@example.com"); in {
			t.Error("TeamHasMemberWithEmail found eve, who is in no team")
		}
		if _, _, err := r.RemoveTeamMember(ctx, 1, 99, 10, 1, act(), 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger removing a member returned %v, want ErrNotFound", err)
		}
		if _, _, err := r.RemoveTeamMember(ctx, 1, 10, 99, 1, act(), 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("removing someone not in the team returned %v, want ErrNotFound", err)
		}
		if _, _, err := r.RemoveTeamMember(ctx, 1, 12, 10, 3, act(), 0); !isConflict(err, 1) {
			t.Errorf("removing from a version not yet made returned %v, want a conflict at 1", err)
		}
		// Removing a member takes their shares with this team, and only those.
		db.Create(&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 5, UserID: 10})
		db.Create(&model.WorkspaceShare{ID: 2, TeamID: 2, WorkspaceID: 5, UserID: 10})
		db.Create(&model.WorkspaceShare{ID: 3, TeamID: 1, WorkspaceID: 6, UserID: 11})
		// Any member removes any other.
		next, deleted, err := r.RemoveTeamMember(ctx, 1, 12, 10, 1, act(), 0)
		if err != nil || next != 2 || deleted {
			t.Fatalf("removing 10 returned %d, %v, %v; want version 2, the team kept", next, deleted, err)
		}
		var ids []int64
		db.Model(&model.WorkspaceShare{}).Order("id").Pluck("id", &ids)
		if fmt.Sprint(ids) != "[2 3]" {
			t.Errorf("shares left after removing user 10 from team 1 are %v, want [2 3]", ids)
		}
		if got := lastActivity(db, 1); got != "member.removed 12 user:10 {}" {
			t.Errorf("the removal recorded %q", got)
		}
		next, deleted, err = r.RemoveTeamMember(ctx, 1, 11, 11, 2, act(), 0)
		if err != nil || next != 3 || deleted {
			t.Fatalf("11 leaving returned %d, %v, %v; want version 3, the team kept", next, deleted, err)
		}
		if got := lastActivity(db, 1); got != "member.left 11 {}" {
			t.Errorf("leaving recorded %q", got)
		}
		if n := countTeamRows(db, &model.WorkspaceShare{}, "user_id = 11"); n != 0 {
			t.Errorf("the member who left has %d shares, want none", n)
		}
	})
}

// The last member to go takes the team with them; its log stays.
func TestTheLastMemberLeavingDeletesTheTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 5, UserID: 10})
		v, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, teamT0.Add(-24*time.Hour), 10, 0, act())
		if err != nil {
			t.Fatal(err)
		}
		a := act()
		next, deleted, err := r.RemoveTeamMember(ctx, 1, 10, 10, v, a, a.ID+500)
		if err != nil || !deleted || next != v+1 {
			t.Fatalf("the last member leaving returned %d, %v, %v; want the team deleted", next, deleted, err)
		}
		for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}} {
			if n := countTeamRows(db, m, "team_id = 1"); n != 0 {
				t.Errorf("%T still has %d rows of the team", m, n)
			}
		}
		if n := countTeamRows(db, &model.Team{}, "id = 1"); n != 0 {
			t.Error("the team row is still there")
		}
		got := activities(db, 1)
		if fmt.Sprint(got[len(got)-2:]) != `[member.left 10 {} team.deleted 10 {"name":"core"}]` {
			t.Errorf("the log ends %v, want the leaving then the deletion", got)
		}
		if n := countTeamRows(db, &model.TeamManagementActivity{}, "id = ?", a.ID+500); n != 1 {
			t.Error("the deletion is not recorded under the id given for it")
		}
	})
}

func TestInvitationsPendingAcceptedRevokedAndDeclined(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		since := teamT0.Add(-24 * time.Hour)
		v, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
		if err != nil || v != 2 {
			t.Fatalf("inviting returned %d, %v; want version 2", v, err)
		}
		if got := lastActivity(db, 1); got != "member.invited 10 email:a@x.io {}" {
			t.Errorf("the invitation recorded %q", got)
		}
		if _, err := r.CreateTeamInvitation(ctx, invitation(2, "a@x.io", teamT0), v, since, 10, 0, act()); !errors.Is(err, ErrInvitationPending) {
			t.Errorf("a second pending invitation for a@x.io returned %v, want ErrInvitationPending", err)
		}
		if teamVersion(db, 1) != v {
			t.Error("a refused invitation moved the version")
		}
		if _, err := r.CreateTeamInvitation(ctx, invitation(2, "b@x.io", teamT0), 1, since, 10, 0, act()); !isConflict(err, v) {
			t.Errorf("inviting from a replaced version returned %v, want a conflict at %d", err, v)
		}
		bad := invitation(2, "b@x.io", teamT0)
		bad.InvitedBy = 99
		if _, err := r.CreateTeamInvitation(ctx, bad, v, since, 10, 0, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger inviting returned %v, want ErrNotFound", err)
		}
		// Once expired, the old one is revoked to make way.
		later := teamT0.Add(8 * 24 * time.Hour)
		if v, err = r.CreateTeamInvitation(ctx, invitation(3, "a@x.io", later), v, later.Add(-24*time.Hour), 10, 0, act()); err != nil {
			t.Fatalf("inviting again after the first expired returned %v", err)
		}
		if old, _ := r.GetTeamInvitation(ctx, 1); old.RevokedAt == nil || !old.RevokedAt.Equal(later) {
			t.Errorf("the expired invitation's revoked_at is %v, want %v", old.RevokedAt, later)
		}
		if v, err = r.CreateTeamInvitation(ctx, invitation(4, "b@x.io", later), v, later.Add(-24*time.Hour), 10, 0, act()); err != nil {
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

		member := model.TeamMember{ID: 20, TeamID: 1, UserID: 11, CreatedAt: later}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later.Add(8*24*time.Hour), act()); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting an expired invitation returned %v, want ErrInvitationNotPending", err)
		}
		if teamVersion(db, 1) != v {
			t.Error("a refused accept moved the version")
		}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later, act()); err != nil {
			t.Fatal(err)
		}
		// The invitee never loaded the team: accepting moves it anyway.
		if v+1 != teamVersion(db, 1) {
			t.Errorf("accepting left the version at %d, want %d", teamVersion(db, 1), v+1)
		}
		v++
		if got := lastActivity(db, 1); got != "invitation.accepted 11 email:a@x.io {}" {
			t.Errorf("accepting recorded %q", got)
		}
		if err := r.AcceptTeamInvitation(ctx, 3, member, later, act()); !errors.Is(err, ErrInvitationNotPending) {
			t.Errorf("accepting twice returned %v, want ErrInvitationNotPending", err)
		}
		if got, _ := r.GetTeamInvitation(ctx, 3); got.AcceptedAt == nil || got.AcceptedBy != 11 {
			t.Errorf("the accepted invitation is %+v, want accepted by 11", got)
		}
		// A member accepting another invitation stays one member.
		if err := r.AcceptTeamInvitation(ctx, 4, model.TeamMember{ID: 21, TeamID: 1, UserID: 11}, later, act()); err != nil {
			t.Fatal(err)
		}
		v++
		if n := countTeamRows(db, &model.TeamMember{}, "team_id = 1 AND user_id = 11"); n != 1 {
			t.Errorf("11 is in the team %d times, want once", n)
		}

		if v, err = r.CreateTeamInvitation(ctx, invitation(5, "c@x.io", later), v, later.Add(-24*time.Hour), 10, 0, act()); err != nil {
			t.Fatal(err)
		}
		db.Create(&model.Team{ID: 2, Name: "other", Version: 1})
		db.Create(&model.TeamMember{ID: 30, TeamID: 2, UserID: 10})
		if _, err := r.RevokeTeamInvitation(ctx, 2, 10, 1, 5, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("revoking through another team returned %v, want ErrNotFound", err)
		}
		if _, err := r.RevokeTeamInvitation(ctx, 1, 99, v, 5, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger revoking returned %v, want ErrNotFound", err)
		}
		if _, err := r.RevokeTeamInvitation(ctx, 1, 11, v-1, 5, act()); !isConflict(err, v) {
			t.Errorf("revoking from a replaced version returned %v, want a conflict at %d", err, v)
		}
		if v, err = r.RevokeTeamInvitation(ctx, 1, 11, v, 5, act()); err != nil {
			t.Fatal(err)
		}
		if got := lastActivity(db, 1); got != "invitation.revoked 11 email:c@x.io {}" {
			t.Errorf("revoking recorded %q", got)
		}
		if _, err := r.RevokeTeamInvitation(ctx, 1, 11, v, 5, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("revoking twice returned %v, want ErrNotFound", err)
		}
		if v, err = r.CreateTeamInvitation(ctx, invitation(6, "a@x.io", later), v, later.Add(-24*time.Hour), 10, 0, act()); err != nil {
			t.Errorf("inviting an address whose invitation was accepted returned %v", err)
		}
		unknownTeam := invitation(8, "z@x.io", later)
		unknownTeam.TeamID = 99
		if _, err := r.CreateTeamInvitation(ctx, unknownTeam, 1, later, 10, 0, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("inviting to an unknown team returned %v, want ErrNotFound", err)
		}

		// Declining: the invitee is the actor, and the version moves.
		decline := act()
		decline.ActorUserID = 40
		if err := r.DeclineTeamInvitation(ctx, 1, 6, decline); err != nil {
			t.Fatal(err)
		}
		if teamVersion(db, 1) != v+1 {
			t.Errorf("declining left the version at %d, want %d", teamVersion(db, 1), v+1)
		}
		if got := lastActivity(db, 1); got != "invitation.declined 40 email:a@x.io {}" {
			t.Errorf("declining recorded %q", got)
		}
		if got, _ := r.GetTeamInvitation(ctx, 6); got.RevokedAt == nil {
			t.Error("the declined invitation is still pending")
		}
		for name, c := range map[string]struct {
			team, id int64
			want     error
		}{
			"declining twice":                   {1, 6, ErrInvitationNotPending},
			"declining an accepted invitation":  {1, 3, ErrInvitationNotPending},
			"declining through another team":    {2, 6, ErrNotFound},
			"declining into an unknown team":    {99, 6, ErrNotFound},
			"declining an unknown invitation":   {1, 77, ErrNotFound},
			"declining a revoked invitation":    {1, 5, ErrInvitationNotPending},
			"declining one revoked to make way": {1, 1, ErrInvitationNotPending},
		} {
			if err := r.DeclineTeamInvitation(ctx, c.team, c.id, act()); !errors.Is(err, c.want) {
				t.Errorf("%s returned %v, want %v", name, err, c.want)
			}
		}
		// An expired invitation still pending may be declined: it only tidies.
		expired := invitation(9, "old@x.io", teamT0.Add(-30*24*time.Hour))
		db.Create(&expired)
		if err := r.DeclineTeamInvitation(ctx, 1, 9, act()); err != nil {
			t.Errorf("declining an expired invitation returned %v", err)
		}
	})
}

// The cap counts every invitation created in the window, revoked ones too,
// and only the team's own.
func TestInvitationCapCountsTheWindow(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.Team{ID: 2, Name: "other", Version: 1})
		other := invitation(50, "o@x.io", teamT0)
		other.TeamID = 2
		db.Create(&other)
		since := teamT0.Add(-24 * time.Hour)
		// One just outside the window does not count.
		old := invitation(1, "old@x.io", since)
		db.Create(&old)
		v := int64(1)
		for i := int64(2); i <= 4; i++ {
			var err error
			if v, err = r.CreateTeamInvitation(ctx, invitation(i, fmt.Sprintf("u%d@x.io", i), teamT0), v, since, 3, 0, act()); err != nil {
				t.Fatalf("invitation %d of 3 returned %v", i-1, err)
			}
		}
		v, err := r.RevokeTeamInvitation(ctx, 1, 10, v, 4, act())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.CreateTeamInvitation(ctx, invitation(5, "u5@x.io", teamT0), v, since, 3, 0, act()); !errors.Is(err, ErrInvitationCapReached) {
			t.Errorf("a 4th invitation with a cap of 3 returned %v, want ErrInvitationCapReached", err)
		}
		if _, err := r.CreateTeamInvitation(ctx, invitation(5, "u5@x.io", teamT0), v, since, 4, 0, act()); err != nil {
			t.Errorf("a 4th invitation with a cap of 4 returned %v", err)
		}
	})
}

// inviteRetrying invites as each member would: from the version it reads,
// again on a conflict, until it is stored or refused for another reason.
func inviteRetrying(r Repository, inv model.TeamInvitation, teamLimit, userLimit int64) error {
	for {
		t, err := r.GetTeam(context.Background(), inv.TeamID)
		if err != nil {
			return err
		}
		_, err = r.CreateTeamInvitation(context.Background(), inv, t.Version, teamT0.Add(-24*time.Hour), teamLimit, userLimit, act())
		var vc *TeamVersionConflict
		if !errors.As(err, &vc) {
			return err
		}
	}
}

// Requests at once never pass the cap together: the team row is locked from
// the count to the insert, whatever version each request carries.
func TestInvitationCapHoldsUnderParallelRequests(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		const limit, requests = 5, 20
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = inviteRetrying(r, invitation(int64(i+1), fmt.Sprintf("u%d@x.io", i), teamT0), limit, 0)
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
		if v := teamVersion(db, 1); v != 1+limit {
			t.Errorf("the team is at version %d, want %d: one step per invitation stored", v, 1+limit)
		}
	})
}

// Members inviting at once from the version they all loaded: one invitation
// is stored, the others are told the team changed.
func TestParallelInvitesFromOneVersion(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		const requests = 8
		var wg sync.WaitGroup
		errs := make([]error, requests)
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = r.CreateTeamInvitation(context.Background(), invitation(int64(i+1), fmt.Sprintf("u%d@x.io", i), teamT0), 1, teamT0.Add(-24*time.Hour), 100, 0, act())
			}()
		}
		wg.Wait()
		ok, conflicts := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case isConflict(err, 2):
				conflicts++
			default:
				t.Errorf("an invite failed with %v, want success or a conflict at 2", err)
			}
		}
		if ok != 1 || conflicts != requests-1 || countTeamRows(db, &model.TeamInvitation{}, "team_id = 1") != 1 || teamVersion(db, 1) != 2 {
			t.Errorf("%d stored, %d conflicts, version %d; want 1, %d, 2", ok, conflicts, teamVersion(db, 1), requests-1)
		}
	})
}

// inviter seeds user 10, who sends the invitations, and team 2, a second
// team of theirs to invite to.
func inviter(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.User{ID: 10, Name: "Ten", Email: "ten@x.io"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Team{ID: 2, Name: "other", Version: 1, CreatedAt: teamT0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TeamMember{ID: 2, TeamID: 2, UserID: 10}).Error; err != nil {
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
			if _, err := r.CreateTeamInvitation(ctx, inTeam(invitation(id, fmt.Sprintf("u%d@x.io", id), teamT0), team), teamVersion(db, team), since, 10, 3, act()); err != nil {
				t.Fatalf("invitation %d of 3 returned %v", i+1, err)
			}
		}
		if _, err := r.RevokeTeamInvitation(ctx, 2, 10, teamVersion(db, 2), 11, act()); err != nil {
			t.Fatal(err)
		}
		if _, err := r.CreateTeamInvitation(ctx, inTeam(invitation(20, "u20@x.io", teamT0), 2), teamVersion(db, 2), since, 10, 3, act()); !errors.Is(err, ErrUserInvitationCapReached) {
			t.Errorf("a 4th invitation with a cap of 3 across teams returned %v, want ErrUserInvitationCapReached", err)
		}
		// The team's cap answers first when both are reached.
		if _, err := r.CreateTeamInvitation(ctx, invitation(20, "u20@x.io", teamT0), teamVersion(db, 1), since, 3, 3, act()); !errors.Is(err, ErrInvitationCapReached) {
			t.Errorf("both caps reached returned %v, want ErrInvitationCapReached", err)
		}
		if _, err := r.CreateTeamInvitation(ctx, inTeam(invitation(20, "u20@x.io", teamT0), 2), teamVersion(db, 2), since, 10, 0, act()); err != nil {
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
			_, err := r.CreateTeamInvitation(context.Background(), invitation(2, "b@x.io", teamT0), 1, since, 10, 1, act())
			done <- err
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
		select {
		case err := <-done:
			if !errors.Is(err, ErrUserInvitationCapReached) {
				t.Errorf("an invitation past the cap once the other committed returned %v, want ErrUserInvitationCapReached", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the invitation never finished")
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
				errs[i] = inviteRetrying(r, inv, 100, limit)
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
			if _, err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), 1, since, 10, 5, act()); !errors.Is(err, ErrNotFound) {
				t.Errorf("an inviter with no account returned %v, want ErrNotFound", err)
			}
		})
	})
	t.Run("locking the inviter", func(t *testing.T) {
		forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
			if err := db.Migrator().DropTable(&model.User{}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), 1, since, 10, 5, act()); err == nil {
				t.Error("an invitation with no users table to lock returned nil")
			}
		})
	})
	t.Run("counting the inviter's invitations", func(t *testing.T) {
		forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
			inviter(t, db)
			failTeamStatement(t, db, "query", "team_invitations", 1)
			if _, err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), 1, since, 10, 5, act()); !errors.Is(err, errTeamDB) {
				t.Errorf("a failed count returned %v, want %v", err, errTeamDB)
			}
		})
	})
}

func TestOnlyOneParallelAcceptWins(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		if _, err := r.CreateTeamInvitation(context.Background(), invitation(1, "a@x.io", teamT0), 1, teamT0.Add(-24*time.Hour), 10, 0, act()); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var won, lost atomic.Int64
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := model.TeamMember{ID: int64(100 + i), TeamID: 1, UserID: int64(20 + i)}
				switch err := r.AcceptTeamInvitation(context.Background(), 1, m, teamT0, act()); {
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
		// The refused accepts rolled their step back.
		if v := teamVersion(db, 1); v != 3 {
			t.Errorf("the team is at version %d, want 3: the invitation and one accept", v)
		}
	})
}

// seedTeamWorkspaces gives user 10 (Alice) a shared workspace 100 "backend"
// with a fork 101, an archived fork 102, an unshared workspace 103 and an
// archived shared one 104; user 11 (Bob), in the team, a shared workspace
// 200 "docs".
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
	db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
	for _, s := range []model.WorkspaceShare{
		{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: 10},
		{ID: 2, TeamID: 1, WorkspaceID: 104, UserID: 10},
		{ID: 3, TeamID: 1, WorkspaceID: 200, UserID: 11},
		// A share of a workspace since deleted.
		{ID: 4, TeamID: 1, WorkspaceID: 999, UserID: 11},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanReadWorkspaceInTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		db.Create(&model.Team{ID: 2, Name: "other", Version: 1})
		db.Create(&model.TeamMember{ID: 9, TeamID: 2, UserID: 12})
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

// A team's workspaces show under the name they are shared by: their own, or
// their parent's for a fork.
func TestListTeamWorkspacesBySharedName(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		rows, err := r.ListTeamWorkspaces(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, w := range rows {
			got = append(got, fmt.Sprintf("%s:%d:%s:%d:%s:%s", w.SharedName, w.ID, w.Name, w.ForkOfID, w.OwnerName, w.OwnerPicture))
		}
		want := []string{"backend:100:backend:0:Alice:a.png", "backend:101:backend-fork:100:Alice:a.png", "docs:200:docs:0:Bob:"}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("team workspaces are %v, want %v", got, want)
		}
	})
}

func TestWorkspaceShares(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.Team{ID: 2, Name: "alpha", Version: 1})
		db.Create(&model.TeamMember{ID: 2, TeamID: 2, UserID: 10})
		share := func(id, team, workspace, user int64, at time.Time) model.WorkspaceShare {
			return model.WorkspaceShare{ID: id, TeamID: team, WorkspaceID: workspace, UserID: user, CreatedAt: at, UpdatedAt: at}
		}
		s, v, err := r.SetWorkspaceShare(ctx, share(1, 1, 100, 10, teamT0), 1, act())
		if err != nil || s.ID != 1 || s.TeamName != "core" || v != 2 {
			t.Fatalf("first share is %+v at version %d (%v), want share 1 in core at version 2", s, v, err)
		}
		if got := lastActivity(db, 1); got != "workspace.shared 10 ws:100 {}" {
			t.Errorf("sharing recorded %q", got)
		}
		if _, _, err := r.SetWorkspaceShare(ctx, share(4, 1, 101, 11, teamT0), v, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("sharing with a team the sharer is not in returned %v, want ErrNotFound", err)
		}
		if _, _, err := r.SetWorkspaceShare(ctx, share(5, 99, 100, 10, teamT0), 1, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("sharing with an unknown team returned %v, want ErrNotFound", err)
		}
		if _, _, err := r.SetWorkspaceShare(ctx, share(5, 1, 102, 10, teamT0), 1, act()); !isConflict(err, v) {
			t.Errorf("sharing from a replaced version returned %v, want a conflict at %d", err, v)
		}
		// Sharing again changes nothing and records nothing.
		later := teamT0.Add(time.Hour)
		logged := len(activities(db, 1))
		s, v, err = r.SetWorkspaceShare(ctx, share(2, 1, 100, 10, later), v, act())
		if err != nil || s.ID != 1 || !s.CreatedAt.Equal(teamT0) || v != 3 {
			t.Fatalf("sharing again is %+v at %d (%v), want share 1 as it was", s, v, err)
		}
		if len(activities(db, 1)) != logged {
			t.Error("sharing a shared workspace again was recorded")
		}
		if _, _, err := r.SetWorkspaceShare(ctx, share(3, 2, 100, 10, teamT0), 1, act()); err != nil {
			t.Fatal(err)
		}
		list, err := r.ListWorkspaceShares(ctx, 100, 10)
		if err != nil || len(list) != 2 || list[0].TeamName != "alpha" || list[1].TeamName != "core" {
			t.Errorf("shares are %+v (%v), want alpha then core", list, err)
		}
		if list, _ := r.ListWorkspaceShares(ctx, 100, 11); len(list) != 0 {
			t.Errorf("another account sees %d shares of workspace 100, want none", len(list))
		}
		db.Create(&model.TeamMember{ID: 3, TeamID: 1, UserID: 11})
		if _, err := r.DeleteWorkspaceShare(ctx, 100, 1, 11, v, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsharing someone else's workspace returned %v, want ErrNotFound", err)
		}
		if _, err := r.DeleteWorkspaceShare(ctx, 100, 1, 12, v, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("a stranger unsharing returned %v, want ErrNotFound", err)
		}
		if _, err := r.DeleteWorkspaceShare(ctx, 100, 1, 10, 1, act()); !isConflict(err, v) {
			t.Errorf("unsharing from a replaced version returned %v, want a conflict at %d", err, v)
		}
		if v, err = r.DeleteWorkspaceShare(ctx, 100, 1, 10, v, act()); err != nil || v != 4 {
			t.Fatalf("unsharing returned %d, %v; want version 4", v, err)
		}
		if got := lastActivity(db, 1); got != "workspace.unshared 10 ws:100 {}" {
			t.Errorf("unsharing recorded %q", got)
		}
		if _, err := r.DeleteWorkspaceShare(ctx, 100, 1, 10, v, act()); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsharing twice returned %v, want ErrNotFound", err)
		}
		if teamVersion(db, 1) != v {
			t.Error("a refused unshare moved the version")
		}
	})
}

// A team's log reads newest first, a page at a time, with each actor's and
// target's account; a page starts after the activity it names.
func TestTeamActivitiesPaging(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		db.Create(&model.User{ID: 10, Email: "alice@x.io", Name: "Alice", Picture: "a.png"})
		db.Create(&model.User{ID: 11, Email: "bob@x.io", Name: "Bob", Picture: "b.png"})
		bob, email, ws := int64(11), "c@x.io", int64(100)
		for _, a := range []model.TeamManagementActivity{
			// Two at the same moment: the id breaks the tie.
			{ID: 20, TeamID: 1, ActorUserID: 10, Action: model.TeamActivityMemberRemoved, TargetUserID: &bob, Details: "{}", CreatedAt: teamT0.Add(time.Minute)},
			{ID: 21, TeamID: 1, ActorUserID: 11, Action: model.TeamActivityMemberInvited, TargetEmail: &email, Details: "{}", CreatedAt: teamT0.Add(time.Minute)},
			{ID: 22, TeamID: 1, ActorUserID: 99, Action: model.TeamActivityWorkspaceShared, TargetWorkspaceID: &ws, Details: `{"workspaceName":"api"}`, CreatedAt: teamT0.Add(2 * time.Minute)},
			{ID: 30, TeamID: 2, ActorUserID: 10, Action: model.TeamActivityTeamCreated, Details: "{}", CreatedAt: teamT0.Add(time.Hour)},
		} {
			if err := db.Create(&a).Error; err != nil {
				t.Fatal(err)
			}
		}
		page := func(limit int, before int64) string {
			rows, err := r.ListTeamActivities(ctx, 1, limit, before)
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for _, a := range rows {
				out = append(out, fmt.Sprintf("%d:%s:%s/%s:%s/%s", a.ID, a.Action, a.ActorName, a.ActorPicture, a.TargetName, a.TargetPicture))
			}
			return strings.Join(out, " ")
		}
		if got := page(2, 0); got != "22:workspace.shared:/:/ 21:member.invited:Bob/b.png:/" {
			t.Errorf("the first page is %s", got)
		}
		if got := page(2, 21); !strings.HasPrefix(got, "20:member.removed:Alice/a.png:Bob/b.png ") || strings.Count(got, " ") != 1 {
			t.Errorf("the page after 21 is %s, want 20 then the creation", got)
		}
		// 21 has the same moment as 20 and a larger id: it is not older.
		if got := page(10, 20); strings.Contains(got, "21:") || strings.Count(got, ":team.created:") != 1 || strings.Count(got, " ") != 0 {
			t.Errorf("the page after 20 is %s, want only the creation", got)
		}
		if got := page(10, 30); got != "" {
			t.Errorf("a page after another team's activity is %q, want none", got)
		}
		if got := page(10, 77); got != "" {
			t.Errorf("a page after an unknown activity is %q, want none", got)
		}
		rows, _ := r.ListTeamActivities(ctx, 1, 1, 0)
		if len(rows) != 1 || rows[0].TargetWorkspaceID == nil || *rows[0].TargetWorkspaceID != 100 || rows[0].Details != `{"workspaceName":"api"}` {
			t.Errorf("the newest activity is %+v", rows)
		}
		failTeamStatement(t, db, "query", "team_management_activities", 0)
		if _, err := r.ListTeamActivities(ctx, 1, 10, 21); !errors.Is(err, errTeamDB) {
			t.Errorf("a failed read of the cursor returned %v, want %v", err, errTeamDB)
		}
	})
}

// Every statement's failure reaches the caller, and a failed transaction
// leaves nothing behind.
func TestTeamStatementFailures(t *testing.T) {
	ctx := context.Background()
	since := teamT0.Add(-24 * time.Hour)
	member := model.TeamMember{ID: 30, TeamID: 1, UserID: 11}
	share := model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: 10}
	invite := func(r Repository) {
		_, _ = r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
	}
	// After invite the team is at version 2.
	cases := []struct {
		name        string
		kind, table string
		skip        int
		setup       func(r Repository)
		call        func(r Repository) error
	}{
		{"creating a team", "create", "teams", 0, nil, func(r Repository) error {
			return r.CreateTeam(ctx, model.Team{ID: 2, Name: "x"}, model.TeamMember{ID: 3, TeamID: 2, UserID: 10}, act())
		}},
		{"creating a team's first member", "create", "team_members", 0, nil, func(r Repository) error {
			return r.CreateTeam(ctx, model.Team{ID: 2, Name: "x"}, model.TeamMember{ID: 3, TeamID: 2, UserID: 10}, act())
		}},
		{"recording a team's creation", "create", "team_management_activities", 0, nil, func(r Repository) error {
			return r.CreateTeam(ctx, model.Team{ID: 2, Name: "x"}, model.TeamMember{ID: 3, TeamID: 2, UserID: 10}, act())
		}},
		{"listing teams", "row", "teams", 0, nil, func(r Repository) error { _, err := r.ListTeamsForUser(ctx, 10); return err }},
		{"reading a team", "query", "teams", 0, nil, func(r Repository) error { _, err := r.GetTeam(ctx, 1); return err }},
		{"counting members", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.CountTeamMembers(ctx, 1); return err }},
		{"moving the version", "raw", "", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 1, "y", act()); return err }},
		{"reading the caller's membership", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 1, "y", act()); return err }},
		{"reading the version on a conflict", "query", "teams", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 7, "y", act()); return err }},
		{"reading the name before a rename", "query", "teams", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 1, "y", act()); return err }},
		{"renaming a team", "update", "teams", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 1, "y", act()); return err }},
		{"recording a rename", "create", "team_management_activities", 0, nil, func(r Repository) error { _, err := r.RenameTeam(ctx, 1, 10, 1, "y", act()); return err }},
		{"reading the team to delete", "query", "teams", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"deleting a team's members", "delete", "team_members", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"deleting a team's invitations", "delete", "team_invitations", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"deleting a team's shares", "delete", "workspace_shares", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"deleting a team", "delete", "teams", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"recording a deletion", "create", "team_management_activities", 0, nil, func(r Repository) error { return r.DeleteTeam(ctx, 1, 10, 1, act()) }},
		{"listing members", "row", "team_members", 0, nil, func(r Repository) error { _, err := r.ListTeamMembers(ctx, 1); return err }},
		{"finding a member by email", "query", "team_members", 0, nil, func(r Repository) error { _, err := r.TeamHasMemberWithEmail(ctx, 1, "a@x.io"); return err }},
		{"reading the member to remove", "query", "team_members", 1, nil, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 11, 1, act(), 0)
			return err
		}},
		{"removing a member", "delete", "team_members", 0, nil, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 11, 1, act(), 0)
			return err
		}},
		{"removing a member's shares", "delete", "workspace_shares", 0, nil, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 11, 1, act(), 0)
			return err
		}},
		{"recording a removal", "create", "team_management_activities", 0, nil, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 11, 1, act(), 0)
			return err
		}},
		{"counting the members left", "query", "team_members", 2, nil, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 11, 1, act(), 0)
			return err
		}},
		{"deleting the team the last member left", "delete", "teams", 0, func(r Repository) {
			_, _, _ = r.RemoveTeamMember(ctx, 1, 11, 11, 1, act(), 0)
		}, func(r Repository) error {
			_, _, err := r.RemoveTeamMember(ctx, 1, 10, 10, 2, act(), 0)
			return err
		}},
		{"counting invitations", "query", "team_invitations", 0, nil, func(r Repository) error {
			_, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
			return err
		}},
		{"revoking an expired invitation", "update", "team_invitations", 0, nil, func(r Repository) error {
			_, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
			return err
		}},
		{"storing an invitation", "create", "team_invitations", 0, nil, func(r Repository) error {
			_, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
			return err
		}},
		{"recording an invitation", "create", "team_management_activities", 0, nil, func(r Repository) error {
			_, err := r.CreateTeamInvitation(ctx, invitation(1, "a@x.io", teamT0), 1, since, 10, 0, act())
			return err
		}},
		{"reading the invitation to revoke", "query", "team_invitations", 0, invite, func(r Repository) error {
			_, err := r.RevokeTeamInvitation(ctx, 1, 10, 2, 1, act())
			return err
		}},
		{"revoking an invitation", "update", "team_invitations", 0, invite, func(r Repository) error {
			_, err := r.RevokeTeamInvitation(ctx, 1, 10, 2, 1, act())
			return err
		}},
		{"recording a revocation", "create", "team_management_activities", 0, invite, func(r Repository) error {
			_, err := r.RevokeTeamInvitation(ctx, 1, 10, 2, 1, act())
			return err
		}},
		{"moving the version to decline", "raw", "", 0, invite, func(r Repository) error { return r.DeclineTeamInvitation(ctx, 1, 1, act()) }},
		{"reading the invitation to decline", "query", "team_invitations", 0, invite, func(r Repository) error { return r.DeclineTeamInvitation(ctx, 1, 1, act()) }},
		{"declining an invitation", "update", "team_invitations", 0, invite, func(r Repository) error { return r.DeclineTeamInvitation(ctx, 1, 1, act()) }},
		{"recording a decline", "create", "team_management_activities", 0, invite, func(r Repository) error { return r.DeclineTeamInvitation(ctx, 1, 1, act()) }},
		{"moving the version to accept", "raw", "", 0, nil, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act()) }},
		{"reading the invitation to accept", "query", "team_invitations", 0, invite, func(r Repository) error {
			return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act())
		}},
		{"reading the inviter", "query", "team_members", 0, invite, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act()) }},
		{"marking an invitation accepted", "update", "team_invitations", 0, invite, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act()) }},
		{"adding the accepted member", "create", "team_members", 0, invite, func(r Repository) error { return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act()) }},
		{"recording an accept", "create", "team_management_activities", 0, invite, func(r Repository) error {
			return r.AcceptTeamInvitation(ctx, 1, member, teamT0, act())
		}},
		{"moving the version to share", "raw", "", 0, nil, func(r Repository) error { _, _, err := r.SetWorkspaceShare(ctx, share, 1, act()); return err }},
		{"storing a share", "create", "workspace_shares", 0, nil, func(r Repository) error { _, _, err := r.SetWorkspaceShare(ctx, share, 1, act()); return err }},
		{"recording a share", "create", "team_management_activities", 0, nil, func(r Repository) error { _, _, err := r.SetWorkspaceShare(ctx, share, 1, act()); return err }},
		{"reading the share back", "query", "workspace_shares", 0, nil, func(r Repository) error { _, _, err := r.SetWorkspaceShare(ctx, share, 1, act()); return err }},
		{"deleting a share", "delete", "workspace_shares", 0, nil, func(r Repository) error { _, err := r.DeleteWorkspaceShare(ctx, 5, 1, 11, 1, act()); return err }},
		{"recording an unshare", "create", "team_management_activities", 0, nil, func(r Repository) error { _, err := r.DeleteWorkspaceShare(ctx, 5, 1, 11, 1, act()); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				db.Create(&model.TeamMember{ID: 2, TeamID: 1, UserID: 11})
				db.Create(&model.WorkspaceShare{ID: 9, TeamID: 1, WorkspaceID: 5, UserID: 11})
				if c.setup != nil {
					c.setup(r)
				}
				before, logged := teamVersion(db, 1), len(activities(db, 1))
				failTeamStatement(t, db, c.kind, c.table, c.skip)
				if err := c.call(r); !errors.Is(err, errTeamDB) {
					t.Errorf("returned %v, want %v", err, errTeamDB)
				}
				if c.kind != "query" || c.table != "teams" {
					if teamVersion(db, 1) != before || len(activities(db, 1)) != logged {
						t.Errorf("a failed change left version %d and %d activities, want %d and %d", teamVersion(db, 1), len(activities(db, 1)), before, logged)
					}
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
			db.Create(&model.Team{ID: id, Name: name, Version: 1})
			db.Create(&model.TeamMember{ID: id, TeamID: id, UserID: 50})
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
