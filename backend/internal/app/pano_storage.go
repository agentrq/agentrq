// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// panoS3Namespace is the key prefix Pano blobs get in the bucket.
const panoS3Namespace = "pano"

// newPanoStorage stores Pano docs and their files under localDir, read
// publicly at urlBase, unless pano.storage asks for S3, where they are public
// in the bucket. An unknown value refuses to start.
func newPanoStorage(c config.Service, localDir, urlBase string) (storage.Service, error) {
	return newBlobStorage(c, "pano",
		func() (storage.Service, error) { return storage.NewNestedPublic(localDir, urlBase) },
		func(client s3.Service) storage.Service { return storage.NewS3Public(client, panoS3Namespace) })
}
