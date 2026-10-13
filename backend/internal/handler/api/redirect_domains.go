// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// RedirectDomains is the auth.allowedRedirectDomains setting: the other hosts
// a sign-in may return to through redirect_url. The yaml takes a list; the
// env var, which can only be a string, takes the same names comma separated.
type RedirectDomains []string

func (d *RedirectDomains) UnmarshalYAML(n *yaml.Node) error {
	var names []string
	if n.Kind == yaml.SequenceNode {
		if err := n.Decode(&names); err != nil {
			return err
		}
	} else {
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		names = strings.Split(s, ",")
	}
	*d = (*d)[:0]
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			*d = append(*d, name)
		}
	}
	return nil
}

// redirectHosts checks each name is a bare host, optionally with a port, and
// returns them lowercased as a set. A scheme or path is refused rather than
// stripped, so a typo stops startup instead of allowing something unintended.
func redirectHosts(domains []string) (map[string]struct{}, error) {
	hosts := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		host := strings.ToLower(d)
		u, err := url.Parse("https://" + host)
		if err != nil || u.Host != host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("auth.allowedRedirectDomains: %q is not a domain name like app.example.com", d)
		}
		hosts[host] = struct{}{}
	}
	return hosts, nil
}
