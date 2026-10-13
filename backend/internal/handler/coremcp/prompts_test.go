// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func promptServer() *WorkspaceServer {
	return NewServer(&mockEventCrud{}, "https://agentrq.example", nil)
}

func promptText(t *testing.T, res *mcp.GetPromptResult) string {
	t.Helper()
	if len(res.Messages) != 1 {
		t.Fatalf("got %d prompt messages, want 1", len(res.Messages))
	}
	text, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("message content is %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	return text.Text
}

func TestPromptNewWorkspace_RequiresName(t *testing.T) {
	s := promptServer()
	_, err := s.promptNewWorkspace(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{
		Name:      "new-workspace",
		Arguments: map[string]string{},
	}})
	if err == nil {
		t.Fatal("expected an error when name is missing")
	}
}

func TestPromptNewWorkspace_IncludesNamePurposeAndGuide(t *testing.T) {
	s := promptServer()
	res, err := s.promptNewWorkspace(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{
		Name:      "new-workspace",
		Arguments: map[string]string{"name": "billing-agent", "purpose": "handle invoicing"},
	}})
	if err != nil {
		t.Fatalf("promptNewWorkspace: %v", err)
	}
	text := promptText(t, res)

	for _, want := range []string{"billing-agent", "handle invoicing", newWorkspaceGuideURI, "workspace action create", "workspace action list"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt text is missing %q:\n%s", want, text)
		}
	}
}

func TestPromptSetupAgentrqd_IncludesGuideAndTool(t *testing.T) {
	s := promptServer()
	res, err := s.promptSetupAgentrqd(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{
		Name:      "setup-agentrqd",
		Arguments: map[string]string{"platform": "linux"},
	}})
	if err != nil {
		t.Fatalf("promptSetupAgentrqd: %v", err)
	}
	text := promptText(t, res)

	for _, want := range []string{"linux", agentrqdSetupGuideURI, "machines action createEnrolmentCode", "no remote enrolment"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt text is missing %q:\n%s", want, text)
		}
	}
}

func TestPromptWorkspaceStatus_CoversEveryWorkspace(t *testing.T) {
	s := promptServer()
	res, err := s.promptWorkspaceStatus(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{Name: "workspace-status"}})
	if err != nil {
		t.Fatalf("promptWorkspaceStatus: %v", err)
	}
	text := promptText(t, res)

	for _, want := range []string{"workspace action list", "workspace action stats", "task action list"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt text is missing %q:\n%s", want, text)
		}
	}
}

// Guards the registration wiring: a prompt built correctly but never added to
// the server would pass every test above and still be unreachable.
func TestRegisterPrompts_ListsAllThree(t *testing.T) {
	srv := promptServer()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}

	names := map[string]bool{}
	for _, p := range listed.Prompts {
		names[p.Name] = true
	}
	for _, name := range []string{"new-workspace", "setup-agentrqd", "workspace-status"} {
		if !names[name] {
			t.Errorf("prompts/list did not include %q", name)
		}
	}

	got, err := clientSession.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "new-workspace",
		Arguments: map[string]string{"name": "billing-agent"},
	})
	if err != nil {
		t.Fatalf("get prompt over the wire: %v", err)
	}
	if len(got.Messages) == 0 {
		t.Errorf("prompts/get over the wire returned no messages")
	}
}
