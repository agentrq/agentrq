// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package slack

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	slackapi "github.com/slack-go/slack"
)

const taskLink = "https://app.agentrq.com/workspaces/w/tasks/t"

// card is a reply's metadata holding these A2UI messages, written as JSON.
func card(t *testing.T, messages string) map[string]any {
	t.Helper()
	var list []any
	if err := json.Unmarshal([]byte(messages), &list); err != nil {
		t.Fatalf("messages: %v", err)
	}
	return map[string]any{"type": "a2ui", "version": "1.0", "surfaceId": "s", "messages": list}
}

// components is a card made of these components and this data model.
func components(t *testing.T, comps, data string) map[string]any {
	t.Helper()
	return card(t, `[
		{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"x"}},
		{"version":"1.0","updateComponents":{"surfaceId":"s","components":`+comps+`}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","value":`+data+`}}
	]`)
}

// describe writes blocks one per line, for comparing.
func describe(blocks []slackapi.Block) string {
	var lines []string
	for _, b := range blocks {
		switch v := b.(type) {
		case *slackapi.ContextBlock:
			lines = append(lines, "context: "+v.ContextElements.Elements[0].(*slackapi.TextBlockObject).Text)
		case *slackapi.SectionBlock:
			if v.Text != nil {
				lines = append(lines, "section: "+v.Text.Text)
				continue
			}
			var fields []string
			for _, f := range v.Fields {
				fields = append(fields, f.Text)
			}
			lines = append(lines, "fields: "+strings.Join(fields, " | "))
		case *slackapi.DividerBlock:
			lines = append(lines, "divider")
		case *slackapi.ImageBlock:
			lines = append(lines, "image: "+v.ImageURL+" ("+v.AltText+")")
		case *slackapi.ActionBlock:
			btn := v.Elements.ElementSet[0].(*slackapi.ButtonBlockElement)
			lines = append(lines, "button: "+btn.ActionID+" "+btn.Text.Text+" "+btn.URL)
		}
	}
	return strings.Join(lines, "\n")
}

const agentLine = "context: 🤖 *Agent:* "

func TestBuildCardBlocksIgnoresAMessageWithoutACard(t *testing.T) {
	for name, meta := range map[string]any{
		"no metadata":              nil,
		"not a map":                "a2ui",
		"another type":             map[string]any{"type": "permission_request"},
		"a question in a form":     map[string]any{"type": "elicitation_request", "mode": "form"},
		"a card without messages":  map[string]any{"type": "a2ui"},
		"a deleted card":           card(t, `[{"version":"1.0","deleteSurface":{"surfaceId":"s"}}]`),
		"a card showing nothing":   components(t, `[{"id":"root","component":"Icon","name":"check"}]`, `{}`),
		"a card with no root":      components(t, `[{"id":"other","component":"Text","text":"hi"}]`, `{}`),
		"a card of an empty text":  components(t, `[{"id":"root","component":"Text","text":"  "}]`, `{}`),
		"a card of only dividers":  components(t, `[{"id":"root","component":"Column","children":["a","b"]},{"id":"a","component":"Divider"},{"id":"b","component":"Divider"}]`, `{}`),
		"a component with no id":   components(t, `[{"component":"Text","text":"hi"}]`, `{}`),
		"components not in a list": card(t, `[{"version":"1.0","updateComponents":{"surfaceId":"s","components":{}}}]`),
	} {
		if got := BuildCardBlocks("agent", "text", meta, taskLink); got != nil {
			t.Errorf("%s: blocks = %s, want none", name, describe(got))
		}
	}
}

func TestBuildCardBlocksDrawsADisplayCard(t *testing.T) {
	meta := components(t, `[
		{"id":"root","component":"Card","child":"col"},
		{"id":"col","component":"Column","children":["title","stats","note","sep","chart","plain","tabs","icon"]},
		{"id":"title","component":"Text","text":{"@path":"/title"}},
		{"id":"stats","component":"Row","children":["passed","total"]},
		{"id":"passed","component":"Text","text":{"@call":"formatString","args":{"value":"Passed: ${/passed}"}}},
		{"id":"total","component":"Text","text":{"@call":"formatNumber","args":{"value":{"@path":"/total"}}},"variant":"caption"},
		{"id":"note","component":"Text","text":"**Note** <!channel> & co","variant":"caption"},
		{"id":"sep","component":"Divider"},
		{"id":"chart","component":"Image","url":"https://files.example/storage/chart.png","description":"Coverage chart"},
		{"id":"plain","component":"Image","url":{"@path":"/image"}},
		{"id":"tabs","component":"Tabs","tabs":[{"title":"Log","child":"log"},{"title":"","child":"ok"}]},
		{"id":"log","component":"Text","text":{"@path":"/ok"}},
		{"id":"ok","component":"Text","text":{"@path":"/count"}},
		{"id":"icon","component":"Icon","name":"check"}
	]`, `{"title":"PR #1400","passed":12,"total":12.5,"image":"http://localhost:3000/storage/a.png","ok":true,"count":3}`)

	want := strings.Join([]string{
		agentLine,
		"section: PR #1400",
		"fields: Passed: 12 | 12.5",
		"context: *Note* &lt;!channel&gt; &amp; co",
		"divider",
		"image: https://files.example/storage/chart.png (Coverage chart)",
		"image: http://localhost:3000/storage/a.png (Image)",
		"section: *Log*",
		"section: true",
		"section: 3",
	}, "\n")
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildCardBlocksOffersThePageForWhatSlackCannotShow(t *testing.T) {
	button := "button: open_in_agentrq Open in AgentRQ " + taskLink
	for name, tc := range map[string]struct {
		comps string
		want  string
	}{
		"an input": {
			`[{"id":"root","component":"Column","children":["t","f"]},{"id":"t","component":"Text","text":"Name?"},{"id":"f","component":"TextField","label":"Name","value":{"@path":"/name"}}]`,
			"section: Name?\n" + button,
		},
		"a link Slack cannot fetch": {
			`[{"id":"root","component":"Column","children":["t","i"]},{"id":"t","component":"Text","text":"Chart"},{"id":"i","component":"Image","url":"/api/v1/workspaces/w/tasks/t/attachments/a"}]`,
			"section: Chart\n" + button,
		},
		"nothing but a button": {
			`[{"id":"root","component":"Button","child":"l","action":{"event":{"name":"ok"}}},{"id":"l","component":"Text","text":"OK"}]`,
			"section: Ship &lt;it&gt;?\n" + button,
		},
	} {
		got := describe(BuildCardBlocks("agent", "Ship <it>?", components(t, tc.comps, `{}`), taskLink))
		if got != agentLine+"\n"+tc.want {
			t.Errorf("%s: blocks =\n%s\nwant\n%s", name, got, agentLine+"\n"+tc.want)
		}
	}
}

func TestBuildCardBlocksDrawsAQuestionAskedAsACard(t *testing.T) {
	meta := components(t, `[{"id":"root","component":"ChoicePicker","options":[]}]`, `{}`)
	meta["type"], meta["mode"] = "elicitation_request", "a2ui"
	want := agentLine + "\nsection: Which one?\nbutton: open_in_agentrq Open in AgentRQ " + taskLink
	if got := describe(BuildCardBlocks("agent", "Which one?", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildCardBlocksRepeatsAListTemplate(t *testing.T) {
	meta := components(t, `[
		{"id":"root","component":"Column","children":["items","byName","missing"]},
		{"id":"items","component":"List","children":{"componentId":"item","path":"/items"}},
		{"id":"item","component":"Row","children":["name","state"]},
		{"id":"name","component":"Text","text":{"@path":"name"}},
		{"id":"state","component":"Text","text":{"@call":"formatString","args":{"value":"${state} \\${raw} ${now()} ${/none}"}}},
		{"id":"byName","component":"List","children":{"componentId":"key","path":"/byName"}},
		{"id":"key","component":"Text","text":{"@path":""}},
		{"id":"missing","component":"List","children":{"componentId":"key","path":"/nothing"}}
	]`, `{"items":[{"name":"lint","state":"ok"},{"name":"test","state":"failed"}],"byName":{"b":"second","a":"first"}}`)

	want := strings.Join([]string{
		agentLine,
		"fields: lint | ok ${raw} ${now()}",
		"fields: test | failed ${raw} ${now()}",
		"section: first",
		"section: second",
	}, "\n")
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildCardBlocksKeepsARowThatIsNotAllText(t *testing.T) {
	texts := make([]string, 11)
	for i := range texts {
		texts[i] = fmt.Sprintf(`"t%d"`, i)
	}
	meta := components(t, `[
		{"id":"root","component":"Column","children":["one","mixed","many"]},
		{"id":"one","component":"Row","children":["t0"]},
		{"id":"mixed","component":"Row","children":["t1","img"]},
		{"id":"img","component":"Image","url":"https://x/storage/a.png"},
		{"id":"many","component":"Row","children":[`+strings.Join(texts, ",")+`]},
		`+textComponents(11)+`
	]`, `{}`)
	got := describe(BuildCardBlocks("agent", "text", meta, taskLink))
	if !strings.HasPrefix(got, agentLine+"\nsection: 0\nsection: 1\nimage: https://x/storage/a.png (Image)\nsection: 0\n") || strings.Contains(got, "fields") {
		t.Errorf("blocks =\n%s\nwant each Row's items one under another", got)
	}
}

// textComponents is components t0…t<n-1>, each a Text showing its number.
func textComponents(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"id":"t%d","component":"Text","text":"%d"}`, i, i)
	}
	return strings.Join(parts, ",")
}

func TestBuildCardBlocksKeepsDividersBetweenContentOnly(t *testing.T) {
	meta := components(t, `[
		{"id":"root","component":"Column","children":["d","first","second","d","d"]},
		{"id":"d","component":"Divider"},
		{"id":"first","component":"Card","child":"a"},
		{"id":"second","component":"Card","child":"b"},
		{"id":"a","component":"Text","text":"A"},
		{"id":"b","component":"Text","text":"B"}
	]`, `{}`)
	want := agentLine + "\nsection: A\ndivider\nsection: B"
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildCardBlocksStaysWithinSlackLimits(t *testing.T) {
	button := "button: open_in_agentrq Open in AgentRQ " + taskLink

	t.Run("blocks", func(t *testing.T) {
		ids := make([]string, 60)
		for i := range ids {
			ids[i] = fmt.Sprintf(`"t%d"`, i)
		}
		meta := components(t, `[{"id":"root","component":"Column","children":[`+strings.Join(ids, ",")+`]},`+textComponents(60)+`]`, `{}`)
		blocks := BuildCardBlocks("agent", "text", meta, taskLink)
		if len(blocks) != 50 || !strings.HasSuffix(describe(blocks), "section: 47\n"+button) {
			t.Errorf("%d blocks, ending %q; want 50 with the button last", len(blocks), describe(blocks[len(blocks)-2:]))
		}
	})
	t.Run("a divider at the limit", func(t *testing.T) {
		ids := make([]string, 0, 50)
		for i := range 47 {
			ids = append(ids, fmt.Sprintf(`"t%d"`, i))
		}
		ids = append(ids, `"d"`, `"t47"`)
		meta := components(t, `[{"id":"root","component":"Column","children":[`+strings.Join(ids, ",")+`]},{"id":"d","component":"Divider"},`+textComponents(48)+`]`, `{}`)
		got := describe(BuildCardBlocks("agent", "text", meta, taskLink))
		if !strings.HasSuffix(got, "section: 46\n"+button) {
			t.Errorf("blocks end %q; want the last text before the divider, then the button", got[len(got)-80:])
		}
	})
	t.Run("texts", func(t *testing.T) {
		long := strings.Repeat("é", 3500)
		meta := components(t, `[
			{"id":"root","component":"Column","children":["body","row","caption","img"]},
			{"id":"body","component":"Text","text":{"@path":"/long"}},
			{"id":"row","component":"Row","children":["body","body"]},
			{"id":"caption","component":"Text","text":{"@path":"/long"},"variant":"caption"},
			{"id":"img","component":"Image","url":"https://x/storage/a.png","description":{"@path":"/long"}}
		]`, `{"long":"`+long+`"}`)
		blocks := BuildCardBlocks("agent", "text", meta, taskLink)
		section := blocks[1].(*slackapi.SectionBlock).Text.Text
		field := blocks[2].(*slackapi.SectionBlock).Fields[0].Text
		caption := blocks[3].(*slackapi.ContextBlock).ContextElements.Elements[0].(*slackapi.TextBlockObject).Text
		alt := blocks[4].(*slackapi.ImageBlock).AltText
		for name, tc := range map[string]struct {
			s     string
			limit int
		}{"section": {section, 3000}, "field": {field, 2000}, "caption": {caption, 3000}, "alt": {alt, 2000}} {
			if n := utf8.RuneCountInString(tc.s); n != tc.limit || !strings.HasSuffix(tc.s, "…") {
				t.Errorf("%s: %d runes, want %d ending in …", name, n, tc.limit)
			}
		}
		if got := describe(blocks[5:]); got != button {
			t.Errorf("last block %q, want the button", got)
		}
	})
}

func TestBuildCardBlocksStopsOnACardThatLoops(t *testing.T) {
	meta := components(t, `[
		{"id":"root","component":"Column","children":["t","root"]},
		{"id":"t","component":"Text","text":"x"}
	]`, `{}`)
	if got := BuildCardBlocks("agent", "text", meta, taskLink); len(got) != 33 {
		t.Errorf("%d blocks, want the sender and one text per level down to the depth cap", len(got))
	}
}

func TestBuildCardBlocksAppliesTheCardsUpdatesInOrder(t *testing.T) {
	meta := card(t, `[
		{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"x","components":[{"id":"root","component":"Text","text":"old"}],"dataModel":{"a":{"b":"x"},"list":["p","q"],"gone":"g"}}},
		{"version":"1.0","updateComponents":{"surfaceId":"s","components":[
			{"id":"root","component":"Column","children":["a","list","gone","bad","deep"]},
			{"id":"a","component":"Text","text":{"@path":"/a/b"}},
			{"id":"list","component":"Text","text":{"@path":"/list/1"}},
			{"id":"gone","component":"Text","text":{"@path":"/gone"}},
			{"id":"bad","component":"Text","text":{"@path":"/list/9"}},
			{"id":"deep","component":"Text","text":{"@path":"/scalar/x/y"}}
		]}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/a/b","value":"new"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/list/1","value":"Q"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/list/7","value":"ignored"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/list/x/y","value":"ignored"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/gone"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/scalar","value":1}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/scalar/z","value":"over"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/a~1b~0c","value":"escaped"}}
	]`)
	want := agentLine + "\nsection: new\nsection: Q"
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
}

// Slack applies a card's data writes as the page does: null removes, a
// removed list item takes its place, and a write just past the end appends.
func TestBuildCardBlocksWritesTheDataModelAsThePageDoes(t *testing.T) {
	meta := card(t, `[
		{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"x","dataModel":{"items":["a","b","c"],"kept":"k","n":5}}},
		{"version":"1.0","updateComponents":{"surfaceId":"s","components":[
			{"id":"root","component":"Column","children":["items","kept","n","made"]},
			{"id":"items","component":"List","children":{"componentId":"item","path":"/items"}},
			{"id":"item","component":"Text","text":{"@path":""}},
			{"id":"kept","component":"Text","text":{"@path":"/kept"}},
			{"id":"n","component":"Text","text":{"@path":"/n"}},
			{"id":"made","component":"Text","text":{"@path":"/n2/deep/x"}}
		]}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/0","value":null}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/2","value":"d"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/9","value":"far"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/9","value":null}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/x","value":"key"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/1/sub","value":"over"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/3/sub","value":"made"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/items/3","value":null}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/kept/__proto__/x","value":"no"}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/n","value":null}},
		{"version":"1.0","updateDataModel":{"surfaceId":"s","path":"/n2/deep/x","value":"made"}}
	]`)
	want := strings.Join([]string{agentLine, "section: b", "section: d", "section: k", "section: made"}, "\n")
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}

	// A null at the root clears the model to an empty object.
	meta["messages"] = append(meta["messages"].([]any), map[string]any{"version": "1.0", "updateDataModel": map[string]any{"surfaceId": "s", "path": "/", "value": nil}})
	if got := BuildCardBlocks("agent", "text", meta, taskLink); got != nil {
		t.Errorf("blocks = %s, want none once the model is cleared", describe(got))
	}
}

func TestBuildCardBlocksReadsAnEscapedPathAndAClearedModel(t *testing.T) {
	meta := components(t, `[{"id":"root","component":"Column","children":["a","b"]},
		{"id":"a","component":"Text","text":{"@path":"/a~1b~0c"}},
		{"id":"b","component":"Text","text":{"@path":"/b"}}]`, `{"a/b~c":"slash","b":"kept"}`)
	if got := describe(BuildCardBlocks("agent", "text", meta, taskLink)); got != agentLine+"\nsection: slash\nsection: kept" {
		t.Errorf("blocks =\n%s", got)
	}

	meta["messages"] = append(meta["messages"].([]any), map[string]any{"version": "1.0", "updateDataModel": map[string]any{"surfaceId": "s", "path": "/"}})
	if got := BuildCardBlocks("agent", "text", meta, taskLink); got != nil {
		t.Errorf("blocks = %s, want none once the model is cleared", describe(got))
	}
}

func TestBuildCardBlocksLeavesTheMetadataAsItWas(t *testing.T) {
	meta := components(t, `[{"id":"root","component":"Text","text":{"@path":"/a"}}]`, `{"a":"x"}`)
	meta["messages"] = append(meta["messages"].([]any), map[string]any{"version": "1.0", "updateDataModel": map[string]any{"surfaceId": "s", "path": "/a", "value": "y"}})
	before, _ := json.Marshal(meta)
	BuildCardBlocks("agent", "text", meta, taskLink)
	if after, _ := json.Marshal(meta); string(after) != string(before) {
		t.Errorf("metadata changed:\n%s\nwas\n%s", after, before)
	}
}

func TestBuildCardBlocksListsAChartsPoints(t *testing.T) {
	button := "button: open_in_agentrq Open in AgentRQ " + taskLink
	var many []string
	for i := range 14 {
		many = append(many, fmt.Sprintf(`{"label":"d%d","value":%d}`, i, i))
	}
	for name, tc := range map[string]struct {
		comps, data, want string
	}{
		"a list": {
			`[{"id":"root","component":"Chart","label":"PRs <merged>","data":[{"label":"Mon_1","value":2},{"label":"Tue","value":0.5},{"label":"","value":1},{"label":"x"}]}]`, `{}`,
			"section: *PRs &lt;merged&gt;*\nMon_1: *2*\nTue: *0.5*\n" + button,
		},
		"bound, no label": {
			`[{"id":"root","component":"Chart","variant":"bar","data":{"@path":"/points"}}]`, `{"points":[{"label":"a","value":3}]}`,
			"section: a: *3*\n" + button,
		},
		"cut at twelve": {
			`[{"id":"root","component":"Chart","data":[` + strings.Join(many, ",") + `]}]`, `{}`,
			"section: d0: *0*\nd1: *1*\nd2: *2*\nd3: *3*\nd4: *4*\nd5: *5*\nd6: *6*\nd7: *7*\nd8: *8*\nd9: *9*\nd10: *10*\nd11: *11*\n_…and 2 more_\n" + button,
		},
		"no points": {
			`[{"id":"root","component":"Chart","data":{"@path":"/none"}}]`, `{}`,
			"section: Weekly\n" + button,
		},
	} {
		got := describe(BuildCardBlocks("agent", "Weekly", components(t, tc.comps, tc.data), taskLink))
		if got != agentLine+"\n"+tc.want {
			t.Errorf("%s: blocks =\n%s\nwant\n%s", name, got, agentLine+"\n"+tc.want)
		}
	}
}
