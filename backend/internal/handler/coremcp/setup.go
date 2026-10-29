// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// manifestPath is where MCP discovery looks for the server's manifest.
const manifestPath = "/.well-known/mcp.json"

// setupPageCSP locks the setup page down: no scripts, frames or requests,
// inline styles only, and images from imgSrc.
func setupPageCSP(imgSrc string) string {
	return "default-src 'none'; style-src 'unsafe-inline'; img-src " + imgSrc + "; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}

type (
	// manifest is the MCP Registry server.json, as at the repository root,
	// with this release and the remote this server answers on.
	manifest struct {
		Schema      string           `json:"$schema"`
		Name        string           `json:"name"`
		Title       string           `json:"title"`
		Description string           `json:"description"`
		WebsiteURL  string           `json:"websiteUrl"`
		Repository  manifestRepo     `json:"repository"`
		Icons       []manifestIcon   `json:"icons"`
		Version     string           `json:"version"`
		Remotes     []manifestRemote `json:"remotes"`
	}

	manifestRepo struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	}

	manifestIcon struct {
		Src      string   `json:"src"`
		MIMEType string   `json:"mimeType"`
		Sizes    []string `json:"sizes"`
	}

	manifestRemote struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}

	// toolSummary is a tool as the setup page lists it.
	toolSummary struct {
		Name    string
		Summary string
	}

	setupPageData struct {
		Endpoint string
		Logo     string
		Tools    []toolSummary
	}
)

// setupTools are the tools the setup page lists, in registration order.
var setupTools = []toolSummary{
	{"workspace", "List, create, update and fork the account's workspaces."},
	{"task", "Read, create and manage the tasks of every workspace."},
	{"memory", "Read a workspace's memory."},
	{"skills", "Search and read the account's skills."},
	{"events", "Define events and the triggers that create tasks when they fire."},
	{"workflows", "Build workflows out of events and the workspaces they hand off to."},
	{"machines", "Enrol machines running agentrqd and launch or stop their agents."},
	{"pano", "Read and write the account's shared markdown docs."},
	{"help", "How to call the tools above."},
}

// serverManifest is the server's MCP Registry manifest (server.json) naming
// endpoint as its streamable HTTP remote.
func serverManifest(endpoint string) []byte {
	b, _ := json.MarshalIndent(manifest{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        "io.github.agentrq/agentrq",
		Title:       serverTitle,
		Description: "Task manager for AI agents. Orchestrate agents to achieve large goals in unified control plane.",
		WebsiteURL:  serverWebsiteURL,
		Repository:  manifestRepo{URL: "https://github.com/agentrq/agentrq", Source: "github"},
		Icons:       []manifestIcon{{Src: "https://agentrq.com/favicon.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}}},
		Version:     config.Release(),
		Remotes:     []manifestRemote{{Type: "streamable-http", URL: endpoint}},
	}, "", "  ")
	// Plain strings only, so it cannot fail.
	return append(b, '\n')
}

// openAIChallengePath is where OpenAI checks that a plugin's submitter
// controls the MCP server's domain.
const openAIChallengePath = "/.well-known/openai-apps-challenge"

// openAIChallengeHandler answers with the token OpenAI issued for the
// submission, exactly as given, or 404 when none is set.
func openAIChallengeHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(token))
	})
}

// manifestHandler answers with the manifest, which names endpoint on every
// host, so clients and directories find the server from any of them.
func manifestHandler(endpoint string) http.Handler {
	body := serverManifest(endpoint)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	})
}

// wantsSetupPage reports whether r is a person or a plain HTTP client
// opening the endpoint, rather than an MCP client: a GET without a token
// that asks for neither JSON nor an event stream. MCP clients always send
// both in Accept, so they still get the 401 that starts their sign-in. The
// `at` cookie does not count as a token, since a signed-in browser sends it.
func wantsSetupPage(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.URL.Path != "/mcp" && r.URL.Path != "/" {
		return false
	}
	if r.Header.Get("Authorization") != "" || r.URL.Query().Get("token") != "" {
		return false
	}
	accept := strings.ToLower(strings.Join(r.Header.Values("Accept"), ","))
	return !strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json")
}

// serveSetupPage tells a person how to add this server, at the URL they
// opened, to their MCP client.
func (h *handler) serveSetupPage(w http.ResponseWriter, r *http.Request) {
	// The logo is the app's, so the page loads nothing from this host.
	logo, imgSrc := "", "'none'"
	if h.baseURL != "" {
		logo, imgSrc = h.baseURL+"/agentrq.png", h.baseURL
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Content-Security-Policy", setupPageCSP(imgSrc))
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Vary", "Accept, Authorization")
	// The template is fixed and the data plain strings, so it cannot fail.
	_ = setupTemplate.Execute(w, setupPageData{
		Endpoint: oauthIdentityFor(r).resource,
		Logo:     logo,
		Tools:    setupTools,
	})
}

var setupTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgentRQ MCP server</title>
<meta name="description" content="Connect an MCP client to AgentRQ to manage every workspace, task and agent on your account.">
<style>
:root {
  --page: #fafafa; --card: #ffffff; --border: #e5e7eb; --row: #fafafa; --text: #111827; --muted: #6b7280; --label: #9ca3af;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root { --page: #09090b; --card: #18181b; --border: #27272a; --row: #111113; --text: #fafafa; --muted: #a1a1aa; --label: #71717a; color-scheme: dark; }
}
* { box-sizing: border-box; }
html, body { margin: 0; }
body {
  background: var(--page); color: var(--text); padding: 40px 16px;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; -webkit-font-smoothing: antialiased;
}
main { max-width: 42rem; margin: 0 auto; padding: 32px; background: var(--card); border: 1px solid var(--border); border-radius: 12px; box-shadow: 0 1px 2px rgba(0,0,0,.05); }
header { display: flex; align-items: center; gap: 14px; margin: 0 0 20px; }
header img { width: 44px; height: 44px; border-radius: 8px; }
h1 { margin: 0; font-size: 22px; line-height: 1.3; font-weight: 900; letter-spacing: -0.02em; }
h2 { margin: 28px 0 8px; font-size: 10px; font-weight: 900; letter-spacing: 0.1em; text-transform: uppercase; color: var(--label); }
p, li { font-size: 14px; line-height: 1.6; }
p { margin: 0 0 12px; }
.lead { color: var(--muted); }
pre, .url { margin: 0 0 12px; padding: 10px 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--row); white-space: pre-wrap; overflow-wrap: anywhere; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
.url { font-size: 15px; font-weight: 700; }
ul { margin: 0 0 12px; padding-left: 20px; }
a { color: inherit; }
@media (max-width: 480px) { body { padding: 16px; } main { padding: 20px; } }
</style>
</head>
<body>
<main>
  <header>
    {{- if .Logo}}
    <img src="{{.Logo}}" alt="">
    {{- end}}
    <h1>AgentRQ MCP server</h1>
  </header>
  <p class="lead">The account-wide AgentRQ server: one connection lets your AI agent manage every workspace, task, machine and workflow on your account.</p>

  <h2>MCP server URL</h2>
  <p class="url"><code>{{.Endpoint}}</code></p>
  <p>It speaks streamable HTTP. This page is for people: MCP clients connect to the same URL with POST requests.</p>

  <h2>Sign in</h2>
  <p>The server uses OAuth 2.1 with dynamic client registration, so there is no token to copy. Your client opens a browser to sign in to AgentRQ the first time it connects.</p>

  <h2>Claude Code</h2>
  <pre>claude mcp add --transport http agentrq {{.Endpoint}}</pre>

  <h2>Claude.ai and Claude Desktop</h2>
  <p>Settings &rarr; Connectors &rarr; Add custom connector, then paste the URL above.</p>

  <h2>Codex</h2>
  <pre>codex mcp add agentrq --url {{.Endpoint}}</pre>

  <h2>Gemini CLI</h2>
  <pre>gemini mcp add --transport http agentrq {{.Endpoint}}</pre>

  <h2>VS Code and other clients</h2>
  <p>Add an HTTP server, for example in <code>.vscode/mcp.json</code>:</p>
  <pre>{
  "servers": {
    "agentrq": { "type": "http", "url": "{{.Endpoint}}" }
  }
}</pre>

  <h2>Tools</h2>
  <ul>
  {{- range .Tools}}
    <li><code>{{.Name}}</code>: {{.Summary}}</li>
  {{- end}}
  </ul>

  <h2>More</h2>
  <p><a href="https://agentrq.com">agentrq.com</a> &middot; <a href="https://github.com/agentrq/agentrq">Source and plugins</a></p>
</main>
</body>
</html>
`))
