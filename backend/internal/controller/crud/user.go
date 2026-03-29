package crud

import (
	"context"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
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
