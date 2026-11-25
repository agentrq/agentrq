// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The `/` skills menu for a `/name` typed anywhere in a textarea: the token at
 * the caret, the text after choosing one, and the composable that ties them
 * to a real textarea.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'

import { applyInlineSkill, skillTokenAt, useInlineSkillMenu } from '../src/composables/useSlashCommands'

describe('skillTokenAt', () => {
  it('finds a /name at the start, or after whitespace, up to the caret', () => {
    expect(skillTokenAt('/rel', 4)).toEqual({ start: 0, end: 4, query: 'rel' })
    expect(skillTokenAt('do this /rel', 12)).toEqual({ start: 8, end: 12, query: 'rel' })
    expect(skillTokenAt('do\n/', 4)).toEqual({ start: 3, end: 4, query: '' })
  })

  it('works in the middle of the text, taking the whole word the caret is in', () => {
    expect(skillTokenAt('first /re then more', 9)).toEqual({ start: 6, end: 9, query: 're' })
    expect(skillTokenAt('a /release b', 5)).toEqual({ start: 2, end: 10, query: 're' })
  })

  it('is null for paths, URLs, a finished word, or no slash', () => {
    expect(skillTokenAt('see a/b', 7)).toBeNull()
    expect(skillTokenAt('https://x', 9)).toBeNull()
    expect(skillTokenAt('/usr/lib', 8)).toBeNull()
    expect(skillTokenAt('/release now', 12)).toBeNull()
    expect(skillTokenAt('plain', 5)).toBeNull()
    expect(skillTokenAt(null, 0)).toBeNull()
  })

  it('reads to the end without a caret, and clamps one out of range', () => {
    expect(skillTokenAt('x /r')).toEqual({ start: 2, end: 4, query: 'r' })
    expect(skillTokenAt('x /r', 99)).toEqual({ start: 2, end: 4, query: 'r' })
    expect(skillTokenAt('/r', -3)).toBeNull()
  })
})

describe('applyInlineSkill', () => {
  const release = { name: 'release', skill: true }

  it('puts the skill in place of the /name, with a space and the caret after it', () => {
    expect(applyInlineSkill(release, '/re', 3)).toEqual({ text: '/release ', caret: 9 })
    expect(applyInlineSkill(release, 'first /re then', 9)).toEqual({ text: 'first /release then', caret: 15 })
    expect(applyInlineSkill(release, 'a /refoo', 4)).toEqual({ text: 'a /release ', caret: 11 })
  })

  it('leaves an agent command that takes no argument bare, and one that does spaced', () => {
    expect(applyInlineSkill({ name: 'compact' }, '/comp', 5)).toEqual({ text: '/compact', caret: 8 })
    expect(applyInlineSkill({ name: 'compact' }, '/comp now', 5)).toEqual({ text: '/compact now', caret: 8 })
    expect(applyInlineSkill({ name: 'init', hint: 'what' }, '/in', 3)).toEqual({ text: '/init ', caret: 6 })
  })

  it('inserts at the caret when there is no /name, and survives no text', () => {
    expect(applyInlineSkill(release, 'ab', 1)).toEqual({ text: 'a/release b', caret: 10 })
    expect(applyInlineSkill(release, undefined)).toEqual({ text: '/release ', caret: 9 })
  })
})

describe('useInlineSkillMenu', () => {
  let app
  afterEach(() => {
    app?.unmount()
    document.body.innerHTML = ''
  })

  const SKILLS = [
    { name: 'release', description: 'Cut a release', workspaceEnabled: true },
    { name: 'review', workspaceEnabled: true },
  ]

  async function mount(initial = '', leading) {
    const text = ref(initial)
    const el = ref(null)
    const show = ref(true)
    let menu
    const load = vi.fn().mockResolvedValue({ skills: SKILLS })
    const loadTeam = vi.fn().mockResolvedValue({ skills: [{ name: 'deploy', description: 'Ship it', on: true, clash: false }] })
    const root = document.createElement('div')
    document.body.appendChild(root)
    app = createApp({
      setup() {
        menu = useInlineSkillMenu({ text, workspaceId: 'ws1', el, load, loadTeam, leading })
        return () => (show.value ? h('textarea', { ref: el, value: text.value, onInput: (e) => { text.value = e.target.value } }) : null)
      },
    })
    app.mount(root)
    await new Promise((r) => setTimeout(r, 0))
    const area = () => root.querySelector('textarea')
    const type = (value, caret = value.length) => {
      area().value = value
      area().setSelectionRange(caret, caret)
      area().dispatchEvent(new Event('input'))
    }
    return { text, el, show, menu: () => menu, area, type, load, loadTeam }
  }

  const key = (k) => new KeyboardEvent('keydown', { key: k, cancelable: true })

  it('offers the leading commands only for a /name that starts the text, ahead of the skills', async () => {
    const t = await mount('', ref([{ name: 'compact' }, { name: 'review', description: 'agent' }]))
    t.type('/')
    await nextTick()
    expect(t.menu().matches.value.map((c) => c.name)).toEqual(['compact', 'review', 'release', 'deploy'])
    expect(t.menu().matches.value[1].description).toBe('agent')

    t.type('then /')
    await nextTick()
    expect(t.menu().matches.value.map((c) => c.name)).toEqual(['release', 'review', 'deploy'])

    t.type('/comp')
    await nextTick()
    expect(t.menu().handleKeydown(key('Tab'))).toBe(true)
    expect(t.text.value).toBe('/compact')
    expect(t.menu().open.value).toBe(false)
  })

  it('offers the workspace\'s team skills too', async () => {
    const t = await mount()
    expect(t.loadTeam).toHaveBeenCalledWith('ws1')
    t.type('/dep')
    await nextTick()
    expect(t.menu().matches.value.map((c) => c.name)).toEqual(['deploy'])
  })

  it('opens on a /name in the middle of the text and Enter puts the skill there', async () => {
    const t = await mount()
    expect(t.load).toHaveBeenCalledWith('ws1')
    t.type('first /rev then more', 10)
    await nextTick()
    expect(t.menu().open.value).toBe(true)
    expect(t.menu().matches.value.map((c) => c.name)).toEqual(['review'])

    const event = key('Enter')
    expect(t.menu().handleKeydown(event)).toBe(true)
    expect(event.defaultPrevented).toBe(true)
    expect(t.text.value).toBe('first /review then more')
    await nextTick()
    await nextTick()
    expect(t.area().selectionStart).toBe(14)
    expect(t.menu().open.value).toBe(false)
  })

  it('follows the caret as it moves, and leaves keys alone while shut', async () => {
    const t = await mount()
    t.type('/re and text')
    await nextTick()
    expect(t.menu().open.value).toBe(false)
    expect(t.menu().handleKeydown(key('Enter'))).toBe(false)

    t.area().setSelectionRange(3, 3)
    t.area().dispatchEvent(new Event('click'))
    await nextTick()
    expect(t.menu().matches.value.map((c) => c.name)).toEqual(['release', 'review'])

    const down = key('ArrowDown')
    expect(t.menu().handleKeydown(down)).toBe(true)
    expect(t.text.value).toBe('/re and text')
  })

  it('accepts a click on a row, and ignores one with the menu shut', async () => {
    const t = await mount()
    expect(t.menu().accept(SKILLS[0])).toBe(false)
    t.type('go /')
    await nextTick()
    expect(t.menu().accept(t.menu().matches.value[0])).toBe(true)
    expect(t.text.value).toBe('go /release ')
  })

  it('reads the caret again when the text is set from code', async () => {
    const t = await mount()
    t.text.value = 'from code /'
    await nextTick()
    await nextTick()
    expect(t.menu().caret.value).toBe(11)
    expect(t.menu().open.value).toBe(true)
  })

  it('stops listening once the textarea is gone', async () => {
    const t = await mount()
    const old = t.area()
    const removed = vi.spyOn(old, 'removeEventListener')
    t.show.value = false
    await nextTick()
    expect(removed.mock.calls.map((c) => c[0]).sort()).toEqual(['click', 'focus', 'input', 'keyup'])
    t.text.value = 'set with no textarea'
    await nextTick()
    await nextTick()
    expect(t.menu().caret.value).toBeNull()
  })
})
