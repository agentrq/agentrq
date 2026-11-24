// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcpskill"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

const workspaceSkillURI = "skill://agentrq/SKILL.md"

// An agent connected to a workspace gets the workspace skill over the Skills
// extension, with no plugin installed, and each read is counted against the
// workspace.
func TestWorkspaceServer_ServesTheWorkspaceSkill(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var counted []MCPEvent
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if ev, ok := req.Event.(MCPEvent); ok && ev.Action == ActionMCPMethodCall {
			counted = append(counted, ev)
		}
		return nil, nil
	}).AnyTimes()
	ps := NewWorkspaceServer(
		100, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	ps.pubsub = pub
	cs := connectTestServer(t, ps.MCPServer())
	ctx := context.Background()

	caps := cs.InitializeResult().Capabilities
	if want := (map[string]any{mcpskill.Extension: map[string]any{"directoryRead": true}}); !reflect.DeepEqual(caps.Extensions, want) {
		t.Errorf("extensions = %v, want %v", caps.Extensions, want)
	}
	if caps.Experimental["claude/channel"] == nil || caps.Experimental["claude/channel/permission"] == nil {
		t.Errorf("the channel capabilities are no longer advertised: %v", caps.Experimental)
	}

	list, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var listed *mcp.Resource
	for _, r := range list.Resources {
		if r.URI == workspaceSkillURI {
			listed = r
		}
	}
	if listed == nil || listed.MIMEType != "text/markdown" || listed.Description == "" {
		t.Fatalf("resources = %v, want %s with its description", list.Resources, workspaceSkillURI)
	}

	want, err := builtinSkillFiles.ReadFile("skills/agentrq/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: workspaceSkillURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Contents) != 1 || rs.Contents[0].Text != string(want) {
		t.Errorf("read %s = %+v, want the skill's SKILL.md", workspaceSkillURI, rs.Contents)
	}

	if len(counted) != 1 || counted[0].ToolName != "resource:agentrq" || counted[0].WorkspaceID != 100 {
		t.Errorf("counted %+v, want one read of the agentrq skill in workspace 100", counted)
	}
}

// Every file of the workspace skill stays small enough to read whole, so an
// agent loads only the parts it needs.
func TestBuiltinSkills_EveryFileFitsIn2000Characters(t *testing.T) {
	if len(builtinSkills.Files()) < 2 {
		t.Fatalf("the workspace skill has %d files; it is meant to be split into a short SKILL.md and references", len(builtinSkills.Files()))
	}
	for uri, body := range builtinSkills.Files() {
		if n := utf8.RuneCount(body); n > 2000 {
			t.Errorf("%s is %d characters; keep each skill file within 2000 and split it", uri, n)
		}
	}
}

// Every reference file the SKILL.md sends an agent to is served: the ones it
// names or links, and references/<tool>.md for each tool in its table but
// help.
func TestBuiltinSkills_SKILLmdNamesOnlyServedFiles(t *testing.T) {
	body, err := builtinSkillFiles.ReadFile("skills/agentrq/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var named []string
	for _, m := range regexp.MustCompile("[`(]references/([a-zA-Z-]+\\.md)[`)]").FindAllSubmatch(body, -1) {
		named = append(named, string(m[1]))
	}
	for _, m := range regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`").FindAllSubmatch(body, -1) {
		if tool := string(m[1]); tool != "help" {
			named = append(named, tool+".md")
		}
	}
	if len(named) < 2 {
		t.Fatalf("the SKILL.md names %v; want its table's tools and its examples", named)
	}
	for _, name := range named {
		if _, ok := builtinSkills.Files()["skill://agentrq/references/"+name]; !ok {
			t.Errorf("the SKILL.md sends an agent to references/%s, which is not served", name)
		}
	}
}

// exampleCall is one call in a skill file's examples.
type exampleCall struct {
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload"`
}

// exampleCalls are the calls in a skill file's json blocks, one per line.
func exampleCalls(t *testing.T, name string, body []byte) []exampleCall {
	t.Helper()
	var calls []exampleCall
	for _, block := range regexp.MustCompile("(?s)```json\\n(.*?)```").FindAllSubmatch(body, -1) {
		for _, line := range strings.Split(strings.TrimSpace(string(block[1])), "\n") {
			var c exampleCall
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				t.Fatalf("%s: %q is not a call: %v", name, line, err)
			}
			calls = append(calls, c)
		}
	}
	return calls
}

// exampleFits says whether a call is one an action accepts: a payload it
// decodes strictly, or none for an action that takes none.
func exampleFits(a ToolAction, payload map[string]any) error {
	if a.payload == nil {
		if len(payload) != 0 {
			return errors.New("the action takes no payload")
		}
		return nil
	}
	return decodePayload(payload, reflect.New(a.payload).Interface())
}

// Every example in the workspace skill is a call the server accepts: in
// references/<tool>.md, one of that tool's actions with its real payload
// fields, every action having one.
func TestBuiltinSkills_ExamplesAreRealCalls(t *testing.T) {
	tools := map[string][]ToolAction{}
	for _, tool := range (&WorkspaceServer{}).tools() {
		tools[tool.Name] = tool.Actions
	}
	files, err := fs.Glob(builtinSkillFiles, "skills/agentrq/references/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no reference files: %v", err)
	}
	for _, file := range files {
		body, err := builtinSkillFiles.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		calls := exampleCalls(t, file, body)
		if len(calls) == 0 {
			t.Errorf("%s has no examples", file)
		}
		candidates := tools[strings.TrimSuffix(path.Base(file), ".md")]
		for _, a := range candidates {
			if !slices.ContainsFunc(calls, func(c exampleCall) bool { return c.Action == a.Name }) {
				t.Errorf("%s has no example of %s", file, a.Name)
			}
		}
		if candidates == nil {
			t.Errorf("%s is named after no tool", file)
		}
		for _, c := range calls {
			var errs []string
			for _, a := range candidates {
				if a.Name == c.Action {
					if err := exampleFits(a, c.Payload); err != nil {
						errs = append(errs, err.Error())
					} else {
						errs = nil
						break
					}
				}
			}
			if errs != nil || !slices.ContainsFunc(candidates, func(a ToolAction) bool { return a.Name == c.Action }) {
				t.Errorf("%s: %s %v is not a call the server accepts: %v", file, c.Action, c.Payload, errs)
			}
		}
	}
}

// SKILL.md links its references relatively, so the one link works from the
// plugin's files on disk and from the server's skill:// resources: resolved
// against the SKILL.md's URI, as a client resolves a relative link, each is
// a file the server serves.
func TestBuiltinSkills_LinksResolveToServedFiles(t *testing.T) {
	base, err := url.Parse(workspaceSkillURI)
	if err != nil {
		t.Fatal(err)
	}
	body := builtinSkills.Files()[workspaceSkillURI]
	links := regexp.MustCompile(`\]\(([^)]+)\)`).FindAllSubmatch(body, -1)
	if len(links) == 0 {
		t.Fatal("the SKILL.md links no files")
	}
	for _, m := range links {
		ref, err := url.Parse(string(m[1]))
		if err != nil {
			t.Fatalf("link %q: %v", m[1], err)
		}
		uri := base.ResolveReference(ref).String()
		if _, ok := builtinSkills.Files()[uri]; !ok {
			t.Errorf("the link %s resolves to %s, which the server does not serve", m[1], uri)
		}
	}
}
