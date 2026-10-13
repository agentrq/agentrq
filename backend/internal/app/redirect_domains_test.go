// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// The env var reaches auth.allowedRedirectDomains through the real base.yaml.
func TestAllowedRedirectDomainsFromEnv(t *testing.T) {
	for _, tt := range []struct {
		env  string
		want []string
	}{
		{"", nil},
		{"app.example.com, docs.example.com", []string{"app.example.com", "docs.example.com"}},
	} {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("ENV", "development")
			t.Setenv("AGENTRQ_AUTH_ALLOWED_REDIRECT_DOMAINS", tt.env)
			t.Chdir("../../cmd/server")
			svc, err := config.New()
			if err != nil {
				t.Fatal(err)
			}
			var cfg Config
			if err := svc.Populate("auth", &cfg.Auth); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Auth.AllowedRedirectDomains, tt.want) {
				t.Errorf("AGENTRQ_AUTH_ALLOWED_REDIRECT_DOMAINS=%q gave %q, want %q", tt.env, cfg.Auth.AllowedRedirectDomains, tt.want)
			}
		})
	}
}
