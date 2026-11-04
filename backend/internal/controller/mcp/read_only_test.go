// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type readOnlyTestPayload struct{}

func readOnlyTestActions(ran *string) []ToolAction {
	run := func(name string) func(context.Context, *mcp.CallToolRequest, readOnlyTestPayload) (*mcp.CallToolResult, any, error) {
		return func(context.Context, *mcp.CallToolRequest, readOnlyTestPayload) (*mcp.CallToolResult, any, error) {
			*ran = name
			return textResult("ran %s", name), nil, nil
		}
	}
	return []ToolAction{
		NewToolAction("get", "Read.", run("get")).ReadOnly(),
		NewToolAction("delete", "Write.", run("delete")),
	}
}

// A read-only connection runs only what changes nothing, and help.
func TestRunAction_ReadOnlyRefusesWhatWrites(t *testing.T) {
	var ran string
	actions := readOnlyTestActions(&ran)
	ro := WithReadOnly(context.Background())

	res, _, _ := RunAction(ro, nil, "thing", actions, "delete", nil)
	if !res.IsError || ran != "" {
		t.Fatalf("a write on a read-only connection ran (%q): %s", ran, resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "thing.delete") || !strings.Contains(text, "read-only") {
		t.Errorf("the refusal %q does not name the action and why", text)
	}

	if res, _, _ := RunAction(ro, nil, "thing", actions, "get", nil); res.IsError || ran != "get" {
		t.Errorf("a read on a read-only connection did not run: %s", resultText(t, res))
	}
	if res, _, _ := RunAction(ro, nil, "thing", actions, "help", nil); res.IsError {
		t.Errorf("help on a read-only connection was refused: %s", resultText(t, res))
	}
}

// Without the mark, every action runs, as before.
func TestRunAction_ReadWriteRunsWrites(t *testing.T) {
	var ran string
	if IsReadOnly(context.Background()) {
		t.Fatal("a plain context reads as read-only")
	}
	res, _, _ := RunAction(context.Background(), nil, "thing", readOnlyTestActions(&ran), "delete", nil)
	if res.IsError || ran != "delete" {
		t.Errorf("a write on a read-write connection did not run: %s", resultText(t, res))
	}
}

func TestPanoActions_OnlyReadReads(t *testing.T) {
	for _, a := range PanoActions(nil, "", 0) {
		if a.Reads != (a.Name == "read") {
			t.Errorf("pano %s: Reads is %v", a.Name, a.Reads)
		}
	}
}
