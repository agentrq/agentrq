// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/** The agent list under the gateway's Agent field, as it reaches the page. */

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, ref } from 'vue'
import AcpAgentList from '../src/components/AcpAgentList.vue'

const AGENTS = [
  { id: 'codex-acp', name: 'Codex', description: 'OpenAI coding agent', runtimes: ['npx'] },
  { id: 'gemini', name: 'Gemini CLI', description: "Google's agent", runtimes: ['npx'] },
  { id: 'goose', name: 'goose', description: '', runtimes: ['binary', 'npx'] },
  { id: 'local-acp' },
]

let app

function mount(agents, initial = '') {
  const query = ref(initial)
  const picks = []
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () =>
      h(AcpAgentList, {
        agents,
        query: query.value,
        idPrefix: 'test',
        onPick: (id) => {
          picks.push(id)
          query.value = id
        },
      }),
  })
  app.mount(el)
  const rows = () => [...el.querySelectorAll('li button')]
  const label = () => el.querySelector('#test-acp-agents-label').textContent.replace(/\s+/g, ' ').trim()
  return { el, query, picks, rows, label }
}

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('AcpAgentList', () => {
  it('renders nothing when there are no agents to list', () => {
    const { el } = mount([])
    expect(el.textContent.trim()).toBe('')
  })

  it('lists every agent with its name, id, what it runs on and what it is', () => {
    const { rows, label } = mount(AGENTS)
    expect(label()).toBe('Available agents · 4')
    expect(rows()).toHaveLength(4)
    const goose = rows()[2]
    const spans = [...goose.querySelectorAll('span')].map((s) => s.textContent.trim())
    expect(spans).toEqual(expect.arrayContaining(['goose', 'binary', 'npx']))
    expect(rows()[0].textContent).toContain('OpenAI coding agent')
    expect(rows()[0].title).toBe('OpenAI coding agent')
    // No description: the name is the tooltip, and an agent with no name shows its id.
    expect(rows()[2].title).toBe('goose')
    expect(rows()[3].textContent).toContain('local-acp')
    expect(rows()[3].title).toBe('local-acp')
  })

  it('narrows to what is typed and says how many it shows', async () => {
    const { rows, label, query } = mount(AGENTS)
    query.value = 'google'
    await Promise.resolve()
    expect(label()).toBe('Available agents · 1 of 4')
    expect(rows().map((b) => b.textContent)).toEqual([expect.stringContaining('Gemini CLI')])
  })

  it('says when nothing matches, and keeps every agent on offer', async () => {
    const { el, query, rows, label } = mount(AGENTS)
    query.value = 'my-own-agent'
    await Promise.resolve()
    expect(el.textContent).toContain('None of these is “my-own-agent”. It is still sent as typed.')
    expect(rows()).toHaveLength(4)
    expect(label()).toBe('Available agents · 4')
  })

  it('fills the field on a click and marks the chosen agent, keeping the others on offer', async () => {
    const { rows, picks } = mount(AGENTS)
    rows()[1].click()
    await Promise.resolve()
    expect(picks).toEqual(['gemini'])
    expect(rows()).toHaveLength(4)
    expect(rows().map((b) => b.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'false', 'false'])
  })
})

describe('AcpAgentList: the chosen agent', () => {
  it('is scrolled to within the list when the form opens on it', async () => {
    const { el } = mount(AGENTS, 'goose')
    const ul = el.querySelector('ul')
    const row = ul.querySelectorAll('li')[2]
    Object.defineProperty(row, 'offsetTop', { value: 140 })
    Object.defineProperty(ul, 'offsetTop', { value: 20 })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(ul.scrollTop).toBe(120)
  })
})
