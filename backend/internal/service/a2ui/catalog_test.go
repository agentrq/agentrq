// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"encoding/json"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCatalog(t *testing.T) {
	b, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := Catalog(); string(again) != string(b) {
		t.Error("Catalog differs between calls")
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if doc["$id"] != AgentRQCatalogID || doc["catalogId"] != AgentRQCatalogID {
		t.Errorf("$id %v, catalogId %v: want both %s", doc["$id"], doc["catalogId"], AgentRQCatalogID)
	}
	if !strings.HasSuffix(AgentRQCatalogID, CatalogPath) {
		t.Errorf("CatalogPath %s is not the path of %s", CatalogPath, AgentRQCatalogID)
	}
	if doc["functions"] == nil || doc["instructions"] == nil {
		t.Error("the basic catalog's functions and instructions are missing")
	}

	// Every component a card may use, and nothing else.
	components, _ := doc["components"].(map[string]any)
	want := slices.Sorted(slices.Values(slices.Concat(displayComponents, interactiveComponents, agentrqComponents)))
	if got := slices.Sorted(maps.Keys(components)); !slices.Equal(got, want) {
		t.Errorf("components %v, want %v", got, want)
	}
	anyComponent := doc["$defs"].(map[string]any)["anyComponent"].(map[string]any)
	var listed []string
	for _, r := range anyComponent["oneOf"].([]any) {
		listed = append(listed, strings.TrimPrefix(r.(map[string]any)["$ref"].(string), "#/components/"))
	}
	if slices.Sort(listed); !slices.Equal(listed, want) {
		t.Errorf("anyComponent lists %v, want %v", listed, want)
	}

	// Every reference lands, in this document or in the official common types.
	common, err := readJSON(schemaFiles, "schema/common_types.json")
	if err != nil {
		t.Fatal(err)
	}
	refs := collectRefs(doc, nil)
	if len(refs) < 100 {
		t.Fatalf("only %d $refs", len(refs))
	}
	for _, ref := range refs {
		base, pointer, _ := strings.Cut(ref, "#")
		target := doc
		switch base {
		case "":
		case specBase + "common_types.json":
			target = common
		default:
			t.Errorf("$ref %s points outside this document and the official common types", ref)
			continue
		}
		if !resolvesPointer(target, pointer) {
			t.Errorf("$ref %s does not resolve", ref)
		}
	}
}

func collectRefs(v any, out []string) []string {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok && k == "$ref" {
				out = append(out, s)
				continue
			}
			out = collectRefs(child, out)
		}
	case []any:
		for _, child := range v {
			out = collectRefs(child, out)
		}
	}
	return out
}

func resolvesPointer(doc map[string]any, pointer string) bool {
	var cur any = doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if part == "" {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[strings.NewReplacer("~1", "/", "~0", "~").Replace(part)]; !ok {
			return false
		}
	}
	return true
}

func TestBuildCatalogRefusesBrokenFiles(t *testing.T) {
	basic, _ := fs.ReadFile(schemaFiles, "schema/catalog.json")
	ours, _ := fs.ReadFile(schemaFiles, ourCatalogFile)
	for name, fsys := range map[string]fstest.MapFS{
		"no basic catalog":   {ourCatalogFile: {Data: ours}},
		"no catalog of ours": {"schema/catalog.json": {Data: basic}},
		"ours is not JSON":   {"schema/catalog.json": {Data: basic}, ourCatalogFile: {Data: []byte("{")}},
		"no anyComponent":    {"schema/catalog.json": {Data: []byte(`{"$defs": {}}`)}, ourCatalogFile: {Data: ours}},
	} {
		if _, err := buildCatalog(fsys); err == nil {
			t.Errorf("%s: built a catalog", name)
		}
	}
}
