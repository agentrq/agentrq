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

	t.Run("PathTraversal", func(t *testing.T) {
		// Create a file outside the baseDir to try to overwrite/read
		outsideFile := filepath.Join(os.TempDir(), "sentinel_test_outside.txt")
		_ = os.WriteFile(outsideFile, []byte("original"), 0644)
		defer os.Remove(outsideFile)

		// The malicious ID
		relPath, _ := filepath.Rel(tmpDir, outsideFile)

		// Try to save
		err := s.Save(relPath, base64.StdEncoding.EncodeToString([]byte("overwritten")))
		if err == nil {
			// Vulnerable!
			data, _ := os.ReadFile(outsideFile)
			if string(data) == "overwritten" {
				t.Errorf("Vulnerability: successfully overwrote file outside baseDir")
			}
		}

		// Try to load
		_, err = s.Load(relPath)
		if err == nil {
			t.Errorf("Vulnerability: successfully loaded file outside baseDir")
		}

		// Try to delete
		err = s.Delete(relPath)
		if err == nil {
			if _, err := os.Stat(outsideFile); os.IsNotExist(err) {
				t.Errorf("Vulnerability: successfully deleted file outside baseDir")
			}
		}
	})
}
