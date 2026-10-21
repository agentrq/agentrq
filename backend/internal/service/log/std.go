// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package log

import (
	stdlog "log"
	"strings"

	legolog "github.com/go-acme/lego/v4/log"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// RouteLibraryLogs sends what libraries write through Go's standard logger to
// zlog: net/http's server errors go through the default logger, and ACME
// certificate renewal through lego's own.
func RouteLibraryLogs() {
	stdlog.SetFlags(0)
	stdlog.SetOutput(writer{level: zerolog.WarnLevel})
	legolog.Logger = stdlog.New(writer{level: zerolog.InfoLevel, prefix: "[acme] "}, "", 0)
}

var levelTags = []struct {
	tag   string
	level zerolog.Level
}{
	{"[INFO] ", zerolog.InfoLevel},
	{"[WARN] ", zerolog.WarnLevel},
}

// writer writes each line as a zlog entry, at the level its "[INFO] " or
// "[WARN] " tag names, or else at level.
type writer struct {
	level  zerolog.Level
	prefix string
}

func (w writer) Write(p []byte) (int, error) {
	msg, level := strings.TrimSpace(string(p)), w.level
	for _, t := range levelTags {
		if rest, ok := strings.CutPrefix(msg, t.tag); ok {
			msg, level = rest, t.level
			break
		}
	}
	log.WithLevel(level).Msg(w.prefix + msg)
	return len(p), nil
}

// Cron is a cron.Logger that writes to zlog. Like cron's own default, it
// drops the scheduler's routine info lines and keeps its errors.
type Cron struct{}

func (Cron) Info(string, ...any) {}

func (Cron) Error(err error, msg string, keysAndValues ...any) {
	log.Error().Err(err).Fields(keysAndValues).Msg("[cron] " + msg)
}
