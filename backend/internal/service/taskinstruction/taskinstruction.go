// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package taskinstruction builds the steps an agent is handed with a task it
// has not started: mark it ongoing, read its messages, read the workspace's
// mission and load its memory, do it, then mark it completed. Agents left to
// infer them started late and missed messages, so every hand-off spells out
// each call, with the task's real ID.
package taskinstruction

import "fmt"

// Start returns the steps, as a line block to put after a not started task's
// ID and before its body, or "" when taskID is empty.
func Start(taskID string) string {
	if taskID == "" {
		return ""
	}
	return "\nBefore anything else:" +
		fmt.Sprintf("\n1) Mark it ongoing: task {\"action\": \"updateStatus\", \"payload\": {\"taskId\": %q, \"status\": \"ongoing\"}}", taskID) +
		fmt.Sprintf("\n2) Read its details and messages: task {\"action\": \"get\", \"payload\": {\"taskId\": %q, \"includeConversation\": true}}", taskID) +
		"\n3) Read the workspace's mission: workspace {\"action\": \"get\"}" +
		"\n4) Load the workspace's memory: memory {\"action\": \"load\"}" +
		fmt.Sprintf("\n5) Work the task described below; when it is done, reply a summary, then mark it completed: task {\"action\": \"updateStatus\", \"payload\": {\"taskId\": %q, \"status\": \"completed\"}}", taskID)
}
