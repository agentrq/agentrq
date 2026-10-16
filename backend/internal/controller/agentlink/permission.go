// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// PermissionRequest is an agent asking to run a tool, which the human allows
// or denies.
type PermissionRequest struct {
	RequestID    string `json:"request_id"`
	TaskID       string `json:"task_id,omitempty"`
	ToolName     string `json:"tool_name"`
	Description  string `json:"description"`
	InputPreview string `json:"input_preview"`
}
