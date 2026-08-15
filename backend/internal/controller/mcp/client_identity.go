package mcp

import (
	"net/http"
	"reflect"

	"github.com/cespare/xxhash/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// clientInfoMetaKey is the per-request "_meta" key MCP spec 2026-07-28 (SEP-2575)
// uses for client identity, replacing the initialize handshake's ClientInfo.
const clientInfoMetaKey = "io.modelcontextprotocol/clientInfo"

// clientIdentity is the best-effort MCP client identity for a single request.
//
// Deliberately not sourced from Session.InitializeParams().ClientInfo: SEP-2575
// removes the initialize handshake entirely in favor of identity carried in
// per-request metadata, so this reads from that same forward-compatible surface
// instead. Most clients don't send the new _meta key yet, so this falls back to
// the HTTP User-Agent header — also request metadata, not anything tied to the
// handshake that's going away.
type clientIdentity struct {
	name    string
	version string
}

// hash returns a deterministic 64-bit identifier for this identity, or 0 if no
// identity information was available. Returned as int64 (reinterpreting the
// same bit pattern) since Postgres has no unsigned bigint type.
func (ci clientIdentity) hash() int64 {
	if ci.name == "" {
		return 0
	}
	return int64(xxhash.Sum64String(ci.name + "@" + ci.version))
}

func clientIdentityFromRequest(req mcp.Request) clientIdentity {
	// req is a *mcp.CallToolRequest (or similar) boxed into the mcp.Request
	// interface. A nil concrete pointer boxed into an interface is not itself
	// a nil interface, so "req == nil" alone wouldn't catch it (e.g. tests
	// calling handlers directly with a nil *mcp.CallToolRequest).
	if req == nil {
		return clientIdentity{}
	}
	if v := reflect.ValueOf(req); v.Kind() == reflect.Pointer && v.IsNil() {
		return clientIdentity{}
	}
	var meta map[string]any
	if p := req.GetParams(); p != nil {
		meta = p.GetMeta()
	}
	var userAgent string
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		userAgent = extra.Header.Get("User-Agent")
	}
	return clientIdentityFromMeta(meta, userAgent)
}

func clientIdentityFromHTTPRequest(r *http.Request) clientIdentity {
	return clientIdentity{name: r.Header.Get("User-Agent")}
}

func clientIdentityFromMeta(meta map[string]any, fallbackUserAgent string) clientIdentity {
	if raw, ok := meta[clientInfoMetaKey]; ok {
		if m, ok := raw.(map[string]any); ok {
			if name, _ := m["name"].(string); name != "" {
				version, _ := m["version"].(string)
				return clientIdentity{name: name, version: version}
			}
		}
	}
	return clientIdentity{name: fallbackUserAgent}
}
