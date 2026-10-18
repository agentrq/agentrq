// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io/fs"
	"testing"
)

// The binary carries what the server used to read from beside it.
func TestTheBinaryCarriesTheWebAppAndConfig(t *testing.T) {
	if _, err := fs.Stat(webApp(), "index.html"); err != nil {
		t.Errorf("web app: %v", err)
	}
	for _, name := range []string{"_config/base.yaml", "_config/development.yaml", "_config/production.yaml"} {
		if _, err := fs.Stat(bundled, name); err != nil {
			t.Errorf("config: %v", err)
		}
	}
}
