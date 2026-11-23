// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h } from 'vue'

import { useWindowTitle, windowTitleText } from '../src/composables/useWindowTitle'

const settle = () => new Promise((r) => setTimeout(r, 0))

describe('windowTitleText', () => {
  it('drops the app name every page appends', () => {
    expect(windowTitleText('agentrq-static | AgentRQ')).toBe('agentrq-static')
    expect(windowTitleText('  All Tasks | AgentRQ ')).toBe('All Tasks')
  })

  it('keeps a title that is only the app name, so the bar is never empty', () => {
    expect(windowTitleText(' | AgentRQ')).toBe('| AgentRQ')
    expect(windowTitleText('AgentRQ')).toBe('AgentRQ')
  })

  it('reads anything that is not a string as no title', () => {
    expect(windowTitleText(undefined)).toBe('')
  })
})

describe('useWindowTitle', () => {
  let app

  afterEach(() => {
    app?.unmount()
    app = null
    document.head.innerHTML = ''
  })

  function mount() {
    let result
    app = createApp({ setup() { result = useWindowTitle(); return () => h('div') } })
    app.mount(document.createElement('div'))
    return result
  }

  it('starts from the current title and follows every change to it', async () => {
    document.title = 'Workspaces | AgentRQ'
    const { title } = mount()
    expect(title.value).toBe('Workspaces')

    document.title = 'agentrq-static | AgentRQ'
    await settle()
    expect(title.value).toBe('agentrq-static')
  })

  it('sees a <title> element that did not exist when it started', async () => {
    document.head.innerHTML = ''
    const { title } = mount()
    expect(title.value).toBe('')

    const el = document.createElement('title')
    el.textContent = 'Machines | AgentRQ'
    document.head.appendChild(el)
    await settle()
    expect(title.value).toBe('Machines')
  })

  it('stops following once its component is gone', async () => {
    document.title = 'Events | AgentRQ'
    const { title } = mount()
    app.unmount()
    app = null

    document.title = 'Kanban | AgentRQ'
    await settle()
    expect(title.value).toBe('Events')
  })
})
