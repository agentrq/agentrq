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

// ACP never goes through the MCP layer: a version package and agentlink, the
// protocol-neutral types they share with the workspace server, import nothing
// from MCP. Tests included, so a fake cannot pull it back in.
func TestACPImportsNothingFromMCP(t *testing.T) {
	const internal = "github.com/agentrq/agentrq/backend/internal/"
	banned := []string{internal + "controller/mcp", internal + "handler/mcp", "github.com/modelcontextprotocol/"}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("finding the backend failed: %v", err)
	}
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		dir := filepath.Base(filepath.Dir(path))
		if dir != "agentlink" && !strings.HasPrefix(dir, "acpv") {
			return nil
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b) {
					t.Errorf("%s imports %s: ACP must not depend on the MCP layer", path, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the backend failed: %v", err)
	}
	if checked == 0 {
		t.Fatal("found no ACP or agentlink source to check")
	}
}
