package crud

import (
	"context"
	"time"

	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
)

// UserController defines user operations.
type UserController interface {
	FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error)
}

func (c *controller) FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
	var u model.User
	var err error

	if req.Email != "" {
		u, err = c.repository.FindUserByEmail(ctx, req.Email)
	} else if req.ExternalID != "" {
		u, err = c.repository.FindUserByExternalID(ctx, req.ExternalID)
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
		}

		return &entity.FindOrCreateUserResponse{
			User: entity.User{
				ID:         u.ID,
				CreatedAt:  u.CreatedAt,
				UpdatedAt:  u.UpdatedAt,
				Email:      u.Email,
				ExternalID: u.ExternalID,
				Name:       u.Name,
				Picture:    u.Picture,
			},
		}, nil
	}

	if err != base.ErrNotFound {
		return nil, err
	}

	// Not found, create new
	newUser := model.User{
		ID:         c.idgen.NextID(),
		Email:      req.Email,
		ExternalID: req.ExternalID,
		Name:       req.Name,
		Picture:    req.Picture,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	created, err := c.repository.CreateUser(ctx, newUser)
	if err != nil {
		return nil, err
	}

	return &entity.FindOrCreateUserResponse{
		User: entity.User{
			ID:         created.ID,
			CreatedAt:  created.CreatedAt,
			UpdatedAt:  created.UpdatedAt,
			Email:      created.Email,
			ExternalID: created.ExternalID,
			Name:       created.Name,
			Picture:    created.Picture,
		},
	}, nil
}
