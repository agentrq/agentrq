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

func TestStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	id := "test-id"
	content := "hello storage"
	contentB64 := base64.StdEncoding.EncodeToString([]byte(content))

	t.Run("SaveAndLoad", func(t *testing.T) {
		err := s.Save(id, contentB64)
		if err != nil {
			t.Fatalf("failed to save: %v", err)
		}

		loaded, err := s.Load(id)
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}
		if loaded != contentB64 {
			t.Errorf("expected %s, got %s", contentB64, loaded)
		}

		raw, err := s.LoadRaw(id)
		if err != nil {
			t.Fatalf("failed to load raw: %v", err)
		}
		if string(raw) != content {
			t.Errorf("expected %s, got %s", content, string(raw))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := s.Delete(id)
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}
		_, err = s.LoadRaw(id)
		if err == nil {
			t.Error("expected error loading deleted file, got nil")
		}
	})

	t.Run("SaveInvalidBase64", func(t *testing.T) {
		err := s.Save("inv", "not-base64-!!!")
		if err == nil {
			t.Error("expected error for invalid base64")
		}
	})

	t.Run("NewDirError", func(t *testing.T) {
		// Try to create storage in a path that is a file
		f, _ := os.CreateTemp("", "not-a-dir")
		defer os.Remove(f.Name())
		_, err := New(f.Name())
		if err == nil {
			t.Error("expected error for existing file as baseDir")
		}
	})
}

func TestNestedStorage(t *testing.T) {
	dir := t.TempDir()
	s, err := NewNested(dir)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("# tdd"))
	for _, id := range []string{"w-1/skill-2/3", "w-1/skill-2/4"} {
		if err := s.Save(id, b64); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	if raw, err := s.LoadRaw("w-1/skill-2/3"); err != nil || string(raw) != "# tdd" {
		t.Fatalf("load: %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "w-1", "skill-2", "3")); err != nil {
		t.Fatalf("blob is not at its path: %v", err)
	}

	// A directory goes with its last blob, and not before.
	if err := s.Delete("w-1/skill-2/3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "w-1", "skill-2")); err != nil {
		t.Fatalf("skill dir went while it still held a blob: %v", err)
	}
	if err := s.Delete("w-1/skill-2/4"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("empty directories left behind: %v", entries)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the base directory was removed: %v", err)
	}
	if err := s.Delete("w-1/skill-2/4"); err == nil {
		t.Error("deleting a missing blob succeeded")
	}

	for _, id := range []string{"", "..", "../x", "a/../../x", "a//b", "/a", "a/"} {
		if err := s.Save(id, b64); err == nil {
			t.Errorf("save %q accepted", id)
		}
	}
	// A file where a directory belongs fails the save rather than clobbering it.
	if err := s.Save("f", b64); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("f/x", b64); err == nil {
		t.Error("saved under a file")
	}

	f, _ := os.CreateTemp(t.TempDir(), "not-a-dir")
	f.Close()
	if _, err := NewNested(f.Name()); err == nil {
		t.Error("expected error for existing file as baseDir")
	}
}

// A flat store still refuses a nested id.
func TestFlatStorageRefusesNestedID(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("a/b", base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Error("flat store saved a nested id")
	}
}

func TestAttachmentKey(t *testing.T) {
	if got := AttachmentKey(62, 63, "0jN"); got != "w-00000000010/00000000011/0jN" {
		t.Errorf("got %q", got)
	}
}

func TestLocalAttachments(t *testing.T) {
	dir := t.TempDir()
	s, err := NewNested(dir)
	if err != nil {
		t.Fatal(err)
	}
	link, err := SaveAttachment(s, 62, 63, "a1", base64.StdEncoding.EncodeToString([]byte("hi")), "text/plain")
	if err != nil || link != "" {
		t.Fatalf("got %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "w-00000000010", "00000000011", "a1")); err != nil {
		t.Fatalf("not filed by workspace and task: %v", err)
	}
	if raw, err := LoadAttachment(s, 62, 63, "a1"); err != nil || string(raw) != "hi" {
		t.Fatalf("load: %q, %v", raw, err)
	}
	// Another task's attachment is not this one.
	if _, err := LoadAttachment(s, 62, 64, "a1"); err == nil {
		t.Error("loaded under the wrong task")
	}
	DeleteAttachment(s, 62, 63, "a1")
	if _, err := os.Stat(filepath.Join(dir, "w-00000000010")); !os.IsNotExist(err) {
		t.Errorf("empty directories left behind: %v", err)
	}

	// One saved flat, before keys named the workspace and task, still loads and deletes.
	if err := s.Save("old", base64.StdEncoding.EncodeToString([]byte("legacy"))); err != nil {
		t.Fatal(err)
	}
	if raw, err := LoadAttachment(s, 62, 63, "old"); err != nil || string(raw) != "legacy" {
		t.Fatalf("legacy load: %q, %v", raw, err)
	}
	DeleteAttachment(s, 62, 63, "old")
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Errorf("legacy file kept: %v", err)
	}
}

// linker is a store that hands out links, as the public S3 store does.
type linker struct{ Service }

func (l linker) SavePublic(id, dataBase64, _ string) (string, error) {
	return PublicURL(l, id), l.Save(id, dataBase64)
}

func (l linker) PublicURL(id string) string { return "https://cdn/" + id }

func TestWithFallback(t *testing.T) {
	newDir := func() Service {
		s, err := NewNested(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	primary, old := newDir(), newDir()
	s := WithFallback(primary, old)
	enc := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }

	if err := old.Save("legacy", enc("was here")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("new", enc("fresh")); err != nil {
		t.Fatal(err)
	}
	if _, err := old.LoadRaw("new"); err == nil {
		t.Error("a new blob went to the fallback")
	}
	for id, want := range map[string]string{"new": "fresh", "legacy": "was here"} {
		if raw, err := s.LoadRaw(id); err != nil || string(raw) != want {
			t.Errorf("LoadRaw(%s) = %q, %v", id, raw, err)
		}
		if b64, err := s.Load(id); err != nil || b64 != enc(want) {
			t.Errorf("Load(%s) = %q, %v", id, b64, err)
		}
	}
	for _, id := range []string{"new", "legacy"} {
		if err := s.Delete(id); err != nil {
			t.Errorf("Delete(%s): %v", id, err)
		}
		if _, err := s.LoadRaw(id); err == nil {
			t.Errorf("%s still loads", id)
		}
	}
	if err := s.Delete("never"); err == nil {
		t.Error("deleting a missing blob succeeded")
	}
	if _, err := s.Load("never"); err == nil {
		t.Error("loading a missing blob succeeded")
	}

	// Wrapping keeps the primary's links, and a plain primary still has none.
	id := monoflake.ID(3).String()
	wrapped := WithFallback(linker{newDir()}, old)
	link, err := SaveAttachment(wrapped, 1, 2, id, enc("x"), "text/plain")
	if err != nil || link != "https://cdn/w-00000000001/00000000002/"+id || PublicURL(wrapped, AttachmentKey(1, 2, id)) != link {
		t.Errorf("link %q, %v", link, err)
	}
	if link, err := SaveAttachment(s, 1, 2, "a", enc("x"), "text/plain"); err != nil || link != "" {
		t.Errorf("plain primary: %q, %v", link, err)
	}
}
