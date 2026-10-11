// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    int
		wantErr bool
	}{
		{name: "nil", input: nil, want: 0},
		{name: "int", input: 16384, want: 16384},
		{name: "int negative", input: -1, wantErr: true},
		{name: "int64", input: int64(32768), want: 32768},
		{name: "int64 negative", input: int64(-5), wantErr: true},
		{name: "float64", input: float64(1024), want: 1024},
		{name: "float64 negative", input: float64(-1.5), wantErr: true},
		{name: "empty string", input: "   ", want: 0},
		{name: "plain int string", input: "16384", want: 16384},
		{name: "negative int string", input: "-100", wantErr: true},
		{name: "suffix kib", input: "16KiB", want: 16384},
		{name: "suffix kb", input: "16KB", want: 16384},
		{name: "suffix k", input: "16k", want: 16384},
		{name: "suffix mib", input: "2MiB", want: 2 * 1024 * 1024},
		{name: "suffix mb", input: "2MB", want: 2 * 1024 * 1024},
		{name: "suffix m", input: "2M", want: 2 * 1024 * 1024},
		{name: "suffix gib", input: "1GiB", want: 1024 * 1024 * 1024},
		{name: "suffix gb", input: "1GB", want: 1024 * 1024 * 1024},
		{name: "suffix g", input: "1G", want: 1024 * 1024 * 1024},
		{name: "suffix b", input: "512B", want: 512},
		{name: "float with unit", input: "1.5MB", want: int(1.5 * 1024 * 1024)},
		{name: "negative float with unit", input: "-1.5MB", wantErr: true},
		{name: "invalid unit", input: "100xyz", wantErr: true},
		{name: "invalid number in unit", input: "abcMB", wantErr: true},
		{name: "negative unit number", input: "-10KB", wantErr: true},
		{name: "unsupported type", input: []string{"invalid"}, wantErr: true},
		{name: "float64 overflow", input: float64(1e20), wantErr: true},
		{name: "mult overflow", input: "999999999999999999GB", wantErr: true},
		{name: "float mult overflow", input: "1e18GB", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseByteSize(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseByteSize(%v) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("ParseByteSize(%v) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestMemoryConfig_ResolveMaxBytes(t *testing.T) {
	t.Run("default when empty and no env", func(t *testing.T) {
		cfg := MemoryConfig{}
		got, err := cfg.ResolveMaxBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != DefaultMemoryMaxBytes {
			t.Fatalf("got %d, want default %d", got, DefaultMemoryMaxBytes)
		}
	})

	t.Run("YAML fields precedence", func(t *testing.T) {
		cfg1 := MemoryConfig{MaxSize: 32768}
		got, err := cfg1.ResolveMaxBytes()
		if err != nil || got != 32768 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg2 := MemoryConfig{MaxMemorySize: "64KiB"}
		got, err = cfg2.ResolveMaxBytes()
		if err != nil || got != 64*1024 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg3 := MemoryConfig{MaxFileSize: 20000}
		got, err = cfg3.ResolveMaxBytes()
		if err != nil || got != 20000 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg4 := MemoryConfig{MaxBytes: 25000}
		got, err = cfg4.ResolveMaxBytes()
		if err != nil || got != 25000 {
			t.Fatalf("got %d, err %v", got, err)
		}

		badCfg := MemoryConfig{MaxSize: "invalid"}
		_, err = badCfg.ResolveMaxBytes()
		if err == nil {
			t.Fatal("expected error for invalid size string")
		}
	})

	t.Run("env var fallback", func(t *testing.T) {
		envs := []string{
			"AGENTRQ_MEMORY_MAX_SIZE",
			"AGENTRQ_MEMORY_MAX_FILE_SIZE",
			"AGENTRQ_MEMORY_MAX_BYTES",
			"AGENTRQ_MAX_MEMORY_SIZE",
			"AGENTRQ_MAX_MEMORY_MD_SIZE",
			"AGENTRQ_MEMORY_MD_MAX_SIZE",
		}
		for _, e := range envs {
			t.Setenv(e, "48KiB")
			cfg := MemoryConfig{}
			got, err := cfg.ResolveMaxBytes()
			if err != nil || got != 48*1024 {
				t.Fatalf("env %s: got %d, err %v", e, got, err)
			}
			os.Unsetenv(e)
		}

		t.Setenv("AGENTRQ_MEMORY_MAX_SIZE", "bad")
		cfg := MemoryConfig{}
		_, err := cfg.ResolveMaxBytes()
		if err == nil {
			t.Fatal("expected error for bad env var value")
		}
	})
}

func TestSkillsConfig_ResolveMaxSkillFileBytes(t *testing.T) {
	t.Run("default when empty and no env", func(t *testing.T) {
		cfg := SkillsConfig{}
		got, err := cfg.ResolveMaxSkillFileBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != DefaultSkillsMaxFileBytes {
			t.Fatalf("got %d, want default %d", got, DefaultSkillsMaxFileBytes)
		}
	})

	t.Run("YAML fields precedence", func(t *testing.T) {
		cfg1 := SkillsConfig{MaxFileSize: "128KiB"}
		got, err := cfg1.ResolveMaxSkillFileBytes()
		if err != nil || got != 128*1024 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg2 := SkillsConfig{MaxSkillFileSize: 150000}
		got, err = cfg2.ResolveMaxSkillFileBytes()
		if err != nil || got != 150000 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg3 := SkillsConfig{MaxSize: 160000}
		got, err = cfg3.ResolveMaxSkillFileBytes()
		if err != nil || got != 160000 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg4 := SkillsConfig{MaxBytes: 170000}
		got, err = cfg4.ResolveMaxSkillFileBytes()
		if err != nil || got != 170000 {
			t.Fatalf("got %d, err %v", got, err)
		}

		badCfg := SkillsConfig{MaxFileSize: "invalid"}
		_, err = badCfg.ResolveMaxSkillFileBytes()
		if err == nil {
			t.Fatal("expected error for invalid size string")
		}
	})

	t.Run("env var fallback", func(t *testing.T) {
		envs := []string{
			"AGENTRQ_SKILLS_MAX_FILE_SIZE",
			"AGENTRQ_SKILLS_MAX_SIZE",
			"AGENTRQ_SKILLS_MAX_BYTES",
			"AGENTRQ_MAX_SKILLS_FILE_SIZE",
			"AGENTRQ_MAX_SKILLS_MD_SIZE",
			"AGENTRQ_SKILLS_MD_MAX_SIZE",
		}
		for _, e := range envs {
			t.Setenv(e, "192KiB")
			cfg := SkillsConfig{}
			got, err := cfg.ResolveMaxSkillFileBytes()
			if err != nil || got != 192*1024 {
				t.Fatalf("env %s: got %d, err %v", e, got, err)
			}
			os.Unsetenv(e)
		}

		t.Setenv("AGENTRQ_SKILLS_MAX_FILE_SIZE", "bad")
		cfg := SkillsConfig{}
		_, err := cfg.ResolveMaxSkillFileBytes()
		if err == nil {
			t.Fatal("expected error for bad env var value")
		}
	})
}

func TestSkillsConfig_ResolveMaxSubFileBytes(t *testing.T) {
	t.Run("default when empty and no env", func(t *testing.T) {
		cfg := SkillsConfig{}
		got, err := cfg.ResolveMaxSubFileBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != DefaultSkillsMaxSubFileBytes {
			t.Fatalf("got %d, want default %d", got, DefaultSkillsMaxSubFileBytes)
		}
	})

	t.Run("YAML and env var", func(t *testing.T) {
		cfg := SkillsConfig{MaxSubFileSize: "100KB"}
		got, err := cfg.ResolveMaxSubFileBytes()
		if err != nil || got != 100*1024 {
			t.Fatalf("got %d, err %v", got, err)
		}

		badCfg := SkillsConfig{MaxSubFileSize: "invalid"}
		if _, err := badCfg.ResolveMaxSubFileBytes(); err == nil {
			t.Fatal("expected error")
		}

		t.Setenv("AGENTRQ_SKILLS_MAX_SUB_FILE_SIZE", "80KB")
		cfg2 := SkillsConfig{}
		got2, err2 := cfg2.ResolveMaxSubFileBytes()
		if err2 != nil || got2 != 80*1024 {
			t.Fatalf("got %d, err %v", got2, err2)
		}

		t.Setenv("AGENTRQ_SKILLS_MAX_SUB_FILE_BYTES", "bad")
		os.Unsetenv("AGENTRQ_SKILLS_MAX_SUB_FILE_SIZE")
		if _, err := cfg2.ResolveMaxSubFileBytes(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestParseCount(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    int
		wantErr bool
	}{
		{name: "nil", input: nil, want: 0},
		{name: "int", input: 256, want: 256},
		{name: "int negative", input: -1, wantErr: true},
		{name: "int64", input: int64(512), want: 512},
		{name: "int64 negative", input: int64(-5), wantErr: true},
		{name: "float64", input: float64(100), want: 100},
		{name: "float64 negative", input: float64(-1.5), wantErr: true},
		{name: "empty string", input: "   ", want: 0},
		{name: "plain int string", input: "256", want: 256},
		{name: "negative int string", input: "-10", wantErr: true},
		{name: "invalid string", input: "abc", wantErr: true},
		{name: "unsupported type", input: []string{"bad"}, wantErr: true},
		{name: "float64 overflow", input: float64(1e20), wantErr: true},
		{name: "int64 overflow", input: "99999999999999999999999", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCount(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseCount(%v) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("ParseCount(%v) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestSkillsConfig_ResolveMaxFiles(t *testing.T) {
	t.Run("default when empty and no env", func(t *testing.T) {
		cfg := SkillsConfig{}
		got, err := cfg.ResolveMaxFiles()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != DefaultSkillsMaxFiles {
			t.Fatalf("got %d, want default %d", got, DefaultSkillsMaxFiles)
		}
	})

	t.Run("YAML fields precedence", func(t *testing.T) {
		cfg1 := SkillsConfig{MaxFiles: 100}
		got, err := cfg1.ResolveMaxFiles()
		if err != nil || got != 100 {
			t.Fatalf("got %d, err %v", got, err)
		}

		cfg2 := SkillsConfig{MaxFileCount: "500"}
		got, err = cfg2.ResolveMaxFiles()
		if err != nil || got != 500 {
			t.Fatalf("got %d, err %v", got, err)
		}

		badCfg := SkillsConfig{MaxFiles: "invalid"}
		_, err = badCfg.ResolveMaxFiles()
		if err == nil {
			t.Fatal("expected error for invalid count")
		}
	})

	t.Run("env var fallback", func(t *testing.T) {
		envs := []string{
			"AGENTRQ_SKILLS_MAX_FILES",
			"AGENTRQ_SKILLS_MAX_FILE_COUNT",
			"AGENTRQ_MAX_SKILLS_FILES",
		}
		for _, e := range envs {
			t.Setenv(e, "300")
			cfg := SkillsConfig{}
			got, err := cfg.ResolveMaxFiles()
			if err != nil || got != 300 {
				t.Fatalf("env %s: got %d, err %v", e, got, err)
			}
			os.Unsetenv(e)
		}

		t.Setenv("AGENTRQ_SKILLS_MAX_FILES", "bad")
		cfg := SkillsConfig{}
		_, err := cfg.ResolveMaxFiles()
		if err == nil {
			t.Fatal("expected error for bad env var value")
		}
	})
}
