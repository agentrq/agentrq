// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// skillS3Namespace is the key prefix skill blobs get in the bucket.
const skillS3Namespace = "skills"

// newSkillStorage stores skills under localDir unless skills.storage asks for
// S3. An unknown value refuses to start, rather than silently writing skills
// to disk.
func newSkillStorage(c config.Service, localDir string) (storage.Service, error) {
	return newBlobStorage(c, "skills",
		func() (storage.Service, error) { return storage.NewNested(localDir) },
		func(client s3.Service) storage.Service { return storage.NewS3(client, skillS3Namespace) })
}
