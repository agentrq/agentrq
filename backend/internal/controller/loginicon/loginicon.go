// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package loginicon keeps the icons the operator gives sign-in providers,
// resized to the size the sign-in page shows them at.
package loginicon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const (
	// Size is the side, in pixels, of every icon kept: the sign-in button
	// shows it at 20 CSS pixels, so twice that stays sharp on a dense screen.
	Size = 40
	// MaxUpload is the largest image taken.
	MaxUpload = 2 << 20
)

type (
	Controller interface {
		// Upload crops the image to its centred square, resizes it to a
		// Size×Size PNG, and stores it under a public link.
		Upload(ctx context.Context, rq entity.UploadLoginIconRequest) (*entity.UploadLoginIconResponse, error)
	}

	Params struct {
		// Storage keeps the icons, and must give each a public link.
		Storage storage.Service
		Image   image.Service
	}

	controller struct {
		storage storage.Service
		image   image.Service
	}
)

func New(p Params) Controller {
	return &controller{storage: p.Storage, image: p.Image}
}

func (c *controller) Upload(ctx context.Context, rq entity.UploadLoginIconRequest) (*entity.UploadLoginIconResponse, error) {
	if len(rq.Data) > MaxUpload {
		return nil, &entity.AppConfigurationError{Message: fmt.Sprintf("the icon is larger than %d bytes", MaxUpload)}
	}
	pic, err := c.image.SquarePNG(ctx, rq.Data, Size)
	if err != nil {
		return nil, &entity.AppConfigurationError{Message: "the icon must be a PNG, JPEG, GIF or WebP image, at most 4096 pixels a side"}
	}
	// Keyed by content, so a new icon gets a new link that no cache holds.
	sum := sha256.Sum256(pic)
	link, err := storage.SaveBlob(c.storage, hex.EncodeToString(sum[:8]), base64.StdEncoding.EncodeToString(pic), "image/png")
	if err != nil {
		return nil, fmt.Errorf("save login icon: %w", err)
	}
	if link == "" {
		return nil, fmt.Errorf("storage gives login icons no public link")
	}
	return &entity.UploadLoginIconResponse{URL: link}, nil
}
