// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundledJWTSecretRequiresConfiguration(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("..", "..", "..", "cmd", "server")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	previous, wasSet := os.LookupEnv("AGENTRQ_AUTH_JWT_SECRET")
	defer func() {
		if wasSet {
			_ = os.Setenv("AGENTRQ_AUTH_JWT_SECRET", previous)
		} else {
			_ = os.Unsetenv("AGENTRQ_AUTH_JWT_SECRET")
		}
	}()
	if err := os.Unsetenv("AGENTRQ_AUTH_JWT_SECRET"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV", "production")

	loadSecret := func() string {
		svc, err := New()
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			JWTSecret string `yaml:"jwtSecret"`
		}
		if err := svc.Populate("auth", &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg.JWTSecret
	}

	if got := loadSecret(); got != "" {
		t.Fatalf("unset JWT secret resolved to a non-empty value")
	}
	if err := os.Setenv("AGENTRQ_AUTH_JWT_SECRET", "configured-test-secret"); err != nil {
		t.Fatal(err)
	}
	if got := loadSecret(); got != "configured-test-secret" {
		t.Fatalf("configured JWT secret = %q, want configured-test-secret", got)
	}
}

func TestConfig(t *testing.T) {
	// Create temporary _config directory
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldConfigPath := _configPath
	// We can't change _configPath because it's a constant.
	// But we can change working directory!

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Setup _config
	os.Mkdir("_config", 0755)

	baseYaml := `
app:
  name: "BaseApp"
database:
  host: "localhost"
  port: 5432
`
	devYaml := `
app:
  name: "DevApp"
database:
  port: ${DB_PORT:5433}
`
	os.WriteFile("_config/base.yaml", []byte(baseYaml), 0644)
	os.WriteFile("_config/development.yaml", []byte(devYaml), 0644)

	t.Run("NewAndPopulate", func(t *testing.T) {
		os.Setenv("DB_PORT", "9999")
		defer os.Unsetenv("DB_PORT")

		s, err := New()
		if err != nil {
			t.Fatalf("failed to init config: %v", err)
		}

		if s.App() != "AgentRQ" {
			t.Errorf("App mismatch: %s", s.App())
		}
		if s.AppShortName() != "agentrq" {
			t.Errorf("AppShortName mismatch: %s", s.AppShortName())
		}
		if s.Version() != "v0.9.8" {
			t.Errorf("Expected version v0.9.8, got %s", s.Version())
		}
		if s.Env() != "development" {
			t.Errorf("Env mismatch: %s", s.Env())
		}

		type DbCfg struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		}
		var db DbCfg
		err = s.Populate("database", &db)
		if err != nil {
			t.Fatal(err)
		}
		if db.Host != "localhost" {
			t.Errorf("DB Host mismatch: %s", db.Host)
		}
		if db.Port != 9999 {
			t.Errorf("DB Port mismatch: %d", db.Port)
		}

		// Non-existent key
		err = s.Populate("nonexistent", &db)
		if err != nil {
			t.Errorf("unexpected error for missing key: %v", err)
		}

		// Invalid unmarshal
		err = s.Populate("app", func() {})
		if err == nil {
			t.Error("expected error for invalid unmarshal target, got nil")
		}
	})

	t.Run("EnvMethods", func(t *testing.T) {
		s := &service{env: ""}
		if s.Env() != "development" { // should call env() helper
			t.Errorf("expected development env, got %s", s.Env())
		}
	})

	t.Run("EnvVarOverride", func(t *testing.T) {
		os.Setenv("ENV", "production")
		defer os.Unsetenv("ENV")

		prodYaml := `
database:
  host: "prod-db"
`
		os.WriteFile("_config/production.yaml", []byte(prodYaml), 0644)

		s, err := New()
		if err != nil {
			t.Fatal(err)
		}
		if s.Env() != "production" {
			t.Errorf("expected production env, got %s", s.Env())
		}
	})

	t.Run("ErrorPaths", func(t *testing.T) {
		// Test missing files or invalid yaml
		os.Remove("_config/base.yaml")
		_, err := New()
		if err == nil {
			t.Error("expected error for missing base.yaml")
		}

		os.WriteFile("_config/base.yaml", []byte("invalid: yaml: :"), 0644)
		_, err = New()
		if err == nil {
			t.Error("expected error for invalid base.yaml")
		}
	})

	_ = oldConfigPath // avoid unused warning in mind
}

func TestMergeMaps(t *testing.T) {
	a := map[string]any{"k1": "v1", "k2": map[string]any{"s1": "v2"}}
	b := map[string]any{"k1": "v1-over", "k2": map[string]any{"s2": "v3"}}

	merged := mergeMaps(a, b)
	if merged["k1"] != "v1-over" {
		t.Errorf("k1 mismatch: %v", merged["k1"])
	}
	k2 := merged["k2"].(map[string]any)
	if k2["s1"] != "v2" || k2["s2"] != "v3" {
		t.Errorf("k2 merge mismatch: %v", k2)
	}
}

func TestErrError(t *testing.T) {
	var e err = "test error"
	if e.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", e.Error())
	}
}

func TestEnvValuesAreLiteral(t *testing.T) {
	origWd, _ := os.Getwd()
	os.Chdir(t.TempDir())
	defer os.Chdir(origWd)
	os.Mkdir("_config", 0755)

	os.WriteFile("_config/base.yaml", []byte(`
s3:
  bucket: "${T_BUCKET:}"
  secret: "${T_SECRET:}"
  endpoint: ${T_ENDPOINT:}
  region: "${T_REGION:us-east-1}"
  port: ${T_PORT:3000}
  dsn: "host=${T_HOST:localhost} port=5432"
`), 0644)
	os.WriteFile("_config/development.yaml", nil, 0644)

	t.Setenv("T_BUCKET", `"my-bucket"`)
	t.Setenv("T_SECRET", "a\"b: #c\nd")
	t.Setenv("T_ENDPOINT", `'https://s3.example.com'`)
	t.Setenv("T_PORT", "9000")
	t.Setenv("T_HOST", "db")

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got struct {
		Bucket   string `yaml:"bucket"`
		Secret   string `yaml:"secret"`
		Endpoint string `yaml:"endpoint"`
		Region   string `yaml:"region"`
		Port     int    `yaml:"port"`
		DSN      string `yaml:"dsn"`
	}
	if err := s.Populate("s3", &got); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"bucket":   {got.Bucket, "my-bucket"},
		"secret":   {got.Secret, "a\"b: #c\nd"},
		"endpoint": {got.Endpoint, "https://s3.example.com"},
		"region":   {got.Region, "us-east-1"},
		"dsn":      {got.DSN, "host=db port=5432"},
	}
	for k, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", k, v[0], v[1])
		}
	}
	if got.Port != 9000 {
		t.Errorf("port = %d, want 9000", got.Port)
	}
}

func TestLoadErrors(t *testing.T) {
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	if _, err := load(dir + "/missing.yaml"); err == nil {
		t.Error("expected error for a missing file")
	}
	os.Chdir(dir)
	defer os.Chdir(origWd)
	os.Mkdir("_config", 0755)
	os.WriteFile("_config/base.yaml", []byte("a: b\n"), 0644)
	t.Setenv("ENV", "staging")
	if _, err := New(); err == nil {
		t.Error("expected error for a missing env file")
	}

	os.WriteFile(dir+"/list.yaml", []byte("- a\n- b\n"), 0644)
	if _, err := load(dir + "/list.yaml"); err == nil {
		t.Error("expected error for a non-mapping document")
	}
}
