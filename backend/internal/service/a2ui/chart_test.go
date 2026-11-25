// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ourCard is card on a surface in our catalog.
func ourCard(components ...string) json.RawMessage {
	ids := make([]string, len(components))
	for i, c := range components {
		var obj struct{ ID string }
		if err := json.Unmarshal([]byte(c), &obj); err != nil {
			panic(err)
		}
		ids[i] = `"` + obj.ID + `"`
	}
	root := `{"id":"root","component":"Column","children":[` + strings.Join(ids, ",") + `]}`
	create := `{"version":"1.0","createSurface":{"surfaceId":"` + surface + `","catalogId":"` + AgentRQCatalogID + `"}}`
	return ui(create, update(append([]string{root}, components...)...))
}

// days is a heatmap's points, one a day for n days from 2026-01-01.
func days(n int, layout string, step time.Duration) string {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := make([]string, n)
	for i := range points {
		points[i] = fmt.Sprintf(`{"label":%q,"value":%d}`, start.Add(time.Duration(i)*step).Format(layout), i)
	}
	return "[" + strings.Join(points, ",") + "]"
}

func chart(variant, data string) string {
	return `{"id":"c","component":"Chart","variant":"` + variant + `","label":"Tasks","data":` + data + `}`
}

func TestValidate_AcceptsCharts(t *testing.T) {
	tests := map[string]json.RawMessage{
		"line":              ourCard(chart("line", `[{"label":"Mon","value":3},{"label":"Tue","value":0.5}]`)),
		"no variant":        ourCard(`{"id":"c","component":"Chart","data":[{"label":"a","value":1}],"color":"messages","weight":1}`),
		"bar, hex colour":   ourCard(`{"id":"c","component":"Chart","variant":"bar","data":[{"label":"a","value":1}],"color":"#AEC477"}`),
		"bound":             ourCard(`{"id":"c","component":"Chart","variant":"heatmap","data":{"@path":"/points"}}`),
		"heatmap of days":   ourCard(chart("heatmap", days(366, heatmapDay, 24*time.Hour))),
		"heatmap of hours":  ourCard(chart("heatmap", days(31*24, heatmapHour, time.Hour))),
		"unordered days":    ourCard(chart("heatmap", `[{"label":"2026-02-01","value":1},{"label":"2026-01-01","value":2},{"label":"2026-03-01","value":0}]`)),
		"empty heatmap":     ourCard(chart("heatmap", `[]`)),
		"our @catalogId":    ourCard(`{"id":"c","component":"Text","text":"x","@catalogId":"` + AgentRQCatalogID + `"}`),
		"basic @catalogId":  ourCard(`{"id":"c","component":"Text","text":"x","@catalogId":"` + BasicCatalogID + `"}`),
		"a basic component": ourCard(`{"id":"c","component":"Divider"}`),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			s, err := svc(t).Validate(context.Background(), raw, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := HasChart(s), strings.Contains(string(raw), `"Chart"`); got != want {
				t.Errorf("HasChart = %v, want %v", got, want)
			}
		})
	}
}

func TestValidate_RefusesCharts(t *testing.T) {
	tests := map[string]struct {
		raw   json.RawMessage
		parts []string
	}{
		"basic surface": {card(chart("line", `[]`)), []string{`components[1] "c": Chart is in our catalog: createSurface's catalogId must be "` + AgentRQCatalogID + `" to use it`}},
		"our @catalogId on a basic surface": {card(`{"id":"c","component":"Text","text":"x","@catalogId":"` + AgentRQCatalogID + `"}`),
			[]string{`@catalogId must be the surface's catalog, "` + BasicCatalogID + `", or left out`}},
		"unknown, named with ours": {ourCard(`{"id":"c","component":"Map"}`), []string{`component "Map" is not one of Text, Image, Icon, Video, AudioPlayer, Row, Column, List, Card, Tabs, Divider, Chart`}},
		"no data":                  {ourCard(`{"id":"c","component":"Chart"}`), []string{`"c": `, "data"}},
		"bad variant":              {ourCard(chart("pie", `[]`)), []string{`"c": variant: `, "pie"}},
		"bad colour":               {ourCard(`{"id":"c","component":"Chart","data":[],"color":"red"}`), []string{`"c": color: `}},
		"point without value":      {ourCard(chart("bar", `[{"label":"a"}]`)), []string{`"c": data: `}},
		"too many points":          {ourCard(chart("bar", days(745, heatmapDay, 24*time.Hour))), []string{`"c": data: `}},
		"negative":                 {ourCard(chart("line", `[{"label":"a","value":1},{"label":"b","value":-2}]`)), []string{`"c": data[1].value is -2; values are counts or amounts, zero or more`}},
		"heatmap, not a date":      {ourCard(chart("heatmap", `[{"label":"Mon","value":1}]`)), []string{`"c": data[0].label "Mon" must be a day, YYYY-MM-DD, or an hour, YYYY-MM-DD HH:00`}},
		"heatmap, days then hours": {ourCard(chart("heatmap", `[{"label":"2026-01-01","value":1},{"label":"2026-01-01 10:00","value":1}]`)), []string{`data[1].label "2026-01-01 10:00" must be a day`}},
		"heatmap, half past":       {ourCard(chart("heatmap", `[{"label":"2026-01-01 10:30","value":1}]`)), []string{`data[0].label "2026-01-01 10:30"`}},
		"heatmap, over a year":     {ourCard(chart("heatmap", days(367, heatmapDay, 24*time.Hour))), []string{`"c": the heatmap spans 367 days; a day grid draws at most 366`}},
		"heatmap, over a month":    {ourCard(chart("heatmap", `[{"label":"2026-01-01 00:00","value":1},{"label":"2026-02-01 00:00","value":1}]`)), []string{`the heatmap spans 32 days; a hour grid draws at most 31`}},
		"unknown catalog":          {ui(`{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"acme"}}`), []string{`catalogId must be "` + BasicCatalogID + `", the basic catalog, or "` + AgentRQCatalogID + `", ours, which adds Chart`}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{})
			wantProblem(t, err, tc.parts...)
		})
	}
}

func TestMerge_KeepsOurCatalog(t *testing.T) {
	s, err := svc(t).Validate(context.Background(), ourCard(chart("bar", `[{"label":"a","value":1}]`)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := svc(t).Merge(context.Background(), s, ui(update(chart("line", `[{"label":"a","value":2}]`))), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !HasChart(merged) || !strings.Contains(string(merged.Messages[0]), AgentRQCatalogID) {
		t.Errorf("merged = %s", merged.Messages)
	}
}
