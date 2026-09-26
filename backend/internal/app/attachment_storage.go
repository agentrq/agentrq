// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// attachmentS3Namespace is the key prefix attachments get in the bucket.
const attachmentS3Namespace = "attachments"

// newAttachmentStorage stores attachments in localDir unless
// attachments.storage asks for S3, where each one gets a public link.
func newAttachmentStorage(c config.Service, localDir string) (storage.Service, error) {
	return newBlobStorage(c, "attachments",
		func() (storage.Service, error) { return storage.New(localDir) },
		func(client s3.Service) storage.Service { return storage.NewS3Public(client, attachmentS3Namespace) })
}
