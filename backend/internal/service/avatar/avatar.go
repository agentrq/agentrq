// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package avatar keeps a copy of each user's sign-in picture, since the
// link a provider hands out may stop working.
package avatar

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
)

const (
	// Size is the side, in pixels, of every picture kept.
	Size = 32
	// maxDownload is the largest picture fetched from a provider.
	maxDownload = 5 << 20
	// fetchTimeout bounds the copy, which runs while somebody signs in.
	fetchTimeout = 5 * time.Second
)

type (
	Service interface {
		// Mirror copies the picture at src to storage as a Size×Size PNG
		// and returns its public link. current is the user's link now: a
		// copy already there is not uploaded again.
		Mirror(ctx context.Context, userID int64, src, current string) (string, error)
	}

	Params struct {
		// Storage keeps the pictures, and must give each a public link.
		Storage storage.Service
		Image   image.Service
		// HTTPClient fetches the provider's picture. Nil is
		// http.DefaultClient.
		HTTPClient *http.Client
	}

	service struct {
		storage storage.Service
		image   image.Service
		client  *http.Client
	}
)

func New(p Params) Service {
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &service{storage: p.Storage, image: p.Image, client: client}
}

func (s *service) Mirror(ctx context.Context, userID int64, src, current string) (string, error) {
	if !strings.HasPrefix(src, "https://") {
		return "", fmt.Errorf("picture link is not https")
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	data, err := s.fetch(ctx, src)
	if err != nil {
		return "", err
	}
	pic, err := s.image.SquarePNG(ctx, data, Size)
	if err != nil {
		return "", err
	}

	// Keyed by content, so a new picture gets a new link that no cache holds.
	sum := sha256.Sum256(pic)
	key := "u-" + monoflake.ID(userID).String() + "/" + hex.EncodeToString(sum[:8]) + ".png"
	if link := storage.PublicURL(s.storage, key); link != "" && link == current {
		return link, nil
	}
	link, err := storage.SaveBlob(s.storage, key, base64.StdEncoding.EncodeToString(pic), "image/png")
	if err != nil {
		return "", fmt.Errorf("save picture: %w", err)
	}
	if link == "" {
		return "", fmt.Errorf("storage gives pictures no public link")
	}
	return link, nil
}

func (s *service) fetch(ctx context.Context, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch picture: status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("picture larger than %d bytes", maxDownload)
	}
	return data, nil
}
