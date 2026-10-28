// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package mcphint builds the annotation block an MCP tool advertises about
// itself: a human-readable title, and the hints a client uses to decide what it
// may run without asking.
//
// The hints are stated once here, as four shapes, rather than written out at
// every tool. Fifty-three tools spread over two servers is where "readOnlyHint
// on the read tools" stops being a rule and starts being fifty-three chances to
// disagree with itself — and a client that auto-approves on a hint is trusting
// the least careful of them.
package mcphint

import "github.com/modelcontextprotocol/go-sdk/mcp"

// The protocol takes two of these hints as pointers, and every annotation
// block gets its own copy rather than sharing one: a package-level `false`
// whose address is handed to fifty-three tools is one stray assignment away
// from re-labelling all of them at once.
func hint(b bool) *bool { return &b }

// closedWorld says the tool's domain of interaction is closed. These tools act
// on the account's own workspaces, tasks and memories: none reaches a search
// engine, a third-party API or anything else whose answer this server cannot
// predict. The protocol's default is the opposite, so it is worth saying.
func closedWorld() *bool { return hint(false) }

// Read is a tool that answers a question and changes nothing.
//
// DestructiveHint is stated as false even though the protocol reads it only
// when ReadOnlyHint is false: OpenAI's plugin review wants all three hints
// explicit on every tool. IdempotentHint is left at its zero value.
func Read(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    true,
		DestructiveHint: hint(false),
		OpenWorldHint:   closedWorld(),
	}
}

// Write is a tool that adds something that was not there — a task, a message,
// an event. Calling it twice leaves two of them, so it is not idempotent.
func Write(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: hint(false),
		OpenWorldHint:   closedWorld(),
	}
}

// Update is a tool that sets named fields to the values it was given. Nothing
// the caller did not name is touched, and sending the same call twice lands in
// the same place as sending it once.
func Update(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: hint(false),
		IdempotentHint:  true,
		OpenWorldHint:   closedWorld(),
	}
}

// Overwrite is a tool that replaces or removes what was already there, so what
// it was holding is gone afterwards. Deletes, and the writes that take the
// whole of something rather than a field of it.
func Overwrite(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: hint(true),
		IdempotentHint:  true,
		OpenWorldHint:   closedWorld(),
	}
}

// OpenWorld marks a tool that reaches a third-party site, whose answer this
// server cannot predict.
func OpenWorld(a *mcp.ToolAnnotations) *mcp.ToolAnnotations {
	a.OpenWorldHint = hint(true)
	return a
}
