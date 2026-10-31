// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { fakeChrome } from './fake-chrome.js'
import { channel, fakeModelContext, fakePage, runScript, settle } from './fake-page.js'

const NONCE = 'the-nonce'

/** A tab with the bridge installed, and the observer's end of its channel. */
function bridged(chrome = fakeChrome()) {
  const page = fakePage()
  runScript('bridge.js', page, { chrome, crypto: { randomUUID: () => NONCE } })
  const observer = channel(page, NONCE, 'agentrq-bridge')
  const post = (message, type) => observer.send({ source: 'agentrq-observer', ...message }, type)
  const sent = () => chrome.calls.filter(([name]) => name === 'runtime.sendMessage').map(([, message]) => message)
  return { page, chrome, observer, post, sent }
}

test('the bridge leaves a fresh nonce for the observer', () => {
  const { page } = bridged()
  assert.equal(page.document.documentElement.dataset.agentrqNonce, NONCE)
})

test('the page’s tools go to the service worker', () => {
  const { post, sent } = bridged()
  post({ type: 'tools', tools: [{ name: 'search' }] })
  assert.deepEqual(sent(), [{ type: 'site-tools', tools: [{ name: 'search' }] }])
})

test('results go to the service worker, answers and errors alike', () => {
  const { post, sent } = bridged()
  post({ type: 'result', callId: 'c1', text: 'found' })
  post({ type: 'result', callId: 'c2', error: 'signed out' })
  assert.deepEqual(sent(), [
    { type: 'site-result', callId: 'c1', text: 'found' },
    { type: 'site-result', callId: 'c2', error: 'signed out' },
  ])
})

test('the page unloading tells the service worker; a return from the back/forward cache or prerender activation re-announces its tools', () => {
  const { page, post, sent } = bridged()
  const show = (persisted) => page.window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted }))
  // Loaded afresh, activated or restored before it had announced anything.
  show(false)
  show(true)
  page.document.dispatchEvent(new Event('prerenderingchange'))
  page.window.dispatchEvent(new Event('pagehide'))
  post({ type: 'tools', tools: [{ name: 'search' }] })
  page.window.dispatchEvent(new Event('pagehide'))
  show(true)
  page.document.dispatchEvent(new Event('prerenderingchange'))
  assert.deepEqual(sent(), [
    { type: 'site-gone' },
    { type: 'site-tools', tools: [{ name: 'search' }] },
    { type: 'site-gone' },
    { type: 'site-tools', tools: [{ name: 'search' }] },
    { type: 'site-tools', tools: [{ name: 'search' }] },
  ])
})

test('the service worker can ask the bridge for the page’s tools, and a call goes to the observer', () => {
  const { chrome, post, observer } = bridged()
  const replies = []
  const collect = (tools) => replies.push(structuredClone(tools))
  chrome.runtime.onMessage.fire({ type: 'site-tools' }, {}, collect)
  post({ type: 'tools', tools: [{ name: 'search' }] })
  chrome.runtime.onMessage.fire({ type: 'site-tools' }, {}, collect)
  assert.deepEqual(replies, [null, { type: 'site-tools', tools: [{ name: 'search' }] }])

  chrome.runtime.onMessage.fire({ type: 'site-call', callId: 'c1', tool: 'search', arguments: { q: 'x' } })
  assert.deepEqual(observer.received, [{ source: 'agentrq-bridge', type: 'call', callId: 'c1', tool: 'search', arguments: { q: 'x' } }])
})

test('other extension messages are not calls', () => {
  const { chrome, observer } = bridged()
  chrome.runtime.onMessage.fire({ type: 'something-else' })
  chrome.runtime.onMessage.fire(undefined)
  assert.deepEqual(observer.received, [])
})

test('only the observer, under the nonce, with a known type, is relayed', () => {
  const { page, observer, post, sent } = bridged()
  post({ type: 'tools', tools: [] }, 'wrong-nonce')
  observer.send({ source: 'the-page', type: 'tools', tools: [] })
  post({ type: 'constructor' })
  post({ type: 'call', callId: 'c1', tool: 't' })
  page.document.dispatchEvent(new CustomEvent(NONCE, { detail: 'not json' }))
  // Its own call, echoed back to it by the shared document.
  observer.send({ source: 'agentrq-bridge', type: 'result', callId: 'c1', text: 'x' })
  assert.deepEqual(sent(), [])
})

test('a service worker that is not listening, or an extension reloaded under the page, is no error in the page', async () => {
  const unhandled = []
  const onUnhandled = (reason) => unhandled.push(reason)
  process.on('unhandledRejection', onUnhandled)
  try {
    const chrome = fakeChrome()
    chrome.runtime.sendMessage = async () => {
      throw new Error('Could not establish connection. Receiving end does not exist.')
    }
    bridged(chrome).post({ type: 'tools', tools: [] })
    const reloaded = fakeChrome()
    reloaded.runtime.sendMessage = () => {
      throw new Error('Extension context invalidated.')
    }
    assert.doesNotThrow(() => bridged(reloaded).post({ type: 'tools', tools: [] }))
    await settle()
    assert.deepEqual(unhandled, [])
  } finally {
    process.off('unhandledRejection', onUnhandled)
  }
})

test('bridge and observer, each in its own world, carry a call both ways', async () => {
  const chrome = fakeChrome()
  const context = fakeModelContext()
  const page = fakePage({ document: { modelContext: context } })
  runScript('bridge.js', page, { chrome, crypto: { randomUUID: () => NONCE } })
  runScript('observer.js', page)
  assert.equal('agentrqNonce' in page.document.documentElement.dataset, false)

  await context.registerTool({ name: 'search', description: 'd', execute: async ({ q }) => `found ${q}` })
  chrome.runtime.onMessage.fire({ type: 'site-call', callId: 'c1', tool: 'search', arguments: { q: 'x' } })
  await settle()
  assert.deepEqual(
    chrome.calls.filter(([name]) => name === 'runtime.sendMessage').map(([, message]) => message),
    [
      { type: 'site-tools', tools: [{ name: 'search', description: 'd' }] },
      { type: 'site-result', callId: 'c1', text: 'found x' },
    ],
  )
})
