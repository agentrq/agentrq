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

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// teamViewTables are what team mode reads beyond the team's own tables.
var teamViewTables = []any{&model.Task{}, &model.Memory{}, &model.Skill{}, &model.WorkspaceSkill{}, &model.Telemetry{}}

// Reads over no workspaces are empty lists, read from nothing.
func TestTeamViewReadsOverNoWorkspaces(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		// No table to read: a read would fail.
		tasks, err := r.ListTasksInWorkspaces(ctx, entity.ListTasksRequest{}, nil)
		if err != nil || tasks == nil || len(tasks) != 0 {
			t.Errorf("tasks: %v, %v", tasks, err)
		}
		mems, err := r.ListWorkspaceMemoryNames(ctx, nil)
		if err != nil || mems == nil || len(mems) != 0 {
			t.Errorf("memories: %v, %v", mems, err)
		}
		skills, err := r.ListSkillsOnInWorkspaces(ctx, nil)
		if err != nil || skills == nil || len(skills) != 0 {
			t.Errorf("skills: %v, %v", skills, err)
		}
	})
}

// A workspace's place in a team is read as is, whether or not the team
// reads it: the check is CanReadWorkspaceInTeam's alone.
func TestTeamWorkspaceInfo(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		w, err := r.TeamWorkspaceInfo(ctx, 1, 101)
		if err != nil || w.ID != 101 || w.ForkOfID != 100 || w.GroupName != "Backend" || w.UserID != 10 || w.OwnerName != "Alice" || w.OwnerPicture != "a.png" {
			t.Errorf("the fork reads %+v, %v: want its parent's group and its owner", w, err)
		}
		if w, err := r.TeamWorkspaceInfo(ctx, 1, 103); err != nil || w.GroupName != "" {
			t.Errorf("an unshared workspace reads %+v, %v: want no group, and no filter", w, err)
		}
		if w, err := r.TeamWorkspaceInfo(ctx, 2, 100); err != nil || w.GroupName != "" {
			t.Errorf("another team's view reads %+v, %v: want no group", w, err)
		}
		if _, err := r.TeamWorkspaceInfo(ctx, 1, 999); !errors.Is(err, ErrNotFound) {
			t.Errorf("a workspace that is not there: %v", err)
		}
	})
}

// A workspace's team readers are the members of each team it, or the
// workspace it is a fork of, is shared with, while neither is archived.
func TestListTeamReaders(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		db.Create(&model.Team{ID: 2, Name: "ops"})
		db.Create(&model.TeamMember{ID: 3, TeamID: 2, UserID: 12, Role: model.TeamRoleOwner})
		db.Create(&model.WorkspaceShare{ID: 5, TeamID: 2, WorkspaceID: 101, UserID: 10, GroupName: "Forks"})
		for ws, want := range map[int64]string{
			100: "[{1 10} {1 11}]",
			101: "[{1 10} {1 11} {2 12}]", // its parent's team, and its own
			102: "[]",                     // an archived fork
			103: "[]",                     // not shared
			104: "[]",                     // shared, then archived
			200: "[{1 10} {1 11}]",
			999: "[]",
		} {
			got, err := r.ListTeamReaders(ctx, ws)
			if err != nil || fmt.Sprint(got) != want {
				t.Errorf("readers of %d: %v, %v; want %s", ws, got, err, want)
			}
		}
		// A fork whose parent is archived is read by nobody through it.
		db.Model(&model.Workspace{}).Where("id = ?", 100).Update("archived_at", teamT0)
		if got, _ := r.ListTeamReaders(ctx, 101); fmt.Sprint(got) != "[{2 12}]" {
			t.Errorf("readers of a fork whose shared parent is archived: %v, want its own team's", got)
		}
	})
}

// A workspace's memories and skills are those filed under its owner, and a
// skill its owner turned off is left out.
func TestTeamViewMemoriesAndSkillsAreTheOwners(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		if err := db.AutoMigrate(teamViewTables...); err != nil {
			t.Fatal(err)
		}
		at := teamT0
		for _, row := range []any{
			&model.Memory{ID: 1, UserID: 10, WorkspaceID: 100, Name: "b.md", UpdatedAt: at},
			&model.Memory{ID: 2, UserID: 10, WorkspaceID: 100, Name: "a.md", UpdatedAt: at},
			&model.Memory{ID: 3, UserID: 11, WorkspaceID: 100, Name: "planted.md", UpdatedAt: at},
			&model.Memory{ID: 4, UserID: 11, WorkspaceID: 200, Name: "a.md", UpdatedAt: at},
			&model.Skill{ID: 1, UserID: 10, Name: "deploy", Description: "d", UpdatedAt: at},
			&model.Skill{ID: 2, UserID: 10, Name: "off", Disabled: true, UpdatedAt: at},
			&model.Skill{ID: 3, UserID: 11, Name: "planted", UpdatedAt: at},
			&model.Skill{ID: 4, UserID: 11, Name: "deploy", UpdatedAt: at.Add(time.Hour)},
			&model.WorkspaceSkill{ID: 1, UserID: 10, SkillID: 1, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 2, UserID: 10, SkillID: 2, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 3, UserID: 11, SkillID: 3, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 4, UserID: 11, SkillID: 4, WorkspaceID: 200},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		mems, err := r.ListWorkspaceMemoryNames(ctx, []int64{100, 200})
		var names []string
		for _, m := range mems {
			names = append(names, fmt.Sprintf("%d/%s", m.WorkspaceID, m.Name))
		}
		if err != nil || fmt.Sprint(names) != "[100/a.md 100/b.md 200/a.md]" {
			t.Errorf("memories: %v, %v; want the owners', by workspace then name", names, err)
		}
		skills, err := r.ListSkillsOnInWorkspaces(ctx, []int64{100, 200})
		names = nil
		for _, s := range skills {
			names = append(names, fmt.Sprintf("%d/%s/%d", s.WorkspaceID, s.Name, s.UserID))
		}
		if err != nil || fmt.Sprint(names) != "[200/deploy/11 100/deploy/10]" {
			t.Errorf("skills: %v, %v; want the owners' that are on, by name, the latest first", names, err)
		}
	})
}

// The team's memory is saved by a member only, read under the team's lock,
// and the save hands back the one it replaced.
func TestSaveTeamMemory(t *testing.T) {
	ctx := context.Background()
	mem := func(id, by int64, content string) model.TeamMemory {
		return model.TeamMemory{ID: id, TeamID: 1, Name: "memory.md", Content: content, UpdatedBy: by, CreatedAt: teamT0, UpdatedAt: teamT0.Add(time.Duration(id) * time.Second)}
	}
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		if _, found, err := r.SaveTeamMemory(ctx, mem(1, 11, "first")); err != nil || found {
			t.Fatalf("the first save: found %v, %v", found, err)
		}
		prev, found, err := r.SaveTeamMemory(ctx, mem(2, 10, "second"))
		if err != nil || !found || prev.Content != "first" || prev.UpdatedBy != 11 || !prev.UpdatedAt.Equal(teamT0.Add(time.Second)) {
			t.Errorf("the second save replaced %+v (found %v, %v)", prev, found, err)
		}
		got, err := r.GetTeamMemory(ctx, 1, "memory.md")
		if err != nil || got.Content != "second" || got.UpdatedBy != 10 || got.ID != 1 {
			t.Errorf("the memory is %+v, %v: want the second save in the first row", got, err)
		}
		if _, _, err := r.SaveTeamMemory(ctx, mem(3, 12, "outsider")); !errors.Is(err, ErrNotFound) {
			t.Errorf("a save by someone outside the team: %v", err)
		}
		if _, err := r.GetTeamMemory(ctx, 2, "memory.md"); !errors.Is(err, ErrNotFound) {
			t.Errorf("another team's memory: %v", err)
		}

		// Bob removed while his save waits for the lock: it is refused.
		err = whileLocked(t, db, func(tx *gorm.DB) error {
			return tx.Where("team_id = 1 AND user_id = 11").Delete(&model.TeamMember{}).Error
		}, func() error { _, _, err := r.SaveTeamMemory(ctx, mem(4, 11, "after removal")); return err })
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("a save by a member removed while it waited: %v", err)
		}
		if got, _ := r.GetTeamMemory(ctx, 1, "memory.md"); got.Content != "second" {
			t.Errorf("the memory is %q after a refused save", got.Content)
		}
	})
}

func TestTeamViewStatementFailures(t *testing.T) {
	ctx := context.Background()
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamWorkspaces(t, db)
		// Telemetry is not migrated: the totals fail before the breakdown.
		if _, _, err := r.GetDetailedStatsOfWorkspaces(ctx, []int64{100}, 0, 1); err == nil {
			t.Error("stats over a missing table answered no error")
		}
		failTeamStatement(t, db, "query", "team_memories", 0)
		if _, _, err := r.SaveTeamMemory(ctx, model.TeamMemory{ID: 1, TeamID: 1, Name: "memory.md", UpdatedBy: 10}); !errors.Is(err, errTeamDB) {
			t.Errorf("a save whose read of the memory failed: %v", err)
		}
	})
}
