// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	_configPath   = "./_config"
	_envDefault   = "development"
	_envVar       = "ENV"
	_appName      = "AgentRQ"
	_appShortName = "agentrq"
	_appVersion   = "v0.9.15"

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
	baseCfg, err := load(_configPath + "/base.yaml")
	if err != nil {
		return nil, err
	}

	env := env()
	envCfg, err := load(fmt.Sprintf(_configPath+"/%s.yaml", env))
	if err != nil {
		return nil, err
	}

	merged := mergeMaps(baseCfg, envCfg)

	content := make(map[string][]byte, len(merged))
	for k, v := range merged {
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

// load parses a yaml file and only then fills in its ${VAR:default}
// placeholders, so an env value is always taken as a literal string: a quote,
// colon or newline in it cannot break the parse.
func load(filename string) (map[string]any, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	expandNode(&doc)

	cfg := map[string]any{}
	if doc.Kind == 0 {
		return cfg, nil
	}
	if err := doc.Decode(&cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func expandNode(n *yaml.Node) {
	for _, c := range n.Content {
		expandNode(c)
	}
	if n.Kind != yaml.ScalarNode || !strings.Contains(n.Value, "$") {
		return
	}
	n.Value = os.Expand(n.Value, lookupEnv)
	if n.Style == 0 {
		// An unquoted value is typed by what it expands to, e.g. a port.
		n.Tag = ""
	}
}

// lookupEnv resolves VAR or VAR:default. One pair of surrounding quotes is
// dropped, as `docker run --env-file` passes them through verbatim.
func lookupEnv(placeholder string) string {
	name, def, _ := strings.Cut(placeholder, ":")
	val, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		return val[1 : len(val)-1]
	}
	return val
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
