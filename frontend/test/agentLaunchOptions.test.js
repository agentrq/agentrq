// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** The launch options both launch forms render: what is typed reaches the form's params. */

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import AgentLaunchOptions from '../src/components/AgentLaunchOptions.vue'

let app

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

function mount() {
  const params = ref({ agent: '', model: '', effort: '' })
  const picks = []
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () =>
      h(AgentLaunchOptions, {
        idPrefix: 'opt',
        kind: 'acp-gateway',
        params: params.value,
        'onUpdate:params': (v) => (params.value = v),
        acpModels: [{ id: 'gpt-5', name: 'GPT-5' }],
        acpAgents: [{ id: 'codex-acp', name: 'Codex', description: '', runtimes: ['npx'] }],
        onPick: (id) => picks.push(id),
      }),
  })
  app.mount(el)
  return { el, params, picks }
}

function type(input, value) {
  input.value = value
  input.dispatchEvent(new Event('input'))
}

describe('AgentLaunchOptions', () => {
  it('writes the gateway agent and model into the params', async () => {
    const { el, params } = mount()
    type(el.querySelector('#opt-agent'), 'goose')
    type(el.querySelector('#opt-model'), 'gpt-5')
    await nextTick()
    expect(params.value.agent).toBe('goose')
    expect(params.value.model).toBe('gpt-5')
    expect([...el.querySelectorAll('#opt-model-options option')].map((o) => o.value)).toEqual(['gpt-5'])
  })

  it('passes a pick from the agent list up to the form', async () => {
    const { el, picks } = mount()
    el.querySelector('li button').click()
    await nextTick()
    expect(picks).toEqual(['codex-acp'])
  })
})
