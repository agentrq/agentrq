// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

func TestNewPanoStorage(t *testing.T) {
	local := t.TempDir()
	const urlBase = "https://agentrq.example/storage/pano"

	t.Run("Pano blobs are kept locally unless S3 is asked for", func(t *testing.T) {
		for _, doc := range []string{"{}", "pano: {storage: local}", "pano: {storage: ' LOCAL '}"} {
			got, err := newPanoStorage(newYAMLConfig(t, doc), local, urlBase)
			if err != nil {
				t.Fatalf("%s: newPanoStorage returned %v, want a local storage", doc, err)
			}
			if err := got.Save("u-1/pano-2/3", ""); err != nil {
				t.Fatalf("%s: saving a blob returned %v, want it saved", doc, err)
			}
			if _, err := os.Stat(filepath.Join(local, "u-1", "pano-2", "3")); err != nil {
				t.Errorf("%s: the blob is not filed by account and doc under the local dir: %v", doc, err)
			}
			key := "u-1/pano-2/" + monoflake.ID(3).String()
			if got := storage.PublicURL(got, key); got != urlBase+"/"+key {
				t.Errorf("%s: the public link is %q, want %q", doc, got, urlBase+"/"+key)
			}
		}
	})

	t.Run("A local dir that is a file refuses to start", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := newPanoStorage(newYAMLConfig(t, "{}"), file, urlBase); err == nil {
			t.Error("newPanoStorage returned no error for a dir that is a file, want one")
		}
	})

	t.Run("An unknown storage refuses to start", func(t *testing.T) {
		_, err := newPanoStorage(newYAMLConfig(t, "pano: {storage: gcs}"), local, urlBase)
		if err == nil || !strings.Contains(err.Error(), `"gcs"`) {
			t.Errorf("newPanoStorage returned %v, want a refusal naming \"gcs\"", err)
		}
	})

	t.Run("S3 keeps Pano blobs public in the bucket under pano", func(t *testing.T) {
		c := newYAMLConfig(t, "pano: {storage: s3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newPanoStorage(c, local, urlBase)
		if err != nil || got == nil {
			t.Fatalf("newPanoStorage returned %v, %v, want an S3 storage", got, err)
		}
		key := "u-1/pano-2/" + monoflake.ID(3).String()
		if got := storage.PublicURL(got, key); got != "http://127.0.0.1:9/b/pano/"+key {
			t.Errorf("the public link is %q, want one in the bucket under pano", got)
		}
	})

	t.Run("An S3 client that cannot be made refuses to start", func(t *testing.T) {
		old := newS3
		defer func() { newS3 = old }()
		newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
		_, err := newPanoStorage(newYAMLConfig(t, "pano: {storage: s3}"), local, urlBase)
		if err == nil || err.Error() != "s3: no S3 credentials configured" {
			t.Errorf("newPanoStorage returned %v, want \"s3: no S3 credentials configured\"", err)
		}
	})
}
