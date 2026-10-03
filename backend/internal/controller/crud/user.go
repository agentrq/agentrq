// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// defaultSupervisorWorkspaceDescription and
// defaultSupervisorWorkspaceSelfLearningLoopNote are what every new account's
// auto-created "supervisor" workspace ships with, so a first-time user opens
// it to a working mission rather than a blank one. Deliberately generic — the
// same text for every account, not tailored to any one operator's business.
const (
	defaultSupervisorWorkspaceDescription = `You are the supervisor workspace — the coordination hub for your other AgentRQ workspaces and agents. Your job is to help your human operator get things done by routing work to the right workspace, tracking it through to completion, and keeping the human informed along the way.

**Important**
- This is the only workspace with access to the supervisor-level MCP tools (creating/listing/managing other workspaces and their tasks).
- If a task is assigned to "human", that means the human operator should do it — don't try to complete it yourself.
- Be a responsible, trustworthy delegate: you represent the human's intent to every other agent you talk to.
- If something needs the human's direct attention or a decision only they can make, ask them — don't guess.`

	defaultSupervisorWorkspaceSelfLearningLoopNote = `**Follow**
- When you start working on a task, mark it "ongoing" immediately so it isn't re-assigned.
- Load this workspace's memory before starting work — it may already answer something you'd otherwise ask the human.
- If you hit a problem and find a fix or a lesson worth keeping, save it to memory so you (or another agent) don't repeat it. Keep entries short and specific.
- When you create a task for another agent (or relay one from the human), follow up on it until it's resolved — don't fire-and-forget.
- Sign every message you post into another workspace's task with "[sent by supervisor]", so it's clear the message is relayed, not written by that workspace's own agent.
- When creating a task for another agent, request a clean-slate context (clearContext=true) if the tool in use supports it, since you'll already have written full context into the task body.`
)

func (c *controller) FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
	var u model.User
	var err error

	if req.Email != "" {
		u, err = c.repository.FindUserByEmail(ctx, req.Email)
	} else {
		return nil, nil
	}

	if err == nil {
		updated := false
		if u.Email == "" && req.Email != "" {
			u.Email = req.Email
			updated = true
		}
		if u.Picture == "" && req.Picture != "" {
			u.Picture = req.Picture
			updated = true
		}
		if u.Name == "" && req.Name != "" {
			u.Name = req.Name
			updated = true
		}

		if updated {
			u.UpdatedAt = time.Now()
			u, err = c.repository.UpdateUser(ctx, u)
			if err != nil {
				return nil, err
			}
			c.emitEvent(ctx, entity.CRUDEvent{
				Action:       entity.ActionUserUpdate,
				WorkspaceID:  0,
				UserID:       u.ID,
				ResourceType: entity.ResourceUser,
				ResourceID:   u.ID,
				Actor:        entity.ActorHuman,
				Origin:       entity.OriginAPI,
			})
		}

		return &entity.FindOrCreateUserResponse{
			User: entity.User{
				ID:        u.ID,
				CreatedAt: u.CreatedAt,
				UpdatedAt: u.UpdatedAt,
				Email:     u.Email,
				Name:      u.Name,
				Picture:   u.Picture,
			},
		}, nil
	}

	if err != base.ErrNotFound {
		return nil, err
	}

	// Not found, create new
	newUser := model.User{
		ID:        c.idgen.NextID(),
		Email:     req.Email,
		Name:      req.Name,
		Picture:   req.Picture,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	created, err := c.repository.CreateUser(ctx, newUser)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionUserCreate,
		WorkspaceID:  0,
		UserID:       created.ID,
		ResourceType: entity.ResourceUser,
		ResourceID:   created.ID,
		Actor:        entity.ActorHuman,
		Origin:       entity.OriginAPI,
	})

	// Every new account gets a "supervisor" workspace, the one whose
	// .mcp.json setup snippet also wires in the cross-workspace coremcp tools.
	supervisorWorkspace := model.Workspace{
		ID:                    c.idgen.NextID(),
		CreatedAt:             created.CreatedAt,
		UpdatedAt:             created.CreatedAt,
		UserID:                created.ID,
		Name:                  "supervisor",
		Description:           defaultSupervisorWorkspaceDescription,
		SelfLearningLoopNote:  defaultSupervisorWorkspaceSelfLearningLoopNote,
		InputSendDelaySeconds: defaultInputSendDelaySeconds,
	}
	createdWorkspace, err := c.repository.CreateWorkspace(ctx, supervisorWorkspace)
	if err != nil {
		return nil, fmt.Errorf("create supervisor workspace: %w", err)
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceCreate,
		WorkspaceID:  createdWorkspace.ID,
		UserID:       createdWorkspace.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   createdWorkspace.ID,
		Actor:        entity.ActorHuman,
	})

	return &entity.FindOrCreateUserResponse{
		User: entity.User{
			ID:        created.ID,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
			Email:     created.Email,
			Name:      created.Name,
			Picture:   created.Picture,
		},
	}, nil
}

func (c *controller) CreateUser(ctx context.Context, u entity.User) (entity.User, error) {
	// Simple implementation if needed, but FindOrCreateUser is primary
	newUser := model.User{
		ID:        c.idgen.NextID(),
		Email:     u.Email,
		Name:      u.Name,
		Picture:   u.Picture,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	created, err := c.repository.CreateUser(ctx, newUser)
	if err != nil {
		return entity.User{}, err
	}
	return entity.User{
		ID:        created.ID,
		CreatedAt: created.CreatedAt,
		UpdatedAt: created.UpdatedAt,
		Email:     created.Email,
		Name:      created.Name,
		Picture:   created.Picture,
	}, nil
}

// FindUserByID looks a user up by the id carried in a token subject.
//
// Refreshing a session mints a new access token, and that token carries the
// name and picture the interface displays. Reading them here rather than
// copying them out of the refresh token means a profile edited during a long
// session is picked up at the next refresh instead of being frozen at sign-in.
func (c *controller) FindUserByID(ctx context.Context, id int64) (entity.User, error) {
	u, err := c.repository.SystemGetUser(ctx, id)
	if err != nil {
		return entity.User{}, err
	}
	return entity.User{
		ID:        u.ID,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		Email:     u.Email,
		Name:      u.Name,
		Picture:   u.Picture,
	}, nil
}

func (c *controller) FindUserByEmail(ctx context.Context, email string) (entity.User, error) {
	u, err := c.repository.FindUserByEmail(ctx, email)
	if err != nil {
		return entity.User{}, err
	}
	return entity.User{
		ID:        u.ID,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		Email:     u.Email,
		Name:      u.Name,
		Picture:   u.Picture,
	}, nil
}
