// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package dbconn

import (
	"log"
	"os"
	"time"

	"gorm.io/gorm/logger"
)

// Logger is GORM's default logger without its "record not found" line: a
// lookup that finds nothing is an expected outcome, which the repository
// returns as ErrNotFound.
var Logger = newLogger(log.New(os.Stdout, "\r\n", log.LstdFlags))

func newLogger(w logger.Writer) logger.Interface {
	return logger.New(w, logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  true,
	})
}
