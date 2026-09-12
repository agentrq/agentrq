// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	zlog "github.com/rs/zerolog/log"
)

type Config struct {
	// RetentionPeriod accepts Go duration strings ("168h") or day-suffixed values ("7d").
	RetentionPeriod string `yaml:"attachmentRetentionPeriod"`
	// StorageDir is injected at runtime, not read from YAML.
	StorageDir string
}

type Service interface {
	Start(ctx context.Context)
	RunOnce(ctx context.Context) error
}

type service struct {
	storageDir      string
	retentionPeriod time.Duration
}

func New(cfg Config) (Service, error) {
	d, err := parseDuration(cfg.RetentionPeriod)
	if err != nil {
		return nil, fmt.Errorf("cleanup: invalid retention period %q: %w", cfg.RetentionPeriod, err)
	}
	return &service{
		storageDir:      cfg.StorageDir,
		retentionPeriod: d,
	}, nil
}

// Start registers a daily midnight UTC cron job that deletes old attachments.
func (s *service) Start(ctx context.Context) {
	c := cron.New()
	_, err := c.AddFunc("0 0 * * *", func() {
		if err := s.RunOnce(context.Background()); err != nil {
			zlog.Error().Err(err).Msg("cleanup: attachment cleanup failed")
		}
	})
	if err != nil {
		zlog.Error().Err(err).Msg("cleanup: failed to register daily cleanup job")
		return
	}
	c.Start()
	zlog.Info().
		Str("retention", s.retentionPeriod.String()).
		Str("storageDir", s.storageDir).
		Msg("cleanup: daily attachment cleanup scheduled (midnight UTC)")

	go func() {
		<-ctx.Done()
		c.Stop()
	}()
}

// RunOnce scans the storage directory and removes files older than the retention period.
// Files with a .db extension are always skipped.
func (s *service) RunOnce(ctx context.Context) error {
	cutoff := time.Now().Add(-s.retentionPeriod)

	entries, err := os.ReadDir(s.storageDir)
	if err != nil {
		return fmt.Errorf("cleanup: read storage dir: %w", err)
	}

	deleted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".db") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			zlog.Warn().Err(err).Str("file", entry.Name()).Msg("cleanup: failed to stat file, skipping")
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(s.storageDir, entry.Name())
			if err := os.Remove(path); err != nil {
				zlog.Warn().Err(err).Str("file", entry.Name()).Msg("cleanup: failed to delete file")
				continue
			}
			deleted++
			zlog.Debug().Str("file", entry.Name()).Msg("cleanup: deleted old attachment")
		}
	}

	zlog.Info().
		Int("deleted", deleted).
		Time("cutoff", cutoff).
		Msg("cleanup: attachment cleanup complete")

	return nil
}

// parseDuration extends time.ParseDuration with a "d" (days) suffix.
func parseDuration(s string) (time.Duration, error) {
	if raw, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(raw)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid day count in %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
