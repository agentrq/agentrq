// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/handler/health"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

type gormConn struct{ db *gorm.DB }

func (c gormConn) Conn(context.Context) *gorm.DB { return c.db }
func (c gormConn) Close(context.Context)         {}

func TestPingDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ping := pingDB(gormConn{db: db})
	if err := ping(context.Background()); err != nil {
		t.Fatalf("ping an open database: %v", err)
	}

	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	if err := ping(context.Background()); err == nil {
		t.Error("ping a closed database: no error")
	}

	if err := pingDB(gormConn{db: &gorm.DB{Config: &gorm.Config{}}})(context.Background()); err == nil {
		t.Error("ping with no connection pool: no error")
	}
}

// recordingServer notes what readiness answered when the listener was closed.
type recordingServer struct {
	probes   http.Handler
	readyAt  int
	closedAt time.Time
}

func (*recordingServer) Run() error { return nil }
func (s *recordingServer) Shutdown(context.Context) error {
	s.closedAt = time.Now()
	rr := httptest.NewRecorder()
	s.probes.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, health.ReadyPath, nil))
	s.readyAt = rr.Code
	return nil
}

func TestShutdownFailsReadinessThenWaitsTheDrainDelay(t *testing.T) {
	probes := health.New(func(context.Context) error { return nil })
	srv := &recordingServer{probes: probes}
	cancelled := false
	a := &App{server: srv, probes: probes, drainDelay: 50 * time.Millisecond, cancel: func() { cancelled = true }}

	start := time.Now()
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if srv.readyAt != http.StatusServiceUnavailable {
		t.Errorf("readiness when the listener closed = %d, want 503", srv.readyAt)
	}
	if waited := srv.closedAt.Sub(start); waited < a.drainDelay {
		t.Errorf("the listener closed after %s, before the %s drain delay", waited, a.drainDelay)
	}
	if !cancelled {
		t.Error("the app context was not cancelled")
	}
}

func TestShutdownCutsTheDrainDelayAtTheDeadline(t *testing.T) {
	probes := health.New(func(context.Context) error { return nil })
	a := &App{server: &recordingServer{probes: probes}, probes: probes, drainDelay: time.Hour, cancel: func() {}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Shutdown(ctx) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited out the drain delay past its deadline")
	}
}

func TestNewRefusesANegativeDrainDelay(t *testing.T) {
	var cfg Config
	cfg.Auth.JWTSecret = "secret"
	cfg.App.DrainDelay = -time.Second
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "app.drainDelay") {
		t.Fatalf("New = %v, want an app.drainDelay error", err)
	}
}

func TestNewRefusesATrustedProxyThatIsNotAnAddress(t *testing.T) {
	var cfg Config
	cfg.Auth.JWTSecret = "secret"
	cfg.Network.TrustedProxies = "proxy.local"
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "network.trustedProxies") {
		t.Fatalf("New = %v, want a network.trustedProxies error", err)
	}
}

// The drain delay is read from AGENTRQ_APP_DRAIN_DELAY through the real
// base.yaml, as main reads it.
func TestDrainDelayFromTheEnvironment(t *testing.T) {
	for env, want := range map[string]time.Duration{"": 0, "10s": 10 * time.Second} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("AGENTRQ_APP_DRAIN_DELAY", env)
			t.Chdir("../../cmd/server")
			svc, err := config.New()
			if err != nil {
				t.Fatal(err)
			}
			var cfg Config
			if err := svc.Populate("app", &cfg.App); err != nil {
				t.Fatal(err)
			}
			if cfg.App.DrainDelay != want {
				t.Errorf("drain delay = %s, want %s", cfg.App.DrainDelay, want)
			}
		})
	}
}
