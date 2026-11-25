// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func progress(props string) string {
	return `{"id":"p","component":"Progress"` + props + `}`
}

func TestValidate_AcceptsProgress(t *testing.T) {
	tests := map[string]json.RawMessage{
		"value only":       ourCard(progress(`,"value":40`)),
		"of max, labelled": ourCard(progress(`,"label":"3 of 5 checks","value":3,"max":5,"weight":1`)),
		"zero":             ourCard(progress(`,"value":0`)),
		"bound":            ourCard(progress(`,"label":{"@path":"/label"},"value":{"@path":"/done"},"max":{"@path":"/total"}`)),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			s, err := svc(t).Validate(context.Background(), raw, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !HasComponent(s, "Progress") || HasComponent(s, "Chart") {
				t.Errorf("HasComponent got the wrong components: %s", s.Messages)
			}
		})
	}
}

func TestValidate_RefusesProgress(t *testing.T) {
	tests := map[string]struct {
		raw   json.RawMessage
		parts []string
	}{
		"basic surface": {card(progress(`,"value":1`)), []string{`"p": Progress is in our catalog: createSurface's catalogId must be "` + AgentRQCatalogID + `" to use it`}},
		"no value":      {ourCard(progress(``)), []string{`"p": `, "value"}},
		"negative":      {ourCard(progress(`,"value":-1`)), []string{`"p": value: `}},
		"zero max":      {ourCard(progress(`,"value":1,"max":0`)), []string{`"p": max: `}},
		"a string":      {ourCard(progress(`,"value":"half"`)), []string{`"p": value: `}},
		"unknown prop":  {ourCard(progress(`,"value":1,"color":"red"`)), []string{`"p": `, "color"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{})
			wantProblem(t, err, tc.parts...)
		})
	}
}

func TestMerge_UpdatesProgressInPlace(t *testing.T) {
	s, err := svc(t).Validate(context.Background(), ourCard(progress(`,"value":{"@path":"/done"},"max":5`)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := svc(t).Merge(context.Background(), s, ui(`{"version":"1.0","updateDataModel":{"surfaceId":"`+surface+`","path":"/done","value":4}}`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if all := string(merged.Messages[len(merged.Messages)-1]); !HasComponent(merged, "Progress") || !strings.Contains(all, `"value":4`) {
		t.Errorf("merged = %s", merged.Messages)
	}
}
