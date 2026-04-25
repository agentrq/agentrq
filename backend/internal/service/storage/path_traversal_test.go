package storage

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestStoragePathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_traversal_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a file outside the storage directory
	outsideFile := filepath.Join(filepath.Dir(tmpDir), "traversal_target.txt")
	err = os.WriteFile(outsideFile, []byte("sensitive data"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outsideFile)

	traversalID := "../" + filepath.Base(outsideFile)

	t.Run("LoadRawTraversal", func(t *testing.T) {
		data, err := s.LoadRaw(traversalID)
		if err == nil {
			t.Errorf("Expected error when attempting path traversal, but got nil. Data: %s", string(data))
		}
	})

	t.Run("BlockedSpecialIDs", func(t *testing.T) {
		specialIDs := []string{".", "..", ""}
		for _, id := range specialIDs {
			_, err := s.LoadRaw(id)
			if err == nil {
				t.Errorf("Expected error for special ID %q, but got nil", id)
			}
		}
	})

	t.Run("SaveTraversal", func(t *testing.T) {
		contentB64 := base64.StdEncoding.EncodeToString([]byte("malicious data"))
		err := s.Save(traversalID, contentB64)
		if err == nil {
			t.Error("Expected error when attempting path traversal in Save, but got nil")
		}

		// Verify if the file outside was overwritten (it shouldn't be if we fix it)
		data, _ := os.ReadFile(outsideFile)
		if string(data) == "malicious data" {
			t.Error("Path traversal successful: outside file was overwritten")
		}
	})

	t.Run("DeleteTraversal", func(t *testing.T) {
		err := s.Delete(traversalID)
		if err == nil {
			t.Error("Expected error when attempting path traversal in Delete, but got nil")
		}

		// Verify if the file outside was deleted
		if _, err := os.Stat(outsideFile); os.IsNotExist(err) {
			t.Error("Path traversal successful: outside file was deleted")
		}
	})
}
