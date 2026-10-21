// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package dbconn

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = old })
	return &buf
}

func query() (string, int64) { return "SELECT 1", 3 }

func TestLoggerSkipsRecordNotFound(t *testing.T) {
	buf := captureLog(t)
	Logger.Trace(context.Background(), time.Now(), query, gorm.ErrRecordNotFound)
	if buf.Len() != 0 {
		t.Errorf("record not found was logged: %q", buf.String())
	}
}

func TestLoggerTraceLevels(t *testing.T) {
	ctx := context.Background()
	failed := errors.New("database unavailable")
	slow := time.Now().Add(-time.Second)
	cases := []struct {
		name  string
		level logger.LogLevel
		begin time.Time
		err   error
		want  string
	}{
		{"error", logger.Warn, time.Now(), failed, `"level":"error","error":"database unavailable"`},
		{"slow", logger.Warn, slow, nil, `"level":"warn"`},
		{"every query at info", logger.Info, time.Now(), nil, `"level":"info"`},
		{"fast query at warn", logger.Warn, time.Now(), nil, ""},
		{"silent", logger.Silent, slow, failed, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := captureLog(t)
			Logger.LogMode(c.level).Trace(ctx, c.begin, query, c.err)
			got := buf.String()
			if c.want == "" {
				if got != "" {
					t.Errorf("logged %q, want nothing", got)
				}
				return
			}
			for _, part := range []string{c.want, `"sql":"SELECT 1"`, `"rows":3`, `"caller":`} {
				if !strings.Contains(got, part) {
					t.Errorf("log %q lacks %s", got, part)
				}
			}
		})
	}
}

func TestLoggerMessages(t *testing.T) {
	ctx := context.Background()
	for _, level := range []logger.LogLevel{logger.Silent, logger.Error, logger.Warn, logger.Info} {
		buf := captureLog(t)
		l := Logger.LogMode(level)
		l.Info(ctx, "info %d", 1)
		l.Warn(ctx, "warn %d", 2)
		l.Error(ctx, "error %d", 3)
		got := strings.Count(buf.String(), "\n")
		if want := int(level) - 1; got != want {
			t.Errorf("level %d logged %d lines, want %d: %q", level, got, want, buf.String())
		}
	}
	buf := captureLog(t)
	Logger.Warn(ctx, "migrating %s", "tasks")
	if !strings.Contains(buf.String(), `"message":"[gorm] migrating tasks"`) {
		t.Errorf("log %q", buf.String())
	}
}
