// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package dbconn

import (
	"bytes"
	"context"
	"errors"
	"log"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestLoggerSkipsRecordNotFoundAndKeepsRealErrors(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(log.New(&buf, "", 0))
	sql := func() (string, int64) { return "SELECT 1", 0 }

	l.Trace(context.Background(), time.Now(), sql, gorm.ErrRecordNotFound)
	if buf.Len() != 0 {
		t.Errorf("record not found was logged: %q", buf.String())
	}

	l.Trace(context.Background(), time.Now(), sql, errors.New("database unavailable"))
	if !bytes.Contains(buf.Bytes(), []byte("database unavailable")) {
		t.Errorf("a real error was not logged: %q", buf.String())
	}
}
