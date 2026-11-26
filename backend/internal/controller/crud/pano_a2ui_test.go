// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
)

const panoOurCatalog = `{"version":"1.0","createSurface":{"surfaceId":"s","catalogId":"` + a2ui.AgentRQCatalogID + `"}}`

// panoBlock is an a2ui block drawing the components under a root Column.
func panoBlock(ids string, components ...string) string {
	root := `{"id":"root","component":"Column","children":[` + ids + `]}`
	update := `{"version":"1.0","updateComponents":{"surfaceId":"s","components":[` + strings.Join(append([]string{root}, components...), ",") + `]}}`
	return "```a2ui\n[" + panoOurCatalog + ",\n" + update + "]\n```\n"
}

func TestPanoA2UIBlocks(t *testing.T) {
	md := "# Doc\n```a2ui\n[1,\n2]\n```\n```go\n```a2ui\n```\n  ~~~a2ui chart \nx\n~~~~\n````a2uiz\n```\n````\n``` a2ui`x\n```a2ui\nopen\n    ```"
	got := panoA2UIBlocks(md)
	want := []panoA2UIBlock{{line: 2, body: "[1,\n2]"}, {line: 9, body: "x"}, {line: 16, body: "open\n    ```"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("panoA2UIBlocks = %+v, want %+v: tagged blocks only, none inside other code, an unclosed one to the end", got, want)
	}
	if got := panoA2UIBlocks("```a2ui\n```"); len(got) != 1 || got[0].body != "" {
		t.Errorf("an empty block is %+v, want one with no content", got)
	}
}

func TestPano_SavesA2UIBlocks(t *testing.T) {
	e := newPanoEnv(t)
	md := "# Results\n" + panoBlock(`"c","i"`,
		`{"id":"c","component":"Chart","variant":"bar","label":"PRs","data":[{"label":"Mon","value":2}]}`,
		`{"id":"i","component":"Image","url":"attachment:shot.png"}`)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: md, Files: []entity.PanoUpload{{Filename: "shot.png", Data: b64("\x89PNG\r\n\x1a\n")}}})
	if doc.Markdown != md || len(doc.Files) != 1 || doc.Files[0].Filename != "shot.png" {
		t.Fatalf("saved %q with files %+v, want the markdown as written and the file a block shows", doc.Markdown, doc.Files)
	}
	// Saved again without the file, the block still links it: it is kept.
	doc = e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: doc.Version, Title: "t", Markdown: md + "\nmore"})
	if len(doc.Files) != 1 {
		t.Errorf("the block's file was dropped: %+v", doc.Files)
	}
	// Once no block shows it, it goes, and a block may not show it then.
	_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: doc.Version, Title: "t", Markdown: strings.Replace(md, "attachment:shot.png", "attachment:other.png", 1)})
	wantPanoRefusal(t, err, `url names "other.png", but no file by that name is attached to this doc (attached: none)`)
	doc = e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: doc.Version, Title: "t", Markdown: "gone"})
	if len(doc.Files) != 0 {
		t.Errorf("a file nothing shows was kept: %+v", doc.Files)
	}
}

func TestPano_RefusesA2UIBlocks(t *testing.T) {
	for _, tc := range []struct{ name, md, want string }{
		{"not JSON", "x\n```a2ui\nnope\n```", "the a2ui block at line 2 is not valid A2UI v1.0: ui must be a JSON array"},
		{"a component a doc does not draw", panoBlock(`"p"`, `{"id":"p","component":"Progress","value":1}`), `component "Progress" is not available here; use Text, Row, Column, Card, Tabs, Divider, Icon, Image, Video, AudioPlayer, Chart`},
		{"an input", panoBlock(`"b"`, `{"id":"b","component":"Button","child":"root","action":{"event":{"name":"x"}}}`), `component "Button" is not available here`},
		{"media from a link", panoBlock(`"v"`, `{"id":"v","component":"Video","url":"https://x/v.mp4"}`), "of a file attached to this doc: media cannot come from a link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPanoEnv(t)
			_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: tc.md})
			wantPanoRefusal(t, err, tc.want)
			if !strings.Contains(err.Error(), "see the pano tool's help") {
				t.Errorf("the refusal %q does not say where to look", err)
			}
		})
	}
}

func TestPano_A2UIValidatorThatCannotLoad(t *testing.T) {
	broken := errors.New("no schemas")
	saved := panoA2UIValidator
	panoA2UIValidator = func() (a2ui.Service, error) { return nil, broken }
	t.Cleanup(func() { panoA2UIValidator = saved })
	e := newPanoEnv(t)
	if _, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: "```a2ui\n[]\n```"}); !errors.Is(err, broken) {
		t.Errorf("SavePanoDoc returned %v, want the validator's error", err)
	}
	// A doc with no block never needs it.
	e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "plain"})
}
