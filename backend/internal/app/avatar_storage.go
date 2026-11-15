// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/avatar"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// bucketConfig is the s3 section, and whether it names an endpoint and a
// bucket: then sign-in pictures and sign-in providers' icons are kept there.
func bucketConfig(c config.Service) (s3.Params, bool, error) {
	var cfg s3.Config
	if err := c.Populate("s3", &cfg); err != nil {
		return s3.Params{}, false, err
	}
	ok := strings.TrimSpace(cfg.Endpoint) != "" && strings.TrimSpace(cfg.Bucket) != ""
	return s3.Params{Config: c}, ok, nil
}

// avatarS3Namespace is the key prefix sign-in pictures get in the bucket.
const avatarS3Namespace = "avatars"

// newAvatars keeps sign-in pictures public in the bucket under avatars, when
// the s3 section names an endpoint and a bucket. Without them it is nil, and
// a user's picture stays the provider's link.
func newAvatars(c config.Service, img image.Service) (avatar.Service, error) {
	params, ok, err := bucketConfig(c)
	if err != nil || !ok {
		return nil, err
	}
	client, err := newS3(params)
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	return avatar.New(avatar.Params{
		Storage: storage.NewS3Public(client, avatarS3Namespace),
		Image:   img,
	}), nil
}
