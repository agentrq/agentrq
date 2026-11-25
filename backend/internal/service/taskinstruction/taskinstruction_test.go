// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package taskinstruction

import "testing"

func TestStartSpellsOutEveryCallWithTheTaskID(t *testing.T) {
	want := "\nBefore anything else:" +
		"\n1) Mark it ongoing: task {\"action\": \"updateStatus\", \"payload\": {\"taskId\": \"0kGogHmaqvZ\", \"status\": \"ongoing\"}}" +
		"\n2) Read its details and messages: task {\"action\": \"get\", \"payload\": {\"taskId\": \"0kGogHmaqvZ\", \"includeConversation\": true}}" +
		"\n   For each attachment listed, either:" +
		"\n   a) get its link: task {\"action\": \"getAttachment\", \"payload\": {\"taskId\": \"0kGogHmaqvZ\", \"attachmentId\": \"<attachmentId>\", \"format\": \"url\"}}" +
		"\n   b) or, where npx is available, download it: npx -y @agentrq/agentrq-ws@latest attachment get <attachmentId> --task 0kGogHmaqvZ --out <dir> (prints its path; better for a large file, since a) answers base64 when the file has no link)" +
		"\n3) Read the workspace's mission: workspace {\"action\": \"get\"}" +
		"\n4) Load the workspace's memory: memory {\"action\": \"load\"}" +
		"\n5) Work the task described below; when it is done, reply a summary, then mark it completed: task {\"action\": \"updateStatus\", \"payload\": {\"taskId\": \"0kGogHmaqvZ\", \"status\": \"completed\"}}"
	if got := Start("0kGogHmaqvZ"); got != want {
		t.Fatalf("Start:\n got %q\nwant %q", got, want)
	}
}

func TestStartWithNoTaskIDIsEmpty(t *testing.T) {
	if got := Start(""); got != "" {
		t.Fatalf("Start(\"\") = %q, want empty", got)
	}
}
