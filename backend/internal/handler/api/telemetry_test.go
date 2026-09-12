// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockTelemetryCrud struct {
	crud.Controller
	recordFunc func(ctx context.Context, req entity.RecordTelemetryRequest) error
	calls      []entity.RecordTelemetryRequest
}

func (m *mockTelemetryCrud) RecordTelemetry(ctx context.Context, req entity.RecordTelemetryRequest) error {
	m.calls = append(m.calls, req)
	if m.recordFunc != nil {
		return m.recordFunc(ctx, req)
	}
	return nil
}

func newTelemetryApp(ctrl *mockTelemetryCrud) *fiber.App {
	app := fiber.New()
	h := &handler{crud: ctrl}
	app.Post("/telemetry", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.recordTelemetry()(c)
	})
	return app
}

func postTelemetry(app *fiber.App, body string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, "/telemetry", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	return resp
}

var testTelemetryWorkspaceID = monoflake.ID(777).String()

func TestRecordTelemetryAcceptsAnAllowlistedAction(t *testing.T) {
	ctrl := &mockTelemetryCrud{}
	app := newTelemetryApp(ctrl)

	resp := postTelemetry(app, fmt.Sprintf(
		`{"action":"local_ai_title_generate","workspaceId":%q}`, testTelemetryWorkspaceID))

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if len(ctrl.calls) != 1 {
		t.Fatalf("expected one controller call, got %d", len(ctrl.calls))
	}
	got := ctrl.calls[0]
	if got.Action != entity.ActionLocalAITitleGenerate {
		t.Errorf("action: got %v", got.Action)
	}
	if got.WorkspaceID != 777 {
		t.Errorf("workspaceID: got %d, want 777", got.WorkspaceID)
	}
	// The identity comes from the session, never the body, so a caller cannot
	// file a report against somebody else.
	if got.UserID != "user1" {
		t.Errorf("userID: got %q, want the session's user", got.UserID)
	}
}

// Each interface-usage name has to survive the whole edge — JSON, allowlist,
// workspace parse — and arrive at the controller as its own action. A name the
// frontend sends that dies here is a metric that silently reads zero.
func TestRecordTelemetryAcceptsEachUIAction(t *testing.T) {
	for name, want := range map[string]entity.Action{
		"ui_shortcut_use":    entity.ActionUIShortcutUse,
		"ui_search":          entity.ActionUISearch,
		"ui_search_open":     entity.ActionUISearchOpen,
		"ui_copy_link":       entity.ActionUICopyLink,
		"ui_copy_markdown":   entity.ActionUICopyMarkdown,
		"ui_trajectory_view": entity.ActionUITrajectoryView,
	} {
		ctrl := &mockTelemetryCrud{}
		app := newTelemetryApp(ctrl)

		resp := postTelemetry(app, fmt.Sprintf(`{"action":%q,"workspaceId":%q}`, name, testTelemetryWorkspaceID))

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("%s: expected 204, got %d", name, resp.StatusCode)
			continue
		}
		if len(ctrl.calls) != 1 {
			t.Errorf("%s: expected one controller call, got %d", name, len(ctrl.calls))
			continue
		}
		if got := ctrl.calls[0].Action; got != want {
			t.Errorf("%s: action got %v, want %v", name, got, want)
		}
		// Still the session's user, never the body's — same rule as every
		// other action on this route.
		if got := ctrl.calls[0].UserID; got != "user1" {
			t.Errorf("%s: userID got %q, want the session's user", name, got)
		}
	}
}

// The allowlist is the security boundary for this route: an action the server
// emits for real work must never be settable by a client.
func TestRecordTelemetryRejectsActionsOutsideTheAllowlist(t *testing.T) {
	for _, action := range []string{
		"task_create", "message_create", "mcp_tool_call", "", "nonsense",
		// Near-misses for the new names, so the allowlist stays a list of
		// exact strings rather than anything prefix-shaped.
		"ui_", "ui_search_", "UI_SEARCH", "task_allow_all_commands_toggle",
	} {
		ctrl := &mockTelemetryCrud{}
		app := newTelemetryApp(ctrl)

		resp := postTelemetry(app, fmt.Sprintf(`{"action":%q,"workspaceId":%q}`, action, testTelemetryWorkspaceID))

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%q: expected 422, got %d", action, resp.StatusCode)
		}
		if len(ctrl.calls) != 0 {
			t.Errorf("%q: reached the controller; it should be rejected at the edge", action)
		}
	}
}

// A body the client fully controls must not be able to steer anything beyond
// the two fields the route reads.
func TestRecordTelemetryIgnoresUnknownFieldsAndRejectsBadWorkspaces(t *testing.T) {
	ctrl := &mockTelemetryCrud{}
	app := newTelemetryApp(ctrl)

	// Extra fields (a forged count, a backdated timestamp, another user) are
	// simply not read.
	resp := postTelemetry(app, fmt.Sprintf(
		`{"action":"local_ai_recording_end","workspaceId":%q,"count":1000,"userId":"someone_else","occurredAt":1}`,
		testTelemetryWorkspaceID))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if ctrl.calls[0].UserID != "user1" {
		t.Errorf("a userId in the body must not override the session")
	}

	for _, body := range []string{
		`{"action":"local_ai_recording_end"}`,
		`{"action":"local_ai_recording_end","workspaceId":""}`,
		`not json`,
	} {
		ctrl := &mockTelemetryCrud{}
		app := newTelemetryApp(ctrl)
		if resp := postTelemetry(app, body); resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: expected 422, got %d", body, resp.StatusCode)
		}
	}
}

func TestRecordTelemetryMapsControllerFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"forbidden workspace", crud.ErrForbidden, http.StatusForbidden},
		{"rate limited", fmt.Errorf("rate limit exceeded"), http.StatusTooManyRequests},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := &mockTelemetryCrud{recordFunc: func(context.Context, entity.RecordTelemetryRequest) error {
				return tc.err
			}}
			app := newTelemetryApp(ctrl)

			resp := postTelemetry(app, fmt.Sprintf(
				`{"action":"local_ai_title_generate","workspaceId":%q}`, testTelemetryWorkspaceID))

			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}
