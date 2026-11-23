// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"path/filepath"

	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// attachmentS3Namespace is the key prefix attachments get in the bucket.
const attachmentS3Namespace = "artifacts"

// newAttachmentStorage stores attachments under dir/artifacts, read
// publicly at urlBase, unless artifacts.storage asks for S3, where they are
// public in the bucket. Either way they are keyed by storage.AttachmentKey.
// Those saved before that, flat in dir, are still found there.
func newAttachmentStorage(c config.Service, dir, urlBase string) (storage.Service, error) {
	legacy, err := storage.New(dir)
	if err != nil {
		return nil, err
	}
	svc, err := newBlobStorage(c, "artifacts",
		func() (storage.Service, error) {
			return storage.NewNestedPublic(filepath.Join(dir, cleanup.ArtifactsDir), urlBase)
		},
		func(client s3.Service) storage.Service { return storage.NewS3Public(client, attachmentS3Namespace) })
	if err != nil {
		return nil, err
	}
	return storage.WithFallback(svc, legacy), nil
}
