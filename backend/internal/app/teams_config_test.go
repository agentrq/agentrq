// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
)

// loadMaxInvites reads, with the env unset when env is empty, teams.maxInvitesPerDay from the real base.yaml, as main does.
func loadMaxInvites(t *testing.T, env string) int {
	t.Helper()
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_TEAMS_MAX_INVITES_PER_DAY", env)
	if env == "" {
		os.Unsetenv("AGENTRQ_TEAMS_MAX_INVITES_PER_DAY") // t.Setenv restores it
	}
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := svc.Populate("teams", &cfg.Teams); err != nil {
		t.Fatal(err)
	}
	return cfg.Teams.MaxInvitesPerDay
}

func TestTeamsInviteCapDefaultsToTen(t *testing.T) {
	if got := loadMaxInvites(t, ""); got != 10 {
		t.Errorf("teams.maxInvitesPerDay = %d with no env, want 10", got)
	}
}

func TestTeamsInviteCapComesFromTheEnv(t *testing.T) {
	for env, want := range map[string]int{"25": 25, "0": 0} {
		t.Run(env, func(t *testing.T) {
			if got := loadMaxInvites(t, env); got != want {
				t.Errorf("teams.maxInvitesPerDay = %d with %q, want %d", got, env, want)
			}
		})
	}
}

func TestNewRefusesANegativeInviteCap(t *testing.T) {
	var cfg Config
	cfg.Auth.JWTSecret = "secret"
	cfg.Teams.MaxInvitesPerDay = -1
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "teams.maxInvitesPerDay") {
		t.Fatalf("New = %v, want a teams.maxInvitesPerDay error", err)
	}
}

// Invitations are mailed only by a mailer that can send.
func TestTeamMailerNeedsSMTPWithAHost(t *testing.T) {
	for name, c := range map[string]struct {
		cfg  smtp.Config
		mail bool
	}{
		"disabled":         {smtp.Config{Host: "mail.example.com"}, false},
		"enabled, no host": {smtp.Config{Enabled: true}, false},
		"enabled":          {smtp.Config{Enabled: true, Host: "mail.example.com"}, true},
	} {
		if got := teamMailer(c.cfg, smtp.New(c.cfg)) != nil; got != c.mail {
			t.Errorf("%s: mailer set is %v, want %v", name, got, c.mail)
		}
	}
}
