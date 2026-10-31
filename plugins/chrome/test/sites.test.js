// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { install } from '../src/extension.js'
import { lastRefusal, listShares, reconcile, share, unshare } from '../src/shares.js'
import { ALL_SITES, TOOL_WAIT } from '../src/sites.js'
import { fakeChrome, fakeTimers, fakeWebSocketClass, settle } from './fake-chrome.js'

const GH = 'https://github.com'
const hi = { name: 'getGreeting', description: 'Say hi', annotations: { readOnlyHint: true } }
const del = { name: 'deleteThing', description: 'Delete it' }

const fetchImpl = async () => ({ status: 200, ok: true, json: async () => ({ ticket: 'tk' }) })

// The worker, started on a browser that may already hold shares and grants.
const start = async ({ shares = {}, granted = [], registered = [] } = {}) => {
  const chrome = fakeChrome({ granted: ['https://app.agentrq.com/*', ...granted] })
  chrome.scripting.registered.push(...registered)
  if (Object.keys(shares).length) await chrome.storage.local.set({ shares })
  const timers = fakeTimers()
  const WebSocketImpl = fakeWebSocketClass()
  const errors = []
  const { tabs } = install(chrome, { error: (...args) => errors.push(args) }, { fetchImpl, WebSocketImpl, timers })
  await settle()
  const sockets = WebSocketImpl.sockets
  const ws = () => sockets.at(-1)
  const sent = (type) => ws().sent.filter((f) => f.type === type)
  // A page's bridge telling the worker what the page offers.
  const page = (tabId, url, tools, frameId = 0, documentId = `doc${tabId}`) =>
    chrome.runtime.onMessage.fire({ type: 'site-tools', tools }, { tab: { id: tabId }, frameId, url, documentId })
  const gone = (tabId, frameId = 0, documentId = `doc${tabId}`) =>
    chrome.runtime.onMessage.fire({ type: 'site-gone' }, { tab: { id: tabId }, frameId, documentId })
  const answer = (tabId, message) => chrome.runtime.onMessage.fire({ type: 'site-result', ...message }, { tab: { id: tabId }, frameId: 0 })
  return { chrome, timers, tabs, sockets, ws, sent, page, gone, answer, errors }
}

const stored = { [GH]: { workspaceId: 'ws1', lastUrl: `${GH}/me`, tools: [hi], alwaysAllow: [], pending: false } }

test('with nothing shared the worker opens no socket, and without all-sites access registers no scripts', async () => {
  const { chrome, sockets, page } = await start()
  page(1, `${GH}/x`, [hi])
  await settle()
  assert.equal(sockets.length, 0)
  assert.deepEqual(chrome.scripting.registered, [])
})

// Review Focus 2: Chrome restarted the worker while sites were shared.
test('a restarted worker reconnects and re-announces every share from storage', async () => {
  const { ws, sent } = await start({ shares: { ...stored, 'https://b.com': { workspaceId: 'ws2', lastUrl: 'https://b.com/' } } })
  ws().open()
  await settle()
  assert.deepEqual(sent('announce'), [
    { type: 'announce', origin: GH, workspaceId: 'ws1', lastUrl: `${GH}/me`, tools: [hi] },
    { type: 'announce', origin: 'https://b.com', workspaceId: 'ws2', lastUrl: 'https://b.com/', tools: [] },
  ])
})

test('content scripts are registered, bridge first, only while every site may be read', async () => {
  const old = { id: 'agentrq-bridge', js: ['src/bridge.js'] }
  const { chrome } = await start({ granted: ALL_SITES, registered: [old] })
  assert.deepEqual(
    chrome.scripting.registered.map(({ id, js, world }) => [id, js[0], world]),
    [
      ['agentrq-bridge', 'src/bridge.js', 'ISOLATED'],
      ['agentrq-observer', 'src/observer.js', 'MAIN'],
    ],
  )
  for (const s of chrome.scripting.registered) {
    assert.deepEqual([s.runAt, s.allFrames, s.matches], ['document_start', false, ['https://*/*', 'http://localhost/*']])
    // The server's own app is a site like any other.
    assert.equal(s.excludeMatches, undefined)
  }

  for (const o of ALL_SITES) chrome.permissions.granted.delete(o)
  chrome.permissions.onRemoved.fire({ origins: ALL_SITES })
  await settle()
  assert.deepEqual(chrome.scripting.registered, [])

  for (const o of ALL_SITES) chrome.permissions.granted.add(o)
  chrome.permissions.onAdded.fire({ origins: ALL_SITES })
  await settle()
  assert.equal(chrome.scripting.registered.length, 2)
})

test('a failed registration is logged and does not stop the next', async () => {
  const { chrome, errors } = await start()
  chrome.permissions.contains = async () => {
    throw new Error('no permissions api')
  }
  chrome.permissions.onAdded.fire({})
  await settle()
  chrome.permissions.contains = async () => true
  chrome.permissions.onAdded.fire({})
  await settle()
  assert.match(String(errors[0][1]), /no permissions api/)
  assert.equal(chrome.scripting.registered.length, 2)
})

test('a page’s tools set its badge; a shared site’s are remembered and announced', async () => {
  const { chrome, ws, sent, page } = await start({ shares: stored })
  ws().open()
  await settle()
  page(7, `${GH}/repo`, [hi, del])
  page(8, 'https://other.com/', [hi])
  page(9, `${GH}/ad`, [del], 3)
  chrome.runtime.onMessage.fire({ type: 'site-tools', tools: [] }, { frameId: 0, url: GH })
  chrome.runtime.onMessage.fire({ type: 'popup-state' }, { tab: { id: 7 }, frameId: 0, url: GH })
  chrome.runtime.onMessage.fire({ type: 'site-tools' }, { tab: { id: 10 }, frameId: 0, url: 'https://none.com/' })
  await settle()
  assert.deepEqual([chrome.action.badges.get(7).text, chrome.action.badges.get(8).text, chrome.action.badges.get(10).text], ['2', '1', ''])
  assert.equal(chrome.action.badges.has(9), false)
  assert.deepEqual(sent('announce').at(-1), { type: 'announce', origin: GH, workspaceId: 'ws1', lastUrl: `${GH}/repo`, tools: [hi, del] })
  assert.equal(sent('announce').filter((f) => f.origin !== GH).length, 0)
  assert.deepEqual((await listShares(chrome))[GH].tools, [hi, del])
})

test('sharing opens the socket; moving announces; stopping withdraws, and the last one closes it', async () => {
  const { chrome, sockets, ws, sent, page } = await start()
  page(7, `${GH}/repo`, [hi])
  await settle()
  await share(chrome, GH, 'ws1', `${GH}/repo`)
  await settle()
  assert.equal(sockets.length, 1)
  ws().open()
  await settle()
  assert.deepEqual(sent('announce'), [{ type: 'announce', origin: GH, workspaceId: 'ws1', lastUrl: `${GH}/repo`, tools: [hi] }])

  await share(chrome, 'https://b.com', 'ws1', 'https://b.com/')
  await share(chrome, GH, 'ws2', `${GH}/repo`)
  await chrome.storage.local.set({ other: 1 })
  await settle()
  assert.deepEqual(sent('announce').map((f) => [f.origin, f.workspaceId]).slice(1), [['https://b.com', 'ws1'], [GH, 'ws2']])

  await unshare(chrome, GH)
  assert.deepEqual(sent('withdraw'), [{ type: 'withdraw', origin: GH }])
  assert.equal(ws().closed, undefined)
  await unshare(chrome, 'https://b.com')
  assert.equal(ws().closed, true)
})

test('the server’s list reconciles the shares, and a refusal of a new share drops it', async () => {
  const { chrome, ws, errors } = await start({ shares: { ...stored, 'https://gone.com': { workspaceId: 'ws9', lastUrl: 'https://gone.com' } } })
  ws().open()
  ws().receive({ type: 'shares', shares: [{ origin: GH, workspaceId: 'ws1', alwaysAllow: ['deleteThing'] }] })
  await settle()
  assert.deepEqual(Object.keys(await listShares(chrome)), [GH])
  assert.deepEqual((await listShares(chrome))[GH].alwaysAllow, ['deleteThing'])

  await share(chrome, 'https://new.com', 'ws-deleted', 'https://new.com/')
  ws().receive({ type: 'refused', origin: 'https://new.com', error: 'no such workspace' })
  ws().receive({ type: 'something-newer' })
  await settle()
  assert.deepEqual(Object.keys(await listShares(chrome)), [GH])
  assert.match(String(errors.at(-1)[0]), /refused https:\/\/new.com: no such workspace/)
  assert.equal(await lastRefusal(chrome, 'https://new.com'), 'no such workspace')

  // Sharing again announces again, and that clears the old reason.
  await share(chrome, 'https://new.com', 'ws1', 'https://new.com/')
  await settle()
  assert.equal(await lastRefusal(chrome, 'https://new.com'), null)
})

test('a refusal is kept with the refused tools, and the page offering others clears it', async () => {
  const { chrome, ws, page } = await start()
  page(3, `${GH}/me`, [hi])
  await settle()
  await share(chrome, GH, 'ws1', `${GH}/me`)
  await settle()
  ws().open()
  await settle()
  ws().receive({ type: 'refused', origin: GH, error: 'too long' })
  await settle()
  assert.deepEqual(chrome.storage.local.data[`refused:${GH}`], { error: 'too long', tools: [hi] })

  page(3, `${GH}/me`, [hi])
  await settle()
  assert.equal(await lastRefusal(chrome, GH), 'too long', 'the same tools would be refused again')
  page(3, `${GH}/me`, [hi, del])
  await settle()
  assert.equal(await lastRefusal(chrome, GH), null)
})

test('the popup is told why the server refused the front tab’s site', async () => {
  const { chrome, page } = await start({ shares: stored })
  chrome.tabs.active = { id: 3, url: `${GH}/me` }
  page(3, `${GH}/me`, [hi])
  await settle()
  await chrome.storage.local.set({ [`refused:${GH}`]: { error: 'too many tools', tools: [hi] } })
  assert.equal((await chrome.runtime.sendMessage({ type: 'popup-state' })).refusal, 'too many tools')
})

const call = { type: 'call', callId: 'c1', origin: GH, tool: 'getGreeting', arguments: { who: 'me' } }

test('a call runs in the site’s most recently used tab and its result goes back', async () => {
  const { chrome, ws, sent, page, answer } = await start({ shares: stored })
  ws().open()
  page(7, `${GH}/a`, [hi])
  page(8, `${GH}/b`, [hi])
  await settle()
  chrome.tabs.onActivated.fire({ tabId: 7 })
  ws().receive(call)
  await settle()
  assert.deepEqual(chrome.tabs.sent, [[7, { type: 'site-call', callId: 'c1', tool: 'getGreeting', arguments: { who: 'me' } }]])
  answer(7, { callId: 'c1', text: 'hi' })
  await settle()
  assert.deepEqual(sent('result'), [{ type: 'result', callId: 'c1', text: 'hi' }])

  ws().receive({ ...call, callId: 'c2' })
  await settle()
  answer(7, { callId: 'c2', error: 'the page withdrew getGreeting' })
  ws().receive({ ...call, callId: 'c3' })
  await settle()
  answer(7, { callId: 'c3' })
  await settle()
  assert.deepEqual(sent('result').slice(1), [
    { type: 'result', callId: 'c2', error: 'the page withdrew getGreeting' },
    { type: 'result', callId: 'c3', text: '' },
  ])
})

test('with no tab of the site open, its last page opens in the background and the call waits for the tool', async () => {
  const { chrome, ws, sent, page, answer } = await start({ shares: stored })
  ws().open()
  ws().receive(call)
  await settle()
  assert.deepEqual(chrome.calls.filter(([n]) => n === 'tabs.create'), [['tabs.create', { url: `${GH}/me`, active: false }]])
  page(12, `${GH}/me`, [hi])
  await settle()
  assert.equal(chrome.tabs.sent[0][0], 12)
  answer(12, { callId: 'c1', text: 'hi' })
  await settle()
  assert.deepEqual(sent('result'), [{ type: 'result', callId: 'c1', text: 'hi' }])
})

test('a page navigating away fails its call at once, and the next call waits for the new page', async () => {
  const { chrome, ws, sent, page, gone, answer } = await start({ shares: stored })
  ws().open()
  page(7, `${GH}/a`, [hi])
  await settle()
  ws().receive(call)
  await settle()
  // A cross-site navigation: the old page is no longer frame 0 when it unloads.
  gone(7, 4)
  await settle()
  assert.deepEqual(sent('result'), [{ type: 'result', callId: 'c1', error: 'the https://github.com page navigated away during the call' }])
  assert.equal(chrome.action.badges.get(7).text, '')

  // The share keeps its last-seen tools for announcing.
  assert.deepEqual((await listShares(chrome))[GH].tools, [hi])
  page(7, `${GH}/b`, [hi], 0, 'doc7b')
  await settle()
  ws().receive({ ...call, callId: 'c2' })
  await settle()
  assert.equal(chrome.tabs.sent.at(-1)[0], 7)
  answer(7, { callId: 'c2', text: 'hi' })
  await settle()
  assert.deepEqual(sent('result').at(-1), { type: 'result', callId: 'c2', text: 'hi' })
})

test('every way a call fails is reported to the server', async () => {
  const { chrome, timers, ws, sent, page } = await start({ shares: stored })
  ws().open()

  // No tab registers the tool in time; a site no longer shared opens at its origin.
  ws().receive({ ...call, origin: 'https://unshared.com' })
  await settle()
  assert.deepEqual(chrome.calls.at(-1), ['tabs.create', { url: 'https://unshared.com', active: false }])
  timers.advance(TOOL_WAIT)
  await settle()

  // Review Focus 4: the tab is closed during the call.
  page(7, `${GH}/a`, [hi])
  page(8, `${GH}/b`, [hi])
  await settle()
  ws().receive({ ...call, callId: 'c2' })
  await settle()
  const [busy] = chrome.tabs.sent.at(-1)
  chrome.tabs.onRemoved.fire(busy)

  // The page is gone before the message arrives.
  chrome.tabs.sendMessage = async () => {
    throw new Error('Could not establish connection. Receiving end does not exist.')
  }
  ws().receive({ ...call, callId: 'c3' })

  // Opening a tab fails.
  chrome.tabs.create = async () => {
    throw new Error('Tabs cannot be edited right now')
  }
  ws().receive({ ...call, callId: 'c4', tool: 'deleteThing' })
  await settle()

  assert.deepEqual(sent('result'), [
    {
      type: 'result',
      callId: 'c1',
      error: 'no https://unshared.com tab registered getGreeting within 20 seconds (the site may have changed, or you may be signed out of it)',
    },
    { type: 'result', callId: 'c2', error: 'the https://github.com tab was closed during the call' },
    { type: 'result', callId: 'c3', error: 'Could not establish connection. Receiving end does not exist.' },
    { type: 'result', callId: 'c4', error: 'Tabs cannot be edited right now' },
  ])
})

test('reconcile emptying the shares closes the socket', async () => {
  const { chrome, ws } = await start({ shares: stored })
  ws().open()
  await reconcile(chrome, [])
  assert.equal(ws().closed, true)
})

test('shares removed from storage altogether withdraw and close too', async () => {
  const { chrome, ws, sent } = await start({ shares: stored })
  ws().open()
  chrome.storage.onChanged.fire({ shares: { oldValue: stored } }, 'local')
  assert.deepEqual(sent('withdraw'), [{ type: 'withdraw', origin: GH }])
  assert.equal(ws().closed, true)
})

test('the popup is told what the front tab offers and who it is shared with', async () => {
  const { chrome, page } = await start({ shares: stored })
  const ask = () => chrome.runtime.sendMessage({ type: 'popup-state' })
  assert.deepEqual(await ask(), { origin: null, url: null, tools: [], toolCount: 0, sharedWith: null, refusal: null })

  chrome.tabs.active = { id: 3, url: `${GH}/me` }
  page(3, `${GH}/me`, [hi, del])
  await settle()
  assert.deepEqual(await ask(), { origin: GH, url: `${GH}/me`, tools: [hi, del], toolCount: 2, sharedWith: 'ws1', refusal: null })

  page(4, 'https://b.com/', [del])
  chrome.tabs.active = { id: 4, url: 'https://b.com/' }
  assert.deepEqual(await ask(), { origin: 'https://b.com', url: 'https://b.com/', tools: [del], toolCount: 1, sharedWith: null, refusal: null })

  // The tab has moved on to a page that has not said anything.
  chrome.tabs.active = { id: 4, url: 'https://c.com/' }
  assert.equal((await ask()).origin, null)

  // A shared site is still reported while its page offers nothing.
  chrome.tabs.active = { id: 4, url: `${GH}/other` }
  assert.deepEqual(await ask(), { origin: GH, url: `${GH}/other`, tools: [], toolCount: 0, sharedWith: 'ws1', refusal: null })
  chrome.tabs.active = { id: 9, url: `${GH}/new` }
  assert.deepEqual(await ask(), { origin: GH, url: `${GH}/new`, tools: [], toolCount: 0, sharedWith: 'ws1', refusal: null })
  // A tab whose URL Chrome withholds.
  chrome.tabs.active = { id: 9 }
  assert.equal((await ask()).origin, null)

  // After a worker restart the tab's bridge still holds its tools, and answers when the popup asks.
  chrome.tabs.active = { id: 11, url: 'https://fresh.com/page' }
  chrome.tabs.sendMessage = async (tabId, message) => (tabId === 11 && message.type === 'site-tools' ? { type: 'site-tools', tools: [hi] } : null)
  assert.deepEqual(await ask(), { origin: 'https://fresh.com', url: 'https://fresh.com/page', tools: [hi], toolCount: 1, sharedWith: null, refusal: null })

  // A tab with no bridge listening (e.g. chrome://) rejects sendMessage and offers nothing.
  chrome.tabs.active = { id: 12, url: 'https://quiet.com/' }
  chrome.tabs.sendMessage = async () => {
    throw new Error('Could not establish connection.')
  }
  assert.equal((await ask()).origin, null)
})

test('a website’s page cannot ask for the popup’s state, and a failure to find the front tab answers null', async () => {
  const { chrome, errors } = await start()
  const fromPage = (url) => chrome.runtime.onMessage.fire({ type: 'popup-state' }, { tab: { id: 1 }, frameId: 0, url })[0]
  assert.equal(fromPage(`${GH}/`), undefined)
  assert.equal(fromPage(undefined), undefined)
  // The popup's page opened in a tab still has the extension's URL.
  assert.equal(fromPage(chrome.runtime.getURL('src/popup.html')), true)

  chrome.tabs.query = async () => {
    throw new Error('no window')
  }
  assert.equal(await chrome.runtime.sendMessage({ type: 'popup-state' }), null)
  assert.match(String(errors.at(-1)), /no window/)
})
