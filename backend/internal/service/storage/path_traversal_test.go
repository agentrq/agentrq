package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage_traversal_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	traversalIDs := []string{
		"../traversal_test.txt",
		"../../etc/passwd",
		"/etc/passwd",
		".",
		"..",
		"sub/dir/file.txt",
	}
	contentB64 := "aGVsbG8=" // "hello" in base64

	for _, id := range traversalIDs {
		t.Run("PreventTraversal_"+id, func(t *testing.T) {
			err = s.Save(id, contentB64)
			if err == nil {
				t.Errorf("Security risk: Save should have failed for ID %s", id)

				// Cleanup if it actually created a file
				outsidePath := filepath.Join(tmpDir, id)
				if _, errStat := os.Stat(outsidePath); errStat == nil {
					os.Remove(outsidePath)
				}
			} else {
				t.Logf("Correctly rejected traversal ID %s: %v", id, err)
			}

			_, err = s.Load(id)
			if err == nil {
				t.Errorf("Security risk: Load should have failed for ID %s", id)
			}

			_, err = s.LoadRaw(id)
			if err == nil {
				t.Errorf("Security risk: LoadRaw should have failed for ID %s", id)
			}

			err = s.Delete(id)
			if err == nil {
				t.Errorf("Security risk: Delete should have failed for ID %s", id)
			}
		})
	}
}
