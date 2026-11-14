// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// teamViewTables are what team mode reads beyond the team's own tables.
var teamViewTables = []any{&model.Task{}, &model.Skill{}, &model.WorkspaceSkill{}}

// Reads over no workspaces are empty lists, read from nothing.
func TestTeamViewReadsOverNoWorkspaces(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		// No table to read: a read would fail.
		tasks, err := r.ListTasksInWorkspaces(ctx, entity.ListTasksRequest{}, nil)
		if err != nil || tasks == nil || len(tasks) != 0 {
			t.Errorf("tasks: %v, %v", tasks, err)
		}
		skills, err := r.ListSkillsOnInWorkspaces(ctx, nil)
		if err != nil || skills == nil || len(skills) != 0 {
			t.Errorf("skills: %v, %v", skills, err)
		}
	})
}

// A workspace reads with its owner and the name it is shared under: its
// own, or its parent's for a fork. Whether a team reads it is
// CanReadWorkspaceInTeam's alone.
func TestTeamWorkspaceInfo(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		w, err := r.TeamWorkspaceInfo(ctx, 101)
		if err != nil || w.ID != 101 || w.Name != "backend-fork" || w.ForkOfID != 100 || w.SharedName != "backend" || w.UserID != 10 || w.OwnerName != "Alice" || w.OwnerPicture != "a.png" {
			t.Errorf("the fork reads %+v, %v: want its parent's name and its owner", w, err)
		}
		if w, err := r.TeamWorkspaceInfo(ctx, 200); err != nil || w.SharedName != "docs" || w.OwnerName != "Bob" {
			t.Errorf("a shared workspace reads %+v, %v: want its own name", w, err)
		}
		// A fork whose parent is gone shows under its own name.
		db.Create(&model.Workspace{ID: 105, UserID: 10, Name: "orphan", ForkOfID: 998})
		if w, err := r.TeamWorkspaceInfo(ctx, 105); err != nil || w.SharedName != "orphan" {
			t.Errorf("a fork of a deleted workspace reads %+v, %v: want its own name", w, err)
		}
		if _, err := r.TeamWorkspaceInfo(ctx, 999); !errors.Is(err, ErrNotFound) {
			t.Errorf("a workspace that is not there: %v", err)
		}
	})
}

// A workspace's peers are the workspaces, itself too, shared unarchived with
// each team it is itself shared with.
func TestListTeamPeerWorkspaces(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		db.Create(&model.Team{ID: 2, Name: "ops", Version: 1})
		// 201's owner has no account any more.
		db.Create(&model.Workspace{ID: 201, UserID: 12, Name: "notes"})
		for _, s := range []model.WorkspaceShare{
			{ID: 10, TeamID: 2, WorkspaceID: 103, UserID: 10},
			{ID: 11, TeamID: 2, WorkspaceID: 200, UserID: 11},
			{ID: 12, TeamID: 2, WorkspaceID: 201, UserID: 12},
		} {
			if err := db.Create(&s).Error; err != nil {
				t.Fatal(err)
			}
		}
		for ws, want := range map[int64]string{
			100: "1/100/10/backend/Alice 1/200/11/docs/Bob",
			200: "1/100/10/backend/Alice 1/200/11/docs/Bob 2/103/10/private/Alice 2/200/11/docs/Bob 2/201/12/notes/",
			103: "2/103/10/private/Alice 2/200/11/docs/Bob 2/201/12/notes/",
			101: "", // a fork is not itself shared
			104: "", // shared, and archived
			999: "",
		} {
			rows, err := r.ListTeamPeerWorkspaces(ctx, ws)
			got := []string{}
			for _, p := range rows {
				got = append(got, fmt.Sprintf("%d/%d/%d/%s/%s", p.TeamID, p.WorkspaceID, p.UserID, p.Name, p.OwnerName))
			}
			if err != nil || strings.Join(got, " ") != want {
				t.Errorf("peers of %d: %v, %v; want %s", ws, got, err, want)
			}
		}
		// A peer archived drops out.
		db.Model(&model.Workspace{}).Where("id = 200").Update("archived_at", teamT0)
		rows, _ := r.ListTeamPeerWorkspaces(ctx, 100)
		if len(rows) != 1 || rows[0].WorkspaceID != 100 {
			t.Errorf("peers of 100 once 200 is archived: %+v, want only itself", rows)
		}
	})
}

// A workspace's team readers are the members of each team it, or the
// workspace it is a fork of, is shared with, while neither is archived.
func TestListTeamReaders(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		db.Create(&model.Team{ID: 2, Name: "ops", Version: 1})
		db.Create(&model.TeamMember{ID: 3, TeamID: 2, UserID: 12})
		db.Create(&model.WorkspaceShare{ID: 5, TeamID: 2, WorkspaceID: 101, UserID: 10})
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

// A workspace's skills are those filed under its owner, and a skill its
// owner turned off is left out.
func TestTeamViewSkillsAreTheOwners(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		if err := db.AutoMigrate(teamViewTables...); err != nil {
			t.Fatal(err)
		}
		at := teamT0
		for _, row := range []any{
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
		skills, err := r.ListSkillsOnInWorkspaces(ctx, []int64{100, 200})
		var names []string
		for _, s := range skills {
			names = append(names, fmt.Sprintf("%d/%s/%d", s.WorkspaceID, s.Name, s.UserID))
		}
		if err != nil || fmt.Sprint(names) != "[200/deploy/11 100/deploy/10]" {
			t.Errorf("skills: %v, %v; want the owners' that are on, by name, the latest first", names, err)
		}
	})
}
