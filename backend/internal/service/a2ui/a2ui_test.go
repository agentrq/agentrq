// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package a2ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

var (
	sharedOnce sync.Once
	shared     Service
)

// svc loads the schemas once for the whole package: resolving them takes a
// few hundred milliseconds.
func svc(t *testing.T) Service {
	t.Helper()
	sharedOnce.Do(func() {
		s, err := New()
		if err != nil {
			panic(err)
		}
		shared = s
	})
	return shared
}

const surface = "card-1"

func create(extra string) string {
	return `{"version":"1.0","createSurface":{"surfaceId":"` + surface + `","catalogId":"` + BasicCatalogID + `"` + extra + `}}`
}

func update(components ...string) string {
	return `{"version":"1.0","updateComponents":{"surfaceId":"` + surface + `","components":[` + strings.Join(components, ",") + `]}}`
}

func ui(msgs ...string) json.RawMessage {
	return json.RawMessage("[" + strings.Join(msgs, ",") + "]")
}

// card is the smallest accepted ui with the given components after root.
func card(components ...string) json.RawMessage {
	ids := make([]string, len(components))
	for i, c := range components {
		var obj struct{ ID string }
		if err := json.Unmarshal([]byte(c), &obj); err != nil {
			panic(err)
		}
		ids[i] = `"` + obj.ID + `"`
	}
	root := `{"id":"root","component":"Column","children":[` + strings.Join(ids, ",") + `]}`
	return ui(create(""), update(append([]string{root}, components...)...))
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	return e.Problems
}

// wantProblem asserts err is an *Error with exactly one problem containing
// every part.
func wantProblem(t *testing.T, err error, parts ...string) {
	t.Helper()
	ps := problems(t, err)
	if len(ps) != 1 {
		t.Fatalf("want one problem, got %d: %q", len(ps), ps)
	}
	for _, p := range parts {
		if !strings.Contains(ps[0], p) {
			t.Errorf("problem %q does not contain %q", ps[0], p)
		}
	}
}

func TestValidate_AcceptsACardAndReturnsItsSurface(t *testing.T) {
	raw := ui(
		create(""),
		update(
			`{"id":"root","component":"Card","child":"col"}`,
			`{"id":"col","component":"Column","children":["title","chart"]}`,
			`{"id":"title","component":"Text","text":{"@path":"/title"}}`,
			`{"id":"chart","component":"Image","url":"attachment:chart.png"}`,
		),
		`{"version":"1.0","updateDataModel":{"surfaceId":"`+surface+`","path":"/","value":{"title":"PR opened","n":12345678901234567890}}}`,
	)
	got, err := svc(t).Validate(context.Background(), raw, Options{Attachments: map[string]string{"chart.png": "https://agentrq.example/storage/a/chart.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.SurfaceID != surface || len(got.Messages) != 3 {
		t.Fatalf("surface %q with %d messages", got.SurfaceID, len(got.Messages))
	}
	if !strings.Contains(string(got.Messages[1]), `"url":"https://agentrq.example/storage/a/chart.png"`) {
		t.Errorf("media not resolved: %s", got.Messages[1])
	}
	if !strings.Contains(string(got.Messages[2]), `12345678901234567890`) {
		t.Errorf("a number lost precision: %s", got.Messages[2])
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"surfaceId":"card-1","messages":[`) {
		t.Errorf("Surface JSON is not camelCase: %s", b)
	}
}

func TestValidate_RefusesWhatIsNotAnArrayOfMessages(t *testing.T) {
	tests := map[string]struct {
		raw  json.RawMessage
		want string
	}{
		"too big":       {json.RawMessage(`"` + strings.Repeat("a", MaxBytes) + `"`), "the limit is 65536 (64 KiB)"},
		"not json":      {json.RawMessage(`[{`), "must be a JSON array of A2UI v1.0 messages"},
		"not an array":  {json.RawMessage(`{"version":"1.0"}`), "must be a JSON array"},
		"trailing data": {json.RawMessage(`[] []`), "unexpected data after the array"},
		"empty":         {json.RawMessage(`[]`), "ui is empty"},
		"null":          {json.RawMessage(`null`), "ui is empty"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{})
			wantProblem(t, err, tc.want)
			if !strings.HasPrefix(err.Error(), "ui is not valid A2UI v1.0: ") {
				t.Errorf("error %q", err)
			}
		})
	}
}

func TestValidate_Messages(t *testing.T) {
	tests := map[string]struct {
		raw   json.RawMessage
		parts []string
	}{
		"not an object":     {ui(`"createSurface"`), []string{"ui[0]: must be an object"}},
		"no version":        {ui(`{"createSurface":{"surfaceId":"s"}}`), []string{`ui[0]: version must be "1.0"`}},
		"old version":       {ui(`{"version":"0.9","createSurface":{"surfaceId":"s"}}`), []string{`version must be "1.0"`}},
		"no kind":           {ui(`{"version":"1.0"}`), []string{"must hold exactly one of createSurface, updateComponents, updateDataModel, deleteSurface; it holds 0"}},
		"two kinds":         {ui(`{"version":"1.0","createSurface":{"surfaceId":"s"},"deleteSurface":{"surfaceId":"s"}}`), []string{"it holds 2"}},
		"renderer function": {ui(`{"version":"1.0","callRendererFunction":{"functionCallId":"c","callFunction":{"@call":"openUrl","catalogId":"x","args":{"url":"https://x"}}}}`), []string{"ui[0]: callRendererFunction is not accepted; send only createSurface"}},
		"function response": {ui(`{"version":"1.0","agentFunctionResponse":{"functionCallId":"c","value":1}}`), []string{"agentFunctionResponse is not accepted"}},
		"unknown kind":      {ui(`{"version":"1.0","drawChart":{}}`), []string{"drawChart is not accepted"}},
		"extra property":    {ui(create(`,"theme":"dark"`)), []string{"ui[0].createSurface: createSurface: ", "theme"}},
		"no value":          {ui(`{"version":"1.0","updateDataModel":{"surfaceId":"s"}}`), []string{"ui[0].updateDataModel: ", "value"}},
		"body not object":   {ui(`{"version":"1.0","deleteSurface":"s"}`), []string{"ui[0].deleteSurface: deleteSurface: type"}},
		"bad surface id":    {ui(`{"version":"1.0","deleteSurface":{"surfaceId":"a b"}}`), []string{`surfaceId "a b" must be 1 to 64 letters, digits, '_' or '-'`}},
		"long surface id":   {ui(`{"version":"1.0","deleteSurface":{"surfaceId":"` + strings.Repeat("a", 65) + `"}}`), []string{"must be 1 to 64"}},
		"two surfaces":      {ui(create(""), `{"version":"1.0","deleteSurface":{"surfaceId":"other"}}`), []string{`ui[1].deleteSurface: surfaceId "other" differs from "card-1": one message carries one surface`}},
		"no catalog":        {ui(`{"version":"1.0","createSurface":{"surfaceId":"s"}}`), []string{"ui[0].createSurface: catalogId must be \"" + BasicCatalogID + "\", the basic catalog"}},
		"shorthand catalog": {ui(`{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"basic"}}`), []string{"catalogId must be"}},
		"bad extension key": {ui(create(`,"metadata":{"extensions":{"1st":true}}`)), []string{"ui[0].createSurface: ", "1st"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{Interactive: true})
			wantProblem(t, err, tc.parts...)
		})
	}
}

func TestValidate_AcceptsEveryKindOfMessage(t *testing.T) {
	raw := ui(
		create(`,"sendDataModel":true,"dataModel":{"a":1},"metadata":{"extensions":{"éxtra_1":{"x":1},"_k":2}}`),
		update(`{"id":"root","component":"Text","text":"hi"}`),
		`{"version":"1.0","updateDataModel":{"surfaceId":"`+surface+`","path":"/a","value":null}}`,
		`{"version":"1.0","deleteSurface":{"surfaceId":"`+surface+`"}}`,
	)
	if _, err := svc(t).Validate(context.Background(), raw, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_Components(t *testing.T) {
	tests := map[string]struct {
		raw         json.RawMessage
		interactive bool
		parts       []string
	}{
		"not an object": {card(), false, nil},
		"unknown":       {card(`{"id":"c","component":"Map"}`), true, []string{`ui[1].updateComponents.components[1] "c": component "Map" is not one of Text, Image, Icon, Video, AudioPlayer, Row, Column, List, Card, Tabs, Divider, Button, TextField`}},
		"no name":       {card(`{"id":"c"}`), false, []string{`component "" is not one of Text, Image, Icon, Video, AudioPlayer, Row, Column, List, Card, Tabs, Divider`}},
		"input on display": {card(`{"id":"c","component":"TextField","label":"Name"}`), false, []string{
			`components[1] "c": TextField is not available here, where the ui only displays; use Text, Image, Icon, Video, AudioPlayer, Row, Column, List, Card, Tabs, Divider`}},
		"unknown property": {card(`{"id":"c","component":"Text","text":"x","color":"red","size":2}`), false, []string{`components[1] "c": "color" or "size" is not a property of Text, which has @accessibility, @catalogId, @metadata, component, id, text, variant, weight`}},
		"input property":   {card(`{"id":"c","component":"Button","child":"c","action":{"event":{"name":"x"}},"value":1}`), true, []string{`"value" is not a property of Button, which has @accessibility, @catalogId, @checks, @metadata, action, child, component, id, variant, weight`}},
		"bad enum":         {card(`{"id":"c","component":"Text","text":"x","variant":"h1"}`), false, []string{`components[1] "c": variant: `, "h1"}},
		"bad binding":      {card(`{"id":"c","component":"Text","text":{"@bind":"/x"}}`), false, []string{`components[1] "c": text: matches none of the forms the schema allows`}},
		"unknown function": {card(`{"id":"c","component":"Text","text":{"@call":"eval","args":{}}}`), false, []string{`"c": text: `}},
		"no id":            {card(`{"component":"Text","text":"x"}`), false, []string{`components[1]: `, "id"}},
		"surface":          {card(`{"id":"c","component":"Surface","child":"root"}`), true, []string{`component "Surface" is not one of`}},
		"duplicate id":     {card(`{"id":"c","component":"Text","text":"x"}`, `{"id":"c","component":"Divider"}`), false, []string{`components[2] "c": id "c" is used twice in this list; ids must be unique`}},
		"other catalog":    {card(`{"id":"c","component":"Text","text":"x","@catalogId":"acme.com:ui"}`), false, []string{`"c": @catalogId must be the surface's catalog, "` + BasicCatalogID + `", or left out`}},
	}
	tests["not an object"] = struct {
		raw         json.RawMessage
		interactive bool
		parts       []string
	}{ui(create(""), update(`{"id":"root","component":"Divider"}`, `"text"`)), false, []string{`ui[1].updateComponents.components[1]: must be an object`}}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{Interactive: tc.interactive})
			wantProblem(t, err, tc.parts...)
		})
	}
}

func TestValidate_InteractiveComponentsNeedTheOption(t *testing.T) {
	raw := card(
		`{"id":"b","component":"Button","child":"bl","action":{"event":{"name":"ok"}}}`,
		`{"id":"bl","component":"Text","text":"OK"}`,
		`{"id":"t","component":"TextField","label":"Name","value":{"@path":"/name"}}`,
		`{"id":"x","component":"CheckBox","label":"Agree","value":false}`,
		`{"id":"p","component":"ChoicePicker","options":[{"label":"A","value":"a"}],"value":["a"]}`,
		`{"id":"s","component":"Slider","max":10,"value":3}`,
		`{"id":"d","component":"DateTimeInput","value":"2026-11-24"}`,
		`{"id":"m","component":"Modal","trigger":"b","content":"bl"}`,
	)
	if _, err := svc(t).Validate(context.Background(), raw, Options{Interactive: true}); err != nil {
		t.Fatal(err)
	}
	_, err := svc(t).Validate(context.Background(), raw, Options{})
	for i, name := range []string{"Button", "TextField", "CheckBox", "ChoicePicker", "Slider", "DateTimeInput", "Modal"} {
		if p := problems(t, err); !strings.Contains(p[i], name+" is not available here") {
			t.Errorf("problem %d = %q, want %s refused", i, p[i], name)
		}
	}
}

func TestValidate_AcceptsEveryDisplayComponent(t *testing.T) {
	raw := card(
		`{"id":"t","component":"Text","text":"x","variant":"caption","weight":1.5,"@accessibility":{"label":"x"}}`,
		`{"id":"i","component":"Icon","name":"check"}`,
		`{"id":"r","component":"Row","children":["l"],"justify":"spaceBetween","align":"center"}`,
		`{"id":"l","component":"List","children":{"componentId":"item","path":"/items"},"direction":"horizontal"}`,
		`{"id":"item","component":"Text","text":{"@path":"name"}}`,
		`{"id":"c","component":"Card","child":"tabs"}`,
		`{"id":"tabs","component":"Tabs","tabs":[{"title":"One","child":"dv"}]}`,
		`{"id":"dv","component":"Divider","axis":"horizontal"}`,
		`{"id":"f","component":"Text","text":{"@call":"formatNumber","args":{"value":{"@path":"/n"}}}}`,
	)
	if _, err := svc(t).Validate(context.Background(), raw, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_TooManyComponents(t *testing.T) {
	comps := make([]string, MaxComponents)
	for i := range comps {
		comps[i] = fmt.Sprintf(`{"id":"t%d","component":"Text","text":"x"}`, i)
	}
	_, err := svc(t).Validate(context.Background(), card(comps...), Options{})
	wantProblem(t, err, `components[200] "t199": more than 200 components; the limit is 200`)

	if _, err := svc(t).Validate(context.Background(), card(comps[:MaxComponents-1]...), Options{}); err != nil {
		t.Errorf("200 components refused: %v", err)
	}
}

func TestValidate_Media(t *testing.T) {
	attached := map[string]string{"a.png": "https://s/a.png", "b.mp4": "https://s/b.mp4", "c.mp3": "https://s/c.mp3"}
	t.Run("resolved in every media property and in createSurface", func(t *testing.T) {
		raw := ui(create(`,"components":[` + strings.Join([]string{
			`{"id":"root","component":"Column","children":["i","v","a"]}`,
			`{"id":"i","component":"Image","url":"attachment:a.png","fit":"cover"}`,
			`{"id":"v","component":"Video","url":"attachment:b.mp4","posterUrl":"attachment:a.png"}`,
			`{"id":"a","component":"AudioPlayer","url":"attachment:c.mp3","description":"Episode"}`,
		}, ",") + `]`))
		got, err := svc(t).Validate(context.Background(), raw, Options{Attachments: attached})
		if err != nil {
			t.Fatal(err)
		}
		s := string(got.Messages[0])
		for _, want := range []string{`"url":"https://s/a.png"`, `"url":"https://s/b.mp4"`, `"posterUrl":"https://s/a.png"`, `"url":"https://s/c.mp3"`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s not in %s", want, s)
			}
		}
		if strings.Contains(s, "attachment:") {
			t.Errorf("an attachment reference is left: %s", s)
		}
	})
	tests := map[string]struct {
		component   string
		attachments map[string]string
		want        string
	}{
		"binding":      {`{"id":"m","component":"Image","url":{"@path":"/img"}}`, attached, `components[1] "m": url must be attachment:<filename> of a file attached to this reply, as a literal string: media cannot come from a data binding or a function call`},
		"call":         {`{"id":"m","component":"Video","url":"attachment:b.mp4","posterUrl":{"@call":"formatString","args":{"value":"x"}}}`, attached, `"m": posterUrl must be attachment:<filename> of a file attached to this reply, as a literal string`},
		"link":         {`{"id":"m","component":"AudioPlayer","url":"https://evil.example/a.mp3"}`, attached, `"m": url must be attachment:<filename> of a file attached to this reply: media cannot come from a link`},
		"no filename":  {`{"id":"m","component":"Image","url":"attachment:"}`, attached, `media cannot come from a link`},
		"not attached": {`{"id":"m","component":"Image","url":"attachment:z.png"}`, attached, `"m": url names "z.png", but no file by that name is attached to this reply (attached: "a.png", "b.mp4", "c.mp3")`},
		"none":         {`{"id":"m","component":"Image","url":"attachment:z.png"}`, nil, `(attached: none)`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), card(tc.component), Options{Attachments: tc.attachments})
			wantProblem(t, err, tc.want)
		})
	}
}

func TestValidate_Graph(t *testing.T) {
	text := func(id string) string { return `{"id":"` + id + `","component":"Text","text":"x"}` }
	chain := func(n int) json.RawMessage {
		comps := []string{`{"id":"root","component":"Card","child":"c1"}`}
		for i := 1; i < n-1; i++ {
			comps = append(comps, fmt.Sprintf(`{"id":"c%d","component":"Card","child":"c%d"}`, i, i+1))
		}
		return ui(create(""), update(append(comps, text(fmt.Sprintf("c%d", n-1)))...))
	}
	tests := map[string]struct {
		raw   json.RawMessage
		parts []string
	}{
		"no root":          {ui(create(""), update(text("a"))), []string{`no component has id "root"`}},
		"missing child":    {ui(create(""), update(`{"id":"root","component":"Card","child":"x"}`)), []string{`ui[1].updateComponents.components[0] "root": child names "x", which is not a component in this ui`}},
		"missing children": {ui(create(""), update(`{"id":"root","component":"Row","children":["a","x"]}`, text("a"))), []string{`"root": children[1] names "x"`}},
		"missing template": {ui(create(""), update(`{"id":"root","component":"List","children":{"componentId":"x","path":"/items"}}`)), []string{`"root": children.componentId names "x"`}},
		"missing tab":      {ui(create(""), update(`{"id":"root","component":"Tabs","tabs":[{"title":"a","child":"a"},{"title":"b","child":"x"}]}`, text("a"))), []string{`"root": tabs[1].child names "x"`}},
		"missing trigger":  {ui(create(""), update(`{"id":"root","component":"Modal","trigger":"x","content":"a"}`, text("a"))), []string{`"root": trigger names "x"`}},
		"missing content":  {ui(create(""), update(`{"id":"root","component":"Modal","trigger":"a","content":"x"}`, text("a"))), []string{`"root": content names "x"`}},
		"self cycle":       {ui(create(""), update(`{"id":"root","component":"Card","child":"root"}`)), []string{"components form a cycle: root -> root"}},
		"cycle":            {ui(create(""), update(`{"id":"root","component":"Card","child":"a"}`, `{"id":"a","component":"Column","children":["b"]}`, `{"id":"b","component":"Card","child":"a"}`)), []string{"components form a cycle: a -> b -> a"}},
		"detached cycle":   {ui(create(""), update(text("root"), `{"id":"a","component":"Card","child":"b"}`, `{"id":"b","component":"Card","child":"a"}`)), []string{"components form a cycle: a -> b -> a"}},
		"too deep":         {chain(MaxDepth + 1), []string{"the component tree is 17 levels deep from root; the limit is 16"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc(t).Validate(context.Background(), tc.raw, Options{Interactive: true})
			wantProblem(t, err, tc.parts...)
		})
	}

	t.Run("16 levels", func(t *testing.T) {
		if _, err := svc(t).Validate(context.Background(), chain(MaxDepth), Options{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("shared subtree", func(t *testing.T) {
		raw := ui(create(""), update(`{"id":"root","component":"Row","children":["a","a"]}`, text("a")))
		if _, err := svc(t).Validate(context.Background(), raw, Options{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a later message replaces a component", func(t *testing.T) {
		raw := ui(create(""),
			update(`{"id":"root","component":"Card","child":"gone"}`, text("gone")),
			update(`{"id":"root","component":"Card","child":"a"}`, text("a")),
		)
		if _, err := svc(t).Validate(context.Background(), raw, Options{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("no components at all", func(t *testing.T) {
		if _, err := svc(t).Validate(context.Background(), ui(create("")), Options{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("not checked while a component is refused", func(t *testing.T) {
		_, err := svc(t).Validate(context.Background(), ui(create(""), update(`{"id":"a","component":"Map"}`)), Options{})
		wantProblem(t, err, `component "Map"`)
	})
}

func TestValidate_ListsAtMostTwentyProblems(t *testing.T) {
	comps := []string{`{"id":"root","component":"Divider"}`}
	for i := 0; i < 25; i++ {
		comps = append(comps, fmt.Sprintf(`{"id":"c%d","component":"Map"}`, i))
	}
	_, err := svc(t).Validate(context.Background(), ui(create(""), update(comps...)), Options{})
	p := problems(t, err)
	if len(p) != maxProblems+1 || p[maxProblems] != "and 5 more" {
		t.Fatalf("%d problems, last %q", len(p), p[len(p)-1])
	}
}

// The official examples, vendored with their licence. Media in them are
// links or data bindings, both of which we refuse: each example is tried as
// it is, and refused for exactly its media, then with every media value
// replaced by an attachment reference, when it passes with Interactive, and
// without it unless it uses an input component.
func TestValidate_OfficialExamples(t *testing.T) {
	files, err := filepath.Glob("testdata/examples/*.json")
	if err != nil || len(files) != 43 {
		t.Fatalf("%d examples, %v", len(files), err)
	}
	var display, withMedia int
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var ex struct {
				Name     string
				Messages []map[string]any
			}
			if err := json.Unmarshal(b, &ex); err != nil || ex.Name == "" {
				t.Fatalf("%v", err)
			}
			var refused []string
			attachments := map[string]string{}
			interactive := false
			for i, m := range ex.Messages {
				for _, kind := range []string{"createSurface", "updateComponents"} {
					body, _ := m[kind].(map[string]any)
					comps, _ := body["components"].([]any)
					for j, c := range comps {
						obj := c.(map[string]any)
						component := obj["component"].(string)
						interactive = interactive || slices.Contains(interactiveComponents, component)
						for _, prop := range mediaProperties[component] {
							val, ok := obj[prop]
							if !ok {
								continue
							}
							reason := "media cannot come from a link"
							if _, literal := val.(string); !literal {
								reason = "media cannot come from a data binding or a function call"
							}
							refused = append(refused, fmt.Sprintf(`ui[%d].%s.components[%d] %q: %s must be attachment:<filename> of a file attached to this reply`, i, kind, j, obj["id"], prop)+"|"+reason)
							file := fmt.Sprintf("%s-%s.bin", obj["id"], prop)
							attachments[file] = "https://storage.example/" + file
							obj[prop] = attachmentPrefix + file
						}
					}
				}
			}

			var asIs struct{ Messages json.RawMessage }
			if err := json.Unmarshal(b, &asIs); err != nil {
				t.Fatal(err)
			}
			_, err = svc(t).Validate(context.Background(), asIs.Messages, Options{Interactive: true})
			if len(refused) == 0 && err != nil {
				t.Fatalf("as it is: %v", err)
			}
			if len(refused) > 0 {
				withMedia++
				got := problems(t, err)
				if len(got) != len(refused) {
					t.Fatalf("as it is: %d problems, want %d: %q", len(got), len(refused), got)
				}
				for k, want := range refused {
					where, reason, _ := strings.Cut(want, "|")
					if !strings.HasPrefix(got[k], where) || !strings.HasSuffix(got[k], reason) {
						t.Errorf("problem %q, want %q ... %q", got[k], where, reason)
					}
				}
			}

			raw, _ := json.Marshal(ex.Messages)
			for _, opt := range []bool{true, false} {
				if !opt && interactive {
					continue
				}
				if !opt {
					display++
				}
				got, err := svc(t).Validate(context.Background(), raw, Options{Interactive: opt, Attachments: attachments})
				if err != nil {
					t.Fatalf("interactive=%v: %v", opt, err)
				}
				if len(got.Messages) != len(ex.Messages) || strings.Contains(fmt.Sprintf("%s", got.Messages), `"attachment:`) {
					t.Errorf("interactive=%v: messages not all returned and resolved", opt)
				}
			}
		})
	}
	if display != 20 || withMedia != 13 {
		t.Errorf("%d examples display-only, %d with media", display, withMedia)
	}
}

func TestNew_RefusesBrokenSchemas(t *testing.T) {
	read := func(name string) []byte {
		b, err := fs.ReadFile(schemaFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	good := func() fstest.MapFS {
		m := fstest.MapFS{}
		for _, n := range []string{"schema/agent_to_renderer.json", "schema/common_types.json", "schema/catalog.json", ourCatalogFile} {
			m[n] = &fstest.MapFile{Data: read(n)}
		}
		return m
	}
	if _, err := newService(good()); err != nil {
		t.Fatalf("the vendored schemas: %v", err)
	}
	tests := map[string]struct {
		file, data string
		want       string
	}{
		"missing message schema": {"schema/agent_to_renderer.json", "", "agent_to_renderer.json"},
		"broken common types":    {"schema/common_types.json", "{", "common_types.json"},
		"broken catalog":         {"schema/catalog.json", "{", "catalog.json"},
		"catalog with no $defs":  {"schema/catalog.json", `{"components":{}}`, `"/$defs/`},
		"no message definitions": {"schema/agent_to_renderer.json", `{"$defs":{}}`, "CreateSurfaceMessage"},
		"catalog with no Text":   {"schema/catalog.json", `{"$defs":{"anyComponent":{},"anyFunction":{},"Weight":{}}}`, "components_Text"},
		"missing own catalog":    {ourCatalogFile, "", ourCatalogFile},
		"broken own catalog":     {ourCatalogFile, "{", ourCatalogFile},
		"own catalog, no Chart":  {ourCatalogFile, `{"components":{}}`, "components_Chart"},
		"own Chart unresolvable": {ourCatalogFile, `{"components":{"Chart":{"$ref":"catalog.json#/$defs/nothing"}}}`, `"/$defs/nothing"`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fsys := good()
			if tc.data == "" {
				delete(fsys, tc.file)
			} else {
				fsys[tc.file] = &fstest.MapFile{Data: []byte(tc.data)}
			}
			_, err := newService(fsys)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "loading the A2UI schemas: ") {
				t.Fatalf("err = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestSchemaError(t *testing.T) {
	tests := map[string]string{
		"plain":                      "plain",
		"validating /a: type: wrong": "type: wrong",
		"validating /p/properties/x: validating /p/properties/x/items: validating /q/properties/y: oneOf: did not validate against any of [a b]": "x.y: matches none of the forms the schema allows",
		"validating /properties/a/properties/b: bad": "a.b: bad",
	}
	for in, want := range tests {
		if got := schemaError(errors.New(in)); got != want {
			t.Errorf("schemaError(%q) = %q, want %q", in, got, want)
		}
	}
}
