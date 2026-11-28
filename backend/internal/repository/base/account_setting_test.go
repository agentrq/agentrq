// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func accountSettingDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.AccountSetting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

func settingValues(t *testing.T, repo Repository, userID int64) map[string]string {
	t.Helper()
	rows, err := repo.ListAccountSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListAccountSettings returned %v", err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Name] = r.Value
	}
	return got
}

func TestAccountSettings_AnAccountThatNeverSavedHasNoRows(t *testing.T) {
	_, repo := accountSettingDB(t)
	rows, err := repo.ListAccountSettings(context.Background(), memUserID)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("ListAccountSettings returned %+v, %v; want an empty list", rows, err)
	}
}

func TestAccountSettings_SaveCreatesThenReplacesAGroup(t *testing.T) {
	db, repo := accountSettingDB(t)
	ctx := context.Background()
	save := func(userID int64, name, value string) {
		t.Helper()
		if err := repo.SaveAccountSetting(ctx, model.AccountSetting{UserID: userID, Name: name, Value: value}); err != nil {
			t.Fatalf("saving %s for %d returned %v", name, userID, err)
		}
	}
	save(memUserID, "display_settings", `{"timeFormat":"12h"}`)
	save(memUserID, "other_settings", `{"a":"b"}`)
	save(memOtherUserID, "display_settings", `{"timeFormat":"12h"}`)
	save(memUserID, "display_settings", `{"timeFormat":"24h"}`)

	if got := settingValues(t, repo, memUserID); len(got) != 2 || got["display_settings"] != `{"timeFormat":"24h"}` || got["other_settings"] != `{"a":"b"}` {
		t.Errorf("the account holds %v, want display_settings replaced by 24h and other_settings left alone", got)
	}
	if got := settingValues(t, repo, memOtherUserID); got["display_settings"] != `{"timeFormat":"12h"}` {
		t.Errorf("the other account holds %v, want its own 12h untouched", got)
	}
	var row model.AccountSetting
	db.Where("user_id = ? AND name = ?", memUserID, "display_settings").First(&row)
	if row.UpdatedAt.IsZero() {
		t.Error("the saved row has no updated_at")
	}
}

func TestAccountSettings_ADatabaseErrorIsReturned(t *testing.T) {
	db, repo := accountSettingDB(t)
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	ctx := context.Background()
	if _, err := repo.ListAccountSettings(ctx, memUserID); err == nil {
		t.Error("ListAccountSettings on a closed database returned no error")
	}
	if err := repo.SaveAccountSetting(ctx, model.AccountSetting{UserID: memUserID, Name: "display_settings", Value: "{}"}); err == nil {
		t.Error("SaveAccountSetting on a closed database returned no error")
	}
}
