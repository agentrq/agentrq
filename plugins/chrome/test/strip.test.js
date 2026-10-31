// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { install } from '../src/extension.js'
import { ALL_SITES } from '../src/settings.js'
import { initStrip } from '../src/strip.js'
import { fakeChrome, fakeTimers, fakeWebSocketClass, settle } from './fake-chrome.js'
import { fakeDocument } from './fake-document.js'

const STRIP_IDS = ['strip', 'strip-text', 'strip-workspace', 'strip-action']
const SERVER = 'https://app.agentrq.com'
const GH = 'https://github.com'
const hi = { name: 'getGreeting', description: 'Say hi', annotations: { readOnlyHint: true } }
const del = { name: 'deleteThing', description: 'Delete it' }
const WORKSPACES = [
  { id: 'ws2', name: 'Zeta' },
  { id: 'ws1', name: 'Alpha' },
]

// The strip against the real worker, on one fake browser whose front tab is
// `tab` and whose page there offered `tools`.
async function open({ granted = ALL_SITES, allow = true, tab = { id: 7, url: `${GH}/me` }, tools = [hi, del], signedIn = true, workspaces = WORKSPACES, shares } = {}) {
  const chrome = fakeChrome({ granted: [`${SERVER}/*`, ...granted], allow })
  if (shares) await chrome.storage.local.set({ shares })
  chrome.tabs.active = tab
  const fetchImpl = async (url) => {
    if (url.endsWith('/workspaces')) return { status: signedIn ? 200 : 401, ok: signedIn, json: async () => ({ workspaces }) }
    if (url.endsWith('/auth/refresh')) return { status: 401, ok: false }
    return { status: 200, ok: true, json: async () => ({ ticket: 'tk' }) }
  }
  install(chrome, { error() {} }, { fetchImpl, WebSocketImpl: fakeWebSocketClass(), timers: fakeTimers() })
  if (tools) chrome.runtime.onMessage.fire({ type: 'site-tools', tools }, { tab: { id: 7 }, frameId: 0, url: `${GH}/me` })
  await settle()
  const doc = fakeDocument(STRIP_IDS)
  await initStrip({ doc, chrome, fetchImpl, server: SERVER })
  const el = doc.elements
  return { chrome, el, text: el['strip-text'], picker: el['strip-workspace'], action: el['strip-action'] }
}

const click = async (el) => {
  el.events.click()
  await settle()
}

test('with detection off it offers to turn it on, and asks Chrome only in the click', async () => {
  const { chrome, el, text, picker, action } = await open({ granted: [] })
  assert.equal(el.strip.hidden, false)
  assert.equal(text.textContent, 'Let AgentRQ notice websites that offer tools to agents')
  assert.equal(action.textContent, 'Turn on')
  assert.equal(picker.hidden, true)
  assert.ok(!chrome.calls.some(([name]) => name === 'permissions.request'), 'nothing asked before the click')

  // Synchronously, before anything is awaited: Chrome drops a prompt that
  // comes after the gesture.
  el['strip-action'].events.click()
  assert.deepEqual(chrome.calls.at(-1), ['permissions.request', ALL_SITES])
  await settle()
  assert.match(text.textContent, /^On\./)
  assert.equal(action.hidden, true)
})

test('refusing detection leaves the offer up', async () => {
  const { text, action } = await open({ granted: [], allow: false })
  await click(action)
  assert.equal(text.textContent, 'Let AgentRQ notice websites that offer tools to agents')
  assert.equal(action.hidden, false)
})

test('a site with tools can be shared with a chosen workspace, and stopped', async () => {
  const { chrome, el, text, picker, action } = await open()
  assert.equal(el.strip.hidden, false)
  assert.equal(text.textContent, 'github.com offers 2 WebMCP tools · Share with')
  assert.equal(picker.hidden, false)
  assert.deepEqual(
    picker.children.map((o) => [o.tagName, o.value, o.textContent]),
    [
      ['option', 'ws1', 'Alpha'],
      ['option', 'ws2', 'Zeta'],
    ],
  )
  assert.equal(action.textContent, 'Share')

  picker.value = 'ws2'
  await click(action)
  const { shares } = chrome.storage.local.data
  assert.equal(shares[GH].workspaceId, 'ws2')
  assert.equal(shares[GH].lastUrl, `${GH}/me`)
  assert.deepEqual(shares[GH].tools, [hi, del])
  assert.equal(text.textContent, 'Shared with Zeta')
  assert.equal(picker.hidden, true)
  assert.equal(action.textContent, 'Stop sharing')

  await click(action)
  assert.deepEqual(chrome.storage.local.data.shares, {})
  assert.equal(text.textContent, 'github.com offers 2 WebMCP tools · Share with')
})

test('one tool is one tool', async () => {
  const { text } = await open({ tools: [hi] })
  assert.equal(text.textContent, 'github.com offers 1 WebMCP tool · Share with')
})

test('no strip when the front tab offers nothing', async () => {
  for (const options of [{ tools: null }, { tools: [] }, { tab: null }, { tab: { id: 7, url: 'https://example.com/' } }]) {
    const { el } = await open(options)
    assert.equal(el.strip.hidden, true, JSON.stringify(options))
  }
})

test('signed out, a site cannot be shared, but its tools and a share still show', async () => {
  const offered = await open({ signedIn: false })
  assert.equal(offered.el.strip.hidden, false)
  assert.equal(offered.text.textContent, 'github.com offers 2 WebMCP tools · Sign in to AgentRQ to share it')
  assert.equal(offered.action.hidden, true)
  assert.equal(offered.picker.hidden, true)

  const shares = { [GH]: { workspaceId: 'gone', lastUrl: `${GH}/me`, tools: [hi], alwaysAllow: [], pending: false } }
  const { chrome, text, action } = await open({ signedIn: false, shares })
  assert.equal(text.textContent, 'Shared with a workspace')
  await click(action)
  assert.deepEqual(chrome.storage.local.data.shares, {})
})

test('a worker that does not answer shows no strip', async () => {
  const chrome = fakeChrome({ granted: [`${SERVER}/*`, ...ALL_SITES] })
  chrome.runtime.sendMessage = async () => {
    throw new Error('Could not establish connection. Receiving end does not exist.')
  }
  const doc = fakeDocument(STRIP_IDS)
  await initStrip({ doc, chrome, fetchImpl: async () => {}, server: SERVER })
  assert.equal(doc.elements.strip.hidden, true)
})

test('with no workspace to share with, a site with tools still shows', async () => {
  const { el, text, action } = await open({ workspaces: [] })
  assert.equal(el.strip.hidden, false)
  assert.equal(text.textContent, 'github.com offers 2 WebMCP tools · Create a workspace to share it')
  assert.equal(action.hidden, true)
})

test('a shared site keeps its strip while its page offers nothing yet', async () => {
  const shares = { [GH]: { workspaceId: 'ws1', lastUrl: `${GH}/me`, tools: [hi], alwaysAllow: [], pending: false } }
  for (const tools of [null, []]) {
    const { chrome, el, text, action } = await open({ tools, shares })
    assert.equal(el.strip.hidden, false, JSON.stringify(tools))
    assert.equal(text.textContent, 'Shared with Alpha')
    await click(action)
    assert.deepEqual(chrome.storage.local.data.shares, {})
    // Stopped, a page with nothing to offer has nothing to say.
    assert.equal(el.strip.hidden, true)
  }
})

const refuse = (chrome, error) => chrome.storage.local.set({ [`refused:${GH}`]: { error, tools: [hi, del] } })

test('a refusal turns the strip into a warning that says why, as soon as it arrives, with no Share to repeat it', async () => {
  const { chrome, el, text, picker, action } = await open()
  assert.equal(el.strip.className, '')

  await click(action)
  assert.equal(text.textContent, 'Shared with Alpha')
  // The server refuses; the worker drops the pending share and keeps the reason.
  await chrome.storage.local.set({ shares: {} })
  await refuse(chrome, 'the description of "x" is over 1024 bytes')
  await settle()
  assert.equal(el.strip.className, 'warn')
  assert.equal(text.textContent, 'github.com could not be shared: the description of "x" is over 1024 bytes')
  assert.equal(action.hidden, true)
  assert.equal(picker.hidden, true)

  // Other changes do not re-render it.
  await chrome.storage.local.set({ unrelated: 1 })
  await chrome.storage.sync.set({ [`refused:${GH}`]: 'elsewhere' })
  await settle()
  assert.match(text.textContent, /could not be shared/)

  // Cleared (the site's tools changed), Share is back.
  await chrome.storage.local.remove(`refused:${GH}`)
  await settle()
  assert.equal(el.strip.className, '')
  assert.equal(text.textContent, 'github.com offers 2 WebMCP tools · Share with')
  assert.equal(action.textContent, 'Share')
})

test('a refused update of a shared site warns, and keeps the way to stop it', async () => {
  const shares = { [GH]: { workspaceId: 'ws1', lastUrl: `${GH}/me`, tools: [hi], alwaysAllow: [], pending: false } }
  const { chrome, el, text, action } = await open({ shares, tools: null })
  await refuse(chrome, 'too many tools')
  await settle()
  assert.equal(el.strip.className, 'warn')
  assert.equal(text.textContent, 'Shared with Alpha · Its latest tools were refused: too many tools')
  assert.equal(action.textContent, 'Stop sharing')
})

test('a page that finishes registering its tools while the popup is open brings the strip up, and unloading hides it', async () => {
  const { chrome, el, text } = await open({ tools: null })
  assert.equal(el.strip.hidden, true)

  chrome.runtime.onMessage.fire({ type: 'site-tools', tools: [hi, del] }, { tab: { id: 7 }, frameId: 0, url: `${GH}/me`, documentId: 'doc7' })
  await settle()
  assert.equal(el.strip.hidden, false)
  assert.equal(text.textContent, 'github.com offers 2 WebMCP tools · Share with')

  chrome.runtime.onMessage.fire({ type: 'site-gone' }, { tab: { id: 7 }, frameId: 0, documentId: 'doc7' })
  await settle()
  assert.equal(el.strip.hidden, true)
})
