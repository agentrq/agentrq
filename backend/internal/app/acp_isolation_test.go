// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The ACP versions are isolated by rule: one never imports another, and only
// this package imports any. Deleting one version must leave the backend
// compiling.
func TestACPVersionsStayIsolated(t *testing.T) {
	const prefix = "github.com/agentrq/agentrq/backend/internal/controller/"
	versions := map[string]string{prefix + "acpv1": "controller/acpv1", prefix + "acpv2": "controller/acpv2"}
	root, err := filepath.Abs("..") // internal/, so a directory reads as …/internal/app
	if err != nil {
		t.Fatalf("finding the backend failed: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			own, isACP := versions[p]
			if !isACP {
				continue
			}
			switch {
			case strings.Contains(dir, "controller/acpv") && !strings.HasSuffix(dir, own):
				t.Errorf("%s imports %s: the ACP versions must not import each other", path, p)
			case !strings.HasSuffix(dir, "internal/app") && !strings.HasSuffix(dir, own):
				t.Errorf("%s imports %s: only package app may import an ACP version", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the backend failed: %v", err)
	}
}
