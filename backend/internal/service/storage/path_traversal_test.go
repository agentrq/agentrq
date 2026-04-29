package storage

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestStorage_PathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_traversal_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a canary file outside the storage directory
	canaryFile := filepath.Join(filepath.Dir(tmpDir), "canary.txt")
	canaryContent := "secret canary content"
	err = os.WriteFile(canaryFile, []byte(canaryContent), 0644)
	if err != nil {
		t.Fatalf("failed to create canary file: %v", err)
	}
	defer os.Remove(canaryFile)

	t.Run("PathTraversal_LoadRaw", func(t *testing.T) {
		// Attempt to read the canary file using path traversal
		traversalID := "../canary.txt"
		_, err := s.LoadRaw(traversalID)

		if err == nil {
			t.Errorf("Security vulnerability: successfully loaded file via path traversal!")
		} else {
			t.Logf("Successfully blocked traversal: %v", err)
		}
	})

	t.Run("PathTraversal_Save", func(t *testing.T) {
		// Attempt to write a file outside using path traversal
		traversalID := "../pwned.txt"
		contentB64 := base64.StdEncoding.EncodeToString([]byte("pwned"))
		err := s.Save(traversalID, contentB64)

		if err == nil {
			t.Errorf("Security vulnerability: successfully saved file via path traversal!")
			os.Remove(filepath.Join(filepath.Dir(tmpDir), "pwned.txt"))
		} else {
			t.Logf("Successfully blocked traversal: %v", err)
		}
	})
}
