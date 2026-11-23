// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// skillS3Namespace is the key prefix skill blobs get in the bucket.
const skillS3Namespace = "skills"

// newSkillStorage stores skills under localDir, read publicly at urlBase,
// unless skills.storage asks for S3, where they are public in the bucket. An
// unknown value refuses to start, rather than silently writing skills to disk.
func newSkillStorage(c config.Service, localDir, urlBase string) (storage.Service, error) {
	return newBlobStorage(c, "skills",
		func() (storage.Service, error) { return storage.NewNestedPublic(localDir, urlBase) },
		func(client s3.Service) storage.Service { return storage.NewS3Public(client, skillS3Namespace) })
}
