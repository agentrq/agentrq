package storage

import (
	"encoding/base64"
	"os"
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

	t.Run("PathTraversalProtection", func(t *testing.T) {
		traversalIDs := []string{
			"../traversal-test",
			"sub/dir/id",
			"/absolute/path",
			"..",
			".",
			"",
		}
		content := "traversal"
		contentB64 := base64.StdEncoding.EncodeToString([]byte(content))

		for _, id := range traversalIDs {
			err := s.Save(id, contentB64)
			if err == nil {
				t.Errorf("expected error for path traversal id: %s", id)
			}

			_, err = s.Load(id)
			if err == nil {
				t.Errorf("expected error for path traversal id: %s", id)
			}

			_, err = s.LoadRaw(id)
			if err == nil {
				t.Errorf("expected error for path traversal id: %s", id)
			}

			err = s.Delete(id)
			if err == nil {
				t.Errorf("expected error for path traversal id: %s", id)
			}
		}
	})
}
