// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"
)

func TestResourceTypeString(t *testing.T) {
	for r, want := range map[ResourceType]string{
		ResourceUser:      "user",
		ResourceWorkspace: "workspace",
		ResourceTask:      "task",
		ResourceMessage:   "message",
		ResourceMachine:   "machine",
		ResourceSession:   "session",
		ResourceSkill:     "skill",
		ResourcePano:      "pano",
		0:                 "unknown",
		ResourcePano + 1:  "unknown",
	} {
		if got := r.String(); got != want {
			t.Errorf("ResourceType(%d) = %q, want %q", r, got, want)
		}
	}
}

func TestOrigin(t *testing.T) {
	for o, want := range map[Origin]string{
		OriginInvalid:   "invalid",
		OriginAPI:       "api",
		OriginMCP:       "mcp",
		OriginScheduler: "scheduler",
		OriginSlack:     "slack",
		OriginSlack + 1: "unknown",
	} {
		if got := o.String(); got != want {
			t.Errorf("Origin(%d) = %q, want %q", o, got, want)
		}
	}
	// The number is what an event stores.
	if OriginAPI.Int64() != 1 || OriginSlack.Int64() != 4 {
		t.Errorf("api = %d, slack = %d", OriginAPI.Int64(), OriginSlack.Int64())
	}
}

func TestOriginContext(t *testing.T) {
	if got := GetOrigin(WithOrigin(context.Background(), OriginMCP)); got != OriginMCP {
		t.Errorf("got %v, want mcp", got)
	}
	var none context.Context
	if got := GetOrigin(none); got != OriginInvalid {
		t.Errorf("a nil context = %v, want invalid", got)
	}
	if got := GetOrigin(context.Background()); got != OriginInvalid {
		t.Errorf("no origin = %v, want invalid", got)
	}
	if got := GetOrigin(context.WithValue(context.Background(), OriginContextKey, 2)); got != OriginInvalid {
		t.Errorf("an origin of the wrong type = %v, want invalid", got)
	}
}

// Every action a browser may report resolves to the action that is logged
// under the same name, and nothing else resolves at all.
func TestClientReportableAction(t *testing.T) {
	allowed := []Action{
		ActionLocalAITitleGenerate, ActionLocalAIRecordingEnd, ActionUIShortcutUse, ActionUISearch, ActionUISearchOpen,
		ActionUICopyLink, ActionUICopyMarkdown, ActionUITrajectoryView, ActionUICopyCode, ActionUIDictationEnd,
		ActionUISpinUp, ActionUISidePanelOpen, ActionUISidePanelLink, ActionUIMessageReact, ActionUIAcpAgentPick,
		ActionUIPlanComment, ActionUITaskLinkOpen, ActionUIOverviewTaskOpen,
	}
	for _, want := range allowed {
		name := want.String()
		if got, ok := ClientReportableAction(name); !ok || got != want {
			t.Errorf("%q = %v, %v; want %v", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "unknown", ActionTaskCreate.String(), "UI_SEARCH"} {
		if got, ok := ClientReportableAction(name); ok || got != 0 {
			t.Errorf("%q = %v, %v; want it refused", name, got, ok)
		}
	}
}

func TestRefusalErrors(t *testing.T) {
	launch := NewLaunchError(ErrLaunchBusy, "the machine is busy")
	if launch.Error() != "the machine is busy" || !errors.Is(launch, ErrLaunchBusy) {
		t.Errorf("launch error = %q, is busy %v", launch, errors.Is(launch, ErrLaunchBusy))
	}
	fork := NewForkError(ErrHasForks, "merge its forks first")
	if fork.Error() != "merge its forks first" || !errors.Is(fork, ErrHasForks) {
		t.Errorf("fork error = %q, has forks %v", fork, errors.Is(fork, ErrHasForks))
	}
	if got := (&PanoError{Message: "the doc changed", Conflict: true}).Error(); got != "the doc changed" {
		t.Errorf("pano error = %q", got)
	}
}
