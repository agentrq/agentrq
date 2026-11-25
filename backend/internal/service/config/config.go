// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package config

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	_configPath   = "_config"
	_envDefault   = "development"
	_envVar       = "ENV"
	_appName      = "AgentRQ"
	_appShortName = "agentrq"
	_appVersion   = "v0.19.4"

	// ErrMissingAppConfig error that shares the app configuration is not provided
	ErrMissingAppConfig err = "[config] app configuration must be provided in " + _configPath + "/<>.yaml file"
)

type (
	err string

	// Service is a config service
	Service interface {
		Populate(key string, cfg any) error
		// Keys lists the config's top-level blocks, sorted.
		Keys() []string
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

// New inits a new Config based on env name, from the working directory's
// _config.
func New() (Service, error) {
	return NewFS(os.DirFS("."))
}

// NewFS inits a new Config based on env name, from fsys's _config: the server
// passes the copy built into its binary.
func NewFS(fsys fs.FS) (Service, error) {
	baseCfg, err := load(fsys, _configPath+"/base.yaml")
	if err != nil {
		return nil, err
	}

	env := env()
	envCfg, err := load(fsys, fmt.Sprintf(_configPath+"/%s.yaml", env))
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

// Keys lists the config's top-level blocks, sorted.
func (s *service) Keys() []string {
	keys := make([]string, 0, len(s.content))
	for k := range s.content {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// overlay is a Service whose named blocks are replaced, each merged over the
// block it replaces.
type overlay struct {
	Service
	content map[string][]byte
}

// Overlay returns base with each block in blocks laid over its own: a field
// the block sets wins, and a field it leaves out keeps base's value, so a
// setting added after the block was written still has its default. A block
// is yaml, or JSON, which yaml reads too.
func Overlay(base Service, blocks map[string][]byte) (Service, error) {
	o := &overlay{Service: base, content: make(map[string][]byte, len(blocks))}
	if prev, ok := base.(*overlay); ok {
		o.Service = prev.Service
		for k, v := range prev.content {
			o.content[k] = v
		}
	}
	for key, raw := range blocks {
		over := map[string]any{}
		if err := yaml.Unmarshal(raw, &over); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		own := map[string]any{}
		if err := o.Populate(key, &own); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		o.content[key], _ = yaml.Marshal(mergeMaps(own, over))
	}
	return o, nil
}

// Populate reads a replaced block from the overlay, and any other from base.
func (o *overlay) Populate(key string, cfg any) error {
	if val, ok := o.content[key]; ok {
		return yaml.Unmarshal(val, cfg)
	}
	return o.Service.Populate(key, cfg)
}

// Keys lists base's blocks and the overlay's, sorted.
func (o *overlay) Keys() []string {
	keys := o.Service.Keys()
	for k := range o.content {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
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

// Release is the app version without its v prefix, as semver, for the MCP
// servers' serverInfo.version so a client can tell which release it reached.
func Release() string {
	return strings.TrimPrefix(_appVersion, "v")
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
func load(fsys fs.FS, name string) (map[string]any, error) {
	raw, err := fs.ReadFile(fsys, name)
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
