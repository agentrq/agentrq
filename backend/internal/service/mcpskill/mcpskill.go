// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package mcpskill serves Agent Skills built into the binary over the MCP
// Skills extension (SEP-2640), so an agent connected to a server gets the
// server's own skills with nothing to install.
package mcpskill

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
	"gopkg.in/yaml.v3"
)

// Extension is the MCP Skills extension (SEP-2640): the server lists its
// Agent Skills with skills/list and skills/get, serves their files as
// resources, and lists their directories with resources/directory/read.
const Extension = "io.modelcontextprotocol/skills"

// The skills methods.
const (
	MethodList          = "skills/list"
	MethodGet           = "skills/get"
	MethodDirectoryRead = "resources/directory/read"
)

// MIMETypeDirectory marks a directory resource.
const MIMETypeDirectory = "inode/directory"

const (
	// skillFile is the file every skill has.
	skillFile = "SKILL.md"
	// scheme prefixes the skill files' resource URIs.
	scheme = "skill://"
	// ttl is how long clients may cache skill entries and files: they
	// change only with a new release.
	ttl = time.Hour
	// resultComplete is the resultType of a final result.
	resultComplete = "complete"
)

type (
	// Entry describes a skill: its SKILL.md's URI, the SKILL.md
	// frontmatter as is, and every file in it.
	Entry struct {
		URI         string         `json:"uri"`
		Frontmatter map[string]any `json:"frontmatter"`
		Resources   []Resource     `json:"resources"`
	}

	// Resource is a file in a skill's manifest, with the SHA-256 digest and
	// size of the bytes resources/read returns.
	Resource struct {
		URI    string `json:"uri"`
		Digest string `json:"digest"`
		Size   int    `json:"size"`
	}

	// ListParams are skills/list's parameters. Every skill fits on one
	// page, so the cursor is never set by the server.
	ListParams struct {
		mcp.ParamsBase
		Cursor string `json:"cursor,omitempty"`
	}

	// ListResult is skills/list's result.
	ListResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string  `json:"resultType"`
		Skills     []Entry `json:"skills"`
	}

	// GetParams are skills/get's parameters.
	GetParams struct {
		mcp.ParamsBase
		URI string `json:"uri"`
	}

	// GetResult is skills/get's result.
	GetResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string `json:"resultType"`
		Skill      Entry  `json:"skill"`
	}

	// DirectoryReadParams are resources/directory/read's parameters. Every
	// directory fits on one page, so the cursor is never set by the server.
	DirectoryReadParams struct {
		mcp.ParamsBase
		URI    string `json:"uri"`
		Cursor string `json:"cursor,omitempty"`
	}

	// DirectoryReadResult is resources/directory/read's result: the
	// directory's direct children, subdirectories as directory resources.
	DirectoryReadResult struct {
		mcp.ResultBase
		mcp.Cacheable
		ResultType string          `json:"resultType"`
		Resources  []*mcp.Resource `json:"resources"`
	}

	// Set is the loaded skills: their entries in name order, every file's
	// bytes by URI, and every directory's children by URI.
	Set struct {
		entries []Entry
		files   map[string][]byte
		dirs    map[string][]*mcp.Resource
	}

	// Counter counts a call: a skills method by its name, a file read by
	// TelemetryName.
	Counter func(ctx context.Context, method string)

	// server answers the skills calls for one Set.
	server struct {
		set   *Set
		count Counter
	}
)

// Load reads the skills under root: each directory is a skill, named after
// it, with a SKILL.md and any supporting files.
func Load(fsys fs.FS, root string) (*Set, error) {
	dirs, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	set := &Set{files: map[string][]byte{}}
	for _, dir := range dirs {
		if !dir.IsDir() {
			return nil, fmt.Errorf("%s: not a skill directory", dir.Name())
		}
		entry, err := set.loadSkill(fsys, root, dir.Name())
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", dir.Name(), err)
		}
		set.entries = append(set.entries, entry)
	}
	set.index()
	return set, nil
}

// MustLoad loads skills built into the binary; a broken one is a build bug.
func MustLoad(fsys fs.FS, root string) *Set {
	set, err := Load(fsys, root)
	if err != nil {
		panic(err)
	}
	return set
}

// Entries are the skills, in name order.
func (s *Set) Entries() []Entry { return s.entries }

// Files are every skill file's bytes, by URI.
func (s *Set) Files() map[string][]byte { return s.files }

// index lists every directory of every skill, the skill root included,
// with its direct children in URI order.
func (s *Set) index() {
	children := map[string]map[string]*mcp.Resource{}
	for _, entry := range s.entries {
		for _, r := range entry.Resources {
			child := entry.fileResource(r)
			for dir := path.Dir(strings.TrimPrefix(r.URI, scheme)); dir != "."; dir = path.Dir(dir) {
				uri := scheme + dir
				if children[uri] == nil {
					children[uri] = map[string]*mcp.Resource{}
				}
				children[uri][child.URI] = child
				child = &mcp.Resource{URI: uri, Name: dir, Description: entry.describe(uri, "directory"), MIMEType: MIMETypeDirectory}
			}
		}
	}
	s.dirs = map[string][]*mcp.Resource{}
	for uri, byURI := range children {
		list := make([]*mcp.Resource, 0, len(byURI))
		for _, child := range byURI {
			list = append(list, child)
		}
		slices.SortFunc(list, func(a, b *mcp.Resource) int { return strings.Compare(a.URI, b.URI) })
		s.dirs[uri] = list
	}
}

// fileResource is a skill file's resource metadata.
func (e Entry) fileResource(r Resource) *mcp.Resource {
	return &mcp.Resource{URI: r.URI, Name: r.URI[len(scheme):], Description: e.describe(r.URI, "file"), MIMEType: mimeType(r.URI), Size: int64(r.Size)}
}

// describe is the description of the skill's resource at uri, a file or a
// directory: the skill's own description for its SKILL.md and its root,
// else what the resource is and which skill it belongs to.
func (e Entry) describe(uri, kind string) string {
	name := e.Frontmatter["name"].(string)
	if uri == e.URI || uri == scheme+name {
		return e.Frontmatter["description"].(string)
	}
	return fmt.Sprintf("The %s %s of the %s skill.", strings.TrimPrefix(uri, scheme+name+"/"), kind, name)
}

// loadSkill reads one skill's files, SKILL.md first, into the set.
func (s *Set) loadSkill(fsys fs.FS, root, name string) (Entry, error) {
	entry := Entry{URI: scheme + name + "/" + skillFile}
	main, err := fs.ReadFile(fsys, path.Join(root, name, skillFile))
	if err != nil {
		return entry, err
	}
	if entry.Frontmatter, err = frontmatter(main); err != nil {
		return entry, err
	}
	if entry.Frontmatter["name"] != name {
		return entry, fmt.Errorf("frontmatter name must be %q", name)
	}
	if d, _ := entry.Frontmatter["description"].(string); d == "" {
		return entry, errors.New("frontmatter needs a description")
	}
	entry.Resources = append(entry.Resources, s.add(entry.URI, main))
	err = fs.WalkDir(fsys, path.Join(root, name), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == path.Join(root, name, skillFile) {
			return err
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		entry.Resources = append(entry.Resources, s.add(scheme+p[len(root)+1:], body))
		return nil
	})
	return entry, err
}

// add keeps a file's bytes and returns its manifest entry.
func (s *Set) add(uri string, body []byte) Resource {
	s.files[uri] = body
	sum := sha256.Sum256(body)
	return Resource{URI: uri, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(body)}
}

// frontmatter parses the YAML block a SKILL.md starts with.
func frontmatter(body []byte) (map[string]any, error) {
	rest, ok := bytes.CutPrefix(body, []byte("---\n"))
	if !ok {
		return nil, errors.New("SKILL.md must start with YAML frontmatter")
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return nil, errors.New("SKILL.md frontmatter has no closing ---")
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal(block, &fields); err != nil {
		return nil, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	return fields, nil
}

// AddCapability advertises the Skills extension in caps. Setting any
// capability drops the mcp package's inferred defaults, so a caller keeps
// the ones it had by building caps with them first.
func AddCapability(caps *mcp.ServerCapabilities) {
	caps.AddExtension(Extension, map[string]any{"directoryRead": true})
}

// Serve serves the set on srv: its files as resources, and the
// skills/list, skills/get and resources/directory/read methods, each call
// counted with count.
func (s *Set) Serve(srv *mcp.Server, count Counter) {
	h := &server{set: s, count: count}
	for _, entry := range s.entries {
		for _, r := range entry.Resources {
			srv.AddResource(entry.fileResource(r), h.readFile)
		}
	}
	// No name shadows a standard MCP method, so registering can't fail.
	_ = mcp.AddReceivingCustomMethod(srv, MethodList, h.list)
	_ = mcp.AddReceivingCustomMethod(srv, MethodGet, h.get)
	_ = mcp.AddReceivingCustomMethod(srv, MethodDirectoryRead, h.readDirectory)
}

// mimeType names a skill file's type from its extension.
func mimeType(uri string) string {
	ext := path.Ext(uri)
	if ext == ".md" {
		return "text/markdown"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// TelemetryName is what a read of a skill's file is counted as: the skill,
// not the file, so adding a file needs no telemetry ID of its own.
func TelemetryName(uri string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(uri, scheme), "/")
	return "resource:" + name
}

// cacheable lets any client cache skills for ttl.
func cacheable() mcp.Cacheable {
	return mcp.Cacheable{TTLMs: int(ttl / time.Millisecond), CacheScope: "public"}
}

// readFile serves a skill file: UTF-8 as text, anything else as a blob.
// The server only routes registered URIs here.
func (h *server) readFile(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	h.count(ctx, TelemetryName(uri))
	body := h.set.files[uri]
	contents := &mcp.ResourceContents{URI: uri, MIMEType: mimeType(uri)}
	if utf8.Valid(body) {
		contents.Text = string(body)
	} else {
		contents.Blob = body
	}
	return &mcp.ReadResourceResult{Cacheable: cacheable(), Contents: []*mcp.ResourceContents{contents}}, nil
}

func (h *server) list(ctx context.Context, _ *mcp.ServerSession, _ *ListParams) (*ListResult, error) {
	h.count(ctx, MethodList)
	return &ListResult{Cacheable: cacheable(), ResultType: resultComplete, Skills: h.set.entries}, nil
}

func (h *server) get(ctx context.Context, _ *mcp.ServerSession, params *GetParams) (*GetResult, error) {
	h.count(ctx, MethodGet)
	for _, entry := range h.set.entries {
		if entry.URI == params.URI {
			return &GetResult{Cacheable: cacheable(), ResultType: resultComplete, Skill: entry}, nil
		}
	}
	return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown skill %q", params.URI)}
}

// readDirectory lists a skill directory's direct children. A URI that is
// not a directory, a file's included, is invalid params, as SEP-2640 says.
func (h *server) readDirectory(ctx context.Context, _ *mcp.ServerSession, params *DirectoryReadParams) (*DirectoryReadResult, error) {
	h.count(ctx, MethodDirectoryRead)
	children, ok := h.set.dirs[params.URI]
	if !ok {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("not a directory %q", params.URI)}
	}
	return &DirectoryReadResult{Cacheable: cacheable(), ResultType: resultComplete, Resources: children}, nil
}
