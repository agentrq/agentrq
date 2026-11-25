// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// UpdatedSurface is the surface a ui updates, when it creates none: no
// createSurface, and a surfaceId on its first message that names one. A ui
// that cannot be read is no update; validation then says what is wrong.
func UpdatedSurface(raw json.RawMessage) (string, bool) {
	var msgs []map[string]json.RawMessage
	if json.Unmarshal(raw, &msgs) != nil || len(msgs) == 0 {
		return "", false
	}
	for _, m := range msgs {
		if _, ok := m["createSurface"]; ok {
			return "", false
		}
	}
	for kind, body := range msgs[0] {
		var b struct {
			SurfaceID string `json:"surfaceId"`
		}
		if kind != "version" && json.Unmarshal(body, &b) == nil && b.SurfaceID != "" {
			return b.SurfaceID, true
		}
	}
	return "", false
}

// Merge applies an update to a stored surface and validates the whole result.
// The result is compact: a component replaces the one with its id, and a
// data model write drops the earlier writes it overwrites, so a surface
// updated again and again does not grow. Media in the stored surface is
// already resolved; opts.Attachments, the files of the message that carries
// it, maps it back. An update ending in deleteSurface returns Deleted.
func (s *service) Merge(ctx context.Context, current *Surface, update json.RawMessage, opts Options) (*Surface, error) {
	msgs, err := decode(update)
	if err != nil {
		return nil, &Error{Problems: []string{"ui must be a JSON array of A2UI v1.0 messages: " + err.Error()}}
	}
	stored := make([]any, 0, len(current.Messages))
	for _, m := range current.Messages {
		d, err := decode(json.RawMessage("[" + string(m) + "]"))
		if err != nil {
			return nil, fmt.Errorf("the stored card %q cannot be read: %w", current.SurfaceID, err)
		}
		stored = append(stored, unresolve(d[0], opts.Attachments))
	}
	m := &merger{surfaceID: current.SurfaceID, index: map[string]int{}}
	for _, msg := range stored {
		m.apply(msg)
	}
	for i, msg := range msgs {
		if m.deleted {
			return nil, &Error{Problems: []string{fmt.Sprintf("ui[%d]: comes after deleteSurface, which must be the last message", i)}}
		}
		if id, ok := bodyOf(msg)["surfaceId"].(string); ok && id != current.SurfaceID {
			return nil, &Error{Problems: []string{fmt.Sprintf("ui[%d]: surfaceId %q differs from %q, the card this updates", i, id, current.SurfaceID)}}
		}
		m.apply(msg)
	}
	if m.deleted {
		return &Surface{SurfaceID: current.SurfaceID, Deleted: true}, nil
	}
	raw, _ := json.Marshal(m.messages()) // every value was decoded from JSON
	return s.Validate(ctx, raw, opts)
}

// unresolve turns a stored media link back into the attachment reference it
// was resolved from, so the merged surface validates as an agent's would.
func unresolve(msg any, attachments map[string]string) any {
	names := make(map[string]string, len(attachments))
	for name, u := range attachments {
		names[u] = name
	}
	body := bodyOf(msg)
	list, _ := body["components"].([]any)
	for _, c := range list {
		obj, _ := c.(map[string]any)
		name, _ := obj["component"].(string)
		for _, prop := range mediaProperties[name] {
			if u, ok := obj[prop].(string); ok && names[u] != "" {
				obj[prop] = attachmentPrefix + names[u]
			}
		}
	}
	return msg
}

// bodyOf is a message's one body, or nil when it is not a message we accept.
func bodyOf(msg any) map[string]any {
	obj, _ := msg.(map[string]any)
	if obj["version"] != "1.0" || len(obj) != 2 {
		return nil
	}
	for _, kind := range acceptedKinds {
		if body, ok := obj[kind].(map[string]any); ok {
			return body
		}
	}
	return nil
}

// merger is a surface's state, kept as the messages that rebuild it.
type merger struct {
	surfaceID  string
	create     map[string]any
	components []any
	index      map[string]int
	data       []dataWrite
	// others are messages it cannot read, kept for the validator to name.
	others  []any
	deleted bool
}

type dataWrite struct {
	path []string
	body map[string]any
}

func (m *merger) apply(msg any) {
	body := bodyOf(msg)
	obj, _ := msg.(map[string]any)
	switch {
	case body == nil:
		m.others = append(m.others, msg)
	case obj["createSurface"] != nil:
		m.create = map[string]any{}
		for k, v := range body {
			if k != "components" && k != "dataModel" {
				m.create[k] = v
			}
		}
		m.components, m.index, m.data, m.deleted = nil, map[string]int{}, nil, false
		list, _ := body["components"].([]any)
		m.putComponents(list)
		if dm, ok := body["dataModel"]; ok {
			m.write(nil, map[string]any{"surfaceId": body["surfaceId"], "value": dm})
		}
	case obj["updateComponents"] != nil:
		list, ok := body["components"].([]any)
		if !ok {
			m.others = append(m.others, msg)
			return
		}
		m.putComponents(list)
	case obj["updateDataModel"] != nil:
		path, ok := body["path"].(string)
		if _, given := body["path"]; given && !ok {
			m.others = append(m.others, msg)
			return
		}
		m.write(pointerTokens(path), body)
	default: // deleteSurface
		m.deleted = true
	}
}

func (m *merger) putComponents(list []any) {
	for _, c := range list {
		obj, _ := c.(map[string]any)
		id, ok := obj["id"].(string)
		if i, seen := m.index[id]; ok && seen {
			m.components[i] = c
			continue
		}
		if ok {
			m.index[id] = len(m.components)
		}
		m.components = append(m.components, c)
	}
}

// write records a data model write, dropping the earlier ones at or below
// its path: it replaces whatever they wrote.
func (m *merger) write(path []string, body map[string]any) {
	m.data = slices.DeleteFunc(m.data, func(w dataWrite) bool {
		return len(w.path) >= len(path) && slices.Equal(w.path[:len(path)], path)
	})
	m.data = append(m.data, dataWrite{path: path, body: body})
}

// pointerTokens splits a data model path as the page does: "" and "/" are
// the whole model. Tokens stay escaped; equal tokens are equal either way.
func pointerTokens(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

func (m *merger) messages() []any {
	var out []any
	if m.create != nil {
		out = append(out, map[string]any{"version": "1.0", "createSurface": m.create})
	}
	if len(m.components) > 0 {
		out = append(out, map[string]any{"version": "1.0", "updateComponents": map[string]any{"surfaceId": m.surfaceID, "components": m.components}})
	}
	for _, w := range m.data {
		out = append(out, map[string]any{"version": "1.0", "updateDataModel": w.body})
	}
	return append(out, m.others...)
}
