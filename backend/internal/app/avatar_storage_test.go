// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
)

func TestNewAvatars(t *testing.T) {
	t.Run("keeps no copies without an S3 endpoint and bucket", func(t *testing.T) {
		for _, doc := range []string{"{}", "s3: {endpoint: 'https://s3.example'}", "s3: {bucket: b}", "s3: {endpoint: ' ', bucket: ' '}"} {
			got, err := newAvatars(newYAMLConfig(t, doc), image.New())
			if err != nil || got != nil {
				t.Errorf("%s: newAvatars returned %v, %v, want none", doc, got, err)
			}
		}
	})

	t.Run("keeps copies in the bucket when S3 is configured", func(t *testing.T) {
		got, err := newAvatars(newYAMLConfig(t, "s3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}"), image.New())
		if err != nil || got == nil {
			t.Errorf("newAvatars returned %v, %v, want an avatar service", got, err)
		}
	})

	t.Run("an s3 section that does not read refuses to start", func(t *testing.T) {
		if _, err := newAvatars(newYAMLConfig(t, "s3: [not, a, map]"), image.New()); err == nil {
			t.Error("newAvatars returned no error, want one")
		}
	})

	t.Run("an S3 client that cannot be built refuses to start", func(t *testing.T) {
		old := newS3
		defer func() { newS3 = old }()
		newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
		_, err := newAvatars(newYAMLConfig(t, "s3: {endpoint: 'https://s3.example', bucket: b}"), image.New())
		if err == nil || err.Error() != "s3: no S3 credentials configured" {
			t.Errorf("newAvatars returned %v, want \"s3: no S3 credentials configured\"", err)
		}
	})
}

func TestNewRefusesAnS3ClientForAvatarsThatCannotBeBuilt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "avatars-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(dir, "agentrq.db"))
	t.Setenv("AGENTRQ_STORAGE_DIR", dir)
	t.Setenv("AGENTRQ_S3_ENDPOINT", "https://s3.example")
	t.Setenv("AGENTRQ_S3_BUCKET", "b")
	t.Chdir("../../cmd/server")
	old := newS3
	defer func() { newS3 = old }()
	newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	for key, dst := range map[string]any{"app": &cfg.App, "auth": &cfg.Auth, "storage": &cfg.Storage, "idgen": &cfg.Idgen} {
		if err := svc.Populate(key, dst); err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
	}
	cfg.ConfigSvc = svc
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "avatars: s3: no S3 credentials configured") {
		t.Fatalf("New = %v, want the avatars' S3 client refused", err)
	}
}
