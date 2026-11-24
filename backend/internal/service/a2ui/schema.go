// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// The official A2UI v1.0 schemas, vendored unmodified (see schema/README.md).
//
//go:embed schema/*.json
var schemaFiles embed.FS

const (
	draft2020 = "https://json-schema.org/draft/2020-12/schema"
	specBase  = "https://a2ui.org/specification/v1_0/"

	// common_types.json names Extensions keys with \p{XID_Start} and
	// \p{XID_Continue} (UAX #31), which Go's regexp cannot compile, so the
	// whole schema would fail to load. They are rewritten, as the file is
	// read, to the general categories those properties are built from; the
	// two differ only on a few dozen compatibility characters.
	xidPattern   = `^[\\p{XID_Start}_][\\p{XID_Continue}]*$`
	goXIDPattern = `^[\\p{L}\\p{Nl}_][\\p{L}\\p{Nl}\\p{Mn}\\p{Mc}\\p{Nd}\\p{Pc}]*$`
)

// schemas holds the official definitions, resolved once: one per accepted
// message, and one per component in the catalog, with the names of the
// properties each component defines.
type schemas struct {
	messages   map[string]*jsonschema.Resolved
	components map[string]*jsonschema.Resolved
	properties map[string][]string
}

func loadSchemas(fsys fs.FS) (*schemas, error) {
	s := &schemas{
		messages:   map[string]*jsonschema.Resolved{},
		components: map[string]*jsonschema.Resolved{},
		properties: map[string][]string{},
	}
	docs := map[string]*jsonschema.Schema{}
	for _, name := range []string{"agent_to_renderer.json", "common_types.json", "catalog.json"} {
		doc, err := readSchema(fsys, name)
		if err != nil {
			return nil, err
		}
		docs[name] = doc
	}
	for _, kind := range acceptedKinds {
		def := strings.ToUpper(kind[:1]) + kind[1:] + "Message"
		r, err := resolve(fsys, &jsonschema.Schema{Ref: "agent_to_renderer.json#/$defs/" + def})
		if err != nil {
			return nil, err
		}
		s.messages[kind] = r
	}
	// The official Component is ComponentCommon plus a oneOf over every
	// catalog component. Once the component's name is known, the same
	// definition with only that branch gives an error about its properties,
	// rather than "did not validate against any of" eighteen.
	for _, name := range append(append([]string{}, displayComponents...), interactiveComponents...) {
		r, err := resolve(fsys, &jsonschema.Schema{
			AllOf: []*jsonschema.Schema{
				{Ref: "common_types.json#/$defs/ComponentCommon"},
				{Ref: "catalog.json#/$defs/components_" + name},
			},
			UnevaluatedProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		})
		if err != nil {
			return nil, err
		}
		s.components[name] = r
		props := map[string]bool{}
		propertyNames(docs, docs["common_types.json"].Defs["ComponentCommon"], props)
		propertyNames(docs, docs["catalog.json"].Defs["components_"+name], props)
		s.properties[name] = slices.Sorted(maps.Keys(props))
	}
	return s, nil
}

// propertyNames collects the properties a component schema defines, through
// its allOf and $ref. The official schema refuses any other property with
// unevaluatedProperties, whose error does not say which one it was.
func propertyNames(docs map[string]*jsonschema.Schema, s *jsonschema.Schema, out map[string]bool) {
	for name := range s.Properties {
		out[name] = true
	}
	for _, sub := range s.AllOf {
		propertyNames(docs, sub, out)
	}
	if file, def, ok := strings.Cut(s.Ref, "#/$defs/"); ok {
		propertyNames(docs, docs[file].Defs[def], out)
	}
}

func resolve(fsys fs.FS, root *jsonschema.Schema) (*jsonschema.Resolved, error) {
	root.Schema = draft2020
	return root.Resolve(&jsonschema.ResolveOptions{
		BaseURI: specBase,
		Loader: func(u *url.URL) (*jsonschema.Schema, error) {
			return readSchema(fsys, path.Base(u.Path))
		},
	})
}

func readSchema(fsys fs.FS, name string) (*jsonschema.Schema, error) {
	b, err := fs.ReadFile(fsys, "schema/"+name)
	if err != nil {
		return nil, err
	}
	switch name {
	case "common_types.json":
		b = bytes.ReplaceAll(b, []byte(xidPattern), []byte(goXIDPattern))
	case "catalog.json":
		if b, err = flattenCatalog(b); err != nil {
			return nil, err
		}
	}
	s := new(jsonschema.Schema)
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return s, nil
}

// flattenCatalog moves the catalog's components and functions into $defs.
// The catalog keeps them under top-level "components" and "functions", which
// are not schema keywords, and the resolver follows a $ref's JSON pointer only
// through keywords, so "#/components/Text" would not resolve.
func flattenCatalog(b []byte) ([]byte, error) {
	var doc struct {
		ID         string                     `json:"$id"`
		Defs       map[string]json.RawMessage `json:"$defs"`
		Components map[string]json.RawMessage `json:"components"`
		Functions  map[string]json.RawMessage `json:"functions"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("catalog.json: %w", err)
	}
	defs := maps.Clone(doc.Defs)
	if defs == nil {
		defs = map[string]json.RawMessage{}
	}
	for name, def := range doc.Components {
		defs["components_"+name] = def
	}
	for name, def := range doc.Functions {
		defs["functions_"+name] = def
	}
	// Every value was just decoded, so this cannot fail.
	out, _ := json.Marshal(map[string]any{"$id": doc.ID, "$defs": defs})
	return []byte(strings.NewReplacer(
		`"#/components/`, `"#/$defs/components_`,
		`"#/functions/`, `"#/$defs/functions_`,
	).Replace(string(out))), nil
}
