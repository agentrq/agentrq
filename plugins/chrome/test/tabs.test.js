// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { BADGE_COLOR, CALL_WAIT, createTabs } from '../src/tabs.js'
import { fakeChrome, fakeTimers } from './fake-chrome.js'

const GH = 'https://github.com'
const t = (name) => ({ name, description: name })

const setup = () => {
  const chrome = fakeChrome()
  const timers = fakeTimers()
  return { chrome, timers, tabs: createTabs(chrome, timers) }
}

test('the badge shows a tab’s tool count in green, and nothing without tools', () => {
  const { chrome, tabs } = setup()
  tabs.set(1, GH, `${GH}/a`, [t('a'), t('b')])
  assert.deepEqual(chrome.action.badges.get(1), { text: '2', color: BADGE_COLOR })
  tabs.set(1, GH, `${GH}/a`, [])
  assert.equal(chrome.action.badges.get(1).text, '')
  tabs.badge(9)
  assert.equal(chrome.action.badges.get(9).text, '')
  assert.deepEqual(tabs.toolsFor(1), [])
  assert.deepEqual(tabs.toolsFor(9), [])
})

test('the best tab is the most recently used one of the site that offers the tool', () => {
  const { tabs } = setup()
  assert.equal(tabs.bestTab(GH), null)
  tabs.set(1, GH, GH, [t('a')])
  tabs.set(2, GH, GH, [t('a'), t('b')])
  tabs.set(3, 'https://other.com', 'https://other.com', [t('a')])
  tabs.set(4, GH, GH, [])
  tabs.touch(4)
  tabs.touch(99)
  assert.equal(tabs.bestTab(GH), 1)
  tabs.touch(2)
  assert.equal(tabs.bestTab(GH), 2)
  tabs.touch(1)
  assert.equal(tabs.bestTab(GH, 'a'), 1)
  assert.equal(tabs.bestTab(GH, 'b'), 2)
  assert.equal(tabs.bestTab(GH, 'c'), null)
  // A tab keeps its place when its page announces again.
  tabs.set(2, GH, GH, [t('a')])
  tabs.touch(1)
  assert.equal(tabs.bestTab(GH), 1)
  assert.equal(tabs.entry(2).url, GH)
})

test('waiting for a tool resolves at once, when a tab registers it, or fails after the deadline', async () => {
  const { tabs, timers } = setup()
  tabs.set(1, GH, GH, [t('a')])
  assert.equal(await tabs.waitForTool(GH, 'a', 20000), 1)

  const later = tabs.waitForTool(GH, 'b', 20000)
  tabs.set(5, 'https://other.com', 'https://other.com', [t('b')])
  tabs.set(6, GH, GH, [t('b')])
  assert.equal(await later, 6)
  assert.equal(timers.pending.size, 0)

  const never = tabs.waitForTool(GH, 'c', 20000)
  timers.advance(19999)
  tabs.set(7, GH, GH, [t('d')])
  timers.advance(1)
  await assert.rejects(never, {
    message: 'no https://github.com tab registered c within 20 seconds (the site may have changed, or you may be signed out of it)',
  })
  tabs.set(8, GH, GH, [t('c')])
})

test('a call is answered by its tab only', async () => {
  const { tabs } = setup()
  tabs.set(1, GH, GH, [t('a')])
  const answer = tabs.expect('c1', 1)
  tabs.deliver('c1', 2, { text: 'from the wrong tab' })
  tabs.deliver('other', 1, { text: 'another call' })
  tabs.deliver('c1', 1, { text: 'hi' })
  assert.deepEqual(await answer, { text: 'hi' })
})

// Review Focus 4: two tabs of the site, one closed during the call.
test('closing a tab fails the calls pending on it, and only those', async () => {
  const { tabs } = setup()
  tabs.set(1, GH, GH, [t('a')])
  tabs.set(2, GH, GH, [t('a')])
  const onClosed = tabs.expect('c1', 1)
  const onOther = tabs.expect('c2', 2)
  tabs.remove(1)
  tabs.remove(1)
  assert.deepEqual(await onClosed, { error: 'the https://github.com tab was closed during the call' })
  assert.equal(tabs.bestTab(GH), 2)
  tabs.deliver('c2', 2, { text: 'ok' })
  assert.deepEqual(await onOther, { text: 'ok' })
})

test('a page navigating away fails the calls sent to it, and its tab offers nothing until the next page announces', async () => {
  const { chrome, tabs } = setup()
  tabs.set(1, GH, `${GH}/a`, [t('a')], 'doc1')
  tabs.set(2, GH, `${GH}/b`, [t('a')], 'doc2')
  const onGone = tabs.expect('c1', 1)
  const onOther = tabs.expect('c2', 2)
  tabs.gone(1, 'doc1')
  tabs.gone(9, 'doc9')
  assert.deepEqual(await onGone, { error: 'the https://github.com page navigated away during the call' })
  assert.deepEqual(tabs.toolsFor(1), [])
  assert.equal(chrome.action.badges.get(1).text, '')
  assert.equal(tabs.bestTab(GH, 'a'), 2)
  tabs.set(1, GH, `${GH}/c`, [t('a')])
  tabs.touch(1)
  assert.equal(tabs.bestTab(GH, 'a'), 1)
  const onRecovered = tabs.expect('c3', 1)
  tabs.gone(1, 'doc3')
  assert.deepEqual(await onRecovered, { error: 'the https://github.com page navigated away during the call' })
  assert.deepEqual(tabs.toolsFor(1), [])
  tabs.deliver('c2', 2, { text: 'ok' })
  assert.deepEqual(await onOther, { text: 'ok' })
})

// A navigate tool's own result arrives before its page's pagehide.
test('a result that arrives before the page unloads still wins', async () => {
  const { tabs } = setup()
  tabs.set(1, GH, `${GH}/a`, [t('navigate')], 'doc1')
  const answer = tabs.expect('c1', 1)
  tabs.deliver('c1', 1, { text: '{"navigatedTo":"/b"}' })
  tabs.gone(1, 'doc1')
  assert.deepEqual(await answer, { text: '{"navigatedTo":"/b"}' })
})

// After a cross-site navigation, the new page can announce before the old one's pagehide arrives.
test('an old page unloading after the new one announced fails only its own calls', async () => {
  const { tabs } = setup()
  tabs.set(1, GH, `${GH}/a`, [t('a')], 'old')
  const onOld = tabs.expect('c1', 1)
  tabs.set(1, GH, `${GH}/b`, [t('a')], 'new')
  const onNew = tabs.expect('c2', 1)
  tabs.gone(1, 'old')
  assert.deepEqual(await onOld, { error: 'the https://github.com page navigated away during the call' })
  assert.deepEqual(tabs.toolsFor(1), [t('a')])
  tabs.deliver('c2', 1, { text: 'ok' })
  assert.deepEqual(await onNew, { text: 'ok' })
})

test('a call nobody answers gives up rather than waiting forever', async () => {
  const { tabs, timers } = setup()
  const answer = tabs.expect('c1', 1)
  timers.advance(CALL_WAIT)
  assert.deepEqual(await answer, { error: 'no result within 60 seconds' })
})
