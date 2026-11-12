// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunner

import (
	"encoding/json"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		ABIVersion: ABIVersion, Name: "github", Version: "1.0.0", Description: "GitHub",
		Inputs: []Input{
			{Name: "webhookSecret", Label: "Webhook secret", Type: InputString, Secret: true, Required: true},
			{Name: "branches", Type: InputArray, Items: InputString},
			{Name: "minutes", Type: InputNumber},
			{Name: "drafts", Type: InputBoolean},
		},
		Events: []Event{{Name: "ci.failed", Description: "A run failed"}, {Name: "pr_opened-2"}},
	}
}

func TestParseManifestAcceptsAValidOne(t *testing.T) {
	b, _ := json.Marshal(validManifest())
	m, err := parseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "github" || !m.Declares("ci.failed") || m.Declares("push") {
		t.Errorf("manifest = %+v", m)
	}
}

func TestParseManifestRefuses(t *testing.T) {
	long := strings.Repeat("x", maxTextSize+1)
	for _, tc := range []struct {
		name   string
		change func(*Manifest)
		want   string
	}{
		{"another ABI", func(m *Manifest) { m.ABIVersion = 2 }, "abiVersion 2"},
		{"a bad name", func(m *Manifest) { m.Name = "GitHub" }, "name"},
		{"no version", func(m *Manifest) { m.Version = "" }, "version"},
		{"a long version", func(m *Manifest) { m.Version = strings.Repeat("1", 33) }, "version"},
		{"a long description", func(m *Manifest) { m.Description = long }, "description"},
		{"too many inputs", func(m *Manifest) {
			for i := range maxInputs {
				m.Inputs = append(m.Inputs, Input{Name: "i" + strings.Repeat("x", i), Type: InputString})
			}
		}, "inputs"},
		{"no events", func(m *Manifest) { m.Events = nil }, "events"},
		{"too many events", func(m *Manifest) { m.Events = make([]Event, maxEvents+1) }, "events"},
		{"a bad input name", func(m *Manifest) { m.Inputs[1].Name = "1st" }, "input name"},
		{"a long label", func(m *Manifest) { m.Inputs[1].Label = long }, "label"},
		{"an unknown type", func(m *Manifest) { m.Inputs[1].Type = "object" }, "type"},
		{"a secret number", func(m *Manifest) { m.Inputs[2].Secret = true }, "only a string"},
		{"an array of booleans", func(m *Manifest) { m.Inputs[1].Items = InputBoolean }, "items"},
		{"items on a string", func(m *Manifest) { m.Inputs[0].Items = InputString }, "items is for an array"},
		{"an input twice", func(m *Manifest) { m.Inputs[1].Name = "webhookSecret" }, "twice"},
		{"no required secret", func(m *Manifest) { m.Inputs[0].Required = false }, "required secret"},
		{"a bad event name", func(m *Manifest) { m.Events[0].Name = "CI failed" }, "event name"},
		{"an event twice", func(m *Manifest) { m.Events[1].Name = "ci.failed" }, "twice"},
		{"a long event description", func(m *Manifest) { m.Events[0].Description = long }, "description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.change(&m)
			b, _ := json.Marshal(m)
			if _, err := parseManifest(b); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one naming %q", err, tc.want)
			}
		})
	}
	if _, err := parseManifest([]byte("{")); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestParseResult(t *testing.T) {
	m := validManifest()
	big := strings.Repeat("x", 1025)
	for _, tc := range []struct {
		name string
		out  string
		want string // an error, or the first payload
	}{
		{"a string payload as is", `{"accept":true,"events":[{"name":"ci.failed","payload":"a \"b\""}]}`, `a "b"`},
		{"an object compacted", `{"accept":true,"events":[{"name":"ci.failed","payload":{ "a" : [1, 2] }}]}`, `{"a":[1,2]}`},
		{"a number", `{"accept":true,"events":[{"name":"ci.failed","payload":42}]}`, `42`},
		{"no payload", `{"accept":true,"events":[{"name":"ci.failed"}]}`, ``},
		{"a null payload", `{"accept":true,"events":[{"name":"ci.failed","payload":null}]}`, ``},
		{"not JSON", `nope`, "not JSON"},
		{"a long reason", `{"accept":false,"reason":"` + strings.Repeat("r", maxReasonSize+1) + `"}`, "reason"},
		{"a rejection with events", `{"accept":false,"events":[{"name":"ci.failed"}]}`, "rejected"},
		{"too many events", `{"accept":true,"events":[` + strings.TrimSuffix(strings.Repeat(`{"name":"ci.failed"},`, MaxEventsPerRequest+1), ",") + `]}`, "at most 32"},
		{"an undeclared event", `{"accept":true,"events":[{"name":"push"}]}`, "not declared"},
		{"a long dedup key", `{"accept":true,"events":[{"name":"ci.failed","dedupKey":"` + strings.Repeat("k", maxDedupKeySize+1) + `"}]}`, "dedupKey"},
		{"too many faq", `{"accept":true,"events":[{"name":"ci.failed","faq":[` + strings.TrimSuffix(strings.Repeat(`{"q":"q","a":"a"},`, maxFAQs+1), ",") + `]}]}`, "faq entries"},
		{"a large payload", `{"accept":true,"events":[{"name":"ci.failed","payload":"` + big + `"}]}`, "at most 1024"},
		{"a large faq", `{"accept":true,"events":[{"name":"ci.failed","payload":"p","faq":[{"q":"` + big + `","a":""}]}]}`, "at most 1024"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parseResult([]byte(tc.out), m, 1024)
			if err != nil {
				if _, final := err.(*OutputError); !final || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("err = %v, want an OutputError naming %q", err, tc.want)
				}
				return
			}
			if len(res.Events) != 1 || res.Events[0].Payload != tc.want {
				t.Errorf("result = %+v, want payload %q", res, tc.want)
			}
		})
	}
}
