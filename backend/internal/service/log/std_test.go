// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package log

import (
	"bytes"
	"errors"
	stdlog "log"
	"os"
	"strings"
	"testing"

	legolog "github.com/go-acme/lego/v4/log"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = old })
	return &buf
}

func TestRouteLibraryLogs(t *testing.T) {
	oldLego, oldFlags := legolog.Logger, stdlog.Flags()
	t.Cleanup(func() {
		stdlog.SetOutput(os.Stderr)
		stdlog.SetFlags(oldFlags)
		legolog.Logger = oldLego
	})
	RouteLibraryLogs()

	cases := []struct {
		name  string
		write func()
		want  string
	}{
		{"net/http", func() { stdlog.Printf("http: TLS handshake error from %s: EOF", "1.2.3.4:5") },
			`{"level":"warn","message":"http: TLS handshake error from 1.2.3.4:5: EOF"}`},
		{"acme info", func() { legolog.Infof("[%s] acme: Obtaining bundled SAN certificate", "a.example") },
			`{"level":"info","message":"[acme] [a.example] acme: Obtaining bundled SAN certificate"}`},
		{"acme warning", func() { legolog.Warnf("[%s] acme: retrying", "a.example") },
			`{"level":"warn","message":"[acme] [a.example] acme: retrying"}`},
		{"acme untagged", func() { legolog.Println("acme: no tag") },
			`{"level":"info","message":"[acme] acme: no tag"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := captureLog(t)
			c.write()
			if got := strings.TrimSpace(buf.String()); got != c.want {
				t.Errorf("logged %s, want %s", got, c.want)
			}
		})
	}
}

func TestCronLogsErrorsOnly(t *testing.T) {
	buf := captureLog(t)
	Cron{}.Info("wake", "now", "00:00")
	if buf.Len() != 0 {
		t.Errorf("an info line was logged: %q", buf.String())
	}
	Cron{}.Error(errors.New("job panicked"), "panic", "entry", 1)
	want := `{"level":"error","error":"job panicked","entry":1,"message":"[cron] panic"}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("logged %s, want %s", got, want)
	}
}
