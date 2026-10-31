// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

const supervisorSkillURI = "skill://agentrq/SKILL.md"

// skillsRPC sends one 2026-07-28 request, with the _meta that revision
// requires plus fields (a JSON object's members), and returns its result
// or its error.
func skillsRPC(t *testing.T, method, fields string) (map[string]any, map[string]any) {
	t.Helper()
	params := `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}`
	if fields != "" {
		params += "," + fields
	}
	status, out, _ := corePost(t, newStatelessTestServer(t).URL, map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": method},
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
	rs, rpcErr := skillsRPC(t, MethodSkillsList, "")
	if rpcErr != nil {
		t.Fatalf("skills/list: %v", rpcErr)
	}
	checkCacheable(t, MethodSkillsList, rs)
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
	want := []any{map[string]any{"uri": supervisorSkillURI, "digest": skillDigest(body), "size": float64(len(body))}}
	if !reflect.DeepEqual(skill["resources"], want) {
		t.Errorf("resources = %v, want %v: the manifest must match the bytes served", skill["resources"], want)
	}
}

func TestSkills_Get(t *testing.T) {
	rs, rpcErr := skillsRPC(t, MethodSkillsGet, `"uri":"`+supervisorSkillURI+`"`)
	if rpcErr != nil {
		t.Fatalf("skills/get: %v", rpcErr)
	}
	checkCacheable(t, MethodSkillsGet, rs)
	if got := rs["skill"].(map[string]any)["uri"]; got != supervisorSkillURI {
		t.Errorf("skill uri = %v, want %s", got, supervisorSkillURI)
	}

	_, rpcErr = skillsRPC(t, MethodSkillsGet, `"uri":"skill://nope/SKILL.md"`)
	if rpcErr == nil || rpcErr["code"] != float64(-32602) {
		t.Errorf("an unknown skill answered %v, want invalid params", rpcErr)
	}
}

func TestSkills_DirectoryRead(t *testing.T) {
	body := supervisorSkillBody(t)
	rs, rpcErr := skillsRPC(t, MethodDirectoryRead, `"uri":"skill://agentrq"`)
	if rpcErr != nil {
		t.Fatalf("resources/directory/read: %v", rpcErr)
	}
	checkCacheable(t, MethodDirectoryRead, rs)
	want := []any{map[string]any{"uri": supervisorSkillURI, "name": "agentrq/SKILL.md", "description": builtinSkills.entries[0].Frontmatter["description"], "mimeType": "text/markdown", "size": float64(len(body))}}
	if !reflect.DeepEqual(rs["resources"], want) {
		t.Errorf("resources = %v, want %v", rs["resources"], want)
	}
	if _, ok := rs["nextCursor"]; ok {
		t.Error("every child fits on one page, so there is no cursor")
	}

	for _, uri := range []string{supervisorSkillURI, "skill://nope", "skill://agentrq/", "skill:"} {
		_, rpcErr := skillsRPC(t, MethodDirectoryRead, `"uri":"`+uri+`"`)
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
		listed.Description != builtinSkills.entries[0].Frontmatter["description"] {
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
	var got []string
	s := NewServer(&mockEventCrud{}, "", &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		e := req.Event.(mcpevent.MCPEvent)
		if e.Action == mcpevent.ActionMCPMethodCall && e.WorkspaceID == 0 {
			got = append(got, e.Method)
		}
	}})
	ctx := authedContext()
	if _, err := s.listSkills(ctx, nil, &SkillsListParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.getBuiltinSkill(ctx, nil, &SkillsGetParams{URI: supervisorSkillURI}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readSkillDirectory(ctx, nil, &DirectoryReadParams{URI: "skill://agentrq"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readSkillFile(ctx, &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: supervisorSkillURI}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"skills/list", "skills/get", "resources/directory/read", "resource:skill"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("telemetry = %v, want %v", got, want)
	}
}

func TestLoadSkills_SupportingFiles(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0xff}
	set, err := loadSkills(fstest.MapFS{
		"s/a/SKILL.md":              {Data: []byte("---\nname: a\ndescription: d\nlicense: MIT\n---\n# A\n")},
		"s/a/references/guide.md":   {Data: []byte("guide")},
		"s/a/assets/logo.png":       {Data: png},
		"s/a/assets/data.zzunknown": {Data: []byte("x")},
	}, "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.entries) != 1 {
		t.Fatalf("entries = %v, want one", set.entries)
	}
	entry := set.entries[0]
	if want := (map[string]any{"name": "a", "description": "d", "license": "MIT"}); !reflect.DeepEqual(entry.Frontmatter, want) {
		t.Errorf("frontmatter = %v, want every field unchanged: %v", entry.Frontmatter, want)
	}
	var uris []string
	for _, r := range entry.Resources {
		uris = append(uris, r.URI)
	}
	if want := []string{"skill://a/SKILL.md", "skill://a/assets/data.zzunknown", "skill://a/assets/logo.png", "skill://a/references/guide.md"}; !reflect.DeepEqual(uris, want) {
		t.Errorf("resources = %v, want %v (SKILL.md first)", uris, want)
	}

	dirs := map[string][]string{}
	descriptions := map[string]string{}
	for uri, children := range set.dirs {
		for _, c := range children {
			dirs[uri] = append(dirs[uri], c.URI+" "+c.MIMEType)
			descriptions[c.URI] = c.Description
		}
	}
	if want := (map[string][]string{
		"skill://a":            {"skill://a/SKILL.md text/markdown", "skill://a/assets inode/directory", "skill://a/references inode/directory"},
		"skill://a/assets":     {"skill://a/assets/data.zzunknown application/octet-stream", "skill://a/assets/logo.png image/png"},
		"skill://a/references": {"skill://a/references/guide.md text/markdown"},
	}); !reflect.DeepEqual(dirs, want) {
		t.Errorf("directories = %v, want every one, the root included, listing its direct children in URI order: %v", dirs, want)
	}
	if got := set.dirs["skill://a"][1].Name; got != "a/assets" {
		t.Errorf("directory name = %q, want a/assets", got)
	}
	if want := (map[string]string{
		"skill://a/SKILL.md":              "d",
		"skill://a/assets":                "The assets directory of the a skill.",
		"skill://a/references":            "The references directory of the a skill.",
		"skill://a/assets/data.zzunknown": "The assets/data.zzunknown file of the a skill.",
		"skill://a/assets/logo.png":       "The assets/logo.png file of the a skill.",
		"skill://a/references/guide.md":   "The references/guide.md file of the a skill.",
	}); !reflect.DeepEqual(descriptions, want) {
		t.Errorf("descriptions = %v, want %v", descriptions, want)
	}
	if got := entry.describe("skill://a", "directory"); got != "d" {
		t.Errorf("the skill root's description = %q, want the skill's", got)
	}

	s := &WorkspaceServer{skills: set}
	rs, err := s.readSkillFile(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "skill://a/assets/logo.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rs.Contents[0].Blob, png) || rs.Contents[0].Text != "" || rs.Contents[0].MIMEType != "image/png" {
		t.Errorf("a binary file read as %+v, want a PNG blob", rs.Contents[0])
	}
}

// failFS fails to open one path. It has only Open, so every read goes
// through it.
type failFS struct {
	fsys fs.FS
	path string
}

func (f failFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, errors.New("boom")
	}
	return f.fsys.Open(name)
}

func TestLoadSkills_Errors(t *testing.T) {
	skill := func(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }
	valid := skill("---\nname: a\ndescription: d\n---\n")
	for name, tc := range map[string]struct {
		fsys fs.FS
		want string
	}{
		"no root":           {fstest.MapFS{}, "open s"},
		"file at the root":  {fstest.MapFS{"s/x.md": skill("x")}, "x.md: not a skill directory"},
		"no SKILL.md":       {fstest.MapFS{"s/a/other.md": skill("x")}, "skill a: open s/a/SKILL.md"},
		"no frontmatter":    {fstest.MapFS{"s/a/SKILL.md": skill("# A\n")}, "must start with YAML frontmatter"},
		"unclosed":          {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: a\n")}, "no closing ---"},
		"bad yaml":          {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: [a\n---\n")}, "SKILL.md frontmatter:"},
		"name mismatch":     {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: b\ndescription: d\n---\n")}, `frontmatter name must be "a"`},
		"no description":    {fstest.MapFS{"s/a/SKILL.md": skill("---\nname: a\n---\n")}, "needs a description"},
		"unreadable file":   {failFS{fstest.MapFS{"s/a/SKILL.md": valid, "s/a/ref.md": skill("x")}, "s/a/ref.md"}, "boom"},
		"unreadable subdir": {failFS{fstest.MapFS{"s/a/SKILL.md": valid, "s/a/refs/r.md": skill("x")}, "s/a/refs"}, "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadSkills(tc.fsys, "s")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestMustLoadSkills(t *testing.T) {
	if got := mustLoadSkills(builtinSkillFiles).entries; !reflect.DeepEqual(got, builtinSkills.entries) {
		t.Errorf("loading again gave %v, want %v", got, builtinSkills.entries)
	}
	defer func() {
		if recover() == nil {
			t.Error("a broken skill set did not panic")
		}
	}()
	mustLoadSkills(fstest.MapFS{})
}
