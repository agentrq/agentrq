// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package storage

import (
	"encoding/base64"
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

func TestSaveAttachmentLocalHasNoLink(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link, err := SaveAttachment(s, "a1", base64.StdEncoding.EncodeToString([]byte("hi")), "text/plain")
	if err != nil || link != "" {
		t.Fatalf("got %q, %v", link, err)
	}
	if raw, err := s.LoadRaw("a1"); err != nil || string(raw) != "hi" {
		t.Fatalf("load: %q, %v", raw, err)
	}
}
