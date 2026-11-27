// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { createApp, h, reactive } from 'vue'

const serverOrigin = vi.fn()
vi.mock('../src/api', () => ({ serverOrigin: (...args) => serverOrigin(...args) }))

const { default: TaskLinksPanel } = await import('../src/components/TaskLinksPanel.vue')
const { panelLinkFromEvent } = await import('../src/composables/useMarkdownLinks')

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))
const mounted = []

function mount(task, { taskId = 't1', pinned = false, panoDocs } = {}) {
  const state = reactive({ task, panoDocs, followed: 0 })
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({
    render: () => h(TaskLinksPanel, { task: state.task, taskId, pinned, panoDocs: state.panoDocs, onFollow: () => state.followed++ }),
  })
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', { 'data-to': p.to }, slots.default?.()) })
  mounted.push(app)
  app.mount(el)
  const q = (id) => el.querySelector(`[data-testid="${id}"]`)
  return { el, state, q }
}

beforeEach(() => {
  serverOrigin.mockReset()
  serverOrigin.mockResolvedValue(window.location.origin)
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
})

const task = {
  body: 'Fixes https://github.com/agentrq/agentrq/pull/1099',
  messages: [{ text: '[Roadmap](/pano/d1), [Login](/workspaces/w1/tasks/t2), https://example.com/docs' }],
}

describe('TaskLinksPanel', () => {
  it('hides itself when the task has no links', async () => {
    const { el } = mount({ body: 'nothing to see', messages: [] })
    await settle()
    expect(el.querySelector('[data-testid="task-links"]')).toBeNull()
  })

  it('shows a one-line bar with the count and the labels', async () => {
    const { q } = mount(task)
    await settle()
    expect(q('task-links-count').textContent.trim()).toBe('4')
    expect(q('task-links-summary').textContent).toBe('· agentrq#1099 · Roadmap · Login · example.com/docs')
    expect(q('task-links').hasAttribute('data-pinned')).toBe(false)
    expect(q('task-links-body')).toBeNull()
    expect(q('task-links-toggle').textContent).toContain('Links')
    expect(q('task-links-toggle').hasAttribute('title')).toBe(false)
  })

  it('opens to the links by group, routing in-app links and opening the rest in a new tab', async () => {
    const { q, state } = mount(task)
    await settle()
    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-toggle').getAttribute('aria-expanded')).toBe('true')
    expect(state.followed).toBe(0)

    const pr = q('task-links-pr').querySelector('a')
    expect(pr.textContent.trim()).toBe('#1099agentrq')
    expect(pr.getAttribute('href')).toBe('https://github.com/agentrq/agentrq/pull/1099')
    expect(pr.getAttribute('target')).toBe('_blank')
    expect(pr.querySelector('[data-testid="task-links-github"]')).not.toBeNull()
    expect(q('task-links-pano').querySelector('a').getAttribute('data-to')).toBe('/pano/d1')
    expect(q('task-links-task').querySelector('a').getAttribute('data-to')).toBe('/workspaces/w1/tasks/t2')
    const external = q('task-links-external').querySelector('a')
    expect(external.getAttribute('href')).toBe('https://example.com/docs')
    expect(external.getAttribute('rel')).toBe('noopener noreferrer')

    for (const kind of ['pr', 'pano', 'task', 'external']) q(`task-links-${kind}`).querySelector('a').click()
    expect(state.followed).toBe(4)

    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-body')).toBeNull()
  })

  it('pinned, starts open as a note listing the links one under another, and folds to its icon and count', async () => {
    const { q, el } = mount(task, { pinned: true })
    await settle()
    expect(q('task-links').getAttribute('data-pinned')).toBe('true')
    expect(q('task-links-toggle').getAttribute('aria-expanded')).toBe('true')
    expect(q('task-links-summary')).toBeNull()
    expect(q('task-links-pr').querySelector('ul').className).toContain('flex-col')
    expect(el.querySelectorAll('[data-testid="task-links-body"] li')).toHaveLength(4)
    expect(q('task-links').className).toContain('w-52')
    expect(q('task-links-toggle').textContent).toContain('Links')
    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-body')).toBeNull()
    expect(q('task-links-count').textContent.trim()).toBe('4')
    expect(q('task-links').className).not.toContain('w-52')
    expect(q('task-links-toggle').textContent.trim()).toBe('4')
    expect(q('task-links-toggle').getAttribute('title')).toBe('Links')
    expect(q('task-links-toggle').querySelectorAll('svg')).toHaveLength(1)
  })

  it('opens its web links in the desktop side panel, as a message\'s own links do', async () => {
    const { q } = mount(task, { pinned: true })
    await settle()
    const opened = []
    const listen = (event) => opened.push(panelLinkFromEvent(event))
    document.addEventListener('click', listen)
    try {
      for (const kind of ['pr', 'external']) q(`task-links-${kind}`).querySelector('a').click()
    } finally {
      document.removeEventListener('click', listen)
    }
    expect(opened).toEqual(['https://github.com/agentrq/agentrq/pull/1099', 'https://example.com/docs'])
  })

  it('shows an issue as a short GitHub chip, like a pull request', async () => {
    const { q, state } = mount({ body: 'Closes https://github.com/agentrq/agentrq/issues/1447', messages: [] })
    await settle()
    expect(q('task-links-summary').textContent).toBe('· agentrq#1447')
    q('task-links-toggle').click()
    await settle()
    const issue = q('task-links-issue').querySelector('a')
    expect(issue.textContent.trim()).toBe('#1447agentrq')
    expect(issue.getAttribute('href')).toBe('https://github.com/agentrq/agentrq/issues/1447')
    expect(issue.getAttribute('target')).toBe('_blank')
    expect(issue.querySelector('[data-testid="task-links-github"]')).not.toBeNull()
    issue.click()
    expect(state.followed).toBe(1)
  })

  it('shows an Actions run as a short GitHub chip, like a pull request', async () => {
    const { q, state } = mount({ body: 'CI: https://github.com/agentrq/agentrq/actions/runs/19734520384/job/56', messages: [] })
    await settle()
    expect(q('task-links-summary').textContent).toBe('· agentrq#19734520384')
    q('task-links-toggle').click()
    await settle()
    const run = q('task-links-run').querySelector('a')
    expect(run.textContent.trim()).toBe('#19734520384agentrq')
    expect(run.getAttribute('href')).toBe('https://github.com/agentrq/agentrq/actions/runs/19734520384/job/56')
    expect(run.getAttribute('target')).toBe('_blank')
    expect(run.querySelector('[data-testid="task-links-github"]')).not.toBeNull()
    expect(q('task-links-run').textContent).toContain('Actions')
    run.click()
    expect(state.followed).toBe(1)
  })

  it('shows the number first and the repository on the right, cutting only a long repository name', async () => {
    const { q } = mount({ body: 'https://github.com/agentrq/a-very-long-repository-name/pull/12', messages: [] })
    await settle()
    expect(q('task-links-summary').textContent).toBe('· a-very-long-repository-name#12')
    q('task-links-toggle').click()
    await settle()
    const number = q('task-links-pr').querySelector('[data-testid="task-links-number"]')
    expect(number.textContent).toBe('#12')
    expect(number.className).toContain('shrink-0')
    const repo = number.nextElementSibling
    expect(repo.getAttribute('data-testid')).toBe('task-links-repo')
    expect(repo.textContent).toBe('a-very-long-repository-name')
    expect(repo.className).toContain('truncate')
    expect(repo.className).toContain('ml-auto')
    // Centred on the icon by the glyphs, not the font's line box; the name keeps room for its descenders.
    expect(number.parentElement.className).toContain('[&>span]:[text-box:trim-both_cap_alphabetic]')
    expect(repo.className).toContain('py-1 -my-1')
  })

  it('lists the Pano docs linked to the task, though the conversation never names them', async () => {
    const { q, el, state } = mount({ body: 'nothing to see', messages: [] }, { panoDocs: [{ id: 'd5', title: 'Release plan' }] })
    await settle()
    expect(q('task-links-summary').textContent).toBe('· Release plan')
    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-pano').querySelector('a').getAttribute('data-to')).toBe('/pano/d5')
    state.panoDocs = []
    await settle()
    expect(el.querySelector('[data-testid="task-links"]')).toBeNull()
  })

  it('counts links to the server as in-app ones in the desktop app, whose page origin is app://', async () => {
    serverOrigin.mockResolvedValue('https://app.agentrq.com')
    const { q } = mount({ body: 'https://app.agentrq.com/pano/d9' })
    await settle()
    expect(q('task-links-summary').textContent).toBe('· d9')
    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-pano')).not.toBeNull()
  })

  it('keeps the page origin when the server address cannot be had', async () => {
    serverOrigin.mockRejectedValue(new Error('bridge disconnected'))
    const { q } = mount({ body: '[Roadmap](/pano/d1)' })
    await settle()
    q('task-links-toggle').click()
    await settle()
    expect(q('task-links-pano')).not.toBeNull()
  })
})
