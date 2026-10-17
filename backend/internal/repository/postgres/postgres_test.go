// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	configmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/config"
	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"
)

func mockConfig(t *testing.T, cfg postgresConfig, err error) *configmocks.MockService {
	t.Helper()
	m := configmocks.NewMockService(gomock.NewController(t))
	m.EXPECT().Populate("postgres", gomock.Any()).DoAndReturn(func(_ string, v any) error {
		*v.(*postgresConfig) = cfg
		return err
	})
	return m
}

// fakeDB stands in for the server: it refuses the first failures attempts,
// then opens an in-memory database. It records every wait between attempts.
func fakeDB(t *testing.T, failures int) (attempts *int, waits *[]time.Duration) {
	t.Helper()
	attempts, waits = new(int), new([]time.Duration)
	oldOpen, oldSleep := openDB, sleep
	t.Cleanup(func() { openDB, sleep = oldOpen, oldSleep })
	openDB = func(dsn string) (*gorm.DB, error) {
		*attempts++
		if *attempts <= failures {
			return nil, errors.New("dial tcp: connection refused")
		}
		return gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	}
	sleep = func(d time.Duration) { *waits = append(*waits, d) }
	return attempts, waits
}

func TestNewIsDisabled(t *testing.T) {
	attempts, _ := fakeDB(t, 0)
	_, err := New(Params{Config: mockConfig(t, postgresConfig{}, nil)})
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("got %v, want ErrDisabled", err)
	}
	if *attempts != 0 {
		t.Error("a disabled database was connected to")
	}
}

func TestNewConfigError(t *testing.T) {
	_, err := New(Params{Config: mockConfig(t, postgresConfig{}, errors.New("postgres section is not valid yaml"))})
	if err == nil || errors.Is(err, ErrDisabled) {
		t.Errorf("got %v, want the config error", err)
	}
}

// A database that is still starting is waited for, with growing waits.
func TestNewRetriesUntilTheDatabaseAnswers(t *testing.T) {
	attempts, waits := fakeDB(t, 3)
	repo, err := New(Params{Config: mockConfig(t, postgresConfig{Enabled: true, DSN: "host=db", MaxIdleConns: 2, MaxOpenConns: 4, MaxConnLifetime: time.Minute}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	if *attempts != 4 {
		t.Errorf("connected on attempt %d, want 4", *attempts)
	}
	if want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}; !slices.Equal(*waits, want) {
		t.Errorf("waited %v, want %v", *waits, want)
	}
	if repo.Conn(context.Background()) == nil {
		t.Error("no connection")
	}
}

// An enabled database that never answers is an error, after every attempt.
func TestNewGivesUpAfterEveryAttempt(t *testing.T) {
	attempts, waits := fakeDB(t, connectAttempts)
	_, err := New(Params{Config: mockConfig(t, postgresConfig{Enabled: true, DSN: "host=db"}, nil)})
	if err == nil || errors.Is(err, ErrDisabled) || !strings.Contains(err.Error(), "failed to connect after 8 attempts: dial tcp: connection refused") {
		t.Fatalf("got %v", err)
	}
	if *attempts != connectAttempts {
		t.Errorf("tried %d times, want %d", *attempts, connectAttempts)
	}
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i := range want {
		want[i] *= time.Second
	}
	if !slices.Equal(*waits, want) {
		t.Errorf("waited %v, want %v", *waits, want)
	}
}

// The real opener reaches the server at once, so a closed port fails fast.
func TestOpenDBReportsAnUnreachableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if _, err := openDB(fmt.Sprintf("host=127.0.0.1 port=%d user=u dbname=d sslmode=disable connect_timeout=2", port)); err == nil {
		t.Error("a closed port opened")
	}
}
