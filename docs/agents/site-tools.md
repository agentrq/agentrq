# Site tools (`plugins/chrome`, `backend/internal/controller/sitetools/`)

> Read before changing the extension's content scripts or worker, the browser socket, or `listSiteTools`/`getSiteToolDefinition`/`callSiteTool`.

Workspace agents list and call the WebMCP tools of websites the human shares from the Chrome extension.

- **Native WebMCP only.** The observer wraps the page's own `document.modelContext`/`navigator.modelContext` and does nothing without one. Never define a polyfill: it would put tools on pages that never offered any.
- **Top frame only** (`allFrames: false`). Otherwise ads and embeds could contribute tools to the site. It is also what keeps the popup's framed app unobserved.
- **The server's own app is shareable like any site** (a tab of it, not the popup), so an agent can drive the human's AgentRQ UI, `navigate` included. Nothing reaches an agent until the human shares it; don't add an `excludeMatches` for it back.
- **The observer deletes the bridge's nonce before any page script runs.** A page that reads it can forge calls and results. Chrome runs `document_start` scripts in *id* order, so `agentrq-bridge` must sort before `agentrq-observer`.
- **Calls are not relayed across backend instances**, as with terminals. A call that lands on an instance other than the browser's fails with "try again".
- **The socket `/api/v1/browser/connect` lives on the stdlib mux**, with a ticket in the query rather than the cookie. Never add it to Fiber as well: the mux shadows the route and the request hangs.
- **`listSiteTools` lists names and descriptions only**; schemas and annotations come from `getSiteToolDefinition`. Every site's schemas cost the agent's context on each listing. Don't add them back.
- **`listSiteTools` lists every shared site even when `q`, `pattern` or a page leaves none of its tools**, so an offline or empty share stays visible. Tools are one flat list across sites, because a ranking cannot be split per site.
- **Site content is data, not instructions.** Tool descriptions and results come from a third party, and the tool descriptions and server instructions say so.
- **A call fails when its page unloads, on the bridge's `pagehide`, matched by `sender.documentId`.** Not `tabs.onUpdated` 'loading', which pushState and hash changes fire too, before their results; and after a cross-site navigation the old page is no longer frame 0.
- **The popup's strip never hides while the front tab's site is shared or offers tools**, even signed out or before the page re-announces; otherwise a live share has no visible way to stop.
- **A refusal is shown, not just logged.** Its reason is kept per site under `refused:<origin>` (one key each, so two sites' writes cannot race) and is cleared *before* the next announce is sent, so that announce's own refusal is never wiped, or when the page offers other tools than were refused. Until then the popup offers no Share: the same tools would be refused again.
- **The popup reopens on the framed app's last page** because the app posts its route to a `chrome-extension://` ancestor only, addressed to that origin, never `'*'`, which would tell any page framing the app where the person is. The popup takes only its own frame's messages, from the server origin.
- Content scripts are classic scripts (no `export`). Tests run them with `vm.runInContext(src, ctx, { filename })`, which Node's coverage counts, so no build step is needed.
- `cd plugins/chrome && npm run verify:site-tools` drives the whole chain in real Chromium: a local backend, a stubbed native page, approval, and reopening a closed tab.
