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

func resourceServer(baseURL string) *WorkspaceServer {
	return NewServer(&mockEventCrud{}, baseURL, nil)
}

func readResource(t *testing.T, s *WorkspaceServer, handler func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error), uri string) string {
	t.Helper()
	res, err := handler(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: uri}})
	if err != nil {
		t.Fatalf("read resource %q: %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("read resource %q: got %d content parts, want 1", uri, len(res.Contents))
	}
	content := res.Contents[0]
	if content.URI != uri {
		t.Errorf("content URI = %q, want %q", content.URI, uri)
	}
	if content.MIMEType != "text/markdown" {
		t.Errorf("content MIMEType = %q, want %q", content.MIMEType, "text/markdown")
	}
	if content.Text == "" {
		t.Errorf("resource %q returned empty text", uri)
	}
	return content.Text
}

func TestReadNewWorkspaceGuide(t *testing.T) {
	s := resourceServer("https://agentrq.example")
	text := readResource(t, s, s.readNewWorkspaceGuide, newWorkspaceGuideURI)

	for _, want := range []string{"listWorkspaces", "createWorkspace", "getWorkspace", "**task** action `create`"} {
		if !strings.Contains(text, want) {
			t.Errorf("guide is missing a reference to %q:\n%s", want, text)
		}
	}
}

// The enrol command depends on which server answers, so the guide must not
// hand out a fixed one somebody would copy into the wrong deployment.
func TestReadAgentrqdSetupGuide_TemplatesTheBaseURL(t *testing.T) {
	s := resourceServer("https://agentrq.example")
	text := readResource(t, s, s.readAgentrqdSetupGuide, agentrqdSetupGuideURI)

	if !strings.Contains(text, "createEnrolmentCode") {
		t.Errorf("guide does not mention the createEnrolmentCode tool:\n%s", text)
	}
	if !strings.Contains(text, "agentrqd enroll --server https://agentrq.example --code <code>") {
		t.Errorf("guide does not template the server's own base URL into the enrol command:\n%s", text)
	}
	if !strings.Contains(text, daemonDocsURL) {
		t.Errorf("guide does not point at the install docs:\n%s", text)
	}
}

func TestReadAgentrqdSetupGuide_PlaceholdsWhenBaseURLUnknown(t *testing.T) {
	s := resourceServer("")
	text := readResource(t, s, s.readAgentrqdSetupGuide, agentrqdSetupGuideURI)

	if !strings.Contains(text, "agentrqd enroll --server <server> --code <code>") {
		t.Errorf("guide should fall back to a placeholder server when none is configured:\n%s", text)
	}
}

// Guards the registration wiring itself, not just the handlers: a resource
// whose URI is never actually added to the server would pass the two tests
// above and still be unreachable from a real client.
func TestRegisterResources_ListsBothGuides(t *testing.T) {
	srv := resourceServer("https://agentrq.example")

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

	listed, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}

	got := map[string]string{}
	for _, r := range listed.Resources {
		got[r.URI] = r.MIMEType
	}
	for _, uri := range []string{newWorkspaceGuideURI, agentrqdSetupGuideURI} {
		if mimeType, ok := got[uri]; !ok {
			t.Errorf("resources/list did not include %q", uri)
		} else if mimeType != "text/markdown" {
			t.Errorf("resource %q mimeType = %q, want %q", uri, mimeType, "text/markdown")
		}
	}

	read, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: newWorkspaceGuideURI})
	if err != nil {
		t.Fatalf("read resource over the wire: %v", err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text == "" {
		t.Errorf("resources/read over the wire returned no text")
	}
}
