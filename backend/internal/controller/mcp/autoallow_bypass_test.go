// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"testing"
)

// A rule of "Bash:git *" says the human allowed git, and nothing else. Each of
// these commands runs something besides git, so none may be auto-allowed.
func TestCheckAutoAllowRefusesCommandsHiddenInsideAnAllowedOne(t *testing.T) {
	ps := &WorkspaceServer{autoAllowedTools: []string{"Bash:git *"}}

	cases := []struct {
		name    string
		command string
	}{
		{"command substitution", "git log $(rm -rf ~)"},
		{"backtick substitution", "git log `rm -rf ~`"},
		{"process substitution", "git diff <(rm -rf ~) HEAD"},
		{"output process substitution", "git log >(sh)"},
		{"newline separator", "git status\nrm -rf ~"},
		{"carriage return and newline", "git status\r\nrm -rf ~"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview, _ := json.Marshal(map[string]string{"command": tc.command})
			if ps.checkAutoAllow("Bash", string(preview)) {
				t.Errorf("checkAutoAllow(Bash, %q) = true under rule \"git *\"; it runs a second command", tc.command)
			}
		})
	}
}

// Controls: plain git commands under the same rule are still allowed, so a
// failure above is the bypass and not a rule that matches nothing.
func TestCheckAutoAllowStillAllowsPlainGit(t *testing.T) {
	ps := &WorkspaceServer{autoAllowedTools: []string{"Bash:git *"}}
	for _, cmd := range []string{"git status", "git log --oneline -n 5", "git commit -m 'fix: a thing'"} {
		preview, _ := json.Marshal(map[string]string{"command": cmd})
		if !ps.checkAutoAllow("Bash", string(preview)) {
			t.Errorf("checkAutoAllow(Bash, %q) = false, want true", cmd)
		}
	}
}
