// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

// pageHTML follows the web app's sign-in card (LoginView.vue): the same
// colours, radii and logo, and the same theme choice — the one saved in the
// app, else the system's — applied before first paint.
const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Allow access to AgentRQ?</title>
<script nonce="{{.Nonce}}">
(function () {
  var theme = null
  try { theme = localStorage.getItem('theme') } catch (e) {}
  var dark = theme === 'dark' || (theme !== 'light' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)
  if (dark) document.documentElement.classList.add('dark')
})()
</script>
<style>
:root {
  --page: #f9fafb; --card: #ffffff; --card-border: #f3f4f6; --row: #f9fafb; --row-border: #f3f4f6;
  --text: #111827; --muted: #6b7280; --label: #9ca3af;
  --logo: #000000; --logo-ink: #ffffff;
  --primary: #111827; --primary-hover: #000000; --primary-text: #ffffff;
  --secondary: #ffffff; --secondary-hover: #f9fafb; --secondary-text: #374151; --secondary-border: #e5e7eb;
  --warn-bg: #fffbeb; --warn-border: #fde68a; --warn-text: #92400e;
  --ok-bg: #ecfdf5; --ok-border: #a7f3d0; --ok-text: #047857;
}
.dark {
  --page: #09090b; --card: #18181b; --card-border: #27272a; --row: rgba(39, 39, 42, 0.5); --row-border: rgba(63, 63, 70, 0.5);
  --text: #fafafa; --muted: #a1a1aa; --label: #71717a;
  --logo: #ffffff; --logo-ink: #000000;
  --primary: #fafafa; --primary-hover: #e4e4e7; --primary-text: #09090b;
  --secondary: #27272a; --secondary-hover: #3f3f46; --secondary-text: #e4e4e7; --secondary-border: #3f3f46;
  --warn-bg: rgba(120, 53, 15, 0.2); --warn-border: rgba(120, 53, 15, 0.4); --warn-text: #fcd34d;
  --ok-bg: rgba(6, 78, 59, 0.2); --ok-border: rgba(6, 78, 59, 0.5); --ok-text: #6ee7b7;
}
* { box-sizing: border-box; }
html, body { margin: 0; }
body {
  min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 16px;
  background: var(--page); color: var(--text);
  font-family: "Inter Variable", "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  -webkit-font-smoothing: antialiased;
}
.card {
  width: 100%; max-width: 28rem; padding: 32px; background: var(--card);
  border: 1px solid var(--card-border); border-radius: 24px;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
}
.logo {
  width: 64px; height: 64px; margin: 0 auto 24px; border-radius: 16px; background: var(--logo); color: var(--logo-ink);
  display: flex; align-items: center; justify-content: center; transform: rotate(3deg);
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1);
}
.logo svg { width: 40px; height: 40px; }
h1 { margin: 0 0 8px; font-size: 24px; line-height: 1.25; font-weight: 900; letter-spacing: -0.025em; text-align: center; overflow-wrap: anywhere; }
.lead { margin: 0 0 20px; font-size: 14px; line-height: 1.6; color: var(--muted); text-align: center; }
.badge {
  display: block; margin: 0 0 20px; padding: 12px 16px; border-radius: 16px; border: 1px solid;
  font-size: 13px; line-height: 1.5; font-weight: 500; overflow-wrap: anywhere;
}
.badge.warn { background: var(--warn-bg); border-color: var(--warn-border); color: var(--warn-text); }
.badge.ok { background: var(--ok-bg); border-color: var(--ok-border); color: var(--ok-text); }
.rows { margin: 0 0 20px; border: 1px solid var(--row-border); border-radius: 16px; background: var(--row); }
.row { padding: 12px 16px; }
.row + .row { border-top: 1px solid var(--row-border); }
.row dt { margin: 0 0 2px; font-size: 11px; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; color: var(--label); }
.row dd { margin: 0; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
.hint { margin: 0 0 24px; font-size: 13px; line-height: 1.6; color: var(--muted); text-align: center; }
.actions { display: flex; gap: 12px; }
button {
  flex: 1; padding: 16px 24px; border-radius: 16px; font: inherit; font-size: 15px; font-weight: 700; cursor: pointer;
  transition: background-color 0.15s, transform 0.15s;
}
button:active { transform: scale(0.98); }
button:focus-visible { outline: 3px solid var(--muted); outline-offset: 2px; }
.deny { background: var(--secondary); color: var(--secondary-text); border: 1px solid var(--secondary-border); }
.deny:hover { background: var(--secondary-hover); }
.allow { background: var(--primary); color: var(--primary-text); border: 1px solid var(--primary); }
.allow:hover { background: var(--primary-hover); }
</style>
</head>
<body>
<main class="card">
  <div class="logo" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
      <path d="M12 7l-3.5 8" /><path d="M12 7l3.5 8" /><path d="M9.5 12h5" />
    </svg>
  </div>

  <h1 data-test="consent-client">{{if .Named}}{{.ClientName}}{{else}}An unnamed app{{end}}</h1>
  <p class="lead">wants to connect to your AgentRQ account.</p>

  {{if metadataDocument .Kind}}
  <p class="badge ok" data-test="consent-identity">Identity checked: this app is published by <strong>{{.ClientHost}}</strong>.</p>
  {{else if registered .Kind}}
  <p class="badge warn" data-test="consent-identity">Unverified name. Any app can call itself anything, so check where it sends you back.</p>
  {{else}}
  <p class="badge warn" data-test="consent-identity">Unknown app. AgentRQ has no record of who made it, so check where it sends you back.</p>
  {{end}}

  <dl class="rows">
    <div class="row"><dt>It will be able to</dt><dd data-test="consent-access">{{.Access}}</dd></div>
    <div class="row"><dt>Sends you back to</dt><dd data-test="consent-destination">{{.Destination}}</dd></div>
    {{if .Email}}<div class="row"><dt>Signed in as</dt><dd data-test="consent-account">{{.Email}}</dd></div>{{end}}
  </dl>

  <p class="hint">Only allow an app you just asked to connect. If you didn’t, deny.</p>

  <form method="post" class="actions">
    <input type="hidden" name="consent" value="{{.Token}}">
    <button type="submit" name="decision" value="deny" class="deny" data-test="consent-deny">Deny</button>
    <button type="submit" name="decision" value="allow" class="allow" data-test="consent-allow">Allow</button>
  </form>
</main>
</body>
</html>
`
