// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/s3"
)

func TestNewSkillStorage(t *testing.T) {
	local := t.TempDir()
	const key = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

	t.Run("DefaultsToLocal", func(t *testing.T) {
		for _, doc := range []string{"{}", "skills: {storage: local}", "skills: {storage: ' LOCAL '}"} {
			got, err := newSkillStorage(newYAMLConfig(t, doc), local)
			if err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			// Local skills are filed by workspace and skill under local.
			if err := got.Save("w-1/skill-2/3", ""); err != nil {
				t.Fatalf("%s: %v", doc, err)
			}
			if _, err := os.Stat(filepath.Join(local, "w-1", "skill-2", "3")); err != nil {
				t.Errorf("%s: %v", doc, err)
			}
		}
	})

	t.Run("LocalDirError", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := newSkillStorage(newYAMLConfig(t, "{}"), file); err == nil {
			t.Error("want error")
		}
	})

	t.Run("UnknownRefusesToStart", func(t *testing.T) {
		_, err := newSkillStorage(newYAMLConfig(t, "skills: {storage: gcs}"), local)
		if err == nil || !strings.Contains(err.Error(), `"gcs"`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("BadConfig", func(t *testing.T) {
		if _, err := newSkillStorage(newYAMLConfig(t, "skills: [1]"), local); err == nil {
			t.Error("want error")
		}
	})

	t.Run("S3", func(t *testing.T) {
		c := newYAMLConfig(t, "skills: {storage: s3}\ns3: {endpoint: 'http://127.0.0.1:9', region: us-east-1, bucket: b}")
		got, err := newSkillStorage(c, local)
		if err != nil || got == nil {
			t.Fatalf("got %v, %v", got, err)
		}
	})

	t.Run("S3Errors", func(t *testing.T) {
		old := newS3
		defer func() { newS3 = old }()
		newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no creds") }
		_, err := newSkillStorage(newYAMLConfig(t, "skills: {storage: s3}"), local)
		if err == nil || err.Error() != "s3: no creds" {
			t.Errorf("s3 failure: %v", err)
		}
	})
}
