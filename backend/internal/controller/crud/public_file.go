// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"net/http"
	"regexp"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// PublicFileController reads the files anyone holding a link may read.
type PublicFileController interface {
	GetPublicFile(ctx context.Context, req entity.GetPublicFileRequest) (*entity.GetPublicFileResponse, error)
}

// The keys a public link may name, exactly as the server makes them: every
// segment a base62 monoflake id of fixed length.
var publicFileKeys = map[string]*regexp.Regexp{
	entity.PublicFileArtifacts: regexp.MustCompile(`^w-[0-9A-Za-z]{11}/[0-9A-Za-z]{11}/[0-9A-Za-z]{11}$`),
	entity.PublicFileSkills:    regexp.MustCompile(`^u-[0-9A-Za-z]{11}/skill-[0-9A-Za-z]{11}/[0-9A-Za-z]{11}$`),
}

// GetPublicFile reads an attachment or a skill file by the key in its public
// link. Anything that is not such a key is not found, whatever is on disk.
func (c *controller) GetPublicFile(_ context.Context, req entity.GetPublicFileRequest) (*entity.GetPublicFileResponse, error) {
	pattern, ok := publicFileKeys[req.Kind]
	if !ok || !pattern.MatchString(req.Key) {
		return nil, base.ErrNotFound
	}
	store := c.storage
	if req.Kind == entity.PublicFileSkills {
		store = c.skillStorage
	}
	data, err := store.LoadRaw(req.Key)
	if err != nil {
		return nil, base.ErrNotFound
	}
	contentType := skillContentType
	if req.Kind == entity.PublicFileArtifacts {
		// Nothing but the bytes is known here, so the type is sniffed, and only a harmless one is kept.
		contentType = storage.SafeContentType(http.DetectContentType(data))
	}
	return &entity.GetPublicFileResponse{Data: data, ContentType: contentType}, nil
}
