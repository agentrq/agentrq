// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcpskill"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

const supervisorSkillURI = "skill://agentrq/SKILL.md"

// skillsRPC sends one 2026-07-28 request, with the _meta that revision
// requires plus fields (a JSON object's members), and returns its result
// or its error.
func skillsRPC(t *testing.T, method, fields string) (map[string]any, map[string]any) {
	t.Helper()
	return skillsRPCTo(t, newStatelessTestServer(t).URL, method, "", fields)
}

// skillsRPCTo is skillsRPC to the server at url, with the Mcp-Name header
// set to name unless it is empty.
func skillsRPCTo(t *testing.T, url, method, name, fields string) (map[string]any, map[string]any) {
	t.Helper()
	headers := map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": method}
	if name != "" {
		headers["Mcp-Name"] = name
	}
	params := `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}`
	if fields != "" {
		params += "," + fields
	}
	status, out, _ := corePost(t, url, headers,
		`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}}`)
	var env struct {
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("%s: decode %s: %v", method, out, err)
	}
	// 2026-07-28 answers an error with 400, and only an error.
	if (status == http.StatusOK) != (env.Error == nil) {
		t.Fatalf("%s: status %d with %s", method, status, out)
	}
	return env.Result, env.Error
}

func skillDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func supervisorSkillBody(t *testing.T) []byte {
	t.Helper()
	body, err := builtinSkillFiles.ReadFile("skills/agentrq/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// checkCacheable checks a result lets any client cache it for an hour.
func checkCacheable(t *testing.T, method string, rs map[string]any) {
	t.Helper()
	if rs["resultType"] != "complete" || rs["ttlMs"] != float64(3600000) || rs["cacheScope"] != "public" {
		t.Errorf("%s: resultType %v, ttlMs %v, cacheScope %v; want complete, 3600000, public", method, rs["resultType"], rs["ttlMs"], rs["cacheScope"])
	}
}

// A client sees the Skills extension in the capabilities, beside everything
// the server advertised before it.
func TestSkills_ExtensionIsAdvertised(t *testing.T) {
	status, out, _ := corePost(t, newStatelessTestServer(t).URL, map[string]string{"MCP-Protocol-Version": "2025-06-18"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`)
	if status != http.StatusOK {
		t.Fatalf("initialize: %d %s", status, out)
	}
	var env struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	caps := env.Result.Capabilities
	want := map[string]any{"io.modelcontextprotocol/skills": map[string]any{"directoryRead": true}}
	if !reflect.DeepEqual(caps["extensions"], want) {
		t.Errorf("extensions = %v, want %v", caps["extensions"], want)
	}
	for _, name := range []string{"logging", "tools", "prompts", "resources"} {
		if caps[name] == nil {
			t.Errorf("the %s capability is no longer advertised: %v", name, caps)
		}
	}
	if r, _ := caps["resources"].(map[string]any); r["subscribe"] != true {
		t.Errorf("resources no longer advertise subscribe: %v", caps["resources"])
	}
}

// An agent that never asks for skills/list still learns the skill is there.
func TestSkills_InstructionsNameTheSkill(t *testing.T) {
	if !strings.Contains(serverInstructions, supervisorSkillURI) {
		t.Errorf("the instructions do not name %s", supervisorSkillURI)
	}
}

func TestSkills_List(t *testing.T) {
	body := supervisorSkillBody(t)
	rs, rpcErr := skillsRPC(t, mcpskill.MethodList, "")
	if rpcErr != nil {
		t.Fatalf("skills/list: %v", rpcErr)
	}
	checkCacheable(t, mcpskill.MethodList, rs)
	skills, _ := rs["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want the agentrq skill alone", rs["skills"])
	}
	skill := skills[0].(map[string]any)
	if skill["uri"] != supervisorSkillURI {
		t.Errorf("uri = %v, want %s", skill["uri"], supervisorSkillURI)
	}
	if fm := skill["frontmatter"].(map[string]any); fm["name"] != "agentrq" || fm["description"] == "" {
		t.Errorf("frontmatter = %v, want the agentrq skill's name and description", fm)
	}
	resources, _ := skill["resources"].([]any)
	if len(resources) != len(builtinSkills.Files()) {
		t.Fatalf("the manifest lists %d files, want all %d of the skill's", len(resources), len(builtinSkills.Files()))
	}
	if first := resources[0].(map[string]any); first["uri"] != supervisorSkillURI || first["digest"] != skillDigest(body) || first["size"] != float64(len(body)) {
		t.Errorf("the manifest starts with %v, want the SKILL.md with its digest and size", first)
	}
	// The manifest must match the bytes served, file by file.
	for _, r := range resources {
		r := r.(map[string]any)
		uri, _ := r["uri"].(string)
		served, err := builtinSkillFiles.ReadFile("skills/" + strings.TrimPrefix(uri, "skill://"))
		if err != nil {
			t.Errorf("the manifest lists %s, which the skill does not have", uri)
			continue
		}
		if r["digest"] != skillDigest(served) || r["size"] != float64(len(served)) {
			t.Errorf("%s is listed as %v, %v; want %s, %d", uri, r["digest"], r["size"], skillDigest(served), len(served))
		}
	}
}

// Every file the server serves for a skill stays small enough to read whole,
// so a client loads only the parts it needs.
func TestSkills_EveryFileFitsIn2000Characters(t *testing.T) {
	if len(builtinSkills.Files()) < 2 {
		t.Fatalf("the agentrq skill has %d files; it is meant to be split into a short SKILL.md and references", len(builtinSkills.Files()))
	}
	for uri, body := range builtinSkills.Files() {
		if n := utf8.RuneCount(body); n > 2000 {
			t.Errorf("%s is %d characters; keep each skill file within 2000 and split it", uri, n)
		}
	}
}

// Every reference file the SKILL.md sends an agent to is served: the ones it
// names, and references/<tool>.md for each tool in its table that has no
// shared file.
func TestSkills_SKILLmdNamesOnlyServedFiles(t *testing.T) {
	body := string(supervisorSkillBody(t))
	var named []string
	for _, m := range regexp.MustCompile("`([a-z-]+\\.md)`").FindAllStringSubmatch(body, -1) {
		named = append(named, m[1])
	}
	for _, m := range regexp.MustCompile("(?m)^\\| `([a-zA-Z]+)`").FindAllStringSubmatch(body, -1) {
		if tool := m[1]; !slices.Contains([]string{"help", "memory", "skills", "pano"}, tool) {
			named = append(named, tool+".md")
		}
	}
	if len(named) == 0 {
		t.Fatal("the SKILL.md names no reference files")
	}
	for _, name := range named {
		if _, ok := builtinSkills.Files()["skill://agentrq/references/"+name]; !ok {
			t.Errorf("the SKILL.md sends an agent to references/%s, which is not served", name)
		}
	}
}

func TestSkills_Get(t *testing.T) {
	rs, rpcErr := skillsRPC(t, mcpskill.MethodGet, `"uri":"`+supervisorSkillURI+`"`)
	if rpcErr != nil {
		t.Fatalf("skills/get: %v", rpcErr)
	}
	checkCacheable(t, mcpskill.MethodGet, rs)
	if got := rs["skill"].(map[string]any)["uri"]; got != supervisorSkillURI {
		t.Errorf("skill uri = %v, want %s", got, supervisorSkillURI)
	}

	_, rpcErr = skillsRPC(t, mcpskill.MethodGet, `"uri":"skill://nope/SKILL.md"`)
	if rpcErr == nil || rpcErr["code"] != float64(-32602) {
		t.Errorf("an unknown skill answered %v, want invalid params", rpcErr)
	}
}

func TestSkills_DirectoryRead(t *testing.T) {
	body := supervisorSkillBody(t)
	rs, rpcErr := skillsRPC(t, mcpskill.MethodDirectoryRead, `"uri":"skill://agentrq"`)
	if rpcErr != nil {
		t.Fatalf("resources/directory/read: %v", rpcErr)
	}
	checkCacheable(t, mcpskill.MethodDirectoryRead, rs)
	want := []any{
		map[string]any{"uri": supervisorSkillURI, "name": "agentrq/SKILL.md", "description": builtinSkills.Entries()[0].Frontmatter["description"], "mimeType": "text/markdown", "size": float64(len(body))},
		map[string]any{"uri": "skill://agentrq/references", "name": "agentrq/references", "description": "The references directory of the agentrq skill.", "mimeType": "inode/directory"},
	}
	if !reflect.DeepEqual(rs["resources"], want) {
		t.Errorf("resources = %v, want %v", rs["resources"], want)
	}
	if _, ok := rs["nextCursor"]; ok {
		t.Error("every child fits on one page, so there is no cursor")
	}

	rs, rpcErr = skillsRPC(t, mcpskill.MethodDirectoryRead, `"uri":"skill://agentrq/references"`)
	if rpcErr != nil {
		t.Fatalf("resources/directory/read of the references: %v", rpcErr)
	}
	references, _ := rs["resources"].([]any)
	if len(references) != len(builtinSkills.Files())-1 {
		t.Errorf("the references directory lists %d files, want every file but the SKILL.md (%d)", len(references), len(builtinSkills.Files())-1)
	}
	for _, r := range references {
		if r := r.(map[string]any); r["mimeType"] != "text/markdown" || !strings.HasPrefix(r["uri"].(string), "skill://agentrq/references/") {
			t.Errorf("the references directory lists %v, want markdown files inside it", r)
		}
	}

	for _, uri := range []string{supervisorSkillURI, "skill://nope", "skill://agentrq/", "skill:"} {
		_, rpcErr := skillsRPC(t, mcpskill.MethodDirectoryRead, `"uri":"`+uri+`"`)
		if rpcErr == nil || rpcErr["code"] != float64(-32602) {
			t.Errorf("%s is not a directory, but answered %v instead of invalid params", uri, rpcErr)
		}
	}
}

// The SKILL.md is a resource, so a client on an older revision, which knows
// nothing of skills/list, still lists and reads it.
func TestSkills_SKILLmdIsAResource(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(&mockEventCrud{}, "https://agentrq.example", nil).server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	list, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	var listed *mcp.Resource
	for _, r := range list.Resources {
		if r.URI == supervisorSkillURI {
			listed = r
		}
	}
	if listed == nil {
		t.Fatalf("%s is not among the resources %v", supervisorSkillURI, list.Resources)
	}
	if listed.Name != "agentrq/SKILL.md" || listed.MIMEType != "text/markdown" || listed.Description == "" ||
		listed.Description != builtinSkills.Entries()[0].Frontmatter["description"] {
		t.Errorf("resource = %+v, want agentrq/SKILL.md, text/markdown and the skill's description", listed)
	}

	rs, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: supervisorSkillURI})
	if err != nil {
		t.Fatalf("read %s: %v", supervisorSkillURI, err)
	}
	if len(rs.Contents) != 1 || rs.Contents[0].Text != string(supervisorSkillBody(t)) || rs.Contents[0].MIMEType != "text/markdown" {
		t.Errorf("read %s returned %+v, want the SKILL.md as markdown", supervisorSkillURI, rs.Contents)
	}
	if rs.TTLMs != 3600000 || rs.CacheScope != "public" {
		t.Errorf("ttlMs %d, cacheScope %q; want 3600000, public", rs.TTLMs, rs.CacheScope)
	}
}

// Each skills call is counted, under the method a client sent.
func TestSkills_EmitTelemetry(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	s := NewServer(&mockEventCrud{}, "", &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		e := req.Event.(mcpevent.MCPEvent)
		if e.Action == mcpevent.ActionMCPMethodCall && e.WorkspaceID == 0 {
			mu.Lock()
			got = append(got, e.Method)
			mu.Unlock()
		}
	}})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	const reference = "skill://agentrq/references/task.md"
	for _, call := range []struct{ method, name, fields string }{
		{mcpskill.MethodList, "", ""},
		{mcpskill.MethodGet, "", `"uri":"` + supervisorSkillURI + `"`},
		{mcpskill.MethodDirectoryRead, "", `"uri":"skill://agentrq"`},
		{"resources/read", supervisorSkillURI, `"uri":"` + supervisorSkillURI + `"`},
		{"resources/read", reference, `"uri":"` + reference + `"`},
	} {
		if _, rpcErr := skillsRPCTo(t, srv.URL, call.method, call.name, call.fields); rpcErr != nil {
			t.Fatalf("%s: %v", call.method, rpcErr)
		}
	}
	// Every file of a skill is counted as the skill.
	want := []string{"skills/list", "skills/get", "resources/directory/read", "resource:agentrq", "resource:agentrq"}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("telemetry = %v, want %v", got, want)
	}
}
