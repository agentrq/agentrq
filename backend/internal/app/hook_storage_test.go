// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

func TestNewHookStorage(t *testing.T) {
	t.Run("LocalAndNeverPublic", func(t *testing.T) {
		local := t.TempDir()
		got, err := newHookStorage(newYAMLConfig(t, "{}"), local)
		if err != nil {
			t.Fatal(err)
		}
		if err := got.Save("u-0/hook-1/ab.wasm", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(local, "u-0", "hook-1", "ab.wasm")); err != nil {
			t.Error(err)
		}
		if _, public := got.(storage.Publisher); public {
			t.Error("hook modules have a public link")
		}
	})

	t.Run("S3AndNeverPublic", func(t *testing.T) {
		c := newYAMLConfig(t, "hooks: {storage: s3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newHookStorage(c, t.TempDir())
		if err != nil || got == nil {
			t.Fatalf("got %v, %v", got, err)
		}
		if _, public := got.(storage.Publisher); public {
			t.Error("hook modules are public in the bucket")
		}
	})

	t.Run("UnknownRefusesToStart", func(t *testing.T) {
		_, err := newHookStorage(newYAMLConfig(t, "hooks: {storage: gcs}"), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), `unknown hooks storage "gcs"`) {
			t.Errorf("got %v", err)
		}
	})
}
