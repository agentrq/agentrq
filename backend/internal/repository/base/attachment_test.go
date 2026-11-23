// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// attachmentDB holds workspace 1's tasks and messages with attachments, and
// one of workspace 2's.
func attachmentDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []model.Task{
		{ID: 10, WorkspaceID: 1, Attachments: datatypes.JSON(`[{"id":"t10"},{"id":""}]`)},
		{ID: 11, WorkspaceID: 1},
		{ID: 12, WorkspaceID: 1, Attachments: datatypes.JSON(`not json`)},
		{ID: 20, WorkspaceID: 2, Attachments: datatypes.JSON(`[{"id":"other"}]`)},
	} {
		if err := db.Create(&tk).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []model.Message{
		{ID: 100, TaskID: 11, Attachments: datatypes.JSON(`[{"id":"m100"}]`)},
		{ID: 101, TaskID: 10},
		{ID: 200, TaskID: 20, Attachments: datatypes.JSON(`[{"id":"other-msg"}]`)},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func TestGetWorkspaceAttachments(t *testing.T) {
	r := New(&mockDB{db: attachmentDB(t)})
	ctx := context.Background()
	got, err := r.GetWorkspaceAttachments(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	// Each one with the task it belongs to, and nothing from workspace 2.
	want := []entity.TaskAttachment{{TaskID: 11, ID: "m100"}, {TaskID: 10, ID: "t10"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Both the task and the message query can fail, and either failure is returned.
	for n := 1; n <= 2; n++ {
		db := attachmentDB(t)
		failNth(db, n)
		if _, err := New(&mockDB{db: db}).GetWorkspaceAttachments(ctx, 1); !errors.Is(err, errInjected) {
			t.Errorf("statement %d: got %v, want the injected failure", n, err)
		}
	}
}
