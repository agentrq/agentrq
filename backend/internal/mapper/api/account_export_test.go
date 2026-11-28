// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/mustafaturan/monoflake"
)

func TestAccountExportResponse(t *testing.T) {
	at := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	archived := at.Add(time.Hour)
	id := func(n int64) string { return monoflake.ID(n).String() }
	raw := FromAccountExportResponseEntityToHTTPResponse(&entity.AccountExport{
		Version:    1,
		ExportedAt: at,
		Profile:    entity.AccountExportProfile{ID: 5, Email: "ada@example.com", Name: "Ada"},
		Settings:   entity.AccountSettings{entity.SettingsDisplay: {entity.SettingTimeFormat: "24h"}},
		Presets:    []entity.AccountExportPreset{{WorkspaceID: 0, Name: "review", Body: "b", CreatedAt: at}},
		Workspaces: []entity.AccountExportWorkspace{{ID: 7, Name: "Site", Description: "d", Icon: "i", WorkingDirectory: "/w", SelfLearningLoopNote: "n", ForkOfID: 3, CreatedAt: at, ArchivedAt: &archived}},
		Memories:   []entity.AccountExportMemory{{WorkspaceID: 7, Name: "memory.md", Content: "# m", UpdatedAt: at}},
		PanoDocs:   []entity.AccountExportPanoDoc{{ID: 4, Title: "Plan", Markdown: "# p", CreatedAt: at, UpdatedAt: at}},
		Tasks: []entity.AccountExportTask{{
			ID: 10, WorkspaceID: 7, Title: "Fix", Body: "b", Status: "completed", CreatedBy: "human", Assignee: "agent",
			CronSchedule: "0 9 * * *", CreatedAt: at, UpdatedAt: at,
			Messages: []entity.AccountExportMessage{{Sender: "agent", Text: "done", CreatedAt: at}},
		}},
	})
	assertCamelCase(t, raw)
	checkResponse(t, raw, view.AccountExport{
		Version:    1,
		ExportedAt: at,
		Profile:    view.AccountExportProfile{ID: id(5), Email: "ada@example.com", Name: "Ada"},
		Settings:   view.AccountSettings{Display: view.DisplaySettings{TimeFormat: "24h"}},
		Presets:    []view.AccountExportPreset{{WorkspaceID: "", Name: "review", Body: "b", CreatedAt: at}},
		Workspaces: []view.AccountExportWorkspace{{ID: id(7), Name: "Site", Description: "d", Icon: "i", WorkingDirectory: "/w", SelfLearningLoopNote: "n", ForkOfID: id(3), CreatedAt: at, ArchivedAt: &archived}},
		Memories:   []view.AccountExportMemory{{WorkspaceID: id(7), Name: "memory.md", Content: "# m", UpdatedAt: at}},
		PanoDocs:   []view.AccountExportPanoDoc{{ID: id(4), Title: "Plan", Markdown: "# p", CreatedAt: at, UpdatedAt: at}},
		Tasks: []view.AccountExportTask{{
			ID: id(10), WorkspaceID: id(7), ParentID: "", Title: "Fix", Body: "b", Status: "completed", CreatedBy: "human", Assignee: "agent",
			CronSchedule: "0 9 * * *", CreatedAt: at, UpdatedAt: at,
			Messages: []view.AccountExportMessage{{Sender: "agent", Text: "done", CreatedAt: at}},
		}},
	})
}

func TestAccountExportResponse_AnEmptyAccountIsEmptyLists(t *testing.T) {
	raw := FromAccountExportResponseEntityToHTTPResponse(&entity.AccountExport{})
	for _, key := range []string{`"presets":[]`, `"workspaces":[]`, `"memories":[]`, `"panoDocs":[]`, `"tasks":[]`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s has no %s", raw, key)
		}
	}
}
