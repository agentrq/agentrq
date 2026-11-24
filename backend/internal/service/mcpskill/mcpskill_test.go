// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcpskill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// testSet is one skill, a, with a reference file.
func testSet(t *testing.T) *Set {
	t.Helper()
	set, err := Load(fstest.MapFS{
		"s/a/SKILL.md":            {Data: []byte("---\nname: a\ndescription: d\n---\n# A\n")},
		"s/a/references/guide.md": {Data: []byte("guide")},
	}, "s")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// rpc posts one 2026-07-28 request to a stateless server serving set, with
// every call counted into counted, and returns its result or its error.
func rpc(t *testing.T, set *Set, counted *[]string, method, name, fields string) (map[string]any, map[string]any) {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	set.Serve(srv, func(_ context.Context, method string) { *counted = append(*counted, method) })
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(ts.Close)

	params := `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}`
	if fields != "" {
		params += "," + fields
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	out := body.Bytes()
	// A success may come framed as one SSE event.
	if i := bytes.Index(out, []byte("data: ")); i >= 0 {
		out = bytes.TrimSpace(out[i+len("data: "):])
	}
	var env struct {
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("%s: decode %s: %v", method, out, err)
	}
	return env.Result, env.Error
}

// Serve answers every skills method and reads the files, counting each
// call: the methods by name, every file of a skill as the skill.
func TestServe(t *testing.T) {
	set := testSet(t)
	if got := set.Files()["skill://a/references/guide.md"]; string(got) != "guide" {
		t.Errorf("Files() has %q for the guide, want its bytes", got)
	}
	var counted []string

	rs, rpcErr := rpc(t, set, &counted, MethodList, "", "")
	if rpcErr != nil {
		t.Fatalf("skills/list: %v", rpcErr)
	}
	if skills, _ := rs["skills"].([]any); len(skills) != 1 || skills[0].(map[string]any)["uri"] != "skill://a/SKILL.md" {
		t.Errorf("skills = %v, want skill a", rs["skills"])
	}
	if rs["resultType"] != "complete" || rs["ttlMs"] != float64(3600000) || rs["cacheScope"] != "public" {
		t.Errorf("skills/list = %v, want a complete result any client caches for an hour", rs)
	}

	if rs, rpcErr = rpc(t, set, &counted, MethodGet, "", `"uri":"skill://a/SKILL.md"`); rpcErr != nil || rs["skill"].(map[string]any)["uri"] != "skill://a/SKILL.md" {
		t.Errorf("skills/get = %v, %v; want skill a", rs, rpcErr)
	}
	if _, rpcErr = rpc(t, set, &counted, MethodGet, "", `"uri":"skill://nope/SKILL.md"`); rpcErr == nil || rpcErr["code"] != float64(-32602) {
		t.Errorf("an unknown skill answered %v, want invalid params", rpcErr)
	}

	if rs, rpcErr = rpc(t, set, &counted, MethodDirectoryRead, "", `"uri":"skill://a"`); rpcErr != nil || len(rs["resources"].([]any)) != 2 {
		t.Errorf("resources/directory/read = %v, %v; want the SKILL.md and the references directory", rs, rpcErr)
	}
	if _, rpcErr = rpc(t, set, &counted, MethodDirectoryRead, "", `"uri":"skill://a/SKILL.md"`); rpcErr == nil || rpcErr["code"] != float64(-32602) {
		t.Errorf("a file read as a directory answered %v, want invalid params", rpcErr)
	}

	const guide = "skill://a/references/guide.md"
	rs, rpcErr = rpc(t, set, &counted, "resources/read", guide, `"uri":"`+guide+`"`)
	if rpcErr != nil {
		t.Fatalf("resources/read: %v", rpcErr)
	}
	if c := rs["contents"].([]any)[0].(map[string]any); c["text"] != "guide" || c["mimeType"] != "text/markdown" {
		t.Errorf("read %s = %v, want its text as markdown", guide, c)
	}

	want := []string{MethodList, MethodGet, MethodGet, MethodDirectoryRead, MethodDirectoryRead, "resource:a"}
	if !reflect.DeepEqual(counted, want) {
		t.Errorf("counted %v, want %v", counted, want)
	}
}

func TestAddCapability(t *testing.T) {
	caps := &mcp.ServerCapabilities{Logging: &mcp.LoggingCapabilities{}}
	AddCapability(caps)
	if want := (map[string]any{Extension: map[string]any{"directoryRead": true}}); !reflect.DeepEqual(caps.Extensions, want) || caps.Logging == nil {
		t.Errorf("capabilities = %+v, want the Skills extension beside logging", caps)
	}
}

func TestTelemetryName(t *testing.T) {
	for uri, want := range map[string]string{
		"skill://a/SKILL.md":            "resource:a",
		"skill://a/references/guide.md": "resource:a",
		"skill://a":                     "resource:a",
	} {
		if got := TelemetryName(uri); got != want {
			t.Errorf("TelemetryName(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestLoadSkills_SupportingFiles(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0xff}
	set, err := Load(fstest.MapFS{
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

	h := &server{set: set, count: func(context.Context, string) {}}
	rs, err := h.readFile(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "skill://a/assets/logo.png"}})
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
			_, err := Load(tc.fsys, "s")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestMustLoad(t *testing.T) {
	fsys := fstest.MapFS{"s/a/SKILL.md": {Data: []byte("---\nname: a\ndescription: d\n---\n")}}
	want, err := Load(fsys, "s")
	if err != nil {
		t.Fatal(err)
	}
	if got := MustLoad(fsys, "s").Entries(); !reflect.DeepEqual(got, want.Entries()) {
		t.Errorf("MustLoad gave %v, want %v", got, want.Entries())
	}
	defer func() {
		if recover() == nil {
			t.Error("a broken skill set did not panic")
		}
	}()
	MustLoad(fstest.MapFS{}, "s")
}
