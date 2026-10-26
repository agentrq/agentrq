// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// WP9's acceptance, run in the main webview through AGENTRQ_E2E_INIT_SCRIPT
// with __EXAMPLES__ filled in: an install refused for want of Node.js, Node
// downloaded from nodejs.org as the page's prompt does, then the five examples
// installed, listed, configured, turned off and on, and uninstalled through
// window.agentrq, as the Extensions page does. On the way the guardrail
// refuses a tool call the server's stream says is waiting, and the supervisor
// is authorised through the app's OAuth window, which catches the server's
// redirect to agentrq://. Each step goes to the server, which is
// extensions-server.mjs, through the app's own proxy.
const EXAMPLES = '__EXAMPLES__'
const NAMES = ['task-stats', 'standup', 'digest', 'guardrail', 'panel-notes']
const GRANTS = {
  standup: { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: ['workspace.get', 'memory.load', 'task.get'], supervisor: [] } },
  digest: { scope: 'supervisor', workspaces: [], tools: { workspace: [], supervisor: ['workspace.list', 'task.listAll', 'task.create'] } },
  guardrail: { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: [], supervisor: [] }, hooks: { toolCall: 'deny' } },
}

const report = (step, value) =>
  fetch('/api/v1/__acceptance', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ step, value }),
  })

/** Reported, then held to `expected`: a step that is wrong fails the run. */
async function step(name, value, expected) {
  await report(name, value)
  if (!expected(value)) throw new Error(`${name}: ${JSON.stringify(value)}`)
  return value
}

const ok = (answer) => answer?.ok === true

/** What `read` resolves to once it is something, or null after `ms`. */
async function eventually(read, ms = 60000) {
  const until = Date.now() + ms
  while (Date.now() < until) {
    const value = await read()
    if (value) return value
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  return null
}

async function run() {
  const ext = window.agentrq.extensions
  await step('node missing', (await ext.state()).node, (node) => node?.installed === false && node.size > 0)
  await step('install before node', await ext.installLocal(`${EXAMPLES}/${NAMES[0]}`, { grant: null }), (answer) =>
    answer?.needsNode === true,
  )
  await step('download node', await ext.downloadNode(), ok)
  await step('node there', (await ext.state()).node, (node) => node?.installed === true)
  for (const name of NAMES) {
    await step(`install ${name}`, await ext.installLocal(`${EXAMPLES}/${name}`, { grant: GRANTS[name] ?? null }), ok)
  }
  const listed = (await ext.state()).installed.map((i) => i.name)
  await step('listed', listed, (names) => names.length === NAMES.length && NAMES.every((n) => names.includes(n)))
  await step('configure digest', await ext.configure('digest', { workspaceId: 'ws1', cron: '0 6 * * *' }), ok)
  // Sealed with a keychain, refused without one: never written in the clear.
  await step('configure a secret', await ext.configure('digest', { webhookToken: 'hunter2' }), (answer) =>
    ok(answer) || /no secure storage/.test(answer?.reason),
  )
  await step('disable standup', await ext.setEnabled('standup', false), ok)
  await step('pages without standup', (await ext.entries('page')).map((e) => e.owner), (owners) => !owners.includes('standup'))
  await step('enable standup', await ext.setEnabled('standup', true), ok)
  await step('pages with standup', (await ext.entries('page')).map((e) => e.owner), (owners) => owners.includes('standup'))
  await step('panel pages', await window.agentrq.sidePanel.pages(), (pages) => pages[0]?.owner === 'panel-notes')
  await step('a drawer nobody has', await ext.drawer('vega'), (answer) => answer.ok === false)
  const frame = await fetch('/__agentrq/drawer-frame.html')
  await step('drawer frame', [frame.status, frame.headers.get('content-security-policy')], ([status, csp]) =>
    status === 200 && /script-src 'nonce-/.test(csp),
  )
  await report('guardrail ready', true)
  const verdict = await eventually(async () => (await fetch('/api/v1/__acceptance/verdict')).json())
  await step('guardrail denies the tool call', verdict, (v) =>
    v?.requestId === 'r1' && v.behavior === 'deny' && v.decidedBy === 'guardrail',
  )
  await step('supervisor before', await ext.supervisor(), (s) => s?.authorized === false)
  await step('authorise the supervisor', await ext.authorize(), ok)
  await step('supervisor after', await ext.supervisor(), (s) => s?.authorized === true)
  await step('deauthorise', await ext.deauthorize(), ok)
  for (const name of NAMES) await step(`uninstall ${name}`, await ext.uninstall(name), ok)
  await step('listed after', (await ext.state()).installed, (installed) => installed.length === 0)
  await report('done', true)
}

document.addEventListener('DOMContentLoaded', () => {
  run().catch((error) => report('failed', String(error?.stack ?? error)))
})
