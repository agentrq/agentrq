// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"github.com/mustafaturan/monoflake"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

func TestNewSkillStorage(t *testing.T) {
	local := t.TempDir()
	const key = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

	t.Run("DefaultsToLocal", func(t *testing.T) {
		for _, doc := range []string{"{}", "skills: {storage: local}", "skills: {storage: ' LOCAL '}"} {
			got, err := newSkillStorage(newYAMLConfig(t, doc), local, "https://agentrq.example/storage/skills")
			if err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			// Local skills are filed by workspace and skill under local.
			if err := got.Save("u-1/skill-2/3", ""); err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			if _, err := os.Stat(filepath.Join(local, "u-1", "skill-2", "3")); err != nil {
				t.Errorf("%s: %v", doc, err)
			}
			// And read publicly through the file routes.
			key := "u-1/skill-2/" + monoflake.ID(3).String()
			if got := storage.PublicURL(got, key); got != "https://agentrq.example/storage/skills/"+key {
				t.Errorf("%s: link %q", doc, got)
			}
		}
	})

	t.Run("LocalDirError", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := newSkillStorage(newYAMLConfig(t, "{}"), file, "https://agentrq.example/storage/skills"); err == nil {
			t.Error("want error")
		}
	})

	t.Run("UnknownRefusesToStart", func(t *testing.T) {
		_, err := newSkillStorage(newYAMLConfig(t, "skills: {storage: gcs}"), local, "https://agentrq.example/storage/skills")
		if err == nil || !strings.Contains(err.Error(), `"gcs"`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("BadConfig", func(t *testing.T) {
		if _, err := newSkillStorage(newYAMLConfig(t, "skills: [1]"), local, "https://agentrq.example/storage/skills"); err == nil {
			t.Error("want error")
		}
	})

	t.Run("S3", func(t *testing.T) {
		c := newYAMLConfig(t, "skills: {storage: s3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newSkillStorage(c, local, "https://agentrq.example/storage/skills")
		if err != nil || got == nil {
			t.Fatalf("got %v, %v", got, err)
		}
		// Public in the bucket, not through the file routes.
		key := "u-1/skill-2/" + monoflake.ID(3).String()
		if got := storage.PublicURL(got, key); got != "http://127.0.0.1:9/b/skills/"+key {
			t.Errorf("link %q", got)
		}
	})

	t.Run("S3Errors", func(t *testing.T) {
		old := newS3
		defer func() { newS3 = old }()
		newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
		_, err := newSkillStorage(newYAMLConfig(t, "skills: {storage: s3}"), local, "https://agentrq.example/storage/skills")
		if err == nil || err.Error() != "s3: no S3 credentials configured" {
			t.Errorf("newSkillStorage returned %v, want \"s3: no S3 credentials configured\"", err)
		}
	})
}
