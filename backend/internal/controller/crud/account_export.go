// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

// AccountMaxExportsPerDay is account.maxExportsPerDay: how many times one
// account may download its data in any 24 hours. 0 is no cap.
var AccountMaxExportsPerDay = DefaultAccountMaxExportsPerDay

// DefaultAccountMaxExportsPerDay is the cap a server starts with.
const DefaultAccountMaxExportsPerDay = 2

// exportWindow is the window AccountMaxExportsPerDay counts over.
const exportWindow = 24 * time.Hour

// exportTaskPage is how many tasks, with their messages, an export reads at
// a time.
const exportTaskPage = 500

// AccountExportController gathers everything an account owns, for the person
// to download.
type AccountExportController interface {
	ExportAccountData(ctx context.Context, req entity.ExportAccountDataRequest) (*entity.AccountExport, error)
}

// ExportAccountData counts the download against AccountMaxExportsPerDay,
// then reads the account's settings, presets, workspaces
// (archived too), memories, Pano docs and every task with its conversation.
// Attachments and skill files are left out: their public links are their
// credential, and they are downloaded where they are shown.
func (c *controller) ExportAccountData(ctx context.Context, req entity.ExportAccountDataRequest) (*entity.AccountExport, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid request")
	}
	now := time.Now().UTC()
	if err := c.countAccountExport(ctx, uid, now); err != nil {
		return nil, err
	}
	out := &entity.AccountExport{
		Version:    entity.AccountExportVersion,
		ExportedAt: now,
		Profile:    entity.AccountExportProfile{ID: uid, Email: req.Email, Name: req.Name},
		Presets:    []entity.AccountExportPreset{},
		Workspaces: []entity.AccountExportWorkspace{},
		Memories:   []entity.AccountExportMemory{},
		PanoDocs:   []entity.AccountExportPanoDoc{},
		Tasks:      []entity.AccountExportTask{},
	}

	settings, err := c.repository.ListAccountSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	out.Settings = accountSettingsOf(settings)

	presets, err := c.repository.ExportPresets(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, p := range presets {
		out.Presets = append(out.Presets, entity.AccountExportPreset{WorkspaceID: p.WorkspaceID, Name: p.Name, Body: p.Body, CreatedAt: p.CreatedAt})
	}

	workspaces, err := c.repository.ListWorkspaces(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	for _, w := range workspaces {
		out.Workspaces = append(out.Workspaces, entity.AccountExportWorkspace{
			ID: w.ID, Name: w.Name, Description: w.Description, Icon: w.Icon,
			WorkingDirectory: w.WorkingDirectory, SelfLearningLoopNote: w.SelfLearningLoopNote,
			ForkOfID: w.ForkOfID, CreatedAt: w.CreatedAt, ArchivedAt: w.ArchivedAt,
		})
	}

	memories, err := c.repository.ExportMemories(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, m := range memories {
		out.Memories = append(out.Memories, entity.AccountExportMemory{WorkspaceID: m.WorkspaceID, Name: m.Name, Content: m.Content, UpdatedAt: m.UpdatedAt})
	}

	docs, err := c.repository.ExportPanoDocs(ctx, uid, now)
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		out.PanoDocs = append(out.PanoDocs, entity.AccountExportPanoDoc{ID: d.ID, Title: d.Title, Markdown: d.Markdown, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt})
	}

	for after := int64(0); ; {
		tasks, err := c.repository.ExportTasks(ctx, uid, after, exportTaskPage)
		if err != nil {
			return nil, err
		}
		if len(tasks) == 0 {
			break
		}
		if err := c.appendExportTasks(ctx, out, tasks); err != nil {
			return nil, err
		}
		if len(tasks) < exportTaskPage {
			break
		}
		after = tasks[len(tasks)-1].ID
	}
	// The account is emailed that its data was taken, from this event.
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionAccountExport,
		UserID:       uid,
		ResourceType: entity.ResourceUser,
		ResourceID:   uid,
		Actor:        entity.ActorHuman,
	})
	return out, nil
}

// countAccountExport keeps this download and refuses it when it is past the
// cap. Kept first and counted after, so two downloads at once cannot both
// slip under it; a refused one is forgotten again.
func (c *controller) countAccountExport(ctx context.Context, uid int64, now time.Time) error {
	limit := AccountMaxExportsPerDay
	if limit <= 0 {
		return nil
	}
	id := c.idgen.NextID()
	if err := c.repository.RecordAccountExport(ctx, model.AccountExport{ID: id, UserID: uid, CreatedAt: now}); err != nil {
		return err
	}
	times, err := c.repository.ListAccountExportsSince(ctx, uid, now.Add(-exportWindow))
	if err != nil {
		return err
	}
	if len(times) <= limit {
		return nil
	}
	if err := c.repository.ForgetAccountExport(ctx, id); err != nil {
		return err
	}
	// This one is the newest. Of the others, a slot frees when all but
	// limit-1 of them have aged out.
	others := times[:len(times)-1]
	return &entity.AccountExportThrottled{Limit: limit, RetryAt: others[len(others)-limit].Add(exportWindow)}
}

// appendExportTasks adds a page of tasks to out, each with its messages.
func (c *controller) appendExportTasks(ctx context.Context, out *entity.AccountExport, tasks []model.Task) error {
	ids := make([]int64, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	messages, err := c.repository.ExportMessages(ctx, ids)
	if err != nil {
		return err
	}
	byTask := make(map[int64][]entity.AccountExportMessage, len(tasks))
	for _, m := range messages {
		byTask[m.TaskID] = append(byTask[m.TaskID], entity.AccountExportMessage{Sender: m.Sender, Text: m.Text, CreatedAt: m.CreatedAt})
	}
	for _, t := range tasks {
		msgs := byTask[t.ID]
		if msgs == nil {
			msgs = []entity.AccountExportMessage{}
		}
		out.Tasks = append(out.Tasks, entity.AccountExportTask{
			ID: t.ID, WorkspaceID: t.WorkspaceID, ParentID: t.ParentID, Title: t.Title, Body: t.Body,
			Status: t.Status, CreatedBy: t.CreatedBy, Assignee: t.Assignee, CronSchedule: t.CronSchedule,
			CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Messages: msgs,
		})
	}
	return nil
}
