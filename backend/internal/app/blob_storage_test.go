// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"gopkg.in/yaml.v3"
)

// yamlConfig answers Populate from a yaml document, the way the real config
// service does, so the test exercises the keys base.yaml actually uses.
type yamlConfig struct {
	sections map[string]any
}

func newYAMLConfig(t *testing.T, doc string) *yamlConfig {
	t.Helper()
	c := &yamlConfig{}
	if err := yaml.Unmarshal([]byte(doc), &c.sections); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *yamlConfig) Populate(key string, out any) error {
	v, ok := c.sections[key]
	if !ok {
		return nil
	}
	b, _ := yaml.Marshal(v)
	return yaml.Unmarshal(b, out)
}
func (c *yamlConfig) Env() string          { return "test" }
func (c *yamlConfig) App() string          { return "AgentRQ" }
func (c *yamlConfig) AppShortName() string { return "agentrq" }
func (c *yamlConfig) Version() string      { return "v0" }

type fakeS3 struct{ s3.Service }

func TestStorageDir(t *testing.T) {
	for in, want := range map[string]string{"": "./_storage", "  ": "./_storage", "/storage": "/storage"} {
		if got := storageDir(cleanup.Config{StorageDir: in}); got != want {
			t.Errorf("storageDir(%q) = %q, want %q", in, got, want)
		}
	}
}
