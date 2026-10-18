// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package wire

import "testing"

func TestVersionAtLeast(t *testing.T) {
	for version, want := range map[string]bool{
		"0.9.3":                    true, // the floor itself is allowed
		"0.9.4":                    true,
		"0.9.10":                   true, // a field, not a string: "0.9.10" < "0.9.3" as text
		"0.10.0":                   true,
		"1.0.0":                    true,
		"0.9.2":                    false,
		"0.8.99":                   false,
		"0.9.3-rc1":                false, // a pre-release is below its release
		"0.9.4-rc1":                true,
		"dev":                      false,
		"":                         false,
		"v0.9.3":                   false, // the daemon is stamped without the tag's v
		"0.9":                      false,
		"0.9.3+abc":                false,
		"0.9.99999999999999999999": false, // does not fit an int
	} {
		if got := VersionAtLeast(version, MinForkVersion); got != want {
			t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", version, MinForkVersion, got, want)
		}
	}
}
