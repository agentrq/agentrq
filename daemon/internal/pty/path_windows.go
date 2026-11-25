// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package pty

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// systemPath is the machine's PATH then the user's, as the registry holds them
// now: what a terminal opened today would get.
var systemPath = func() string {
	var parts []string
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		if v := registryPath(k.root, k.path); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func registryPath(root registry.Key, path string) string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	v, typ, err := key.GetStringValue("Path")
	if err != nil {
		return ""
	}
	if typ == registry.EXPAND_SZ {
		if expanded, err := registry.ExpandString(v); err == nil {
			v = expanded
		}
	}
	return v
}
