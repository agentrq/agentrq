// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/sitetools"
	"github.com/agentrq/agentrq/backend/internal/service/bm25"
)

// SiteToolsBackend is the sites shared into this workspace, and the route to
// the browser that shared them.
type SiteToolsBackend interface {
	List(ctx context.Context, workspaceID, userID int64) ([]SiteShareView, error)
	Get(ctx context.Context, workspaceID, userID int64, origin string) (SiteShareView, bool, error)
	AllowAlways(ctx context.Context, workspaceID, userID int64, origin, tool string) error
	Call(ctx context.Context, userID int64, share SiteShareView, tool string, args json.RawMessage) (text string, err error)
}

// SiteShareView is one shared site, with its tools' full definitions.
type SiteShareView struct {
	Site        string           `json:"site"` // origin
	Online      bool             `json:"online"`
	Tools       []sitetools.Tool `json:"tools"`
	AlwaysAllow []string         `json:"-"`
	BrowserID   string           `json:"-"`
	InstanceID  string           `json:"-"`
}

// Errors a SiteToolsBackend's Call returns besides the hub's own.
var ErrSiteOtherInstance = errors.New("sitetools: the browser is connected to another instance")

// SiteToolFailedError is a result frame that carried the page's error.
type SiteToolFailedError struct{ Message string }

func (e *SiteToolFailedError) Error() string { return e.Message }

// ListSiteToolsParams narrows and pages the listing. Every field is optional;
// the workspace is the connection's.
type ListSiteToolsParams struct {
	Q       string `json:"q,omitempty" jsonschema:"Words to rank tools by (BM25 over each tool's name and description, a word matching any word it begins); 3 to 16 characters. Leave it out to list tools by name."`
	Pattern string `json:"pattern,omitempty" jsonschema:"A regular expression (RE2, ignoring case) a tool's name or description must match; 3 to 16 characters."`
	Limit   int    `json:"limit,omitempty" jsonschema:"How many tools to return, at most 100. Leave it out to return every match."`
	Offset  int    `json:"offset,omitempty" jsonschema:"How many matches to skip, for the next page."`
}

const (
	// maxSiteToolsLimit is the largest page listSiteTools returns.
	maxSiteToolsLimit = 100
	// minSiteToolsQuery and maxSiteToolsQuery bound q and pattern, in
	// characters, when they are given at all.
	minSiteToolsQuery = 3
	maxSiteToolsQuery = 16
)

// siteListing is what listSiteTools shows: every shared site, then one page of
// the matching tools, names and descriptions only, since the schemas of every
// tool on every site cost the agent's context on each call.
// getSiteToolDefinition has the rest. Next is the offset of the next page,
// left out on the last one.
type siteListing struct {
	Sites []siteSummary     `json:"sites"`
	Total int               `json:"total"`
	Next  int               `json:"next,omitempty"`
	Tools []siteToolSummary `json:"tools"`
}

type siteSummary struct {
	Site   string `json:"site"`
	Online bool   `json:"online"`
	Tools  int    `json:"tools"`
}

type siteToolSummary struct {
	Site        string `json:"site"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetSiteToolDefinitionParams names one tool of one shared site.
type GetSiteToolDefinitionParams struct {
	Site string `json:"site" jsonschema:"The site's origin, exactly as listSiteTools prints it, e.g. https://github.com."`
	Tool string `json:"tool" jsonschema:"The tool's name, as listSiteTools prints it."`
}

// CallSiteToolParams is one call of a shared site's tool. Arguments is a map,
// not a json.RawMessage, because the SDK infers a RawMessage as a byte array.
type CallSiteToolParams struct {
	TaskID    string         `json:"taskId" jsonschema:"The ID of the task you are working on (base62). A tool that is not read-only asks the human there first."`
	Site      string         `json:"site" jsonschema:"The site's origin, exactly as listSiteTools prints it, e.g. https://github.com."`
	Tool      string         `json:"tool" jsonschema:"The tool's name, as listSiteTools prints it."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"The tool's arguments, matching the inputSchema getSiteToolDefinition returns. Omitted means {}."`
}

// siteApprovalTimeout is how long a call waits for the human to decide.
var siteApprovalTimeout = elicitDefaultTimeout

func siteToolError(format string, a ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, a...)}},
	}
}

func (ps *WorkspaceServer) ownerID() int64 { return monoflake.IDFromBase62(ps.userID).Int64() }

func (ps *WorkspaceServer) handleListSiteTools(ctx context.Context, req *mcp.CallToolRequest, params ListSiteToolsParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "listSiteTools", clientIdentityFromRequest(req))
	q, pattern := strings.TrimSpace(params.Q), strings.TrimSpace(params.Pattern)
	if refusal := checkSiteToolsSearch("q", q); refusal != "" {
		return siteToolError("%s", refusal), nil, nil
	}
	if refusal := checkSiteToolsSearch("pattern", pattern); refusal != "" {
		return siteToolError("%s", refusal), nil, nil
	}
	if params.Limit < 0 || params.Offset < 0 {
		return siteToolError("limit and offset cannot be negative"), nil, nil
	}
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		if re, err = regexp.Compile("(?i)" + pattern); err != nil {
			return siteToolError("pattern is not a valid regular expression: %v", err), nil, nil
		}
	}
	shares, err := ps.siteTools.List(ctx, ps.contentID(), ps.ownerID())
	if err != nil {
		return siteToolError("failed to list shared websites: %v", err), nil, nil
	}

	listing := siteListing{Sites: make([]siteSummary, len(shares)), Tools: []siteToolSummary{}}
	var found []siteToolSummary
	for i, share := range shares {
		listing.Sites[i] = siteSummary{Site: share.Site, Online: share.Online, Tools: len(share.Tools)}
		for _, t := range share.Tools {
			if re == nil || re.MatchString(t.Name) || re.MatchString(t.Description) {
				found = append(found, siteToolSummary{Site: share.Site, Name: t.Name, Description: t.Description})
			}
		}
	}
	found = rankSiteTools(found, q)

	listing.Total = len(found)
	if params.Offset < len(found) {
		page := found[params.Offset:]
		if limit := min(params.Limit, maxSiteToolsLimit); limit > 0 && limit < len(page) {
			page = page[:limit]
			listing.Next = params.Offset + limit
		}
		listing.Tools = page
	}
	b, _ := json.Marshal(listing)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// checkSiteToolsSearch says what is wrong with a q or pattern, or "" when it is
// empty or fits.
func checkSiteToolsSearch(field, value string) string {
	switch n := utf8.RuneCountInString(value); {
	case n > 0 && n < minSiteToolsQuery:
		return fmt.Sprintf("%s %q is too short; give at least %d characters, or leave it out", field, value, minSiteToolsQuery)
	case n > maxSiteToolsQuery:
		return fmt.Sprintf("%s is %d characters; the limit is %d", field, n, maxSiteToolsQuery)
	}
	return ""
}

// rankSiteTools orders tools by name ignoring case, then site. With a query it
// keeps only the tools sharing a word with it, best BM25 score first, ties
// still by name.
func rankSiteTools(tools []siteToolSummary, q string) []siteToolSummary {
	byName := func(a, b siteToolSummary) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Site, b.Site))
	}
	if q == "" {
		slices.SortStableFunc(tools, byName)
		return tools
	}
	docs := make([]string, len(tools))
	for i, t := range tools {
		docs[i] = t.Name + " " + t.Description
	}
	scores := bm25.New(docs).Scores(q)
	type scored struct {
		tool  siteToolSummary
		score float64
	}
	var hits []scored
	for i, t := range tools {
		if scores[i] > 0 {
			hits = append(hits, scored{t, scores[i]})
		}
	}
	slices.SortStableFunc(hits, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.score, a.score), byName(a.tool, b.tool))
	})
	ranked := make([]siteToolSummary, len(hits))
	for i, h := range hits {
		ranked[i] = h.tool
	}
	return ranked
}

func (ps *WorkspaceServer) handleGetSiteToolDefinition(ctx context.Context, req *mcp.CallToolRequest, params GetSiteToolDefinitionParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "getSiteToolDefinition", clientIdentityFromRequest(req))
	if params.Site == "" || params.Tool == "" {
		return siteToolError("site and tool are required"), nil, nil
	}
	_, tool, refusal := ps.findSiteTool(ctx, ps.ownerID(), params.Site, params.Tool)
	if refusal != nil {
		return refusal, nil, nil
	}
	b, _ := json.Marshal(tool)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// findSiteTool looks up a shared site's tool, or says why it cannot, naming
// what there is instead.
func (ps *WorkspaceServer) findSiteTool(ctx context.Context, userID int64, site, name string) (SiteShareView, sitetools.Tool, *mcp.CallToolResult) {
	share, found, err := ps.siteTools.Get(ctx, ps.contentID(), userID, site)
	if err != nil {
		return share, sitetools.Tool{}, siteToolError("failed to look up %s: %v", site, err)
	}
	if !found {
		return share, sitetools.Tool{}, siteToolError("%s is not shared with this workspace. Shared: %s", site, ps.sharedSites(ctx, userID))
	}
	i := slices.IndexFunc(share.Tools, func(t sitetools.Tool) bool { return t.Name == name })
	if i < 0 {
		names := make([]string, len(share.Tools))
		for j, t := range share.Tools {
			names[j] = t.Name
		}
		return share, sitetools.Tool{}, siteToolError("%s has no tool %s. It offers: %s", site, name, orNone(names))
	}
	return share, share.Tools[i], nil
}

func (ps *WorkspaceServer) handleCallSiteTool(ctx context.Context, req *mcp.CallToolRequest, params CallSiteToolParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "callSiteTool", clientIdentityFromRequest(req))
	if params.Site == "" || params.Tool == "" {
		return siteToolError("site and tool are required"), nil, nil
	}
	taskID := monoflake.IDFromBase62(params.TaskID).Int64()
	if taskID == 0 {
		return siteToolError("invalid taskId format"), nil, nil
	}
	userID := ps.ownerID()

	share, tool, refusal := ps.findSiteTool(ctx, userID, params.Site, params.Tool)
	if refusal != nil {
		return refusal, nil, nil
	}

	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	if err := validateSiteArgs(tool.InputSchema, args); err != nil {
		return siteToolError("arguments do not match %s's schema: %v", tool.Name, err), nil, nil
	}
	raw, _ := json.Marshal(args)

	if !tool.ReadOnly() && !slices.Contains(share.AlwaysAllow, tool.Name) {
		allowed, err := ps.approveSiteCall(ctx, taskID, userID, share.Site, tool.Name, args)
		if err != nil {
			return siteToolError("%v", err), nil, nil
		}
		if !allowed {
			return siteToolError("denied by the human"), nil, nil
		}
	}

	text, err := ps.siteTools.Call(ctx, userID, share, tool.Name, raw)
	var failed *SiteToolFailedError
	switch {
	case errors.As(err, &failed):
		return siteToolError("%s › %s failed: %s", share.Site, tool.Name, failed.Message), nil, nil
	case errors.Is(err, sitetools.ErrOffline):
		return siteToolError("the human's Chrome with AgentRQ is not connected; ask them to open Chrome, or try later"), nil, nil
	case errors.Is(err, sitetools.ErrTimeout):
		return siteToolError("%s did not answer within %d seconds", share.Site, int(sitetools.CallDeadline/time.Second)), nil, nil
	case errors.Is(err, ErrSiteOtherInstance):
		return siteToolError("your browser is connected to another server instance; try again"), nil, nil
	case err != nil:
		return siteToolError("%s › %s failed: %v", share.Site, tool.Name, err), nil, nil
	}
	if len(text) > sitetools.MaxResult {
		return siteToolError("the result was over %d KiB", sitetools.MaxResult>>10), nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

// sharedSites names the workspace's shared sites for an error, or "none".
func (ps *WorkspaceServer) sharedSites(ctx context.Context, userID int64) string {
	shares, _ := ps.siteTools.List(ctx, ps.contentID(), userID)
	sites := make([]string, len(shares))
	for i, s := range shares {
		sites[i] = s.Site
	}
	return orNone(sites)
}

func orNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// validateSiteArgs checks args against the page's inputSchema; a tool that
// declared none takes anything.
func validateSiteArgs(schema json.RawMessage, args map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(schema, &s); err != nil {
		return err
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return err
	}
	return resolved.Validate(args)
}

// approveSiteCall asks the human in the task whether the call may run. Only
// "allow" and "always" say yes, and "always" is remembered first.
func (ps *WorkspaceServer) approveSiteCall(ctx context.Context, taskID, userID int64, site, tool string, args map[string]any) (bool, error) {
	pretty, _ := json.MarshalIndent(args, "", "  ")
	message := fmt.Sprintf("The agent wants to run **%s › %s** with:\n```json\n%s\n```", site, tool, pretty)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"decision": map[string]any{
				"type":  "string",
				"title": "Decision",
				"oneOf": []any{
					map[string]any{"const": "allow", "title": "Allow once"},
					map[string]any{"const": "always", "title": fmt.Sprintf("Always allow %s on this site", tool)},
					map[string]any{"const": "deny", "title": "Deny"},
				},
			},
		},
		"required": []any{"decision"},
	}
	metadata := map[string]any{
		"type":            "elicitation_request",
		"message":         message,
		"mode":            "form",
		"status":          "pending",
		"requestedSchema": schema,
	}
	resp, err := ps.askHuman(ctx, taskID, message, metadata, siteApprovalTimeout)
	if err != nil {
		return false, fmt.Errorf("failed to ask the human: %v", err)
	}
	if resp.Action != "accept" {
		return false, nil
	}
	switch resp.Content["decision"] {
	case "always":
		if err := ps.siteTools.AllowAlways(ctx, ps.contentID(), userID, site, tool); err != nil {
			return false, fmt.Errorf("failed to remember the approval: %v", err)
		}
		return true, nil
	case "allow":
		return true, nil
	}
	return false, nil
}
