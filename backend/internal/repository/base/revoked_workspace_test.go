// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"gorm.io/gorm"
)

// revokeDB is a database a workspace can be deleted from, holding dtWorkspaceID
// named name.
func revokeDB(t *testing.T, name string) (*gorm.DB, Repository) {
	t.Helper()
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.TeamSkillChoice{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.Preset{}, &model.WorkspaceShare{}, &model.MemoryExclusion{}, &model.AgentNote{}, &model.RevokedWorkspace{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.Workspace{ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: name}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	return db, New(&mockDB{db: db})
}

func revokedIDs(t *testing.T, repo Repository) []int64 {
	t.Helper()
	ids, err := repo.ListRevokedWorkspaceIDs(context.Background(), time.Now().Add(-auth.SupervisorLaunchTokenTTL))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return ids
}

// Deleting the supervisor revokes its id; deleting any other workspace
// records nothing, so the table holds supervisors only.
func TestDeleteWorkspace_RevokesOnlyASupervisor(t *testing.T) {
	for name, want := range map[string][]int64{
		supervisorWorkspaceName: {dtWorkspaceID},
		"app":                   nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, repo := revokeDB(t, name)
			if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err != nil {
				t.Fatalf("DeleteWorkspace: %v", err)
			}
			if got := revokedIDs(t, repo); !slices.Equal(got, want) {
				t.Errorf("revoked %v, want %v", got, want)
			}
		})
	}
}

// Resetting the supervisor's id revokes the old one, not the new.
func TestResetWorkspaceID_RevokesASupervisorsOldID(t *testing.T) {
	db, repo := resetDB(t)
	if err := db.AutoMigrate(&model.RevokedWorkspace{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Workspace{}).Where("id = ?", rsOld).UpdateColumn("name", supervisorWorkspaceName).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetWorkspaceID(context.Background(), rsOld, rsNew, rsUser); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := revokedIDs(t, repo); !slices.Equal(got, []int64{rsOld}) {
		t.Errorf("revoked %v, want the old id %d", got, rsOld)
	}
}

// A revocation older than a launch token's lifetime, and an hour, is pruned
// by the next one and not listed meanwhile: no token can still name it.
func TestRevokedWorkspaces_KeepOnlyALaunchTokensLifetime(t *testing.T) {
	db, repo := revokeDB(t, supervisorWorkspaceName)
	old := time.Now().Add(-auth.SupervisorLaunchTokenTTL - 2*time.Hour)
	if err := db.Create(&[]model.RevokedWorkspace{
		{ID: 1, RevokedAt: old},
		{ID: 2, RevokedAt: time.Now().Add(-auth.SupervisorLaunchTokenTTL - 30*time.Minute)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if got := revokedIDs(t, repo); len(got) != 0 {
		t.Errorf("listed %v, want nothing older than the lifetime", got)
	}
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	var kept []int64
	db.Model(&model.RevokedWorkspace{}).Order("id").Pluck("id", &kept)
	if !slices.Equal(kept, []int64{2, dtWorkspaceID}) {
		t.Errorf("rows %v, want the one past the hour's grace pruned", kept)
	}
}

// A revocation that cannot be written fails the delete, and nothing is
// deleted: a supervisor gone with its tokens still working is the one
// outcome this exists to prevent.
func TestDeleteWorkspace_FailsWhenTheRevocationCannotBeWritten(t *testing.T) {
	db, repo := revokeDB(t, supervisorWorkspaceName)
	if err := db.Migrator().DropTable(&model.RevokedWorkspace{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err == nil {
		t.Fatal("want an error")
	}
	var n int64
	db.Model(&model.Workspace{}).Where("id = ?", dtWorkspaceID).Count(&n)
	if n != 1 {
		t.Errorf("%d workspaces left, want the supervisor kept", n)
	}
}

// A reset that cannot read which workspace it is fails.
func TestResetWorkspaceID_FailsWhenTheRevocationCannotBeChecked(t *testing.T) {
	db, repo := resetDB(t)
	if err := db.Migrator().DropTable(&model.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetWorkspaceID(context.Background(), rsOld, rsNew, rsUser); err == nil {
		t.Error("no workspaces table: want an error")
	}
}
