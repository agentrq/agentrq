// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package dbconn

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

const (
	_logPrefix = "[gorm] "

	slowQuery = 200 * time.Millisecond
)

// Logger writes GORM's log through zlog, at GORM's default level. A lookup
// that finds no row is not logged: it is an expected outcome, which the
// repository returns as ErrNotFound.
var Logger logger.Interface = gormLogger{level: logger.Warn}

type gormLogger struct {
	level logger.LogLevel
}

func (l gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return gormLogger{level: level}
}

func (l gormLogger) Info(_ context.Context, msg string, args ...any) {
	if l.level >= logger.Info {
		zlog.Info().Msgf(_logPrefix+msg, args...)
	}
}

func (l gormLogger) Warn(_ context.Context, msg string, args ...any) {
	if l.level >= logger.Warn {
		zlog.Warn().Msgf(_logPrefix+msg, args...)
	}
}

func (l gormLogger) Error(_ context.Context, msg string, args ...any) {
	if l.level >= logger.Error {
		zlog.Error().Msgf(_logPrefix+msg, args...)
	}
}

func (l gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	var ev *zerolog.Event
	var msg string
	switch {
	case err != nil && l.level >= logger.Error && !errors.Is(err, gorm.ErrRecordNotFound):
		ev, msg = zlog.Error().Err(err), "query failed"
	case elapsed > slowQuery && l.level >= logger.Warn:
		ev, msg = zlog.Warn(), "slow query"
	case l.level >= logger.Info:
		ev, msg = zlog.Info(), "query"
	default:
		return
	}
	sql, rows := fc()
	// FileWithLineNum must be called here: it skips a fixed number of frames
	// to reach the repository method that ran the query.
	ev.Str("caller", utils.FileWithLineNum()).Str("sql", sql).Int64("rows", rows).Dur("elapsed", elapsed).Msg(_logPrefix + msg)
}
