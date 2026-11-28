// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/mustafaturan/monoflake"
)

const (
	// MaxPresetBodyBytes caps what one preset puts in the composer.
	MaxPresetBodyBytes = 16 * 1024
	// MaxPresetsPerWorkspace caps the presets one workspace keeps.
	MaxPresetsPerWorkspace = 100
)

// PresetController keeps a workspace's presets: messages the composer puts in
// place of #name. A fork reads and writes its parent's. Workspace 0 is the
// account's own presets, offered in every workspace.
type PresetController interface {
	ListPresets(ctx context.Context, req entity.ListPresetsRequest) (*entity.ListPresetsResponse, error)
	CreatePreset(ctx context.Context, req entity.CreatePresetRequest) (*entity.CreatePresetResponse, error)
	UpdatePreset(ctx context.Context, req entity.UpdatePresetRequest) (*entity.UpdatePresetResponse, error)
	DeletePreset(ctx context.Context, req entity.DeletePresetRequest) error
}

func (c *controller) ListPresets(ctx context.Context, req entity.ListPresetsRequest) (*entity.ListPresetsResponse, error) {
	uid, workspaceID, err := c.presetOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	models, err := c.repository.ListPresets(ctx, uid, workspaceID)
	if err != nil {
		return nil, err
	}
	presets := make([]entity.Preset, len(models))
	for i, m := range models {
		presets[i] = fromModelPreset(m)
	}
	return &entity.ListPresetsResponse{Presets: presets}, nil
}

// CreatePreset saves a preset. Its name follows the skill naming rules, so
// #name and /name are typed the same way.
func (c *controller) CreatePreset(ctx context.Context, req entity.CreatePresetRequest) (*entity.CreatePresetResponse, error) {
	name, err := validPreset(req.Name, req.Body)
	if err != nil {
		return nil, err
	}
	uid, workspaceID, err := c.presetOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	existing, err := c.repository.ListPresets(ctx, uid, workspaceID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxPresetsPerWorkspace {
		keeper := "a workspace"
		if workspaceID == 0 {
			keeper = "an account"
		}
		return nil, &entity.PresetError{Message: fmt.Sprintf("%s keeps up to %d presets; remove one first", keeper, MaxPresetsPerWorkspace)}
	}
	saved, err := c.repository.CreatePreset(ctx, model.Preset{
		ID:          c.idgen.NextID(),
		CreatedAt:   time.Now(),
		UserID:      uid,
		WorkspaceID: workspaceID,
		Name:        name,
		Body:        req.Body,
	})
	if errors.Is(err, base.ErrPresetExists) {
		return nil, &entity.PresetError{Message: fmt.Sprintf("a preset named %q exists", name), Conflict: true}
	}
	if err != nil {
		return nil, err
	}
	c.emitPresetEvent(ctx, entity.ActionPresetCreate, req.WorkspaceID, uid)
	return &entity.CreatePresetResponse{Preset: fromModelPreset(saved)}, nil
}

// UpdatePreset edits a preset's body, and renames it when a new name is
// given. The new name is checked as CreatePreset checks one.
func (c *controller) UpdatePreset(ctx context.Context, req entity.UpdatePresetRequest) (*entity.UpdatePresetResponse, error) {
	newName := req.NewName
	if newName == "" {
		newName = req.Name
	}
	name, err := validPreset(newName, req.Body)
	if err != nil {
		return nil, err
	}
	uid, workspaceID, err := c.presetOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	saved, err := c.repository.UpdatePreset(ctx, uid, workspaceID, req.Name, model.Preset{Name: name, Body: req.Body})
	if errors.Is(err, base.ErrPresetExists) {
		return nil, &entity.PresetError{Message: fmt.Sprintf("a preset named %q exists", name), Conflict: true}
	}
	if err != nil {
		return nil, err
	}
	c.emitPresetEvent(ctx, entity.ActionPresetUpdate, req.WorkspaceID, uid)
	return &entity.UpdatePresetResponse{Preset: fromModelPreset(saved)}, nil
}

// presetOwner returns the account and the workspace whose presets a request
// reads and writes. Workspace 0 is the account's own presets, which need no
// workspace access; any other is checked as a memory request is, and a fork
// resolves to its parent.
func (c *controller) presetOwner(ctx context.Context, workspaceID int64, userID string) (int64, int64, error) {
	if workspaceID != 0 {
		return c.memoryOwner(ctx, workspaceID, userID)
	}
	uid := monoflake.IDFromBase62(userID).Int64()
	if uid == 0 {
		return 0, 0, fmt.Errorf("invalid request")
	}
	return uid, 0, nil
}

// validPreset returns name in its canonical form, or the PresetError that
// refuses the name or the body.
func validPreset(name, body string) (string, error) {
	canonical, err := skill.CanonicalName(name)
	if err != nil {
		return "", &entity.PresetError{Message: fmt.Sprintf("preset name %q is not usable; names are lowercase letters and digits joined by single hyphens, like %q, up to %d characters", name, "pull-n-push", skill.MaxNameLength)}
	}
	if strings.TrimSpace(body) == "" {
		return "", &entity.PresetError{Message: "preset body is empty"}
	}
	if len(body) > MaxPresetBodyBytes {
		return "", &entity.PresetError{Message: fmt.Sprintf("preset body is %d bytes; the limit is %d", len(body), MaxPresetBodyBytes)}
	}
	return canonical, nil
}

func (c *controller) DeletePreset(ctx context.Context, req entity.DeletePresetRequest) error {
	uid, workspaceID, err := c.presetOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return err
	}
	if err := c.repository.DeletePreset(ctx, uid, workspaceID, req.Name); err != nil {
		return err
	}
	c.emitPresetEvent(ctx, entity.ActionPresetDelete, req.WorkspaceID, uid)
	return nil
}

// emitPresetEvent counts a preset change against the workspace it was made
// on, a fork included; an account preset's counts against workspace 0.
func (c *controller) emitPresetEvent(ctx context.Context, action entity.Action, workspaceID, userID int64) {
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		WorkspaceID:  workspaceID,
		UserID:       userID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   workspaceID,
		Actor:        entity.ActorHuman,
	})
}

func fromModelPreset(m model.Preset) entity.Preset {
	return entity.Preset{
		ID:          m.ID,
		CreatedAt:   m.CreatedAt,
		WorkspaceID: m.WorkspaceID,
		Name:        m.Name,
		Body:        m.Body,
	}
}
