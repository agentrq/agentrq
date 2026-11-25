// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
)

// CatalogPath is AgentRQCatalogID's path, where every server serves Catalog.
const CatalogPath = "/a2ui/v1_0/catalogs/agentrq/catalog.json"

var catalogDoc = sync.OnceValues(func() ([]byte, error) { return buildCatalog(schemaFiles) })

// Catalog is the document AgentRQCatalogID names: the basic catalog with our
// components added, built from the files the validator loads, so it can never
// differ from what a card is checked against.
func Catalog() ([]byte, error) { return catalogDoc() }

func buildCatalog(fsys fs.FS) ([]byte, error) {
	basic, err := readJSON(fsys, "schema/catalog.json")
	if err != nil {
		return nil, err
	}
	ours, err := readJSON(fsys, ourCatalogFile)
	if err != nil {
		return nil, err
	}
	doc := maps.Clone(basic)
	for _, key := range []string{"$id", "title", "description"} {
		doc[key] = ours[key]
	}
	doc["catalogId"] = AgentRQCatalogID

	components := map[string]any{}
	basicComponents, _ := basic["components"].(map[string]any)
	maps.Copy(components, basicComponents)
	defs, _ := basic["$defs"].(map[string]any)
	anyComponent, ok := defs["anyComponent"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema/catalog.json: no $defs.anyComponent")
	}
	oneOf, _ := anyComponent["oneOf"].([]any)
	added, _ := ours["components"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(added)) {
		components[name] = added[name]
		oneOf = append(oneOf, map[string]any{"$ref": "#/components/" + name})
	}
	doc["components"] = components
	anyComponent["oneOf"] = oneOf

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc) // every value was just decoded, so this cannot fail
	// The vendored files point at each other by name. Published alone, a
	// reference to the basic catalog is to this document, and one to
	// common_types.json is to the official copy beside the specification.
	return []byte(strings.NewReplacer(
		`"$ref": "catalog.json#`, `"$ref": "#`,
		`"$ref": "common_types.json#`, `"$ref": "`+specBase+`common_types.json#`,
	).Replace(out.String())), nil
}

func readJSON(fsys fs.FS, name string) (map[string]any, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return doc, nil
}
