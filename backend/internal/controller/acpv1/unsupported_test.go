// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"strings"
	"testing"
)

func TestUnsupportedClientMethods(t *testing.T) {
	methods := []string{
		"fs/read_text_file", "fs/write_text_file",
		"terminal/create", "terminal/output", "terminal/wait_for_exit", "terminal/kill", "terminal/release",
		"mcp/connect", "_zed/whatever",
	}
	rec, a, l := midTurn(t, agentOpts{})
	for i, method := range methods {
		t.Run(method+" is refused as not found", func(t *testing.T) {
			a.request(100+i, method, `{"sessionId":"sess-1"}`)
			got := a.answerTo(100 + i)
			if got.Error == nil || got.Error.Code != codeMethodNotFound || !strings.Contains(got.Error.Message, method) {
				t.Fatalf("the agent was answered %+v, want -32601 naming %s", got, method)
			}
		})
	}

	t.Run("an unknown notification changes nothing", func(t *testing.T) {
		var before int
		rec.read(func() { before = len(rec.log) + len(rec.permissions) + len(rec.questions) })
		a.write(`{"jsonrpc":"2.0","method":"_zed/notice","params":{"sessionId":"sess-1"}}`)
		a.sync(l)
		rec.read(func() {
			if after := len(rec.log) + len(rec.permissions) + len(rec.questions); after != before {
				t.Fatalf("the recorder grew from %d to %d entries, want no change", before, after)
			}
		})
	})
}
