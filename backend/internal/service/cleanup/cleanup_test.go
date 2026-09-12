// Copyright 2026 Contextual, Inc. https://agentrq.com

package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"168h", 168 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"0d", 0, true},
		{"-1d", 0, true},
		{"invalid", 0, true},
		{"d", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDuration(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		svc, err := New(Config{RetentionPeriod: "7d", StorageDir: t.TempDir()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if svc == nil {
			t.Fatal("expected non-nil service")
		}
	})

	t.Run("InvalidRetentionPeriod", func(t *testing.T) {
		_, err := New(Config{RetentionPeriod: "bad", StorageDir: t.TempDir()})
		if err == nil {
			t.Fatal("expected error for invalid retention period")
		}
	})
}

func TestRunOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour) // 10 days ago

	writeTestFile(t, filepath.Join(dir, "old-attachment"), old)
	writeTestFile(t, filepath.Join(dir, "new-attachment"), now)
	writeTestFile(t, filepath.Join(dir, "agentrq.db"), old)                       // must be kept
	writeTestFile(t, filepath.Join(dir, "OTHER.DB"), old)                         // uppercase .DB — must be kept
	writeTestFile(t, filepath.Join(dir, "recent-file"), now.Add(-3*24*time.Hour)) // 3 days old, within 7d

	svc := &service{storageDir: dir, retentionPeriod: 7 * 24 * time.Hour}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: unexpected error: %v", err)
	}

	assertAbsent(t, filepath.Join(dir, "old-attachment"))
	assertPresent(t, filepath.Join(dir, "new-attachment"))
	assertPresent(t, filepath.Join(dir, "agentrq.db"))
	assertPresent(t, filepath.Join(dir, "OTHER.DB"))
	assertPresent(t, filepath.Join(dir, "recent-file"))
}

func TestRunOnce_MissingDir(t *testing.T) {
	svc := &service{storageDir: "/nonexistent/path/xyz", retentionPeriod: 7 * 24 * time.Hour}
	if err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error for missing storage directory")
	}
}

func TestStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, err := New(Config{RetentionPeriod: "7d", StorageDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	svc.Start(ctx)
	cancel()
	time.Sleep(20 * time.Millisecond)
}

func writeTestFile(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatalf("writeTestFile %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file to be deleted: %s", path)
	}
}
