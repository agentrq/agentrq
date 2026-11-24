// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"fmt"
	"strings"
)

// The field names agents guess for the workspace server's payloads, taken
// from their real refusals. An alias is used only when the field it stands
// for is absent: sent together, the call is refused rather than one of the
// two picked, since either may be the one the agent meant.

// aliasField moves the first of aliases the payload has to field.
func aliasField(field string, aliases ...string) func(map[string]any) error {
	return func(p map[string]any) error {
		found := ""
		for _, alias := range aliases {
			if _, ok := p[alias]; !ok {
				continue
			}
			if _, ok := p[field]; ok {
				return fmt.Errorf("send %s only: %s is another name for it", field, alias)
			}
			if found != "" {
				return fmt.Errorf("send %s only, not both %s and %s", field, found, alias)
			}
			found = alias
		}
		if found != "" {
			p[field] = p[found]
			delete(p, found)
		}
		return nil
	}
}

// memoryNameAliases takes the memory:// link the index uses, as uri or name,
// for its name, and adds the .md a name left out.
func memoryNameAliases(p map[string]any) error {
	if err := aliasField("name", "uri")(p); err != nil {
		return err
	}
	name, ok := p["name"].(string)
	if !ok {
		return nil
	}
	name = strings.TrimSpace(name)
	if len(name) >= len("memory://") && strings.EqualFold(name[:len("memory://")], "memory://") {
		// memory:// alone must not become no name, which is memory.md.
		if name = name[len("memory://"):]; name == "" {
			return fmt.Errorf("name memory:// names no memory")
		}
	}
	if name != "" && !strings.HasSuffix(strings.ToLower(name), ".md") {
		if _, err := resolveMemoryName(name + ".md"); err == nil {
			name += ".md"
		}
	}
	p["name"] = name
	return nil
}

// skillURIAliases takes a skill's name for the skill:// URI of its SKILL.md.
func skillURIAliases(p map[string]any) error {
	_, named := p["name"]
	if err := aliasField("uri", "name")(p); err != nil {
		return err
	}
	if uri, ok := p["uri"].(string); named && ok {
		if uri = strings.TrimSpace(uri); uri != "" && !strings.HasPrefix(strings.ToLower(uri), "skill://") {
			uri = "skill://" + uri
		}
		p["uri"] = uri
	}
	return nil
}
