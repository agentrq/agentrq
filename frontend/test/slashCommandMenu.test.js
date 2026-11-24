// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The `/` menu's list, mounted: a team skill carries the team icon, and
 * nothing else does.
 */

import { afterEach, describe, it, expect, vi } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import SlashCommandMenu from '../src/components/SlashCommandMenu.vue'

describe('SlashCommandMenu', () => {
  let app
  afterEach(() => {
    app?.unmount()
    document.body.innerHTML = ''
  })

  let menu, onAccept
  function mount(matches) {
    menu = { open: ref(true), matches: ref(matches), selected: ref(0), highlight: vi.fn() }
    onAccept = vi.fn()
    const root = document.createElement('div')
    document.body.appendChild(root)
    app = createApp({ render: () => h(SlashCommandMenu, { menu, onAccept }) })
    app.mount(root)
    return root
  }

  it('marks a team skill with the team icon, and only a team skill', () => {
    const root = mount([
      { name: 'mine', description: 'Own skill', skill: true },
      { name: 'theirs', description: 'Team skill', skill: true, team: true },
      { name: 'compact', description: 'Agent command' },
    ])
    const rows = [...root.querySelectorAll('[role=option]')]
    expect(rows.map((r) => !!r.querySelector('[data-test=team-skill-icon]'))).toEqual([false, true, false])
    expect(rows[1].querySelector('[data-test=team-skill-icon]').getAttribute('aria-label')).toBe('Team skill')
    expect(rows[1].textContent).toContain('skill')
  })

  it('highlights on hover, accepts on press, and scrolls the highlight into view', async () => {
    const root = mount([{ name: 'a', skill: true }, { name: 'b', skill: true, team: true }])
    const rows = root.querySelectorAll('[role=option]')
    rows[1].scrollIntoView = vi.fn()
    rows[1].dispatchEvent(new Event('mouseenter'))
    expect(menu.highlight).toHaveBeenCalledWith(1)
    rows[1].dispatchEvent(new MouseEvent('mousedown', { cancelable: true }))
    expect(onAccept).toHaveBeenCalledWith(menu.matches.value[1])
    menu.selected.value = 1
    await nextTick()
    await nextTick()
    expect(rows[1].scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' })
  })
})
