// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "testing"

func TestResourcePanoString(t *testing.T) {
	if got := ResourcePano.String(); got != "pano" {
		t.Errorf("ResourcePano is named %q, want pano", got)
	}
}

func TestPanoColor(t *testing.T) {
	for i, name := range PanoColors {
		c, ok := ParsePanoColor(name)
		if !ok || int(c) != i || c.String() != name {
			t.Errorf("ParsePanoColor(%q) returned %v, %v, want color %d named %s", name, c, ok, i, name)
		}
	}
	if c, ok := ParsePanoColor("red"); ok || c != PanoColorYellow {
		t.Errorf("ParsePanoColor(red) returned %v, %v, want no color", c, ok)
	}
	if got := PanoColor(200).String(); got != "yellow" {
		t.Errorf("a color stored out of range is named %q, want yellow", got)
	}
	if PanoColorPurple.String() != "purple" || PanoColorYellow != 0 {
		t.Error("the stored numbers of the colors moved, want yellow 0 and purple last")
	}
}
