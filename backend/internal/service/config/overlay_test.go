// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func overlayBase(t *testing.T) Service {
	t.Helper()
	t.Setenv("ENV", "development")
	svc, err := NewFS(fstest.MapFS{
		"_config/base.yaml":        {Data: []byte("smtp:\n  host: mail\n  port: 587\n  tls:\n    on: true\n    min: 12\nddos:\n  enabled: true\n")},
		"_config/development.yaml": {Data: []byte("")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

type smtpBlock struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	User string `yaml:"user"`
	TLS  struct {
		On  bool `yaml:"on"`
		Min int  `yaml:"min"`
	} `yaml:"tls"`
}

func TestKeysListsTheBlocksSorted(t *testing.T) {
	if got := overlayBase(t).Keys(); !slices.Equal(got, []string{"ddos", "smtp"}) {
		t.Fatalf("Keys = %v", got)
	}
}

// A stored block wins field by field; a field it lacks keeps the yaml's value.
func TestOverlayMergesEachBlockOverItsOwn(t *testing.T) {
	base := overlayBase(t)
	svc, err := Overlay(base, map[string][]byte{
		"smtp":  []byte(`{"port":2525,"user":"ops","tls":{"min":13}}`),
		"extra": []byte(`{"on":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got smtpBlock
	if err := svc.Populate("smtp", &got); err != nil {
		t.Fatal(err)
	}
	if got.Host != "mail" || got.Port != 2525 || got.User != "ops" || !got.TLS.On || got.TLS.Min != 13 {
		t.Fatalf("smtp = %+v", got)
	}
	var ddos struct {
		Enabled bool `yaml:"enabled"`
	}
	if err := svc.Populate("ddos", &ddos); err != nil || !ddos.Enabled {
		t.Fatalf("a block not stored is read from yaml: %+v, %v", ddos, err)
	}
	if keys := svc.Keys(); !slices.Equal(keys, []string{"ddos", "extra", "smtp"}) {
		t.Fatalf("Keys = %v", keys)
	}
	if svc.Env() != "development" {
		t.Fatalf("Env = %q", svc.Env())
	}

	// An overlay of an overlay keeps the first one's blocks.
	again, err := Overlay(svc, map[string][]byte{"ddos": []byte(`{"enabled":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	got = smtpBlock{}
	if err := again.Populate("smtp", &got); err != nil || got.Port != 2525 {
		t.Fatalf("smtp after a second overlay = %+v, %v", got, err)
	}
	if err := again.Populate("ddos", &ddos); err != nil || ddos.Enabled {
		t.Fatalf("ddos after a second overlay = %+v, %v", ddos, err)
	}
	// The first is left as it was.
	if err := svc.Populate("ddos", &ddos); err != nil || !ddos.Enabled {
		t.Fatalf("the first overlay changed: %+v, %v", ddos, err)
	}
}

// failingConfig fails every Populate.
type failingConfig struct{ Service }

func (failingConfig) Populate(string, any) error { return errors.New("unreadable") }

func TestOverlayRefusesABadBlock(t *testing.T) {
	if _, err := Overlay(overlayBase(t), map[string][]byte{"smtp": []byte(`[1, 2`)}); err == nil || !strings.HasPrefix(err.Error(), "smtp: ") {
		t.Fatalf("a block that is not yaml: %v", err)
	}
	if _, err := Overlay(failingConfig{overlayBase(t)}, map[string][]byte{"smtp": []byte(`{}`)}); err == nil || err.Error() != "smtp: unreadable" {
		t.Fatalf("a base that cannot be read: %v", err)
	}
}
