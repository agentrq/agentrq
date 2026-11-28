// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/mustafaturan/monoflake"
)

// exportID is an id as the API writes it, or "" for none.
func exportID(id int64) string {
	if id == 0 {
		return ""
	}
	return monoflake.ID(id).String()
}

func FromAccountExportResponseEntityToHTTPResponse(rs *entity.AccountExport) []byte {
	out := view.AccountExport{
		Version:    rs.Version,
		ExportedAt: rs.ExportedAt,
		Profile:    view.AccountExportProfile{ID: exportID(rs.Profile.ID), Email: rs.Profile.Email, Name: rs.Profile.Name},
		Settings: view.AccountSettings{Display: view.DisplaySettings{
			TimeFormat: rs.Settings[entity.SettingsDisplay][entity.SettingTimeFormat],
		}},
		Presets:    make([]view.AccountExportPreset, len(rs.Presets)),
		Workspaces: make([]view.AccountExportWorkspace, len(rs.Workspaces)),
		Memories:   make([]view.AccountExportMemory, len(rs.Memories)),
		PanoDocs:   make([]view.AccountExportPanoDoc, len(rs.PanoDocs)),
		Tasks:      make([]view.AccountExportTask, len(rs.Tasks)),
	}
	for i, p := range rs.Presets {
		out.Presets[i] = view.AccountExportPreset{WorkspaceID: exportID(p.WorkspaceID), Name: p.Name, Body: p.Body, CreatedAt: p.CreatedAt}
	}
	for i, w := range rs.Workspaces {
		out.Workspaces[i] = view.AccountExportWorkspace{
			ID: exportID(w.ID), Name: w.Name, Description: w.Description, Icon: w.Icon,
			WorkingDirectory: w.WorkingDirectory, SelfLearningLoopNote: w.SelfLearningLoopNote,
			ForkOfID: exportID(w.ForkOfID), CreatedAt: w.CreatedAt, ArchivedAt: w.ArchivedAt,
		}
	}
	for i, m := range rs.Memories {
		out.Memories[i] = view.AccountExportMemory{WorkspaceID: exportID(m.WorkspaceID), Name: m.Name, Content: m.Content, UpdatedAt: m.UpdatedAt}
	}
	for i, d := range rs.PanoDocs {
		out.PanoDocs[i] = view.AccountExportPanoDoc{ID: exportID(d.ID), Title: d.Title, Markdown: d.Markdown, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	}
	for i, t := range rs.Tasks {
		msgs := make([]view.AccountExportMessage, len(t.Messages))
		for j, m := range t.Messages {
			msgs[j] = view.AccountExportMessage{Sender: m.Sender, Text: m.Text, CreatedAt: m.CreatedAt}
		}
		out.Tasks[i] = view.AccountExportTask{
			ID: exportID(t.ID), WorkspaceID: exportID(t.WorkspaceID), ParentID: exportID(t.ParentID),
			Title: t.Title, Body: t.Body, Status: t.Status, CreatedBy: t.CreatedBy, Assignee: t.Assignee,
			CronSchedule: t.CronSchedule, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Messages: msgs,
		}
	}
	payload, _ := json.Marshal(out)
	return payload
}
