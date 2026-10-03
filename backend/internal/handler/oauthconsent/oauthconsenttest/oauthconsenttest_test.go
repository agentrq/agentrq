// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsenttest

import "testing"

func TestToken(t *testing.T) {
	if got := Token(`<input type="hidden" name="consent" value="a&amp;b">`); got != "a&b" {
		t.Errorf("Token read %q from the page, want a&b", got)
	}
	if got := Token("<p>Not found</p>"); got != "" {
		t.Errorf("Token read %q from a page with no form, want nothing", got)
	}
}
