// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolCall is what a tool call was counted as: the tool or action name, and
// the workspace it was for.
type ToolCall struct {
	Name        string
	WorkspaceID int64
}

type toolCallKey struct{}

// NoteToolCall records how the call running in ctx was counted, so
// ToolErrorsMiddleware counts its error under the same name.
func NoteToolCall(ctx context.Context, name string, workspaceID int64) {
	if c, ok := ctx.Value(toolCallKey{}).(*ToolCall); ok {
		*c = ToolCall{Name: name, WorkspaceID: workspaceID}
	}
}

// ToolErrorsMiddleware calls emit once for each tools/call that ends in an
// error, a Go one or a result with isError. It must be the outermost
// middleware, to see every refusal. A call refused before it was counted is
// counted under the tool it named.
func ToolErrorsMiddleware(emit func(ctx context.Context, req *mcp.CallToolRequest, call ToolCall)) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			callReq, ok := req.(*mcp.CallToolRequest)
			if !ok || method != "tools/call" || callReq.Params == nil {
				return next(ctx, method, req)
			}
			call := &ToolCall{}
			res, err := next(context.WithValue(ctx, toolCallKey{}, call), method, req)
			if r, _ := res.(*mcp.CallToolResult); err != nil || (r != nil && r.IsError) {
				if call.Name == "" {
					call.Name = callReq.Params.Name
				}
				emit(ctx, callReq, *call)
			}
			return res, err
		}
	}
}
