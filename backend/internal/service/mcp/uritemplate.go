// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A resource template's URI template is RFC 6570 level 1: literal text and
// {name} expressions. A template with anything more is refused when it is
// added, rather than matched wrongly.

// varValue is what a {name} expands to: unreserved characters, the comma
// that separates a list's values, and percent-encoded octets.
const varValue = `((?:[,\-.0-9A-Z_a-z~]|%[0-9A-Fa-f]{2})*)`

var varName = regexp.MustCompile(`^[0-9A-Z_a-z]+(?:\.[0-9A-Z_a-z]+)*$`)

// compileURITemplate is the expression a URI matches when template names
// it.
func compileURITemplate(template string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for rest := template; rest != ""; {
		i := strings.IndexAny(rest, "{}")
		if i < 0 {
			b.WriteString(regexp.QuoteMeta(rest))
			break
		}
		if rest[i] == '}' {
			return nil, errors.New("'}' with no '{'")
		}
		b.WriteString(regexp.QuoteMeta(rest[:i]))
		end := strings.IndexAny(rest[i+1:], "{}")
		if end < 0 || rest[i+1+end] != '}' {
			return nil, errors.New("'{' with no '}'")
		}
		name := rest[i+1 : i+1+end]
		if !varName.MatchString(name) {
			return nil, fmt.Errorf("{%s} is not a {name} expression", name)
		}
		b.WriteString(varValue)
		rest = rest[i+1+end+1:]
	}
	b.WriteByte('$')
	return regexp.MustCompile(b.String()), nil
}
