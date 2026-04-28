package config

import (
	"os"
	"testing"
)

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
		if s.Version() != "v0.1.0" {
			t.Errorf("Version mismatch: %s", s.Version())
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
