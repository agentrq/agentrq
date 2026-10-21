// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cleanup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	zlog "github.com/rs/zerolog/log"

	svclog "github.com/agentrq/agentrq/backend/internal/service/log"
)

type Config struct {
	// RetentionPeriod accepts Go duration strings ("168h") or day-suffixed values ("7d").
	RetentionPeriod string `yaml:"attachmentRetentionPeriod"`
	// StorageDir holds attachments, and skills under skills/ when they are local.
	StorageDir string `yaml:"dir"`
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
	c := cron.New(cron.WithLogger(svclog.Cron{}))
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

// ArtifactsDir is the subdirectory of the storage directory local
// attachments are filed in, as w-<workspace>/<task>/<attachment>.
const ArtifactsDir = "artifacts"

// RunOnce removes attachments older than the retention period: those under
// artifacts/, and those kept flat in the storage directory before that.
// Database files are always skipped, and so are other directories: skill
// blobs live under skills/ and are kept until deleted.
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

	deleted += s.sweepAttachments(cutoff)

	zlog.Info().
		Int("deleted", deleted).
		Time("cutoff", cutoff).
		Msg("cleanup: attachment cleanup complete")

	return nil
}

// sweepAttachments removes the files under artifacts/ older than cutoff,
// then the directories that leaves empty, and returns how many files went.
func (s *service) sweepAttachments(cutoff time.Time) int {
	root := filepath.Join(s.storageDir, ArtifactsDir)
	deleted := 0
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a missing artifacts/ is nothing to clean
		}
		if d.IsDir() {
			if path != root {
				dirs = append(dirs, path)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil {
			zlog.Warn().Err(err).Str("file", path).Msg("cleanup: failed to delete file")
			return nil
		}
		deleted++
		return nil
	})
	// Deepest first, so a task's directory empties before its workspace's.
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // refuses a directory that still has files
	}
	return deleted
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
