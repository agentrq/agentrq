// Copyright 2026 Contextual, Inc. https://agentrq.com

package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Per-workspace memory: what an agent chooses to remember about a workspace,
// so that what it learned in one task is still there in the next.
//
// The memory belongs to the workspace rather than to the agent that wrote it,
// so several agents working the same workspace share one set of notes. Any
// number of named memories can live side by side; MEMORY.md is the one agents
// are told to read first, which makes it the natural place for an index to the
// rest.
//
// Both limits below are enforced here rather than in the repository, because
// this is where there is a caller to answer. An agent that is told its memory
// was too large can split it and try again; an agent whose memory was silently
// cut in half at the storage layer has no idea anything went wrong.

const (
	// DefaultMemoryName is what both tools read and write when given no name.
	// The index, by convention — an agent that calls loadMemory with no
	// arguments should land on something that tells it what else is here.
	//
	// Stored lowercase like every other name, so an agent that asks for
	// MEMORY.md — the spelling the README-style convention suggests — lands on
	// the same memory rather than creating a second one beside it.
	DefaultMemoryName = "memory.md"

	// MaxMemoryNameLength matches the column. Counted in characters, as the
	// column is.
	MaxMemoryNameLength = 32

	// MaxMemoryBytes caps one memory at 16 KiB of UTF-8.
	MaxMemoryBytes = 16 * 1024
)

// LoadMemoryFunc reads one of the workspace's memories.
//
// `found` is separate from `err` because a memory that was never written is
// the ordinary case, not a failure: every agent's first call on a fresh
// workspace misses, and answering that with an error would teach agents to
// stop asking.
type LoadMemoryFunc func(ctx context.Context, name string) (content string, found bool, err error)

// SaveMemoryFunc writes one of the workspace's memories, replacing whatever
// was stored under that name.
type SaveMemoryFunc func(ctx context.Context, name string, content string) error

// DeleteMemoryFunc removes one of the workspace's memories.
//
// `deleted` mirrors LoadMemoryFunc's `found`: deleting a memory that was never
// written (or already deleted) is not a failure, so the tool can say plainly
// that there was nothing to remove instead of erroring on a harmless retry.
type DeleteMemoryFunc func(ctx context.Context, name string) (deleted bool, err error)

// LoadMemoryParams is the input to the loadMemory tool.
type LoadMemoryParams struct {
	Name string `json:"name,omitempty" jsonschema:"Which memory to read. Defaults to MEMORY.md, the index that says what else this workspace remembers."`
}

// SaveMemoryParams is the input to the saveMemory tool.
type SaveMemoryParams struct {
	Name    string `json:"name,omitempty" jsonschema:"Which memory to write. Defaults to MEMORY.md, the index that says what else this workspace remembers."`
	Content string `json:"content" jsonschema:"The full new content. This replaces the memory entirely — there is no append, so include everything worth keeping."`
}

// DeleteMemoryParams is the input to the deleteMemory tool.
type DeleteMemoryParams struct {
	Name string `json:"name,omitempty" jsonschema:"Which memory to delete. Defaults to MEMORY.md, the index that says what else this workspace remembers."`
}

// memoryNamePattern is the shape a stored name takes: a lowercase slug with a
// .md suffix. Hyphens separate words and never double up or sit at an edge.
//
// Being this strict is what lets `memory://<name>` links inside a memory be
// parsed at all — a name with a space in it is not a URL — and it rules out
// path separators, control characters and everything else in one rule rather
// than a list of individual refusals.
var memoryNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.md$`)

// resolveMemoryName turns what an agent asked for into the name to store under.
//
// Strict about what is kept, forgiving about what is accepted: case is folded
// and surrounding whitespace dropped, so MEMORY.md, Memory.md and memory.md are
// one memory rather than three that each look like the only one. Anything that
// is still not a valid name after that is refused rather than repaired — a name
// bent into shape files the memory somewhere the agent did not choose, and its
// next load will miss.
//
// Folding here rather than in a query is deliberate. AgentRQ runs on SQLite and
// Postgres, `=` is case-sensitive by default in both, and a NOCASE collation
// does not port between them — so canonicalising once at this boundary is what
// makes the unique key behave the same on either database.
func resolveMemoryName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return DefaultMemoryName, nil
	}

	canonical := strings.ToLower(trimmed)
	if n := utf8.RuneCountInString(canonical); n > MaxMemoryNameLength {
		return "", fmt.Errorf("memory name is %d characters; the limit is %d", n, MaxMemoryNameLength)
	}
	if !memoryNamePattern.MatchString(canonical) {
		return "", fmt.Errorf(
			"memory name %q is not usable; names are lowercase words joined by single hyphens and ending in .md, like %q or %q",
			name, DefaultMemoryName, "release-notes.md",
		)
	}
	return canonical, nil
}

// validateMemoryContent refuses a memory that is over the cap.
//
// Refusing rather than truncating: an agent told that its memory is too large
// can decide what to drop, which is a judgement only it can make.
func validateMemoryContent(content string) error {
	if n := len(content); n > MaxMemoryBytes {
		return fmt.Errorf("memory is %d bytes; the limit is %d bytes (16 KiB). Split it across named memories and list them in %s", n, MaxMemoryBytes, DefaultMemoryName)
	}
	return nil
}

func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

func (ps *WorkspaceServer) handleLoadMemory(ctx context.Context, req *mcp.CallToolRequest, params LoadMemoryParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "loadMemory", clientIdentityFromRequest(req))

	name, err := resolveMemoryName(params.Name)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	if ps.loadMemory == nil {
		return toolError("memory is not available on this server"), nil, nil
	}

	content, found, err := ps.loadMemory(ctx, name)
	if err != nil {
		return toolError("failed to load memory %q: %v", name, err), nil, nil
	}
	if !found {
		// Not an error: this is what a fresh workspace looks like, and saying
		// so plainly is what tells the agent to write one.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("No memory saved under %q yet. Use saveMemory to write one.", name),
			}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: content}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleSaveMemory(ctx context.Context, req *mcp.CallToolRequest, params SaveMemoryParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "saveMemory", clientIdentityFromRequest(req))

	name, err := resolveMemoryName(params.Name)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	if err := validateMemoryContent(params.Content); err != nil {
		return toolError("%v", err), nil, nil
	}
	if ps.saveMemory == nil {
		return toolError("memory is not available on this server"), nil, nil
	}

	if err := ps.saveMemory(ctx, name, params.Content); err != nil {
		return toolError("failed to save memory %q: %v", name, err), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Saved %q (%d bytes).", name, len(params.Content)),
		}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleDeleteMemory(ctx context.Context, req *mcp.CallToolRequest, params DeleteMemoryParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "deleteMemory", clientIdentityFromRequest(req))

	name, err := resolveMemoryName(params.Name)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	if ps.deleteMemory == nil {
		return toolError("memory is not available on this server"), nil, nil
	}

	deleted, err := ps.deleteMemory(ctx, name)
	if err != nil {
		return toolError("failed to delete memory %q: %v", name, err), nil, nil
	}
	if !deleted {
		// Not an error: deleting something that was never written, or was
		// already removed, ends in the same state the caller wanted.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("No memory was stored under %q; nothing to delete.", name),
			}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Deleted %q.", name)}},
	}, nil, nil
}
