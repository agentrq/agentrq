// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package log

import (
	"bytes"
	"encoding/json"
	stdlog "log"
	"os"
	"strings"
	"testing"
	"time"

	legolog "github.com/go-acme/lego/v4/log"
	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	configmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/config"
)

func TestSetupStampsEveryLine(t *testing.T) {
	oldLogger, oldTime, oldLego, oldFlags := log.Logger, zerolog.TimestampFunc, legolog.Logger, stdlog.Flags()
	t.Cleanup(func() {
		log.Logger, zerolog.TimestampFunc, legolog.Logger = oldLogger, oldTime, oldLego
		stdlog.SetOutput(os.Stderr)
		stdlog.SetFlags(oldFlags)
	})
	cfg := configmocks.NewMockService(gomock.NewController(t))
	cfg.EXPECT().AppShortName().Return("agentrq").AnyTimes()
	cfg.EXPECT().Version().Return("v0.13.2").AnyTimes()
	cfg.EXPECT().Env().Return("production").AnyTimes()

	Setup(cfg)
	Setup(cfg)
	var buf bytes.Buffer
	log.Logger = log.Logger.Output(&buf)
	stdlog.Print("http: TLS handshake error")

	line := strings.TrimSpace(buf.String())
	for _, field := range []string{`"name":"agentrq"`, `"version":"v0.13.2"`, `"env":"production"`} {
		if strings.Count(line, field) != 1 {
			t.Errorf("log %s should carry %s once", line, field)
		}
	}
	var entry struct{ Time string }
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log %s: %v", line, err)
	}
	if ts, err := time.Parse(time.RFC3339, entry.Time); err != nil || ts.Location() != time.UTC {
		t.Errorf("time %q is not UTC (%v)", entry.Time, err)
	}
}
