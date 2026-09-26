// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

func TestNewAttachmentStorage(t *testing.T) {
	local := t.TempDir()

	t.Run("DefaultsToLocal", func(t *testing.T) {
		for _, doc := range []string{"{}", "attachments: {storage: local}", "skills: {storage: s3}"} {
			got, err := newAttachmentStorage(newYAMLConfig(t, doc), local)
			if err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			// Local attachments are flat files, with no link.
			link, err := storage.SaveAttachment(got, "a1", "", "text/plain")
			if err != nil || link != "" {
				t.Fatalf("%s: %q, %v", doc, link, err)
			}
			if _, err := os.Stat(filepath.Join(local, "a1")); err != nil {
				t.Errorf("%s: %v", doc, err)
			}
		}
	})

	t.Run("UnknownRefusesToStart", func(t *testing.T) {
		_, err := newAttachmentStorage(newYAMLConfig(t, "attachments: {storage: gcs}"), local)
		if err == nil || !strings.Contains(err.Error(), `unknown attachments storage "gcs"`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("S3IsPublic", func(t *testing.T) {
		c := newYAMLConfig(t, "attachments: {storage: S3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newAttachmentStorage(c, local)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.(storage.Publisher); !ok {
			t.Errorf("S3 attachments get no links: %T", got)
		}
	})
}
