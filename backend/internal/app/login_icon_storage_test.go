// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/config"

	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

func TestNewLoginIconStorage(t *testing.T) {
	t.Run("keeps icons on disk, read through the server, without an S3 endpoint and bucket", func(t *testing.T) {
		dir := t.TempDir()
		for _, doc := range []string{"{}", "s3: {endpoint: 'https://s3.example'}", "s3: {bucket: b}"} {
			got, err := newLoginIconStorage(newYAMLConfig(t, doc), filepath.Join(dir, "login-icons"), "https://app.example/storage/login-icons")
			if err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			if link := storage.PublicURL(got, "0123456789abcdef"); link != "https://app.example/storage/login-icons/0123456789abcdef" {
				t.Errorf("%s: link = %q, want the server's", doc, link)
			}
		}
	})

	t.Run("keeps icons public in the bucket when S3 is configured", func(t *testing.T) {
		got, err := newLoginIconStorage(newYAMLConfig(t, "s3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}"), t.TempDir(), "unused")
		if err != nil {
			t.Fatal(err)
		}
		if link := storage.PublicURL(got, "0123456789abcdef"); link != "http://127.0.0.1:9/b/login-icons/0123456789abcdef" {
			t.Errorf("link = %q, want the bucket's", link)
		}
	})

	t.Run("an s3 section that does not read refuses to start", func(t *testing.T) {
		if _, err := newLoginIconStorage(newYAMLConfig(t, "s3: [not, a, map]"), t.TempDir(), "u"); err == nil {
			t.Error("newLoginIconStorage returned no error, want one")
		}
	})

	t.Run("an S3 client that cannot be built refuses to start", func(t *testing.T) {
		old := newS3
		defer func() { newS3 = old }()
		newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
		_, err := newLoginIconStorage(newYAMLConfig(t, "s3: {endpoint: 'https://s3.example', bucket: b}"), t.TempDir(), "u")
		if err == nil || err.Error() != "s3: no S3 credentials configured" {
			t.Errorf("newLoginIconStorage returned %v", err)
		}
	})
}

func TestNewOAuth2Logins(t *testing.T) {
	if got := newOAuth2Logins("https://app.example", nil); len(got) != 0 {
		t.Fatalf("no providers = %v", got)
	}
	p := auth.OAuth2Provider{ID: "gitlab", Name: "GitLab", IconURL: "https://i/x", ClientID: "c", AuthURL: "https://gl/a"}
	got := newOAuth2Logins("https://app.example", []auth.OAuth2Provider{p})
	if len(got) != 1 || got[0].ID != "gitlab" || got[0].Name != "GitLab" || got[0].IconURL != "https://i/x" {
		t.Fatalf("logins = %+v", got)
	}
	u, _ := url.Parse(got[0].Service.GetAuthURL("st"))
	if cb := u.Query().Get("redirect_uri"); cb != "https://app.example/api/v1/auth/oauth2/gitlab/callback" {
		t.Errorf("callback = %q", cb)
	}
}

// The avatars' S3 client is built first; the icons' failing after it still
// stops the server.
func TestNewRefusesAnS3ClientForLoginIconsThatCannotBeBuilt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "icons-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(dir, "agentrq.db"))
	t.Setenv("AGENTRQ_STORAGE_DIR", dir)
	t.Setenv("AGENTRQ_S3_ENDPOINT", "http://127.0.0.1:9")
	t.Setenv("AGENTRQ_S3_BUCKET", "b")
	t.Chdir("../../cmd/server")
	old := newS3
	defer func() { newS3 = old }()
	calls := 0
	newS3 = func(p s3.Params) (s3.Service, error) {
		if calls++; calls > 1 {
			return nil, errors.New("no S3 credentials configured")
		}
		return old(p)
	}
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
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "login icon storage: s3: no S3 credentials configured") {
		t.Fatalf("New = %v, want the icons' S3 client refused", err)
	}
}
