// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
)

// loadTeams reads the teams settings from the real base.yaml, as main does,
// with the env var named set to env, or unset when env is empty.
func loadTeams(t *testing.T, name, env string) teamsConfig {
	t.Helper()
	t.Setenv("ENV", "development")
	t.Setenv(name, env)
	if env == "" {
		os.Unsetenv(name) // t.Setenv restores it
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
	return cfg.Teams
}

type teamsConfig = struct {
	MaxInvitesPerDay        int  `yaml:"maxInvitesPerDay"`
	MaxInvitesPerUserPerDay int  `yaml:"maxInvitesPerUserPerDay"`
	MaxSkillsPerWorkspace   int  `yaml:"maxSkillsPerWorkspace"`
	EmailInvites            bool `yaml:"emailInvites"`
}

const (
	teamCapEnv  = "AGENTRQ_TEAMS_MAX_INVITES_PER_DAY"
	userCapEnv  = "AGENTRQ_TEAMS_MAX_INVITES_PER_USER_PER_DAY"
	emailEnv    = "AGENTRQ_TEAMS_EMAIL_INVITES"
	skillCapEnv = "AGENTRQ_TEAMS_MAX_SKILLS_PER_WORKSPACE"
)

func TestTeamsEmailInvitesDefaultsToOn(t *testing.T) {
	if !loadTeams(t, emailEnv, "").EmailInvites {
		t.Error("teams.emailInvites is off with no env, want on")
	}
}

func TestTeamsEmailInvitesComesFromTheEnv(t *testing.T) {
	for env, want := range map[string]bool{"false": false, "true": true} {
		t.Run(env, func(t *testing.T) {
			if got := loadTeams(t, emailEnv, env).EmailInvites; got != want {
				t.Errorf("teams.emailInvites = %v with %q, want %v", got, env, want)
			}
		})
	}
}

func TestTeamsInviteCapDefaultsToTen(t *testing.T) {
	if got := loadTeams(t, teamCapEnv, "").MaxInvitesPerDay; got != 10 {
		t.Errorf("teams.maxInvitesPerDay = %d with no env, want 10", got)
	}
}

func TestTeamsInviteCapComesFromTheEnv(t *testing.T) {
	for env, want := range map[string]int{"25": 25, "0": 0} {
		t.Run(env, func(t *testing.T) {
			if got := loadTeams(t, teamCapEnv, env).MaxInvitesPerDay; got != want {
				t.Errorf("teams.maxInvitesPerDay = %d with %q, want %d", got, env, want)
			}
		})
	}
}

func TestTeamsPerUserInviteCapDefaultsToTwenty(t *testing.T) {
	if got := loadTeams(t, userCapEnv, "").MaxInvitesPerUserPerDay; got != 20 {
		t.Errorf("teams.maxInvitesPerUserPerDay = %d with no env, want 20", got)
	}
}

func TestTeamsPerUserInviteCapComesFromTheEnv(t *testing.T) {
	for env, want := range map[string]int{"50": 50, "0": 0} {
		t.Run(env, func(t *testing.T) {
			if got := loadTeams(t, userCapEnv, env).MaxInvitesPerUserPerDay; got != want {
				t.Errorf("teams.maxInvitesPerUserPerDay = %d with %q, want %d", got, env, want)
			}
		})
	}
}

func TestNewRefusesANegativeInviteCap(t *testing.T) {
	for key, set := range map[string]func(*Config){
		"teams.maxInvitesPerDay":        func(c *Config) { c.Teams.MaxInvitesPerDay = -1 },
		"teams.maxInvitesPerUserPerDay": func(c *Config) { c.Teams.MaxInvitesPerUserPerDay = -1 },
		"teams.maxSkillsPerWorkspace":   func(c *Config) { c.Teams.MaxSkillsPerWorkspace = -1 },
	} {
		t.Run(key, func(t *testing.T) {
			var cfg Config
			cfg.Auth.JWTSecret = "secret"
			set(&cfg)
			_, err := New(cfg)
			if err == nil || !strings.Contains(err.Error(), key+":") {
				t.Fatalf("New = %v, want a %s error", err, key)
			}
		})
	}
}

// Invitations are mailed only when email invitations are on, by a mailer
// that can send.
func TestTeamMailerNeedsEmailInvitesAndSMTPWithAHost(t *testing.T) {
	for name, c := range map[string]struct {
		emailInvites bool
		cfg          smtp.Config
		mail         bool
	}{
		"disabled":          {true, smtp.Config{Host: "mail.example.com"}, false},
		"enabled, no host":  {true, smtp.Config{Enabled: true}, false},
		"enabled":           {true, smtp.Config{Enabled: true, Host: "mail.example.com"}, true},
		"email invites off": {false, smtp.Config{Enabled: true, Host: "mail.example.com"}, false},
		"off and no smtp":   {false, smtp.Config{}, false},
	} {
		if got := teamMailer(c.emailInvites, c.cfg, smtp.New(c.cfg)) != nil; got != c.mail {
			t.Errorf("%s: mailer set is %v, want %v", name, got, c.mail)
		}
	}
}

func TestTeamsSkillCapDefaultsToAHundred(t *testing.T) {
	resetLimits(t)
	var cfg Config
	cfg.Teams = loadTeams(t, skillCapEnv, "")
	if err := applyLimits(cfg); err != nil || cfg.Teams.MaxSkillsPerWorkspace != 100 || crud.TeamSkillsPerWorkspace != 100 {
		t.Errorf("teams.maxSkillsPerWorkspace = %d with no env (%v), and the cap %d; want 100", cfg.Teams.MaxSkillsPerWorkspace, err, crud.TeamSkillsPerWorkspace)
	}
}

func TestTeamsSkillCapComesFromTheEnv(t *testing.T) {
	for env, want := range map[string]int{"7": 7, "0": crud.DefaultTeamSkillsPerWorkspace} {
		t.Run(env, func(t *testing.T) {
			resetLimits(t)
			var cfg Config
			cfg.Teams = loadTeams(t, skillCapEnv, env)
			if err := applyLimits(cfg); err != nil || crud.TeamSkillsPerWorkspace != want {
				t.Errorf("the team skill cap is %d with %q (%v), want %d", crud.TeamSkillsPerWorkspace, env, err, want)
			}
		})
	}
}
