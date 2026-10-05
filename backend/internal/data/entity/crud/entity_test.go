// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"
)

// Every case in Action.String() exists to give a raw stored value a name in
// a log line; a case silently missing an entry falls through to "unknown"
// and reads as if nothing happened. This walks every named Action, plus one
// value nothing claims, so a mismatch there is caught rather than only
// showing up later in a log nobody was watching for it.
func TestActionString(t *testing.T) {
	cases := []struct {
		action Action
		want   string
	}{
		{ActionUserCreate, "user_create"},
		{ActionUserUpdate, "user_update"},
		{ActionUserDelete, "user_delete"},
		{ActionWorkspaceCreate, "workspace_create"},
		{ActionWorkspaceUpdate, "workspace_update"},
		{ActionWorkspaceDelete, "workspace_delete"},
		{ActionTaskCreate, "task_create"},
		{ActionTaskUpdate, "task_update"},
		{ActionTaskDelete, "task_delete"},
		{ActionMessageCreate, "message_create"},
		{ActionMessageUpdate, "message_update"},
		{ActionMessageDelete, "message_delete"},
		{ActionTaskComplete, "task_complete"},
		{ActionTaskApproveManual, "task_approve_manual"},
		{ActionTaskFromScheduled, "task_from_scheduled"},
		{ActionMCPToolCall, "mcp_tool_call"},
		{ActionMCPPermissionManual, "mcp_permission_manual"},
		{ActionMCPPermissionAuto, "mcp_permission_auto"},
		{ActionTaskAllowAllCommandsToggle, "task_allow_all_commands_toggle"},
		{ActionAgentModelSelect, "agent_model_select"},
		{ActionEventPublished, "event_published"},
		{ActionLocalAITitleGenerate, "local_ai_title_generate"},
		{ActionLocalAIRecordingEnd, "local_ai_recording_end"},
		{ActionUIShortcutUse, "ui_shortcut_use"},
		{ActionUISearch, "ui_search"},
		{ActionUISearchOpen, "ui_search_open"},
		{ActionUICopyLink, "ui_copy_link"},
		{ActionUICopyMarkdown, "ui_copy_markdown"},
		{ActionUITrajectoryView, "ui_trajectory_view"},
		{ActionUICopyCode, "ui_copy_code"},
		{ActionUIDictationEnd, "ui_dictation_end"},
		{ActionMachineAdd, "machine_add"},
		{ActionMachineRemove, "machine_remove"},
		{ActionMachineDisable, "machine_disable"},
		{ActionMachineSessionCreate, "machine_session_create"},
		{ActionMachineSessionOpen, "machine_session_open"},
		{ActionMachineSessionClose, "machine_session_close"},
		{ActionMachineTerminalOpen, "machine_terminal_open"},
		{ActionMachineTerminalClose, "machine_terminal_close"},
		{ActionMachineEnable, "machine_enable"},
		{ActionMachineSessionKill, "machine_session_kill"},
		{ActionMachineEnrolCodeCreate, "machine_enrol_code_create"},
		{ActionAgentConcurrencySelect, "agent_concurrency_select"},
		{ActionAgentLaunchClaudeCode, "agent_launch_claude_code"},
		{ActionAgentLaunchACPGateway, "agent_launch_acp_gateway"},
		{ActionSkillImport, "skill_import"},
		{ActionSkillView, "skill_view"},
		{ActionSkillSearch, "skill_search"},
		{ActionTaskFork, "task_fork"},
		{ActionSiteShare, "site_share"},
		{ActionSiteUnshare, "site_unshare"},
		{ActionWorkspaceForkCreate, "workspace_fork_create"},
		{ActionWorkspaceForkMerge, "workspace_fork_merge"},
		{ActionUISpinUp, "ui_spin_up"},
		{ActionMachineRestart, "machine_restart"},
		{ActionMachineUpdate, "machine_update"},
		{ActionUISidePanelOpen, "ui_side_panel_open"},
		{ActionUISidePanelLink, "ui_side_panel_link"},
		{ActionOAuthConsentAllow, "oauth_consent_allow"},
		{ActionOAuthConsentDeny, "oauth_consent_deny"},
		{ActionTaskTitleUpdate, "task_title_update"},
		{ActionSkillEnable, "skill_enable"},
		{ActionSkillDisable, "skill_disable"},
		// Not a case in the switch today, and the fallback both it and any
		// truly unknown value share.
		{ActionTaskRejectManual, "unknown"},
		{Action(9999), "unknown"},
	}

	seen := map[Action]bool{}
	for _, tc := range cases {
		if got := tc.action.String(); got != tc.want {
			t.Errorf("Action(%d).String() = %q, want %q", tc.action, got, tc.want)
		}
		if tc.want != "unknown" {
			if seen[tc.action] {
				t.Errorf("Action(%d) listed twice", tc.action)
			}
			seen[tc.action] = true
		}
	}
}

func TestTaskAgentContext(t *testing.T) {
	//nolint:staticcheck // a nil context is what GetTaskAgent guards against
	if got := GetTaskAgent(nil); got != (TaskAgent{}) {
		t.Errorf("GetTaskAgent(nil) = %+v, want zero", got)
	}
	if got := GetTaskAgent(context.Background()); got != (TaskAgent{}) {
		t.Errorf("GetTaskAgent(empty) = %+v, want zero", got)
	}
	want := TaskAgent{Name: "claude-code", Model: "Opus"}
	if got := GetTaskAgent(WithTaskAgent(context.Background(), want)); got != want {
		t.Errorf("GetTaskAgent = %+v, want %+v", got, want)
	}
}
