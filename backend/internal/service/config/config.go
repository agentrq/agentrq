package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	_configPath   = "./_config"
	_envDefault   = "development"
	_envVar       = "ENV"
	_appName      = "AgentRQ"
	_appShortName = "agentrq"
	_appVersion   = "v0.1.0"

	// ErrMissingAppConfig error that shares the app configuration is not provided
	ErrMissingAppConfig err = "[config] app configuration must be provided in " + _configPath + "/<>.yaml file"
)

type (
	err string

	// Service is a config service
	Service interface {
		Populate(key string, cfg any) error
		Env() string
		App() string
		AppShortName() string
		Version() string
	}

	service struct {
		content map[string][]byte
		env     string
	}
)

// New inits a new Config based on env name
func New() (Service, error) {
	// read base yaml file
	basefilename, err := filepath.Abs(_configPath + "/base.yaml")
	if err != nil {
		return nil, err
	}

	baseYaml, err := os.ReadFile(basefilename)
	if err != nil {
		return nil, err
	}
	baseCfg := map[string]any{}
	if err := yaml.Unmarshal(baseYaml, &baseCfg); err != nil {
		return nil, err
	}

	// read env yaml file
	env := env()
	envfilename, err := filepath.Abs(fmt.Sprintf(_configPath+"/%s.yaml", env))
	if err != nil {
		return nil, err
	}
	envYaml, err := os.ReadFile(envfilename)
	if err != nil {
		return nil, err
	}
	envCfg := map[string]any{}
	if err := yaml.Unmarshal(envYaml, &envCfg); err != nil {
		return nil, err
	}

	merged := mergeMaps(baseCfg, envCfg)
	yamlFile, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}

	// expand env vars
	mapper := func(placeholderName string) string {
		split := strings.Split(placeholderName, ":")
		defaultValue := ""
		if len(split) >= 2 {
			placeholderName = split[0]
			defaultValue = strings.Join(split[1:], ":")
		}

		val, ok := os.LookupEnv(placeholderName)
		if !ok {
			return defaultValue
		}

		return val
	}
	body := []byte(os.Expand(string(yamlFile), mapper))

	// parse into a generic map
	var cfg map[string]any
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return nil, err
	}

	content := make(map[string][]byte, len(cfg))
	for k, v := range cfg {
		content[k], _ = yaml.Marshal(v)
	}

	s := &service{
		content: content,
		env:     env,
	}

	return s, nil
}

// Populate populates configuration
func (s *service) Populate(key string, cfg any) error {
	if val, ok := s.content[key]; ok {
		if err := yaml.Unmarshal(val, cfg); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// Env return current config environment
func (s *service) Env() string {
	if s.env != "" {
		return s.env
	}
	return env()
}

// App return current app name
func (s *service) App() string {
	return string(_appName)
}

// AppShortName return current app short name
func (s *service) AppShortName() string {
	return string(_appShortName)
}

// Version return current app version with v prefix
func (s *service) Version() string {
	return string(_appVersion)
}

// Env return current config environment
func env() string {
	env := os.Getenv(_envVar)
	if env != "" {
		return env
	}
	return _envDefault
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]any); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]any); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

func (e err) Error() string {
	return string(e)
}
