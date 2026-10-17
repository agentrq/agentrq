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
	"github.com/agentrq/agentrq/backend/internal/service/s3"
)

func TestNewCertStore(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		store, err := newCertStore(s3.Config{Bucket: "certs"})
		if store != nil || err != nil {
			t.Errorf("got %v, %v; want no bucket", store, err)
		}
	})
	t.Run("NoBucket", func(t *testing.T) {
		_, err := newCertStore(s3.Config{Enabled: true, Bucket: " "})
		if err == nil || !strings.Contains(err.Error(), "ssl.s3.bucket is required") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("ClientFails", func(t *testing.T) {
		old := newCertS3
		defer func() { newCertS3 = old }()
		newCertS3 = func(s3.Config) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
		_, err := newCertStore(s3.Config{Enabled: true, Bucket: "certs"})
		if err == nil || !strings.Contains(err.Error(), "ssl s3: no S3 credentials configured") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("Enabled", func(t *testing.T) {
		store, err := newCertStore(s3.Config{Enabled: true, Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "certs"})
		if store == nil || err != nil {
			t.Errorf("got %v, %v; want a bucket", store, err)
		}
	})
}

// The AGENTRQ_SSL_S3_* variables reach ssl.s3 through the real base.yaml, and
// leave the s3 section files are stored in alone.
func TestSSLS3FromEnv(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_SSL_S3_ENABLED", "true")
	t.Setenv("AGENTRQ_SSL_S3_ENDPOINT", "https://certs.example.com")
	t.Setenv("AGENTRQ_SSL_S3_ACCESS_KEY", "cert-ak")
	t.Setenv("AGENTRQ_SSL_S3_SECRET_ACCESS_KEY", "cert-sk")
	t.Setenv("AGENTRQ_SSL_S3_REGION", "eu-west-1")
	t.Setenv("AGENTRQ_SSL_S3_BUCKET", "agentrq-certs")
	t.Setenv("AGENTRQ_S3_BUCKET", "agentrq-files")
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := svc.Populate("ssl", &cfg.SSL); err != nil {
		t.Fatal(err)
	}
	want := s3.Config{Enabled: true, Endpoint: "https://certs.example.com", AccessKey: "cert-ak", SecretAccessKey: "cert-sk", Region: "eu-west-1", Bucket: "agentrq-certs"}
	if cfg.SSL.S3 != want {
		t.Errorf("ssl.s3 = %+v, want %+v", cfg.SSL.S3, want)
	}
	var files s3.Config
	if err := svc.Populate("s3", &files); err != nil {
		t.Fatal(err)
	}
	if files.Bucket != "agentrq-files" {
		t.Errorf("s3.bucket = %q, want the files bucket", files.Bucket)
	}
}

// Off unless asked for.
func TestSSLS3IsOffByDefault(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := svc.Populate("ssl", &cfg.SSL); err != nil {
		t.Fatal(err)
	}
	if cfg.SSL.S3.Enabled || cfg.SSL.S3.Region != "us-east-1" {
		t.Errorf("ssl.s3 = %+v, want disabled in us-east-1", cfg.SSL.S3)
	}
}

// A certificate bucket switched on with no bucket named stops startup,
// before the database is opened.
func TestNewRefusesSSLS3WithoutABucket(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "cert-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(t.TempDir(), "missing", "agentrq.db"))
	t.Setenv("AGENTRQ_SSL_S3_ENABLED", "true")
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	for key, dst := range map[string]any{"app": &cfg.App, "auth": &cfg.Auth, "smtp": &cfg.SMTP, "ssl": &cfg.SSL,
		"slack": &cfg.Slack, "webPush": &cfg.WebPush, "storage": &cfg.Storage, "memory": &cfg.Memory,
		"skills": &cfg.Skills, "pano": &cfg.Pano, "acp": &cfg.ACP, "idgen": &cfg.Idgen} {
		if err := svc.Populate(key, dst); err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
	}
	cfg.ConfigSvc = svc
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "ssl.s3.bucket is required") {
		t.Errorf("New: %v, want the missing bucket refused", err)
	}
}
