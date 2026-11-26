// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"context"
	"testing"
)

func TestValidate_OnlyTheComponentsNamed(t *testing.T) {
	only := Options{Components: []string{"Column", "Text", "Chart"}}
	t.Run("accepts them", func(t *testing.T) {
		raw := ourCard(`{"id":"t","component":"Text","text":"x"}`, chart("bar", `[{"label":"a","value":1}]`))
		if _, err := svc(t).Validate(context.Background(), raw, only); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("refuses the rest, naming what may be used", func(t *testing.T) {
		raw := ourCard(`{"id":"p","component":"Progress","value":1}`)
		_, err := svc(t).Validate(context.Background(), raw, only)
		wantProblem(t, err, `"p": component "Progress" is not available here; use Column, Text, Chart`)
	})
	t.Run("still needs our catalog for ours", func(t *testing.T) {
		raw := card(chart("bar", `[{"label":"a","value":1}]`))
		_, err := svc(t).Validate(context.Background(), raw, only)
		wantProblem(t, err, "Chart is in our catalog")
	})
}

func TestValidate_SaysWhatMediaIsAttachedTo(t *testing.T) {
	opts := Options{AttachedTo: "this doc", Attachments: map[string]string{"a.png": "attachment:a.png"}}
	tests := map[string]struct{ component, want string }{
		"binding":      {`{"id":"m","component":"Image","url":{"@path":"/img"}}`, "of a file attached to this doc, as a literal string"},
		"link":         {`{"id":"m","component":"Image","url":"https://x/a.png"}`, "of a file attached to this doc: media cannot come from a link"},
		"not attached": {`{"id":"m","component":"Image","url":"attachment:z.png"}`, `no file by that name is attached to this doc (attached: "a.png")`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), card(tc.component), opts)
			wantProblem(t, err, tc.want)
		})
	}
	got, err := svc(t).Validate(context.Background(), card(`{"id":"m","component":"Image","url":"attachment:a.png"}`), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !HasComponent(got, "Image") {
		t.Error("the image is not in the surface")
	}
}
