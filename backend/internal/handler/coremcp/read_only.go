// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcphint"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readOnlyToolsMiddleware lists, to a read-only connection, every tool as
// read-only and each action tool with only the actions it may run, so a
// client never offers its model a write. It only changes what is listed:
// RunAction is what refuses a write.
func (s *WorkspaceServer) readOnlyToolsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		listed, ok := res.(*mcp.ListToolsResult)
		if err != nil || !ok || !mcpevent.IsReadOnly(ctx) {
			return res, err
		}
		reads := map[string][]any{}
		for _, tool := range s.tools() {
			names := []any{}
			for _, a := range tool.Actions {
				if a.Reads {
					names = append(names, a.Name)
				}
			}
			reads[tool.Name] = append(names, "help")
		}
		// The listed tools are the server's own, shared by every connection:
		// change copies.
		out := *listed
		out.Tools = make([]*mcp.Tool, len(listed.Tools))
		for i, t := range listed.Tools {
			c := *t
			title := ""
			if t.Annotations != nil {
				title = t.Annotations.Title
			}
			c.Annotations = mcphint.Read(title)
			if schema, ok := t.InputSchema.(*jsonschema.Schema); ok && reads[t.Name] != nil && schema.Properties["action"] != nil {
				schema = schema.CloneSchemas()
				schema.Properties["action"].Enum = reads[t.Name]
				c.InputSchema = schema
			}
			out.Tools[i] = &c
		}
		return &out, nil
	}
}
