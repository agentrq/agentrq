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

func TestUpdatedSurface(t *testing.T) {
	for _, tc := range []struct {
		name string
		ui   string
		want string
	}{
		{"an update names its surface", `[` + update(`{"id":"t","component":"Text","text":"x"}`) + `]`, surface},
		{"a data model update", `[{"version":"1.0","updateDataModel":{"surfaceId":"s2","value":{}}}]`, "s2"},
		{"a new card is no update", string(card()), ""},
		{"createSurface later is still a new card", `[` + update() + `,` + create("") + `]`, ""},
		{"not JSON", `nope`, ""},
		{"empty", `[]`, ""},
		{"the first message names no surface", `[{"version":"1.0","updateComponents":{"components":[]}}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := UpdatedSurface(json.RawMessage(tc.ui))
			if got != tc.want || ok != (tc.want != "") {
				t.Errorf("UpdatedSurface = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

// stored is a card as a message stores it: validated, media resolved.
func stored(t *testing.T, raw json.RawMessage, opts Options) *Surface {
	t.Helper()
	s, err := svc(t).Validate(context.Background(), raw, opts)
	if err != nil {
		t.Fatalf("stored card: %v", err)
	}
	return s
}

// decoded is the surface's messages as plain values, to compare.
func decoded(t *testing.T, s *Surface) []map[string]map[string]any {
	t.Helper()
	out := make([]map[string]map[string]any, len(s.Messages))
	for i, m := range s.Messages {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(m, &msg); err != nil {
			t.Fatal(err)
		}
		out[i] = map[string]map[string]any{}
		for k, v := range msg {
			if k != "version" {
				var body map[string]any
				_ = json.Unmarshal(v, &body)
				out[i][k] = body
			}
		}
	}
	return out
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestMerge_ReplacesComponentsByIDAndAddsNewOnes(t *testing.T) {
	cur := stored(t, card(`{"id":"title","component":"Text","text":"Building"}`), Options{})
	upd := ui(update(
		`{"id":"title","component":"Text","text":"Built"}`,
		`{"id":"root","component":"Column","children":["title","note"]}`,
		`{"id":"note","component":"Text","text":"412 tests"}`,
	))
	got, err := svc(t).Merge(context.Background(), cur, upd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.SurfaceID != surface || got.Deleted {
		t.Errorf("surface = %+v", got)
	}
	msgs := decoded(t, got)
	if len(msgs) != 2 || msgs[0]["createSurface"] == nil {
		t.Fatalf("messages = %v, want createSurface and one updateComponents", msgs)
	}
	want := `[{"children":["title","note"],"component":"Column","id":"root"},{"component":"Text","id":"title","text":"Built"},{"component":"Text","id":"note","text":"412 tests"}]`
	if c := compact(msgs[1]["updateComponents"]["components"]); c != want {
		t.Errorf("components = %s\nwant %s", c, want)
	}
}

func TestMerge_KeepsOnlyTheDataModelWritesThatStillCount(t *testing.T) {
	write := func(path, value string) string {
		p := ""
		if path != "-" {
			p = `"path":"` + path + `",`
		}
		return `{"version":"1.0","updateDataModel":{"surfaceId":"` + surface + `",` + p + `"value":` + value + `}}`
	}
	text := `{"id":"t","component":"Text","text":{"@path":"/status"}}`
	cur := stored(t, ui(
		create(`,"dataModel":{"status":"queued"}`),
		update(`{"id":"root","component":"Column","children":["t"]}`, text),
		write("/build/step", `"compile"`),
		write("/other", `1`),
	), Options{})

	ctx := context.Background()
	got, err := svc(t).Merge(ctx, cur, ui(write("/build", `{"step":"test"}`), write("/build/pct", `40`), write("/build/pct", `60`)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	for _, m := range decoded(t, got)[2:] {
		writes = append(writes, compact(m["updateDataModel"]))
	}
	want := []string{
		`{"surfaceId":"card-1","value":{"status":"queued"}}`,
		`{"path":"/other","surfaceId":"card-1","value":1}`,
		`{"path":"/build","surfaceId":"card-1","value":{"step":"test"}}`,
		`{"path":"/build/pct","surfaceId":"card-1","value":60}`,
	}
	if strings.Join(writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes =\n%s\nwant\n%s", strings.Join(writes, "\n"), strings.Join(want, "\n"))
	}
	if cs := compact(decoded(t, got)[0]["createSurface"]); strings.Contains(cs, "dataModel") {
		t.Errorf("createSurface still carries the data model: %s", cs)
	}

	// A write to the whole model replaces every one before it.
	got, err = svc(t).Merge(ctx, got, ui(write("-", `{"status":"done"}`)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	msgs := decoded(t, got)
	if len(msgs) != 3 || compact(msgs[2]["updateDataModel"]) != `{"surfaceId":"card-1","value":{"status":"done"}}` {
		t.Errorf("messages = %v, want one write of the whole model", msgs)
	}
	got, err = svc(t).Merge(ctx, got, ui(write("/", `null`)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if msgs := decoded(t, got); len(msgs) != 3 || compact(msgs[2]["updateDataModel"]) != `{"path":"/","surfaceId":"card-1","value":null}` {
		t.Errorf("messages = %v, want only the clearing write", msgs)
	}
}

func TestMerge_StoredMediaIsTheMessagesOwnFile(t *testing.T) {
	files := Options{Attachments: map[string]string{"chart.png": "https://files.test/storage/a/1", "logo.png": "https://files.test/storage/a/2"}}
	cur := stored(t, card(`{"id":"chart","component":"Image","url":"attachment:chart.png"}`), files)

	got, err := svc(t).Merge(context.Background(), cur, ui(update(`{"id":"logo","component":"Image","url":"attachment:logo.png"}`, `{"id":"root","component":"Column","children":["chart","logo"]}`)), files)
	if err != nil {
		t.Fatal(err)
	}
	c := compact(decoded(t, got)[1])
	if !strings.Contains(c, `"url":"https://files.test/storage/a/1"`) || !strings.Contains(c, `"url":"https://files.test/storage/a/2"`) {
		t.Errorf("components = %s, want both images resolved", c)
	}

	// A link the message's files no longer name is refused, as any link is.
	_, err = svc(t).Merge(context.Background(), cur, ui(update(`{"id":"chart","component":"Image","url":"attachment:chart.png"}`)), Options{})
	wantProblem(t, err, "no file by that name")
	_, err = svc(t).Merge(context.Background(), cur, ui(update(`{"id":"root","component":"Column","children":["chart"]}`)), Options{})
	wantProblem(t, err, "media cannot come from a link")
}

func TestMerge_DeleteSurface(t *testing.T) {
	cur := stored(t, card(), Options{})
	del := `{"version":"1.0","deleteSurface":{"surfaceId":"` + surface + `"}}`
	got, err := svc(t).Merge(context.Background(), cur, ui(update(`{"id":"x","component":"Text","text":"bye"}`), del), Options{})
	if err != nil || !got.Deleted || got.SurfaceID != surface || got.Messages != nil {
		t.Fatalf("Merge = %+v, %v; want the surface deleted", got, err)
	}
	_, err = svc(t).Merge(context.Background(), cur, ui(del, update(`{"id":"x","component":"Text","text":"bye"}`)), Options{})
	wantProblem(t, err, "ui[1]", "must be the last message")
}

func TestMerge_Refusals(t *testing.T) {
	cur := stored(t, card(`{"id":"t","component":"Text","text":"x"}`), Options{})
	for _, tc := range []struct {
		name  string
		ui    string
		parts []string
	}{
		{"not an array", `{}`, []string{"must be a JSON array"}},
		{"another surface", `[{"version":"1.0","updateComponents":{"surfaceId":"other","components":[]}}]`, []string{`"other" differs from "card-1"`}},
		{"a broken tree", `[` + update(`{"id":"t","component":"Text","text":"x"}`, `{"id":"root","component":"Column","children":["gone"]}`) + `]`, []string{`"gone"`}},
		{"an unknown version is kept for the validator", `[{"version":"0.9","updateComponents":{"surfaceId":"card-1","components":[]}}]`, []string{`version must be "1.0"`}},
		{"components that are no list", `[{"version":"1.0","updateComponents":{"surfaceId":"card-1","components":{}}}]`, []string{"updateComponents"}},
		{"a path that is no string", `[{"version":"1.0","updateDataModel":{"surfaceId":"card-1","path":1,"value":2}}]`, []string{"updateDataModel"}},
		{"a message we do not accept", `[{"version":"1.0","callRendererFunction":{"surfaceId":"card-1"}}]`, []string{"callRendererFunction is not accepted"}},
		{"a component with no id", `[` + update(`{"component":"Text","text":"x"}`) + `]`, []string{"id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc(t).Merge(context.Background(), cur, json.RawMessage(tc.ui), Options{})
			ps := problems(t, err)
			if len(ps) == 0 {
				t.Fatal("no problem")
			}
			for _, p := range tc.parts {
				if !strings.Contains(strings.Join(ps, "; "), p) {
					t.Errorf("problems %q do not contain %q", ps, p)
				}
			}
		})
	}
}

func TestMerge_AStoredCardThatCannotBeRead(t *testing.T) {
	cur := &Surface{SurfaceID: surface, Messages: []json.RawMessage{json.RawMessage(`{`)}}
	_, err := svc(t).Merge(context.Background(), cur, ui(update()), Options{})
	if err == nil || !strings.Contains(err.Error(), `the stored card "card-1" cannot be read`) {
		t.Errorf("err = %v", err)
	}
}
