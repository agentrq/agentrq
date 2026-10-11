// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

func resetLimits(t *testing.T) {
	t.Cleanup(func() {
		mcp.MaxMemoryBytes = mcp.DefaultMaxMemoryBytes
		skill.MaxSkillFileBytes = skill.DefaultMaxSkillFileBytes
		skill.MaxFiles = skill.DefaultMaxFiles
	})
}

// loadLimits reads the limits from the real base.yaml, as main does.
func loadLimits(t *testing.T) (Config, error) {
	t.Helper()
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := svc.Populate("memory", &cfg.Memory); err != nil {
		return cfg, err
	}
	return cfg, svc.Populate("skills", &cfg.Skills)
}

func TestLimitsDefaultWhenNoEnvIsSet(t *testing.T) {
	resetLimits(t)
	t.Setenv("ENV", "development")
	for _, env := range []string{"AGENTRQ_MEMORYMD_MAX_FILE_SIZE", "AGENTRQ_SKILLSMD_MAX_FILE_SIZE", "AGENTRQ_SKILLS_MAX_FILES"} {
		t.Setenv(env, "")
	}
	cfg, err := loadLimits(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLimits(cfg); err != nil {
		t.Fatal(err)
	}
	if mcp.MaxMemoryBytes != 16*1024 || skill.MaxSkillFileBytes != 96*1024 || skill.MaxFiles != 256 {
		t.Errorf("limits = %d, %d, %d; want 16 KiB, 96 KiB, 256", mcp.MaxMemoryBytes, skill.MaxSkillFileBytes, skill.MaxFiles)
	}
}

func TestLimitsAreReadFromTheirThreeEnvVars(t *testing.T) {
	resetLimits(t)
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_MEMORYMD_MAX_FILE_SIZE", "32768")
	t.Setenv("AGENTRQ_SKILLSMD_MAX_FILE_SIZE", "131072")
	t.Setenv("AGENTRQ_SKILLS_MAX_FILES", "10")
	cfg, err := loadLimits(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLimits(cfg); err != nil {
		t.Fatal(err)
	}
	if mcp.MaxMemoryBytes != 32768 || skill.MaxSkillFileBytes != 131072 || skill.MaxFiles != 10 {
		t.Errorf("limits = %d, %d, %d; want 32768, 131072, 10", mcp.MaxMemoryBytes, skill.MaxSkillFileBytes, skill.MaxFiles)
	}
}

// A misspelt name is not an alias: it is ignored, and the default stays.
func TestLimitsIgnoreMisspeltEnvVars(t *testing.T) {
	resetLimits(t)
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_MEMORYMD_MAX_FILE_SIZE", "")
	t.Setenv("AGENTRQ_MAX_MEMORY_SIZE", "32768")
	t.Setenv("AGENTRQ_MEMORY_MAX_BYTES", "32768")
	t.Setenv("AGENTRQ_MEMORY_MAX_SIZE", "32768")
	cfg, err := loadLimits(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyLimits(cfg); err != nil {
		t.Fatal(err)
	}
	if mcp.MaxMemoryBytes != mcp.DefaultMaxMemoryBytes {
		t.Errorf("MaxMemoryBytes = %d, want the default %d", mcp.MaxMemoryBytes, mcp.DefaultMaxMemoryBytes)
	}
}

func TestLimitsRefuseAValueThatIsNotANumber(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_SKILLS_MAX_FILES", "lots")
	if _, err := loadLimits(t); err == nil {
		t.Error("expected skills.maxFiles: lots to be refused")
	}
}

func TestApplyLimitsRefusesValuesItCannotHonour(t *testing.T) {
	resetLimits(t)
	for name, tc := range map[string]struct {
		mem, size, files int
		want             string
	}{
		"memory over the database cap": {mem: model.MaxMemoryContentBytes + 1, want: "memory.maxSize"},
		"negative memory":              {mem: -1, want: "memory.maxSize"},
		"negative skill file size":     {size: -1, want: "skills.maxFileSize"},
		"negative skill file count":    {files: -1, want: "skills.maxFiles"},
	} {
		var cfg Config
		cfg.Memory.MaxSize, cfg.Skills.MaxFileSize, cfg.Skills.MaxFiles = tc.mem, tc.size, tc.files
		if err := applyLimits(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}
	var cfg Config
	cfg.Memory.MaxSize = model.MaxMemoryContentBytes
	if err := applyLimits(cfg); err != nil || mcp.MaxMemoryBytes != model.MaxMemoryContentBytes {
		t.Errorf("memory at the database cap: err = %v, MaxMemoryBytes = %d", err, mcp.MaxMemoryBytes)
	}
}

func TestNewRefusesABadLimitBeforeInitialization(t *testing.T) {
	resetLimits(t)
	var cfg Config
	cfg.Auth.JWTSecret = testJWTSecret
	cfg.Memory.MaxSize = model.MaxMemoryContentBytes + 1
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "memory.maxSize") {
		t.Errorf("err = %v, want memory.maxSize refused", err)
	}
}
