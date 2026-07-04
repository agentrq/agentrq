package app

import "testing"

type testConfigSvc struct {
	env string
}

func (s testConfigSvc) Populate(key string, cfg any) error { return nil }
func (s testConfigSvc) Env() string                        { return s.env }
func (s testConfigSvc) App() string                        { return "AgentRQ" }
func (s testConfigSvc) AppShortName() string               { return "agentrq" }
func (s testConfigSvc) Version() string                    { return "test" }

func TestValidateDeploymentSecrets_AllowsDevelopmentDefaults(t *testing.T) {
	var cfg Config
	cfg.ConfigSvc = testConfigSvc{env: "development"}
	cfg.Auth.JWTSecret = defaultJWTSecret
	cfg.Auth.WorkspaceTokenKey = defaultWorkspaceTokenKey
	cfg.Auth.RootLoginEnabled = true
	cfg.Auth.RootAccessToken = defaultRootAccessToken

	if err := validateDeploymentSecrets(cfg); err != nil {
		t.Fatalf("development defaults should be allowed, got %v", err)
	}
}

func TestValidateDeploymentSecrets_RejectsProductionDefaults(t *testing.T) {
	var cfg Config
	cfg.ConfigSvc = testConfigSvc{env: "production"}
	cfg.Auth.JWTSecret = defaultJWTSecret
	cfg.Auth.WorkspaceTokenKey = "0123456789abcdef0123456789abcdef"

	if err := validateDeploymentSecrets(cfg); err == nil {
		t.Fatal("expected production default JWT secret to be rejected")
	}
}

func TestValidateDeploymentSecrets_RejectsInvalidWorkspaceKey(t *testing.T) {
	var cfg Config
	cfg.ConfigSvc = testConfigSvc{env: "production"}
	cfg.Auth.JWTSecret = "not-the-default-secret"
	cfg.Auth.WorkspaceTokenKey = "short"

	if err := validateDeploymentSecrets(cfg); err == nil {
		t.Fatal("expected invalid workspace token key to be rejected")
	}
}
