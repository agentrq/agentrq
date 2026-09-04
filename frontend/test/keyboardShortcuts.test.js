import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h } from 'vue'

import {
  SHORTCUTS,
  dispatchShortcut,
  formatAccelerator,
  formatShortcut,
  isTypingTarget,
  matchShortcut,
  newTaskRoute,
  resetShortcuts,
  useShortcuts,
  usesCommandKey,
} from '../src/composables/useKeyboardShortcuts'

/** A key event as the dispatcher sees it, with only what it reads. */
const press = (key, extra = {}) => ({
  key,
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  target: null,
  preventDefault: vi.fn(),
  ...extra,
})

/**
 * Run `setup` inside a real component instance, so `onMounted` and
 * `onUnmounted` are the genuine article rather than injected stand-ins. That is
 * the only way to exercise the composable's defaults.
 */
function mountWith(setup) {
  const app = createApp({ setup, render: () => h('div') })
  app.mount(document.createElement('div'))
  return () => app.unmount()
}

afterEach(() => {
  resetShortcuts()
})

describe('SHORTCUTS', () => {
  it('binds nothing to a key the operating system or browser has already taken', () => {
    // The point of the whole scheme. Cmd+N is New Window, Cmd+H is Hide on
    // macOS and Cmd+M is Minimize — a handler on any of them would never run,
    // so the table must not claim them.
    const claimed = SHORTCUTS.filter((s) => s.mod).map((s) => s.key)

    expect(claimed).toEqual(['k'])
  })

  it('gives every shortcut an id, a key and a scope the help sheet groups by', () => {
    for (const s of SHORTCUTS) {
      expect(s.id).toBeTruthy()
      expect(s.key).toBeTruthy()
      expect(['global', 'task']).toContain(s.scope)
    }
  })
})

describe('usesCommandKey', () => {
  it('trusts the desktop shell when it reports macOS', () => {
    expect(usesCommandKey({ os: 'darwin' }, null)).toBe(true)
  })

  it('falls back to the navigator in the browser, where the store has no os', () => {
    expect(usesCommandKey({ os: '' }, { userAgentData: { platform: 'macOS' } })).toBe(true)
    expect(usesCommandKey({}, { platform: 'MacIntel' })).toBe(true)
    expect(usesCommandKey({}, { platform: 'Win32' })).toBe(false)
  })

  it('assumes Control when nothing can say otherwise', () => {
    expect(usesCommandKey({}, null)).toBe(false)
    expect(usesCommandKey({}, {})).toBe(false)
  })

  it('reads the real navigator when none is passed', () => {
    // jsdom reports a non-Mac platform, which is all this needs to assert:
    // the default argument is wired to something real.
    expect(usesCommandKey({})).toBe(false)
  })
})

describe('isTypingTarget', () => {
  it('recognises the fields a bare letter must not disturb', () => {
    for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) {
      expect(isTypingTarget({ tagName })).toBe(true)
    }
  })

  it('counts a rich-text region as typing', () => {
    expect(isTypingTarget({ isContentEditable: true })).toBe(true)
  })

  it('honours an explicit opt-out on an ancestor', () => {
    expect(isTypingTarget({ tagName: 'DIV', closest: () => ({}) })).toBe(true)
    expect(isTypingTarget({ tagName: 'DIV', closest: () => null })).toBe(false)
  })

  it('says no for anything that is not an element', () => {
    expect(isTypingTarget(null)).toBe(false)
    expect(isTypingTarget('window')).toBe(false)
    expect(isTypingTarget({})).toBe(false)
  })
})

describe('matchShortcut', () => {
  it('reads Cmd+K on a Mac keyboard and Ctrl+K elsewhere', () => {
    expect(matchShortcut(press('k', { metaKey: true }), { mac: true }).id).toBe('find-task')
    expect(matchShortcut(press('k', { ctrlKey: true })).id).toBe('find-task')
  })

  it('does not read the other platform’s modifier', () => {
    // Ctrl+K on a Mac is a text-editing binding, and Cmd+K on Windows is not a
    // thing users press. Matching either would fire the finder by accident.
    expect(matchShortcut(press('k', { ctrlKey: true }), { mac: true })).toBeNull()
    expect(matchShortcut(press('k', { metaKey: true }))).toBeNull()
    expect(matchShortcut(press('k', { ctrlKey: true, altKey: true }))).toBeNull()
    expect(matchShortcut(press('k'))).toBeNull()
  })

  it('reads the bare letters', () => {
    expect(matchShortcut(press('n')).id).toBe('new-task')
    expect(matchShortcut(press('N')).id).toBe('new-task')
    expect(matchShortcut(press('m')).id).toBe('chat-view')
    expect(matchShortcut(press('t')).id).toBe('trajectory-view')
    expect(matchShortcut(press('?')).id).toBe('show-help')
  })

  it('stands down while the user is typing', () => {
    // Writing "not started" in the reply box must not open the task form.
    const inReply = { target: { tagName: 'TEXTAREA' } }

    expect(matchShortcut(press('n', inReply))).toBeNull()
    expect(matchShortcut(press('t', inReply))).toBeNull()
  })

  it('keeps the finder live while typing, because it is the way out', () => {
    const inReply = { target: { tagName: 'TEXTAREA' }, ctrlKey: true }

    expect(matchShortcut(press('k', inReply)).id).toBe('find-task')
  })

  it('leaves a bare letter alone when any modifier is held', () => {
    expect(matchShortcut(press('n', { ctrlKey: true }))).toBeNull()
    expect(matchShortcut(press('n', { metaKey: true }))).toBeNull()
    expect(matchShortcut(press('n', { altKey: true }))).toBeNull()
  })

  it('ignores a key nothing is bound to, and a key event with no key', () => {
    expect(matchShortcut(press('z'))).toBeNull()
    expect(matchShortcut({ key: '' })).toBeNull()
    expect(matchShortcut(null)).toBeNull()
  })

  it('can be pointed at a different table', () => {
    const only = [{ id: 'x', key: 'x', scope: 'global' }]

    expect(matchShortcut(press('x'), { shortcuts: only }).id).toBe('x')
    expect(matchShortcut(press('n'), { shortcuts: only })).toBeNull()
  })
})

describe('formatShortcut', () => {
  it('writes macOS modifiers as glyphs and everything else as words', () => {
    const findTask = SHORTCUTS.find((s) => s.id === 'find-task')

    expect(formatShortcut(findTask, { mac: true })).toBe('⌘K')
    expect(formatShortcut(findTask)).toBe('Ctrl+K')
  })

  it('writes a bare key as itself on every platform', () => {
    expect(formatShortcut({ key: 'n' }, { mac: true })).toBe('N')
    expect(formatShortcut({ key: '?' })).toBe('?')
  })
})

describe('formatAccelerator', () => {
  it('renders the desktop menu’s accelerator the way each platform writes it', () => {
    expect(formatAccelerator('CommandOrControl+Shift+N', { mac: true })).toBe('⌘⇧N')
    expect(formatAccelerator('CommandOrControl+Shift+N')).toBe('Ctrl+Shift+N')
  })

  it('passes through parts it has no name for', () => {
    expect(formatAccelerator('Command+Control+Alt+F1', { mac: true })).toBe('⌘⌃⌥F1')
    expect(formatAccelerator('Control+Alt+F1')).toBe('Ctrl+Alt+F1')
  })
})

describe('dispatchShortcut', () => {
  it('does nothing when nobody is listening', () => {
    const event = press('n')

    expect(dispatchShortcut(event)).toBeNull()
    expect(event.preventDefault).not.toHaveBeenCalled()
  })

  it('does nothing for a key that is not a shortcut', () => {
    useShortcuts({ 'new-task': vi.fn() }, fakeLifecycle().options)

    expect(dispatchShortcut(press('z'))).toBeNull()
  })

  it('suppresses the browser default only once a handler has claimed the key', () => {
    // Cmd+K would otherwise drop into the address bar. On a screen with no
    // handler the browser keeps its own behaviour.
    const handler = vi.fn()
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'find-task': handler }, options)
    mount()

    const claimed = press('k', { ctrlKey: true })
    expect(dispatchShortcut(claimed)).toBe('find-task')
    expect(handler).toHaveBeenCalledOnce()
    expect(claimed.preventDefault).toHaveBeenCalledOnce()

    const unclaimed = press('n')
    expect(dispatchShortcut(unclaimed)).toBeNull()
    expect(unclaimed.preventDefault).not.toHaveBeenCalled()
  })

  it('survives an event object with no preventDefault', () => {
    const handler = vi.fn()
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': handler }, options)
    mount()

    expect(dispatchShortcut({ key: 'n' })).toBe('new-task')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('lets the view on top win over the shell underneath', () => {
    // The task detail mounts after the shell, so its handler is the one that
    // runs — that is what lets a view take a shortcut over.
    const shell = vi.fn()
    const view = vi.fn()
    const a = fakeLifecycle()
    const b = fakeLifecycle()
    useShortcuts({ 'chat-view': shell }, a.options)
    a.mount()
    useShortcuts({ 'chat-view': view }, b.options)
    b.mount()

    dispatchShortcut(press('m'))

    expect(view).toHaveBeenCalledOnce()
    expect(shell).not.toHaveBeenCalled()
  })

  it('falls through a registration that does not handle the key', () => {
    const shell = vi.fn()
    const a = fakeLifecycle()
    const b = fakeLifecycle()
    useShortcuts({ 'new-task': shell }, a.options)
    a.mount()
    useShortcuts({ 'chat-view': vi.fn() }, b.options)
    b.mount()

    dispatchShortcut(press('n'))

    expect(shell).toHaveBeenCalledOnce()
  })
})

describe('useShortcuts', () => {
  it('listens only while its component is mounted', () => {
    const handler = vi.fn()
    const { options, mount, unmount, target } = fakeLifecycle()

    useShortcuts({ 'new-task': handler }, options)
    expect(target.addEventListener).not.toHaveBeenCalled()

    mount()
    expect(target.addEventListener).toHaveBeenCalledWith('keydown', expect.any(Function))
    expect(dispatchShortcut(press('n'))).toBe('new-task')

    unmount()
    expect(target.removeEventListener).toHaveBeenCalledWith('keydown', expect.any(Function))
    expect(dispatchShortcut(press('n'))).toBeNull()
  })

  it('unmounts cleanly even if it was never registered', () => {
    const { options, unmount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, options)

    expect(() => unmount()).not.toThrow()
  })

  it('tolerates having nowhere to listen', () => {
    const { options, mount, unmount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, { ...options, target: null })

    expect(() => {
      mount()
      unmount()
    }).not.toThrow()
  })

  it('routes a real key press through the window by default', () => {
    // No options at all: real lifecycle hooks, the real window, and the
    // Control-key default.
    const handler = vi.fn()
    const unmount = mountWith(() => {
      useShortcuts({ 'find-task': handler })
      return {}
    })

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    expect(handler).toHaveBeenCalledOnce()

    unmount()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    expect(handler).toHaveBeenCalledOnce()
  })
})

describe('newTaskRoute', () => {
  it('creates the task where the user already is', () => {
    expect(newTaskRoute('ws1', [{ id: 'ws1' }, { id: 'ws2' }])).toBe('/workspaces/ws1/tasks/new')
  })

  it('uses the only workspace there is when the route names none', () => {
    expect(newTaskRoute(undefined, [{ id: 'solo' }])).toBe('/workspaces/solo/tasks/new')
  })

  it('asks rather than guesses when several workspaces are in play', () => {
    // Dropping a task into an arbitrary workspace is worse than a short detour
    // through the list.
    expect(newTaskRoute(undefined, [{ id: 'a' }, { id: 'b' }])).toBe('/')
    expect(newTaskRoute(undefined)).toBe('/')
  })
})

/** Lifecycle hooks and an event target the test drives by hand. */
function fakeLifecycle() {
  const mounts = []
  const unmounts = []
  const target = { addEventListener: vi.fn(), removeEventListener: vi.fn() }

  return {
    target,
    mount: () => mounts.forEach((fn) => fn()),
    unmount: () => unmounts.forEach((fn) => fn()),
    options: {
      target,
      mac: () => false,
      onMounted: (fn) => mounts.push(fn),
      onUnmounted: (fn) => unmounts.push(fn),
    },
  }
}
