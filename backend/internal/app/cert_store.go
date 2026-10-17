// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/server"
)

var newCertS3 = s3.NewFromConfig

// newCertStore is the bucket ssl.s3 names, which certificates are shared
// through, or nil when it is not enabled. It is a bucket of its own, apart
// from the s3 section files are stored in.
func newCertStore(cfg s3.Config) (server.CertStore, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("ssl.s3.bucket is required when ssl.s3.enabled is true")
	}
	client, err := newCertS3(cfg)
	if err != nil {
		return nil, fmt.Errorf("ssl s3: %w", err)
	}
	return client, nil
}
