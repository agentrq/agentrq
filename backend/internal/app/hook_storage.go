// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const (
	// hookStorageDir is the storage directory's folder for hook modules. No
	// public link reaches it, and the attachment cleanup leaves it alone.
	hookStorageDir = "hooks"
	// hookS3Namespace is the key prefix hook modules get in the bucket.
	hookS3Namespace = "hooks"
)

// newHookStorage keeps hook modules under localDir, unless hooks.storage
// asks for S3. Never public, in either: a module may be an account's own.
func newHookStorage(c config.Service, localDir string) (storage.Service, error) {
	return newBlobStorage(c, "hooks",
		func() (storage.Service, error) { return storage.NewNested(localDir) },
		func(client s3.Service) storage.Service { return storage.NewS3(client, hookS3Namespace) })
}
