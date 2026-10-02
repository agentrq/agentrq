// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The macOS title bar, mounted: the page title, the profile button, and the
 * card that switches profiles from it.
 */

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, reactive } from 'vue'

import DesktopTitleBar from '../src/components/DesktopTitleBar.vue'

const tick = () => new Promise((r) => setTimeout(r, 0))

const PROFILES = [
  { id: 'p1', label: 'Work', serverUrl: 'https://app.agentrq.com', active: true, identity: { name: 'Ada', email: 'ada@example.com' } },
  { id: 'p2', label: 'Home', serverUrl: 'https://self.example', identity: { name: 'Grace', email: 'grace@example.com' } },
]

let app
let el

function mount(props = {}) {
  const handlers = { onSwitch: vi.fn(), onRemove: vi.fn(), onAdd: vi.fn(), onToggleSidePanel: vi.fn() }
  const state = reactive({
    title: 'agentrq-static',
    user: { name: 'Ada', email: 'ada@example.com' },
    profiles: PROFILES,
    ...props,
  })
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(DesktopTitleBar, { ...state, ...handlers }) })
  app.mount(el)
  return { state, ...handlers }
}

const avatar = () => el.querySelector('button[aria-haspopup="true"]')
const card = () => el.querySelector('[data-profile-card]')
const buttons = () => [...el.querySelectorAll('button')]
const buttonNamed = (text) => buttons().find((b) => b.textContent.trim() === text)
const buttonContaining = (text) => buttons().find((b) => b.textContent.includes(text))

afterEach(() => {
  app?.unmount()
  el?.remove()
  app = null
})

describe('DesktopTitleBar', () => {
  it('is the window drag handle and shows the page title', async () => {
    const { state } = mount()
    const bar = el.firstElementChild
    expect(bar.classList.contains('app-drag')).toBe(true)
    expect(el.querySelector('[data-window-title]').textContent).toBe('agentrq-static')

    state.title = 'All Tasks'
    await tick()
    expect(el.querySelector('[data-window-title]').textContent).toBe('All Tasks')
  })

  it('keeps the profile button clickable inside the drag region', () => {
    mount()
    expect(avatar().closest('.app-no-drag')).not.toBeNull()
  })

  it('opens and closes the side panel from a button the drag region lets through', async () => {
    const { state, onToggleSidePanel } = mount()
    const toggle = el.querySelector('[data-side-panel-toggle]')
    expect(toggle.classList.contains('app-no-drag')).toBe(true)
    expect(toggle.getAttribute('aria-pressed')).toBe('false')

    toggle.click()
    expect(onToggleSidePanel).toHaveBeenCalledOnce()

    state.sidePanelOpen = true
    await tick()
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
  })

  it('has no profile button before anyone is signed in', () => {
    mount({ user: null })
    expect(avatar()).toBeNull()
  })

  it('uses the account picture, or its first letter', async () => {
    const { state } = mount({ user: { email: 'ada@example.com' } })
    expect(avatar().textContent.trim()).toBe('A')

    state.user = { name: 'Ada', picture: 'https://example.com/ada.png' }
    await tick()
    expect(avatar().querySelector('img').getAttribute('src')).toBe('https://example.com/ada.png')
  })

  it('opens a card naming the account, its profile and server', async () => {
    mount()
    expect(card()).toBeNull()

    avatar().click()
    await tick()
    expect(avatar().getAttribute('aria-expanded')).toBe('true')
    const text = card().textContent
    expect(text).toContain('Ada · Work')
    expect(text).toContain('ada@example.com')
    expect(text).toContain('https://app.agentrq.com')
    // The other profile, not the active one, is offered.
    expect(text).toContain('Grace')
  })

  it('switches, adds and removes profiles from the card, closing it', async () => {
    const { onSwitch, onAdd, onRemove } = mount()

    avatar().click()
    await tick()
    buttonContaining('grace@example.com').click()
    await tick()
    expect(onSwitch).toHaveBeenCalledWith('p2')
    expect(card()).toBeNull()

    avatar().click()
    await tick()
    buttonNamed('Add profile').click()
    await tick()
    expect(onAdd).toHaveBeenCalledWith()
    expect(card()).toBeNull()

    avatar().click()
    await tick()
    el.querySelector('button[title="Remove this profile"]').click()
    await tick()
    expect(onRemove).toHaveBeenCalledWith('p2')
    expect(card()).toBeNull()
  })

  it('says when this profile is a second window onto another one', async () => {
    mount({ profiles: [{ ...PROFILES[0], duplicateOf: 'p2' }, PROFILES[1]] })
    avatar().click()
    await tick()
    expect(card().textContent).toContain('Same account as Grace')
  })

  it('shows pictures and duplicates for the other profiles too', async () => {
    mount({
      user: { name: 'Ada', picture: 'https://example.com/ada.png' },
      profiles: [
        { id: 'p1', active: true, identity: { name: 'Ada' } },
        { id: 'p2', duplicateOf: 'p1', account: { name: 'Ada', picture: 'https://example.com/old.png' } },
        { id: 'p3', identity: { name: 'Grace', picture: 'https://example.com/grace.png' } },
      ],
    })
    avatar().click()
    await tick()
    const pictures = [...card().querySelectorAll('img')].map((i) => i.getAttribute('src'))
    expect(pictures).toEqual(['https://example.com/ada.png', 'https://example.com/old.png', 'https://example.com/grace.png'])
    expect(card().textContent).toContain('Same account as Ada')
    // No profile label to add after the name.
    expect(card().textContent).not.toContain('·')
  })

  it('reads well with no profiles, name or email to show', async () => {
    mount({ user: { id: 'u1' }, profiles: [] })
    expect(avatar().textContent.trim()).toBe('A')
    expect(avatar().getAttribute('title')).toBe('Account')

    avatar().click()
    await tick()
    // Only the letter and the name: nothing else to say.
    expect(card().textContent.trim()).toBe('AAccount')
  })

  it('closes on a click elsewhere or Escape, not on a click inside', async () => {
    mount()
    avatar().click()
    await tick()

    card().click()
    await tick()
    expect(card()).not.toBeNull()

    document.body.click()
    await tick()
    expect(card()).toBeNull()

    avatar().click()
    await tick()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await tick()
    expect(card()).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await tick()
    expect(card()).toBeNull()
  })

  it('stops listening once it is gone', () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    mount()
    app.unmount()
    app = null
    expect(remove).toHaveBeenCalledWith('click', expect.any(Function))
    remove.mockRestore()
  })
})
