// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGitHubInstallations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.GitHubInstallation{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("github_installations") {
		t.Fatal("table is not github_installations")
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	names := func(userID int64) string {
		rows, err := repo.ListGitHubInstallations(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, r := range rows {
			out += fmt.Sprintf("%s:%d ", r.Account, r.InstallationID)
		}
		return out
	}

	if err := repo.ReplaceGitHubInstallations(ctx, 1, []model.GitHubInstallation{
		{ID: 1, UserID: 1, InstallationID: 20, Account: "zeta"},
		{ID: 2, UserID: 1, InstallationID: 10, Account: "acme"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceGitHubInstallations(ctx, 2, []model.GitHubInstallation{{ID: 3, UserID: 2, InstallationID: 10, Account: "acme"}}); err != nil {
		t.Fatal(err)
	}
	if got := names(1); got != "acme:10 zeta:20 " {
		t.Fatalf("user 1: %q", got)
	}

	// A reconnect replaces the account's rows, and no one else's.
	if err := repo.ReplaceGitHubInstallations(ctx, 1, []model.GitHubInstallation{{ID: 4, UserID: 1, InstallationID: 30, Account: "me"}}); err != nil {
		t.Fatal(err)
	}
	if got := names(1) + "| " + names(2); got != "me:30 | acme:10 " {
		t.Fatalf("after replace: %q", got)
	}
	if err := repo.ReplaceGitHubInstallations(ctx, 1, nil); err != nil || names(1) != "" {
		t.Fatalf("replace with none: %v %q", err, names(1))
	}

	if err := repo.DeleteGitHubInstallation(ctx, 1, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account's installation: %v", err)
	}
	if err := repo.DeleteGitHubInstallation(ctx, 2, 10); err != nil || names(2) != "" {
		t.Fatalf("delete: %v %q", err, names(2))
	}

	// Rows clashing on the unique index leave the account as it was.
	if err := repo.ReplaceGitHubInstallations(ctx, 1, []model.GitHubInstallation{{ID: 5, UserID: 1, InstallationID: 7}, {ID: 6, UserID: 1, InstallationID: 7}}); err == nil {
		t.Error("duplicate rows stored")
	}
	db.Exec("DROP TABLE github_installations")
	if err := repo.ReplaceGitHubInstallations(ctx, 1, nil); err == nil {
		t.Error("replace with no table")
	}

	sqlDB, _ := db.DB()
	sqlDB.Close()
	if _, err := repo.ListGitHubInstallations(ctx, 1); err == nil {
		t.Error("list on a closed db")
	}
	if err := repo.ReplaceGitHubInstallations(ctx, 1, nil); err == nil {
		t.Error("replace on a closed db")
	}
	if err := repo.DeleteGitHubInstallation(ctx, 1, 1); err == nil {
		t.Error("delete on a closed db")
	}
}
