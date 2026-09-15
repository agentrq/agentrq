// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/config"
	"github.com/agentrq/agentrq/daemon/internal/secret"
)

// store is the daemon's state on disk: the config file and the token store.
type store struct {
	dir    string
	path   string
	tokens secret.Store
}

// load reads the config, bringing forward whatever shape it is in.
//
// A missing file is not an error — it is what a machine that has never enrolled
// looks like, and every first run would otherwise fail with something alarming.
func (s *store) load() (config.File, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return config.File{Version: config.Version}, nil
	}
	if err != nil {
		return config.File{}, fmt.Errorf("read %s: %w", s.path, err)
	}

	var raw config.File
	if err := json.Unmarshal(b, &raw); err != nil {
		return config.File{}, fmt.Errorf("read %s: %w", s.path, err)
	}
	// A pre-profiles config carried these at the top level. Migrating rather
	// than ignoring them means an upgrade is not a re-enrolment.
	var legacy config.LegacyFields
	_ = json.Unmarshal(b, &legacy)

	return config.Migrate(raw, legacy), nil
}

// save writes the config atomically.
//
// Temp file then rename, for the same reason the token store does it: a crash
// midway through should leave the previous config intact rather than a
// truncated one that no longer parses and strands the machine.
func (s *store) save(f config.File) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.dir, err)
	}
	f.Version = config.Version

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(s.dir, ".config-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace %s: %w", s.path, err)
	}
	return nil
}

// newFlags makes a flag set that reports errors through the caller rather than
// calling os.Exit, so `run` stays the single place the process ends.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// httpClient is the enrolment transport.
//
// Certificate verification is never disabled. --insecure permits plain HTTP to
// a host that asked for it; it does not mean "and stop checking certificates",
// because "there is no certificate" and "the certificate is wrong" are
// different problems and only one of them is ever deliberate.
type httpClient struct {
	timeout time.Duration
}

func (c *httpClient) Do(r *http.Request) (*http.Response, error) {
	return (&http.Client{Timeout: c.timeout}).Do(r)
}
