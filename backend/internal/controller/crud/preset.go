// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
)

const (
	// MaxPresetBodyBytes caps what one preset puts in the composer.
	MaxPresetBodyBytes = 16 * 1024
	// MaxPresetsPerWorkspace caps the presets one workspace keeps.
	MaxPresetsPerWorkspace = 100
)

// PresetController keeps a workspace's presets: messages the composer puts in
// place of #name. A fork reads and writes its parent's.
type PresetController interface {
	ListPresets(ctx context.Context, req entity.ListPresetsRequest) (*entity.ListPresetsResponse, error)
	CreatePreset(ctx context.Context, req entity.CreatePresetRequest) (*entity.CreatePresetResponse, error)
	DeletePreset(ctx context.Context, req entity.DeletePresetRequest) error
}

func (c *controller) ListPresets(ctx context.Context, req entity.ListPresetsRequest) (*entity.ListPresetsResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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
	name, err := skill.CanonicalName(req.Name)
	if err != nil {
		return nil, &entity.PresetError{Message: fmt.Sprintf("preset name %q is not usable; names are lowercase letters and digits joined by single hyphens, like %q, up to %d characters", req.Name, "pull-n-push", skill.MaxNameLength)}
	}
	if strings.TrimSpace(req.Body) == "" {
		return nil, &entity.PresetError{Message: "preset body is empty"}
	}
	if len(req.Body) > MaxPresetBodyBytes {
		return nil, &entity.PresetError{Message: fmt.Sprintf("preset body is %d bytes; the limit is %d", len(req.Body), MaxPresetBodyBytes)}
	}
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	existing, err := c.repository.ListPresets(ctx, uid, workspaceID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxPresetsPerWorkspace {
		return nil, &entity.PresetError{Message: fmt.Sprintf("a workspace keeps up to %d presets; remove one first", MaxPresetsPerWorkspace)}
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

func (c *controller) DeletePreset(ctx context.Context, req entity.DeletePresetRequest) error {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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
// on, a fork included.
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
