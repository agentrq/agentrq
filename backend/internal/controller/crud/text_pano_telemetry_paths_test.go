// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

func TestParseWorkflowText_MoreErrors(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		wantLine    int
		wantMsg     string
	}{
		{"a header without a colon", "workflow w\n", 1, `expected "workflow: <name>"`},
		{"an empty workflow name", "workflow:\n", 1, "workflow name cannot be empty"},
		{"an empty start event", "workflow: w\nevent:\n", 2, "start event name cannot be empty"},
		{"an empty list item", "workflow: w\nevent: s\n-  \n", 3, "list item is empty"},
		{"an indented bare word", "workflow: w\n  foo\n", 2, `"foo" must not be indented`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWorkflowText(tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantMsg)
			}
			te, ok := err.(*WorkflowTextError)
			if !ok || te.Line != tc.wantLine {
				t.Errorf("err = %#v, want line %d", err, tc.wantLine)
			}
		})
	}
}

// One agent emitting two events is listed in emitted-event order.
func TestRenderWorkflowText_SortsBranchesOfOneAgentByEvent(t *testing.T) {
	got := RenderWorkflowText("w", "s", []TextStep{
		{EventName: "s", WorkspaceName: "a", EmitEventName: "z"},
		{EventName: "s", WorkspaceName: "a", EmitEventName: "m"},
	})
	if m, z := strings.Index(got, "event:m"), strings.Index(got, "event:z"); m < 0 || z < 0 || m > z {
		t.Errorf("RenderWorkflowText =\n%s\nwant event:m before event:z", got)
	}
}

// Updating a doc whose files cannot be listed fails before anything is written.
func TestPano_UpdateFailsWhenItsFilesCannotBeListed(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "![a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: b64("a")}}})
	before := e.blobs(t)
	*e.events = (*e.events)[:0]
	failOn(e.db, "query", "pano_files")
	_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: doc.Version, Title: "t2", Markdown: "![a](a.png)"})
	wantErrText(t, "SavePanoDoc", err, "injected query failure on pano_files")
	if after := e.blobs(t); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("storage went from %v to %v across a failed update", before, after)
	}
	e.wantEvents(t)
	var got model.PanoDoc
	if err := e.db.First(&got, doc.ID).Error; err != nil {
		t.Fatalf("read the doc back: %v", err)
	}
	if got.Title != "t" || got.Version != doc.Version {
		t.Errorf("doc after a failed update = %q v%d, want %q v%d", got.Title, got.Version, "t", doc.Version)
	}
}

func TestRecordTelemetry_PassesOnAFailedAccessCheck(t *testing.T) {
	env := newTelemetryController(t, 1)
	env.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), testWorkspaceID, testUserID).Return(false, errDBDown)
	err := env.controller.RecordTelemetry(context.Background(), telemetryRequest(entity.ActionLocalAITitleGenerate))
	wantErrIs(t, "RecordTelemetry", err, errDBDown)
}
