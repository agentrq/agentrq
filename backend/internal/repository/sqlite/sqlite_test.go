// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

func newConfig(t *testing.T, dsn string) config.Service {
	t.Helper()
	t.Setenv("ENV", "development")
	t.Setenv("TEST_SQLITE_DSN", dsn)
	svc, err := config.NewFS(fstest.MapFS{
		"_config/base.yaml":        {Data: []byte("sqlite:\n  enabled: true\n  dsn: ${TEST_SQLITE_DSN:}\n")},
		"_config/development.yaml": {Data: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// A fresh install has no _storage folder: the database's is created.
func TestNewCreatesTheDatabaseFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "_storage", "nested", "agentrq.db")
	conn, err := New(Params{Config: newConfig(t, path)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close(t.Context())
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file: %v", err)
	}
	if conn.Conn(t.Context()).Logger != dbconn.Logger {
		t.Error("the connection does not use the shared logger")
	}
}

func TestNewReportsAFolderItCannotCreate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(Params{Config: newConfig(t, filepath.Join(file, "agentrq.db"))})
	if err == nil || !strings.Contains(err.Error(), "failed to create the database folder") {
		t.Fatalf("New = %v, want a folder error", err)
	}
}

func TestMakeDir(t *testing.T) {
	dir := t.TempDir()
	for _, dsn := range []string{
		"file:" + filepath.Join(dir, "a", "x.db") + "?cache=shared",
		filepath.Join(dir, "b", "x.db"),
	} {
		if err := makeDir(dsn); err != nil {
			t.Fatalf("makeDir(%q): %v", dsn, err)
		}
	}
	for _, sub := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Errorf("folder %s: %v", sub, err)
		}
	}
	// Nothing to create: these name no folder, or one in memory.
	t.Chdir(dir)
	for _, dsn := range []string{"", ":memory:", "file::memory:?cache=shared", "file:mem/x.db?mode=memory"} {
		if err := makeDir(dsn); err != nil {
			t.Errorf("makeDir(%q): %v", dsn, err)
		}
	}
	if _, err := os.Stat("mem"); !os.IsNotExist(err) {
		t.Errorf("an in-memory DSN made a folder: %v", err)
	}
}
