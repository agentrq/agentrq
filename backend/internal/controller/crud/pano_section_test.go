// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

const panoPlanDoc = "Intro\n" +
	"# Plan\n" +
	"- [x] scope\n" +
	"\n" +
	"## Build ##\n" +
	"- [x] api\n" +
	"- [ ] ui\n" +
	"```\n" +
	"# not a heading\n" +
	"- [ ] not an item\n" +
	"```\n" +
	"\n" +
	"\n" +
	"### Details\n" +
	"text\n" +
	"## build\n" +
	"- [ ] again\n" +
	"#nospace\n" +
	"# Rollout\n" +
	"- [ ] ship\n"

func TestPanoOutline(t *testing.T) {
	want := []entity.PanoHeading{
		{Level: 1, Text: "Plan", ChecklistDone: 2, ChecklistTotal: 4},
		{Level: 2, Text: "Build", ChecklistDone: 1, ChecklistTotal: 2},
		{Level: 3, Text: "Details"},
		{Level: 2, Text: "build", ChecklistTotal: 1},
		{Level: 1, Text: "Rollout", ChecklistTotal: 1},
	}
	if got := PanoOutline(panoPlanDoc); !reflect.DeepEqual(got, want) {
		t.Errorf("PanoOutline = %+v, want %+v", got, want)
	}
	if got := PanoOutline("no headings\n"); len(got) != 0 {
		t.Errorf("PanoOutline of a doc without headings = %+v, want none", got)
	}
}

func TestPanoSection(t *testing.T) {
	for _, tc := range []struct {
		name, section, heading, markdown string
	}{
		{"an exact match wins over a case-insensitive one", "Build", "Build",
			"## Build ##\n- [x] api\n- [ ] ui\n```\n# not a heading\n- [ ] not an item\n```\n\n\n### Details\ntext"},
		{"the #s before the name are dropped", " ## build ", "build", "## build\n- [ ] again\n#nospace"},
		{"a match ignoring case", "ROLLOUT", "Rollout", "# Rollout\n- [ ] ship"},
		{"a section runs up to a heading of its level or a higher one", "Details", "Details", "### Details\ntext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, md, err := PanoSection(panoPlanDoc, tc.section)
			if err != nil || h.Text != tc.heading || md != tc.markdown {
				t.Errorf("PanoSection(%q) = %q, %q, %v; want %q, %q", tc.section, h.Text, md, err, tc.heading, tc.markdown)
			}
		})
	}
	for _, tc := range []struct {
		name, markdown, section, want string
	}{
		{"an empty name", panoPlanDoc, " ## ", "section needs a heading's text"},
		{"a name no heading has", panoPlanDoc, "Launch", "no heading is \"Launch\"; the headings:\n# Plan\n## Build\n### Details\n## build\n# Rollout"},
		{"a doc without headings", "text", "Plan", "section \"Plan\": the doc has no headings"},
		{"two headings with the same name", "# Dup\n# Dup\n", "dup", "section \"dup\" matches 2 headings:\n# Dup\n# Dup"},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			_, _, err := PanoSection(tc.markdown, tc.section)
			var refusal *entity.PanoError
			if !errors.As(err, &refusal) || refusal.Message != tc.want {
				t.Errorf("PanoSection(%q) returned %v, want the refusal %q", tc.section, err, tc.want)
			}
		})
	}
}

// One exact match is taken although others match ignoring case.
func TestPanoSection_ExactAmongCaseVariants(t *testing.T) {
	if h, md, err := PanoSection("# A\n## a\n# a\n", "A"); err != nil || h.Level != 1 || md != "# A\n## a" {
		t.Errorf("PanoSection = %+v, %q, %v; want the exact heading's section", h, md, err)
	}
}

func TestPano_EditReplacesASection(t *testing.T) {
	setup := func(t *testing.T, md string) (*panoEnv, entity.PanoDoc) {
		e := newPanoEnv(t)
		return e, e.save(t, entity.SavePanoDocRequest{Title: "Plan", Markdown: md})
	}
	edit := func(e *panoEnv, doc entity.PanoDoc, ops ...entity.PanoEditOp) (*entity.SavePanoDocResponse, error) {
		return e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, ID: doc.ID, Version: doc.Version, Ops: ops})
	}

	t.Run("the section, heading included, becomes the markdown and the rest is kept", func(t *testing.T) {
		e, doc := setup(t, panoPlanDoc)
		rs, err := edit(e, doc, entity.PanoEditOp{Op: "replaceSection", Section: "Details", Markdown: "### Details\nmore text"})
		if err != nil {
			t.Fatalf("EditPanoDoc returned %v, want the section replaced", err)
		}
		want := strings.Replace(panoPlanDoc, "### Details\ntext\n", "### Details\nmore text\n", 1)
		if got := e.get(t, doc.ID); got.Markdown != want || rs.Doc.Version != 2 {
			t.Errorf("the doc is %q at %d, want %q at 2", got.Markdown, rs.Doc.Version, want)
		}
	})

	t.Run("the blank lines before the next heading stay", func(t *testing.T) {
		e, doc := setup(t, "# A\n- [ ] one\n\n# B\n")
		if _, err := edit(e, doc, entity.PanoEditOp{Op: "replaceSection", Section: "A", Markdown: "# A\n- [x] one"}); err != nil {
			t.Fatal(err)
		}
		if got := e.get(t, doc.ID); got.Markdown != "# A\n- [x] one\n\n# B\n" || got.ChecklistDone != 1 {
			t.Errorf("the doc is %q with %d done, want the blank line kept and the item ticked", got.Markdown, got.ChecklistDone)
		}
	})

	t.Run("a section that cannot be found refuses the edit", func(t *testing.T) {
		e, doc := setup(t, "# A\n")
		_, err := edit(e, doc, entity.PanoEditOp{Op: "append", Text: "x"}, entity.PanoEditOp{Op: "replaceSection", Section: "B", Markdown: "# B"})
		var refusal *entity.PanoError
		if !errors.As(err, &refusal) || refusal.Message != "op 2: nothing was changed: no heading is \"B\"; the headings:\n# A" {
			t.Fatalf("EditPanoDoc returned %v, want the op's refusal", err)
		}
		var n int64
		e.db.Model(&model.PanoDoc{}).Where("version = 1").Count(&n)
		if n != 1 {
			t.Error("the refused edit changed the doc")
		}
	})
}
