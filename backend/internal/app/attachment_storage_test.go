// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"github.com/mustafaturan/monoflake"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

const filesURL = "https://agentrq.example/storage/artifacts"

func TestNewAttachmentStorage(t *testing.T) {
	local := t.TempDir()

	t.Run("DefaultsToLocal", func(t *testing.T) {
		for _, doc := range []string{"{}", "artifacts: {storage: local}", "skills: {storage: s3}"} {
			got, err := newAttachmentStorage(newYAMLConfig(t, doc), local, filesURL)
			if err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			// Local attachments are filed by workspace and task, and linked through the file routes.
			id := monoflake.ID(3).String()
			link, err := storage.SaveAttachment(got, 1, 2, id, "", "text/plain")
			if err != nil || link != filesURL+"/w-00000000001/00000000002/"+id {
				t.Fatalf("%s: %q, %v", doc, link, err)
			}
			if _, err := os.Stat(filepath.Join(local, "artifacts", "w-00000000001", "00000000002", id)); err != nil {
				t.Errorf("%s: %v", doc, err)
			}
		}
	})

	t.Run("FindsFlatOnes", func(t *testing.T) {
		// Attachments saved before they were filed by task sit flat in the storage dir.
		if err := os.WriteFile(filepath.Join(local, "old"), []byte("legacy"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := newAttachmentStorage(newYAMLConfig(t, "{}"), local, filesURL)
		if err != nil {
			t.Fatal(err)
		}
		if raw, err := storage.LoadAttachment(got, 1, 2, "old"); err != nil || string(raw) != "legacy" {
			t.Fatalf("got %q, %v", raw, err)
		}
		storage.DeleteAttachment(got, 1, 2, "old")
		if _, err := os.Stat(filepath.Join(local, "old")); !os.IsNotExist(err) {
			t.Errorf("flat attachment not deleted: %v", err)
		}
	})

	t.Run("DirError", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := newAttachmentStorage(newYAMLConfig(t, "{}"), file, filesURL); err == nil {
			t.Error("want error")
		}
	})

	t.Run("UnknownRefusesToStart", func(t *testing.T) {
		_, err := newAttachmentStorage(newYAMLConfig(t, "artifacts: {storage: gcs}"), local, filesURL)
		if err == nil || !strings.Contains(err.Error(), `unknown artifacts storage "gcs"`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("S3IsPublic", func(t *testing.T) {
		c := newYAMLConfig(t, "artifacts: {storage: S3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newAttachmentStorage(c, local, filesURL)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.(storage.Publisher); !ok {
			t.Errorf("S3 attachments get no links: %T", got)
		}
	})
}
