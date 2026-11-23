// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package storage

import (
	"encoding/base64"
	"github.com/mustafaturan/monoflake"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeContentType(t *testing.T) {
	for in, want := range map[string]string{
		"image/png":                 "image/png",
		" IMAGE/JPEG ":              "image/jpeg",
		"application/pdf":           "application/pdf",
		"video/mp4":                 "video/mp4",
		"audio/mpeg":                "audio/mpeg",
		"text/plain; charset=utf-8": "text/plain; charset=utf-8",
		"text/html; charset=utf-8":  octetStream,
		"image/svg+xml":             octetStream,
		"text/xml; charset=utf-8":   octetStream,
		"application/javascript":    octetStream,
		"":                          octetStream,
	} {
		if got := SafeContentType(in); got != want {
			t.Errorf("SafeContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNestedPublic(t *testing.T) {
	dir := t.TempDir()
	s, err := NewNestedPublic(dir, "https://agentrq.example/storage/artifacts/")
	if err != nil {
		t.Fatal(err)
	}
	id := monoflake.ID(3).String()
	link, err := SaveAttachment(s, 1, 2, id, base64.StdEncoding.EncodeToString([]byte("hi")), "text/plain")
	if err != nil || link != "https://agentrq.example/storage/artifacts/w-00000000001/00000000002/"+id {
		t.Fatalf("link %q, %v", link, err)
	}
	if raw, err := LoadAttachment(s, 1, 2, id); err != nil || string(raw) != "hi" {
		t.Fatalf("load %q, %v", raw, err)
	}
	if got := PublicURL(privateStore(t), id); got != "" {
		t.Errorf("a private store linked %q", got)
	}
	if _, err := SaveAttachment(s, 1, 2, id, "not base64!", "text/plain"); err == nil {
		t.Error("bad base64 saved")
	}

	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewNestedPublic(file, "u"); err == nil {
		t.Error("want error for a base dir that is a file")
	}
}

// privateStore is a local store that gives no links.
func privateStore(t *testing.T) Service {
	t.Helper()
	s, err := NewNested(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestLoadRawStaysInside checks a symlink cannot take a read out of the base
// directory, even where every id check passes: these files are public.
func TestLoadRawStaysInside(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := NewNested(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "w-1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "w-1", "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "w-2")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w-1/file", "w-2/secret"} {
		if data, err := s.LoadRaw(id); err == nil {
			t.Errorf("%s read %q from outside the base dir", id, data)
		}
	}
	if _, err := s.Load("w-1/file"); err == nil {
		t.Error("Load followed the symlink out")
	}
}

func TestLoadRawWithoutItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	s, err := NewNested(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRaw("w-1/a"); err == nil {
		t.Error("read from a directory that is gone")
	}
}
