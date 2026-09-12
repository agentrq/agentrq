// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h } from 'vue'

import {
  SHORTCUTS,
  dispatchShortcut,
  shortcutHint,
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
  it('binds nothing to a bare modifier combination the platform has taken', () => {
    // Cmd+N is New Window, Cmd+H is Hide on macOS and Cmd+M is Minimize — a
    // handler on any of them would never run. Cmd+K is the one such chord
    // browsers leave to the page.
    const claimed = SHORTCUTS.filter((s) => s.mod).map((s) => s.key)

    expect(claimed).toEqual(['k'])
  })

  it('binds no two shortcuts to the same key', () => {
    // A duplicate would not error — `matchShortcut` takes the first match — so
    // the second binding would simply never fire.
    const keys = SHORTCUTS.map((s) => `${s.mod ? 'mod+' : ''}${s.key}`);

    expect(keys).toEqual([...new Set(keys)]);
  });

  it('leaves Cmd/Ctrl+W to the browser, which closes the tab with it', () => {
    // `w` is bound bare, and a bare letter never fires with a modifier held.
    // Answering to Cmd+W here would mean swallowing a keystroke people expect
    // to close the tab.
    const w = SHORTCUTS.find((s) => s.key === 'w');

    expect(w).toBeTruthy();
    expect(w.mod).toBeFalsy();
  });

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

  it('leaves a bare letter alone whenever a modifier is held', () => {
    // Those keystrokes belong to the browser or the system — Cmd+M minimises a
    // window, Cmd+Shift+T reopens a tab — and most never arrive at all.
    // Answering to them would fire the shortcut on the way past.
    expect(matchShortcut(press('m', { ctrlKey: true }))).toBeNull()
    expect(matchShortcut(press('m', { ctrlKey: true, shiftKey: true }))).toBeNull()
    expect(matchShortcut(press('t', { metaKey: true, shiftKey: true }), { mac: true })).toBeNull()
    expect(matchShortcut(press('n', { metaKey: true, shiftKey: true }), { mac: true })).toBeNull()
  })

  it('refuses a shifted version of a declared chord', () => {
    // Cmd+Shift+K is not Cmd+K.
    expect(matchShortcut(press('k', { ctrlKey: true, shiftKey: true }))).toBeNull()
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

describe('shortcutHint', () => {
  it('spells the finder for a Mac keyboard', () => {
    expect(shortcutHint('find-task', { mac: true })).toEqual({ keys: '⌘K', label: 'Search tasks' })
  })

  it('spells it for every other keyboard', () => {
    expect(shortcutHint('find-task', { mac: false })).toEqual({ keys: 'Ctrl+K', label: 'Search tasks' })
  })

  it('defaults to the non-Mac spelling rather than guessing', () => {
    expect(shortcutHint('find-task').keys).toBe('Ctrl+K')
  })

  it('offers nothing for a shortcut that does not ask to be advertised', () => {
    // A hint takes space beside the controls; most of the table has not earned
    // one and lives in the help sheet instead.
    expect(shortcutHint('new-task')).toBeNull()
    expect(shortcutHint('trajectory-view')).toBeNull()
  })

  it('offers nothing for a shortcut that does not exist', () => {
    expect(shortcutHint('no-such-shortcut')).toBeNull()
  })

  it('reads the table it is given, so a caller can test its own', () => {
    const table = [{ id: 'x', key: 'j', mod: true, label: 'X', hintLabel: 'Do the thing' }]

    expect(shortcutHint('x', { mac: true, shortcuts: table }))
      .toEqual({ keys: '⌘J', label: 'Do the thing' })
  })

  it('advertises exactly one shortcut today', () => {
    // Guards the claim the header makes: adding a hintLabel puts a hint on
    // screen, so it should be a decision rather than a side effect.
    const advertised = SHORTCUTS.filter((s) => s.hintLabel).map((s) => s.id)

    expect(advertised).toEqual(['find-task'])
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

  it('reports a shortcut that actually ran', () => {
    const onUse = vi.fn()
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, options)
    mount()

    dispatchShortcut(press('n'), { onUse })

    expect(onUse).toHaveBeenCalledWith('new-task')
  })

  it('reports nothing for a key nobody handled', () => {
    // Pressing a key on a screen that ignores it is not a use of the feature.
    const onUse = vi.fn()
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, options)
    mount()

    dispatchShortcut(press('m'), { onUse })
    dispatchShortcut(press('z'), { onUse })

    expect(onUse).not.toHaveBeenCalled()
  })

  it('does not treat the reporter as a shortcut table', () => {
    // `onUse` is pulled out of the options before they reach matchShortcut;
    // leaving it in would make every match fail.
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, options)
    mount()

    expect(dispatchShortcut(press('n'), { onUse: vi.fn() })).toBe('new-task')
  })

  it('needs no reporter at all', () => {
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': vi.fn() }, options)
    mount()

    expect(() => dispatchShortcut(press('n'))).not.toThrow()
  })

  it('passes the reporter through the listener useShortcuts installs', () => {
    // The production path: the component gives onUse to useShortcuts, not to
    // dispatchShortcut, so the keydown listener has to carry it.
    const onUse = vi.fn()
    const { options, mount } = fakeLifecycle()
    const { onKeydown } = useShortcuts({ 'new-task': vi.fn() }, { ...options, onUse })
    mount()

    onKeydown(press('n'))

    expect(onUse).toHaveBeenCalledWith('new-task')
  })

  it('answers one keypress once, however many listeners see it', () => {
    // Two registrations means two window listeners, so the same event reaches
    // dispatch twice. Running the handler twice was invisible while handlers
    // were idempotent; counting the press made it visible.
    const shell = vi.fn()
    const view = vi.fn()
    const onUse = vi.fn()
    const a = fakeLifecycle()
    const b = fakeLifecycle()
    useShortcuts({ 'chat-view': shell }, a.options)
    a.mount()
    useShortcuts({ 'chat-view': view }, b.options)
    b.mount()

    const event = press('m')
    expect(dispatchShortcut(event, { onUse })).toBe('chat-view')
    expect(dispatchShortcut(event, { onUse })).toBeNull()

    expect(view).toHaveBeenCalledOnce()
    expect(shell).not.toHaveBeenCalled()
    expect(onUse).toHaveBeenCalledOnce()
  })

  it('still answers the next keypress', () => {
    const handler = vi.fn()
    const { options, mount } = fakeLifecycle()
    useShortcuts({ 'new-task': handler }, options)
    mount()

    dispatchShortcut(press('n'))
    dispatchShortcut(press('n'))

    expect(handler).toHaveBeenCalledTimes(2)
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
