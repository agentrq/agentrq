// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

func TestAppLimitsConfiguration(t *testing.T) {
	t.Run("applies limits from Config struct", func(t *testing.T) {
		cfg := Config{
			Memory: config.MemoryConfig{
				MaxSize: "32KiB",
			},
			Skills: config.SkillsConfig{
				MaxFileSize:    "128KiB",
				MaxSubFileSize: "80KiB",
				MaxFiles:       50,
			},
			ConfigSvc: newYAMLConfig(t, "{}"),
		}
		cfg.Auth.JWTSecret = "test-secret"

		_, _ = New(cfg)

		if got := mcp.MaxMemoryLimit(); got != 32*1024 {
			t.Errorf("mcp.MaxMemoryLimit() = %d, want %d", got, 32*1024)
		}
		if got := skill.MaxSkillFileLimit(); got != 128*1024 {
			t.Errorf("skill.MaxSkillFileLimit() = %d, want %d", got, 128*1024)
		}
		if got := skill.MaxSubFileLimit(); got != 80*1024 {
			t.Errorf("skill.MaxSubFileLimit() = %d, want %d", got, 80*1024)
		}
		if got := skill.MaxFilesLimit(); got != 50 {
			t.Errorf("skill.MaxFilesLimit() = %d, want %d", got, 50)
		}

		// Reset
		mcp.SetMaxMemoryBytes(0)
		skill.SetMaxSkillFileBytes(0)
		skill.SetMaxSubFileBytes(0)
		skill.SetMaxFiles(0)
	})

	t.Run("applies limits from ConfigSvc", func(t *testing.T) {
		yamlDoc := `
memory:
  maxSize: 40000
skills:
  maxFileSize: 150000
  maxSubFileSize: 70000
  maxFiles: 120
`
		cfg := Config{
			ConfigSvc: newYAMLConfig(t, yamlDoc),
		}
		cfg.Auth.JWTSecret = "test-secret"

		_, _ = New(cfg)

		if got := mcp.MaxMemoryLimit(); got != 40000 {
			t.Errorf("mcp.MaxMemoryLimit() = %d, want %d", got, 40000)
		}
		if got := skill.MaxSkillFileLimit(); got != 150000 {
			t.Errorf("skill.MaxSkillFileLimit() = %d, want %d", got, 150000)
		}
		if got := skill.MaxSubFileLimit(); got != 70000 {
			t.Errorf("skill.MaxSubFileLimit() = %d, want %d", got, 70000)
		}
		if got := skill.MaxFilesLimit(); got != 120 {
			t.Errorf("skill.MaxFilesLimit() = %d, want %d", got, 120)
		}

		// Reset
		mcp.SetMaxMemoryBytes(0)
		skill.SetMaxSkillFileBytes(0)
		skill.SetMaxSubFileBytes(0)
		skill.SetMaxFiles(0)
	})

	t.Run("applies limits from environment variables", func(t *testing.T) {
		t.Setenv("AGENTRQ_MEMORY_MAX_SIZE", "48KiB")
		t.Setenv("AGENTRQ_SKILLS_MAX_FILE_SIZE", "200KiB")
		t.Setenv("AGENTRQ_SKILLS_MAX_FILES", "75")

		cfg := Config{
			ConfigSvc: newYAMLConfig(t, "{}"),
		}
		cfg.Auth.JWTSecret = "test-secret"

		_, _ = New(cfg)

		if got := mcp.MaxMemoryLimit(); got != 48*1024 {
			t.Errorf("mcp.MaxMemoryLimit() = %d, want %d", got, 48*1024)
		}
		if got := skill.MaxSkillFileLimit(); got != 200*1024 {
			t.Errorf("skill.MaxSkillFileLimit() = %d, want %d", got, 200*1024)
		}
		if got := skill.MaxFilesLimit(); got != 75 {
			t.Errorf("skill.MaxFilesLimit() = %d, want %d", got, 75)
		}

		os.Unsetenv("AGENTRQ_MEMORY_MAX_SIZE")
		os.Unsetenv("AGENTRQ_SKILLS_MAX_FILE_SIZE")
		os.Unsetenv("AGENTRQ_SKILLS_MAX_FILES")
		mcp.SetMaxMemoryBytes(0)
		skill.SetMaxSkillFileBytes(0)
		skill.SetMaxFiles(0)
	})
}
