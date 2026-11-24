// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"strings"
	"testing"
)

// A {name} takes what RFC 6570's simple expansion writes: unreserved
// characters, commas and percent-encoded octets, possibly none, and never a
// slash or anything else reserved. Literal text matches only itself.
func TestURITemplateMatches(t *testing.T) {
	cases := map[string]map[string]bool{
		"agentrq://workspaces/{workspaceId}/tasks/{taskId}": {
			"agentrq://workspaces/0mVA1/tasks/0mVB2":       true,
			"agentrq://workspaces/a.b-c_d~e/tasks/1,2":     true,
			"agentrq://workspaces/%2F%2f/tasks/x":          true,
			"agentrq://workspaces//tasks/":                 true,
			"agentrq://workspaces/a/b/tasks/x":             false,
			"agentrq://workspaces/a/tasks/x/":              false,
			"agentrq://workspaces/a/tasks/x?y=1":           false,
			"agentrq://workspaces/a b/tasks/x":             false,
			"agentrq://workspaces/%zz/tasks/x":             false,
			"agentrq://workspaces/%2/tasks/x":              false,
			"xagentrq://workspaces/a/tasks/x":              false,
			"agentrq://workspaces/a/tasks":                 false,
			"agentrq://workspaces/a/tasks/x\nagentrq://w/": false,
		},
		"agentrq://tasks/delegated-by/{workspaceId}": {
			"agentrq://tasks/delegated-by/0mVA1": true,
			"agentrq://tasks/delegated-by/":      true,
			"agentrq://tasks/delegated-by/a/b":   false,
			"agentrq://tasks/delegated-by":       false,
		},
		"x://a.b+c(d)/{v.w}": {
			"x://a.b+c(d)/1": true,
			"x://aXb+c(d)/1": false,
			"x://a.bbc(d)/1": false,
		},
		"plain://text": {
			"plain://text":  true,
			"plain://text2": false,
		},
	}
	for template, uris := range cases {
		re, err := compileURITemplate(template)
		if err != nil {
			t.Fatalf("%s: %v", template, err)
		}
		for uri, want := range uris {
			if got := re.MatchString(uri); got != want {
				t.Errorf("%s matches %q: %v, want %v", template, uri, got, want)
			}
		}
	}
}

// A template that is not literal text and {name}s is refused, whatever is
// wrong with it.
func TestURITemplateRefusals(t *testing.T) {
	cases := map[string]string{
		"x://{":         "'{' with no '}'",
		"x://{a{b}":     "'{' with no '}'",
		"x://a}":        "'}' with no '{'",
		"x://{}":        "{} is not a {name} expression",
		"x://{+path}":   "{+path} is not a {name} expression",
		"x://{/a,b}":    "{/a,b} is not a {name} expression",
		"x://{list*}":   "{list*} is not a {name} expression",
		"x://{var:3}":   "{var:3} is not a {name} expression",
		"x://{a..b}":    "{a..b} is not a {name} expression",
		"x://{a b}":     "{a b} is not a {name} expression",
		"x://{a}/{?q}":  "{?q} is not a {name} expression",
		"x://{a}}":      "'}' with no '{'",
		"x://{%41}/{b}": "{%41} is not a {name} expression",
	}
	for template, want := range cases {
		_, err := compileURITemplate(template)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", template, err, want)
		}
	}
}
