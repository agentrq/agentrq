// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"fmt"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const (
	blobStorageLocal = "local"
	blobStorageS3    = "s3"
)

// BlobStorageConfig chooses where skill file content, or attachments, are kept.
type BlobStorageConfig struct {
	Storage string `yaml:"storage"`
}

var newS3 = s3.New

// defaultStorageDir is where files go when storage.dir is not set.
const defaultStorageDir = "./_storage"

// storageDir is the configured storage.dir, or the default when it is unset.
func storageDir(c cleanup.Config) string {
	if dir := strings.TrimSpace(c.StorageDir); dir != "" {
		return dir
	}
	return defaultStorageDir
}

// newBlobStorage reads <section>.storage and builds the local or S3 store it
// names. An unknown value is an error, so a typo cannot fall back to disk.
func newBlobStorage(c config.Service, section string, local func() (storage.Service, error), remote func(s3.Service) storage.Service) (storage.Service, error) {
	var cfg BlobStorageConfig
	if err := c.Populate(section, &cfg); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Storage)) {
	case "", blobStorageLocal:
		return local()
	case blobStorageS3:
	default:
		return nil, fmt.Errorf("unknown %s storage %q: want %q or %q", section, cfg.Storage, blobStorageLocal, blobStorageS3)
	}
	client, err := newS3(s3.Params{Config: c})
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	return remote(client), nil
}
