// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	// DefaultMemoryMaxBytes is the default maximum size for a memory (16 KiB).
	DefaultMemoryMaxBytes = 16 * 1024

	// DefaultSkillsMaxFileBytes is the default maximum size for a SKILL.md file (96 KiB).
	DefaultSkillsMaxFileBytes = 96 * 1024

	// DefaultSkillsMaxSubFileBytes is the default maximum size for a skill sub file (64 KiB).
	DefaultSkillsMaxSubFileBytes = 64 * 1024

	// DefaultSkillsMaxFiles is the default maximum number of files in a skill (256).
	DefaultSkillsMaxFiles = 256
)

// ParseByteSize parses a size value from an int, float, or string (e.g. "16KiB", "96KB", "1MB", "16384").
// Case-insensitive suffixes supported: B, K, KB, KiB, M, MB, MiB, G, GB, GiB.
func ParseByteSize(val any) (int, error) {
	if val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case int:
		if v < 0 {
			return 0, fmt.Errorf("invalid size %d: must be positive", v)
		}
		return v, nil
	case int64:
		if v < 0 || v > math.MaxInt {
			return 0, fmt.Errorf("invalid size %d: must be between 0 and %d", v, math.MaxInt)
		}
		return int(v), nil
	case float64:
		if v < 0 || v > math.MaxInt {
			return 0, fmt.Errorf("invalid size %f: must be between 0 and %d", v, math.MaxInt)
		}
		return int(v), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, nil
		}
		// Try plain integer
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			if n < 0 || n > math.MaxInt {
				return 0, fmt.Errorf("invalid size %d: must be between 0 and %d", n, math.MaxInt)
			}
			return int(n), nil
		}
		// Match number with unit suffix
		lower := strings.ToLower(s)
		var mult int64 = 1
		var numStr string
		switch {
		case strings.HasSuffix(lower, "kib"):
			mult = 1024
			numStr = strings.TrimSpace(s[:len(s)-3])
		case strings.HasSuffix(lower, "kb"):
			mult = 1024
			numStr = strings.TrimSpace(s[:len(s)-2])
		case strings.HasSuffix(lower, "k"):
			mult = 1024
			numStr = strings.TrimSpace(s[:len(s)-1])
		case strings.HasSuffix(lower, "mib"):
			mult = 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-3])
		case strings.HasSuffix(lower, "mb"):
			mult = 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-2])
		case strings.HasSuffix(lower, "m"):
			mult = 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-1])
		case strings.HasSuffix(lower, "gib"):
			mult = 1024 * 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-3])
		case strings.HasSuffix(lower, "gb"):
			mult = 1024 * 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-2])
		case strings.HasSuffix(lower, "g"):
			mult = 1024 * 1024 * 1024
			numStr = strings.TrimSpace(s[:len(s)-1])
		case strings.HasSuffix(lower, "b"):
			mult = 1
			numStr = strings.TrimSpace(s[:len(s)-1])
		default:
			return 0, fmt.Errorf("invalid size string %q", s)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(numStr), 10, 64)
		if err != nil {
			if f, fErr := strconv.ParseFloat(strings.TrimSpace(numStr), 64); fErr == nil {
				fTotal := f * float64(mult)
				if fTotal < 0 || fTotal > math.MaxInt {
					return 0, fmt.Errorf("invalid size %f: must be between 0 and %d", fTotal, math.MaxInt)
				}
				return int(fTotal), nil
			}
			return 0, fmt.Errorf("invalid size number in %q: %w", s, err)
		}
		if n < 0 || (mult > 0 && n > math.MaxInt/mult) {
			return 0, fmt.Errorf("invalid size %d with mult %d: exceeds max int", n, mult)
		}
		total := n * mult
		if total < 0 || total > math.MaxInt {
			return 0, fmt.Errorf("invalid size %d: exceeds max int", total)
		}
		return int(total), nil
	default:
		return 0, fmt.Errorf("unsupported size type %T", val)
	}
}

// ParseCount parses an integer count from an int, float, or string (e.g. 256, "256").
func ParseCount(val any) (int, error) {
	if val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case int:
		if v < 0 {
			return 0, fmt.Errorf("invalid count %d: must be positive", v)
		}
		return v, nil
	case int64:
		if v < 0 || v > math.MaxInt {
			return 0, fmt.Errorf("invalid count %d: must be between 0 and %d", v, math.MaxInt)
		}
		return int(v), nil
	case float64:
		if v < 0 || v > float64(math.MaxInt) {
			return 0, fmt.Errorf("invalid count %v: out of range", v)
		}
		return int(v), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid count %q: %w", v, err)
		}
		if n < 0 || n > math.MaxInt {
			return 0, fmt.Errorf("invalid count %d: must be between 0 and %d", n, math.MaxInt)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("unsupported count value %v of type %T", val, val)
	}
}

// MemoryConfig configures limits for workspace memories (memory.md).
type MemoryConfig struct {
	MaxSize       any `yaml:"maxSize"`
	MaxMemorySize any `yaml:"maxMemorySize"`
	MaxFileSize   any `yaml:"maxFileSize"`
	MaxBytes      any `yaml:"maxBytes"`
}

// ResolveMaxBytes returns the configured maximum size in bytes for a memory,
// checking YAML fields, then environment variables, falling back to DefaultMemoryMaxBytes (16 KiB).
func (c MemoryConfig) ResolveMaxBytes() (int, error) {
	for _, candidate := range []any{c.MaxSize, c.MaxMemorySize, c.MaxFileSize, c.MaxBytes} {
		if candidate != nil && candidate != "" {
			n, err := ParseByteSize(candidate)
			if err != nil {
				return 0, fmt.Errorf("memory config: %w", err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	for _, env := range []string{
		"AGENTRQ_MEMORY_MAX_SIZE",
		"AGENTRQ_MEMORY_MAX_FILE_SIZE",
		"AGENTRQ_MEMORY_MAX_BYTES",
		"AGENTRQ_MAX_MEMORY_SIZE",
		"AGENTRQ_MAX_MEMORY_MD_SIZE",
		"AGENTRQ_MEMORY_MD_MAX_SIZE",
	} {
		if val := os.Getenv(env); val != "" {
			n, err := ParseByteSize(val)
			if err != nil {
				return 0, fmt.Errorf("memory env %s: %w", env, err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	return DefaultMemoryMaxBytes, nil
}

// SkillsConfig configures limits and storage for workspace skills (SKILL.md).
type SkillsConfig struct {
	Storage          string `yaml:"storage"`
	MaxFileSize      any    `yaml:"maxFileSize"`
	MaxSkillFileSize any    `yaml:"maxSkillFileSize"`
	MaxSize          any    `yaml:"maxSize"`
	MaxBytes         any    `yaml:"maxBytes"`
	MaxSubFileSize   any    `yaml:"maxSubFileSize"`
	MaxFiles         any    `yaml:"maxFiles"`
	MaxFileCount     any    `yaml:"maxFileCount"`
}

// ResolveMaxSkillFileBytes returns the configured maximum size in bytes for SKILL.md,
// checking YAML fields, then environment variables, falling back to DefaultSkillsMaxFileBytes (96 KiB).
func (c SkillsConfig) ResolveMaxSkillFileBytes() (int, error) {
	for _, candidate := range []any{c.MaxFileSize, c.MaxSkillFileSize, c.MaxSize, c.MaxBytes} {
		if candidate != nil && candidate != "" {
			n, err := ParseByteSize(candidate)
			if err != nil {
				return 0, fmt.Errorf("skills config: %w", err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	for _, env := range []string{
		"AGENTRQ_SKILLS_MAX_FILE_SIZE",
		"AGENTRQ_SKILLS_MAX_SIZE",
		"AGENTRQ_SKILLS_MAX_BYTES",
		"AGENTRQ_MAX_SKILLS_FILE_SIZE",
		"AGENTRQ_MAX_SKILLS_MD_SIZE",
		"AGENTRQ_SKILLS_MD_MAX_SIZE",
	} {
		if val := os.Getenv(env); val != "" {
			n, err := ParseByteSize(val)
			if err != nil {
				return 0, fmt.Errorf("skills env %s: %w", env, err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	return DefaultSkillsMaxFileBytes, nil
}

// ResolveMaxSubFileBytes returns the configured maximum size in bytes for skill sub files,
// checking YAML fields, then environment variables, falling back to DefaultSkillsMaxSubFileBytes (64 KiB).
func (c SkillsConfig) ResolveMaxSubFileBytes() (int, error) {
	if c.MaxSubFileSize != nil && c.MaxSubFileSize != "" {
		n, err := ParseByteSize(c.MaxSubFileSize)
		if err != nil {
			return 0, fmt.Errorf("skills sub file config: %w", err)
		}
		if n > 0 {
			return n, nil
		}
	}
	for _, env := range []string{
		"AGENTRQ_SKILLS_MAX_SUB_FILE_SIZE",
		"AGENTRQ_SKILLS_MAX_SUB_FILE_BYTES",
	} {
		if val := os.Getenv(env); val != "" {
			n, err := ParseByteSize(val)
			if err != nil {
				return 0, fmt.Errorf("skills env %s: %w", env, err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	return DefaultSkillsMaxSubFileBytes, nil
}

// ResolveMaxFiles returns the configured maximum number of files per skill,
// checking YAML fields, then environment variables, falling back to DefaultSkillsMaxFiles (256).
func (c SkillsConfig) ResolveMaxFiles() (int, error) {
	for _, candidate := range []any{c.MaxFiles, c.MaxFileCount} {
		if candidate != nil && candidate != "" {
			n, err := ParseCount(candidate)
			if err != nil {
				return 0, fmt.Errorf("skills maxFiles config: %w", err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	for _, env := range []string{
		"AGENTRQ_SKILLS_MAX_FILES",
		"AGENTRQ_SKILLS_MAX_FILE_COUNT",
		"AGENTRQ_MAX_SKILLS_FILES",
	} {
		if val := os.Getenv(env); val != "" {
			n, err := ParseCount(val)
			if err != nil {
				return 0, fmt.Errorf("skills env %s: %w", env, err)
			}
			if n > 0 {
				return n, nil
			}
		}
	}
	return DefaultSkillsMaxFiles, nil
}
