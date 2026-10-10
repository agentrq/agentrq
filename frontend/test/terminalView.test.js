// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  useTerminalView,
  statusLabel,
  statusTone,
  hasEnded,
  endedReason,
  terminalPath,
  TERMINAL_DARK_THEME,
  TERMINAL_LIGHT_THEME,
  LIGHT_MINIMUM_CONTRAST,
  isDarkDocument,
  terminalAppearance,
  applyTerminalAppearance,
  followDarkClass,
  TERMINAL_OPTIONS,
  TERMINAL_FONT,
  TERMINAL_FONT_SPECS,
  TERMINAL_FALLBACK_FAMILY,
  remeasureCell,
} from '../src/composables/useTerminalView.js'

const RUNNING = { id: 's1', machineId: 'm1', kind: 'claude-code', status: 'running' }

function harness(over = {}) {
  const deps = {
    sessionId: 's1',
    getSession: vi.fn().mockResolvedValue({ session: { ...RUNNING } }),
    ...over,
  }
  return { deps, v: useTerminalView(deps) }
}

const presence = (viewers, you = 0) =>
  new TextEncoder().encode(JSON.stringify({ op: 'presence', body: { viewers, you } }))

describe('terminalPath', () => {
  it('addresses a session by its id', () => {
    expect(terminalPath(RUNNING)).toBe('/sessions/s1')
  })

  it('answers with nowhere rather than a page that cannot work', () => {
    // `/sessions/undefined` is the failure this exists to prevent: it is a
    // route that resolves, loads, finds no session and tells somebody their
    // agent has ended — which is a lie, and a worse one than staying put.
    expect(terminalPath({})).toBe('')
    expect(terminalPath({ id: '' })).toBe('')
    expect(terminalPath(null)).toBe('')
    expect(terminalPath(undefined)).toBe('')
  })
})

describe('statusLabel', () => {
  it('says what each state means in words', () => {
    expect(statusLabel('connected')).toBe('Live')
    expect(statusLabel('connecting')).toBe('Connecting')
    expect(statusLabel('reconnecting')).toBe('Reconnecting')
    expect(statusLabel('ended')).toBe('Session ended')
  })
  it('shows an unknown state rather than nothing', () => {
    expect(statusLabel('something-new')).toBe('something-new')
  })
})

describe('statusTone', () => {
  it('reads live as good and dead as bad', () => {
    expect(statusTone('connected')).toBe('good')
    expect(statusTone('disconnected')).toBe('bad')
  })

  // A state that usually fixes itself should not read as an error.
  it('reads reconnecting as pending rather than as a failure', () => {
    expect(statusTone('connecting')).toBe('pending')
    expect(statusTone('reconnecting')).toBe('pending')
  })

  it('reads an ended session as quiet', () => {
    expect(statusTone('ended')).toBe('muted')
    expect(statusTone('closed')).toBe('muted')
  })
})

describe('hasEnded', () => {
  it('knows the states that are over', () => {
    for (const status of ['exited', 'killed', 'failed']) {
      expect(hasEnded({ status }), status).toBe(true)
    }
  })
  it('knows the states that are not', () => {
    expect(hasEnded({ status: 'running' })).toBe(false)
    expect(hasEnded({ status: 'starting' })).toBe(false)
  })

  // A finished session's row is removed, so "not there" and "finished" mean
  // the same thing to somebody looking at the screen.
  it('treats a session that is not there as ended', () => {
    expect(hasEnded(null)).toBe(true)
    expect(hasEnded(undefined)).toBe(true)
  })
})

// A blank rectangle is the worst version of this: it looks identical to a
// terminal that is simply quiet, and somebody will wait for output that is
// never coming.
describe('endedReason', () => {
  it('gives a failed start its reason', () => {
    expect(endedReason({ status: 'failed', error: 'no such folder: /srv/app' })).toContain(
      '/srv/app'
    )
  })
  it('says a failure happened even with no reason to give', () => {
    expect(endedReason({ status: 'failed' })).toContain('failed to start')
  })
  it('distinguishes a stop from an exit', () => {
    expect(endedReason({ status: 'killed' })).toContain('stopped')
    expect(endedReason({ status: 'exited', exitCode: 0 })).toContain('exited')
  })
  it('names a non-zero exit code, which is the part worth knowing', () => {
    expect(endedReason({ status: 'exited', exitCode: 130 })).toContain('130')
  })
  it('does not dwell on a clean exit', () => {
    expect(endedReason({ status: 'exited', exitCode: 0 })).not.toContain('code')
  })
  it('explains a session that is not there at all', () => {
    expect(endedReason(null)).toContain('no longer on the machine')
  })
})

describe('loading the session', () => {
  it('loads it', async () => {
    const h = harness()
    await h.v.load()
    expect(h.v.session.value.kind).toBe('claude-code')
    expect(h.v.ended.value).toBe(false)
    expect(h.v.title.value).toBe('claude-code')
  })

  // The row is removed when a session ends, so a 404 is the ordinary way this
  // page finds out rather than a failure.
  it('treats a session that is gone as ended', async () => {
    const h = harness({ getSession: vi.fn().mockResolvedValue(null) })
    await h.v.load()
    expect(h.v.ended.value).toBe(true)
    expect(h.v.shownStatus.value).toBe('ended')
  })

  it('treats a session it cannot read as ended rather than showing a dead terminal', async () => {
    const h = harness({ getSession: vi.fn().mockRejectedValue(new Error('offline')) })
    await h.v.load()
    expect(h.v.ended.value).toBe(true)
  })

  // A refused launch is deleted the instant it fails, so this page's own read
  // races a 404 against the event carrying the reason. The read used to win
  // and replace "failed to start: <reason>" with the generic text.
  it('keeps the reason the stream reported when its own read finds nothing', async () => {
    let settle
    const h = harness({
      getSession: vi.fn().mockReturnValue(new Promise((_, reject) => (settle = reject))),
    })
    const loading = h.v.load()

    h.v.handleEvent({
      type: 'session.updated',
      payload: { id: 's1', status: 'failed', error: 'too many sessions running' },
    })
    settle(new Error('404'))
    await loading

    expect(h.v.ended.value).toBe(true)
    expect(endedReason(h.v.session.value)).toBe('This agent failed to start: too many sessions running')
  })

  // Same race, the other way the read can come back empty.
  it('keeps it when the read resolves to no session at all', async () => {
    let settle
    const h = harness({
      getSession: vi.fn().mockReturnValue(new Promise((resolve) => (settle = resolve))),
    })
    const loading = h.v.load()

    h.v.handleEvent({
      type: 'session.updated',
      payload: { id: 's1', status: 'failed', error: 'no such directory: /srv/gone' },
    })
    settle(null)
    await loading

    expect(endedReason(h.v.session.value)).toBe('This agent failed to start: no such directory: /srv/gone')
  })

  // Before the first load there is nothing to say, and claiming "ended" would
  // flash the wrong thing on every page open.
  it('says nothing has ended while it is still loading', () => {
    const h = harness()
    expect(h.v.loading.value).toBe(true)
    expect(h.v.ended.value).toBe(false)
  })

  // An ended session overrides the socket's own status, and a live one does
  // not — otherwise the header would say "Live" over a dead terminal, or
  // "Session ended" over a working one.
  it('shows the socket status while the session is alive', async () => {
    const h = harness()
    await h.v.load()
    h.v.status.value = 'connected'
    expect(h.v.shownStatus.value).toBe('connected')

    h.v.status.value = 'reconnecting'
    expect(h.v.shownStatus.value).toBe('reconnecting')

    h.v.handleEvent({ type: 'session.updated', payload: { id: 's1', status: 'exited' } })
    expect(h.v.shownStatus.value).toBe('ended')
  })

  it('falls back to a generic title', async () => {
    const h = harness({ getSession: vi.fn().mockResolvedValue({ session: { id: 's1' } }) })
    await h.v.load()
    expect(h.v.title.value).toBe('Terminal')
    expect(h.v.subtitle.value).toBe('')
  })

  // "claude-code" names the kind of agent, not which one you are looking at.
  // Somebody arriving from a link deserves to see what it is working on.
  it('is headed by the workspace, with the kind underneath', async () => {
    const h = harness({
      getSession: vi
        .fn()
        .mockResolvedValue({ session: { id: 's1', kind: 'claude-code', workspaceName: 'Q3 migration' } }),
    })
    await h.v.load()
    expect(h.v.title.value).toBe('Q3 migration')
    expect(h.v.subtitle.value).toBe('claude-code')
  })

  // A session the server named but could not classify: the heading is still
  // the workspace, and there is nothing to say underneath.
  it('says nothing underneath when there is no kind to say', async () => {
    const h = harness({
      getSession: vi.fn().mockResolvedValue({ session: { id: 's1', workspaceName: 'Q3 migration' } }),
    })
    await h.v.load()
    expect(h.v.title.value).toBe('Q3 migration')
    expect(h.v.subtitle.value).toBe('')
  })

  // A session whose workspace has gone, or one recorded before the name was
  // carried, keeps the heading it always had rather than losing one.
  it('keeps the kind as the heading when no workspace is named', async () => {
    const h = harness({
      getSession: vi.fn().mockResolvedValue({ session: { id: 's1', kind: 'acp-gateway', workspaceName: '  ' } }),
    })
    await h.v.load()
    expect(h.v.title.value).toBe('acp-gateway')
    expect(h.v.subtitle.value).toBe('')
  })
})

// The page says which machine the terminal is running on, through the link
// back to that machine.
describe('the machine it is running on', () => {
  it('is named when the server named it', async () => {
    const h = harness({
      getSession: vi.fn().mockResolvedValue({ session: { ...RUNNING, machineName: ' studio-mac ' } }),
    })
    await h.v.load()
    expect(h.v.machineLabel.value).toBe('studio-mac')
  })

  it('is "Machine" before the session loads, or when it carries no name', async () => {
    const h = harness({
      getSession: vi.fn().mockResolvedValue({ session: { ...RUNNING, machineName: '  ' } }),
    })
    expect(h.v.machineLabel.value).toBe('Machine')
    await h.v.load()
    expect(h.v.machineLabel.value).toBe('Machine')
  })

  // A live update carries no name, and must not take the one already shown.
  it('survives a live update', async () => {
    const h = harness({
      getSession: vi.fn().mockResolvedValue({ session: { ...RUNNING, machineName: 'studio-mac' } }),
    })
    await h.v.load()
    h.v.handleEvent({ type: 'session.updated', payload: { id: 's1', status: 'running' } })
    expect(h.v.machineLabel.value).toBe('studio-mac')
  })
})

/**
 * Whether the heading may carry the link to the workspace.
 *
 * The page used to offer a separate "Workspace" breadcrumb beside the heading,
 * which was two controls for one destination while the heading was already the
 * workspace's name. The heading takes the link over — but only when it *is*
 * that name, and the two questions genuinely come apart: naming is best-effort
 * on the server, so a session can carry a workspace id while its heading falls
 * back to the kind. Linking a heading that reads "claude-code" to a workspace
 * would be a link whose text is not what it opens.
 */
describe('whether the heading is the workspace name', () => {
  it('is, when the server named the workspace', async () => {
    const h = harness({
      getSession: vi
        .fn()
        .mockResolvedValue({ session: { id: 's1', kind: 'claude-code', workspaceName: 'Q3 migration' } }),
    })
    await h.v.load()
    expect(h.v.titleIsWorkspaceName.value).toBe(true)
  })

  // The case the separate breadcrumb still exists for: there is somewhere to
  // go, and the heading is not it.
  it('is not, when the heading fell back to the kind', async () => {
    const h = harness({
      getSession: vi
        .fn()
        .mockResolvedValue({ session: { id: 's1', kind: 'claude-code', workspaceId: 'w1' } }),
    })
    await h.v.load()
    expect(h.v.title.value).toBe('claude-code')
    expect(h.v.titleIsWorkspaceName.value).toBe(false)
  })

  it('is not, when a name of only spaces leaves the heading on the kind', async () => {
    const h = harness({
      getSession: vi
        .fn()
        .mockResolvedValue({ session: { id: 's1', kind: 'acp-gateway', workspaceName: '  ' } }),
    })
    await h.v.load()
    expect(h.v.titleIsWorkspaceName.value).toBe(false)
  })

  // Before anything has loaded the heading is the placeholder, which is not a
  // workspace and must not be dressed as a link to one.
  it('is not, before the session has loaded', () => {
    const h = harness()
    expect(h.v.title.value).toBe('Terminal')
    expect(h.v.titleIsWorkspaceName.value).toBe(false)
  })
})

describe('live updates', () => {
  it('folds a state change into the session', async () => {
    const h = harness()
    await h.v.load()
    h.v.handleEvent({ type: 'session.updated', payload: { id: 's1', status: 'exited', exitCode: 0 } })
    expect(h.v.ended.value).toBe(true)
    // Merged rather than replaced: the event carries a status and not a kind.
    expect(h.v.session.value.kind).toBe('claude-code')
  })

  it('ignores another session, and events it does not understand', async () => {
    const h = harness()
    await h.v.load()
    h.v.handleEvent({ type: 'session.updated', payload: { id: 'other', status: 'exited' } })
    h.v.handleEvent({ type: 'session.updated', payload: {} })
    h.v.handleEvent({ type: 'machine.updated', payload: { id: 's1' } })
    h.v.handleEvent(undefined)
    expect(h.v.ended.value).toBe(false)
  })

  // An event can arrive before the first load finishes.
  it('copes with an event for a session it has not loaded yet', () => {
    const h = harness()
    h.v.handleEvent({ type: 'session.updated', payload: { id: 's1', status: 'running' } })
    expect(h.v.session.value.status).toBe('running')
  })
})

describe('presence', () => {
  // Which entry is us comes from the server: the same person in two tabs is
  // two identical names, and the list alone cannot tell them apart.
  it('leaves us out of the list of other people', () => {
    const h = harness()
    h.v.handleControl(presence(['Ada', 'Grace'], 0))
    expect(h.v.others.value).toEqual(['Grace'])

    h.v.handleControl(presence(['Ada', 'Grace'], 1))
    expect(h.v.others.value).toEqual(['Ada'])
  })

  it('shows nobody else when watching alone', () => {
    const h = harness()
    h.v.handleControl(presence(['Ada'], 0))
    expect(h.v.others.value).toEqual([])
  })

  it('ignores a control frame it cannot read, and ops it does not know', () => {
    const h = harness()
    h.v.handleControl(new TextEncoder().encode('{not json'))
    h.v.handleControl(new TextEncoder().encode('{"op":"somethingElse"}'))
    expect(h.v.others.value).toEqual([])
  })

  it('copes with a presence frame carrying no body', () => {
    const h = harness()
    h.v.handleControl(new TextEncoder().encode('{"op":"presence"}'))
    expect(h.v.others.value).toEqual([])
  })
})

const ANSI = [
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow',
  'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
]

/** WCAG contrast ratio between two `#rrggbb` colours. */
function contrast(a, b) {
  const lum = (hex) => {
    const [r, g, b] = [1, 3, 5]
      .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
      .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
    return 0.2126 * r + 0.7152 * g + 0.0722 * b
  }
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

// An agent's output assumes a dark background; rendering it on white is how
// you get yellow on white.
describe('the terminal itself', () => {
  it('sets a dark background and a readable foreground in dark mode', () => {
    expect(TERMINAL_DARK_THEME.background).toMatch(/^#0/)
    expect(TERMINAL_DARK_THEME.foreground).toMatch(/^#[de]/)
  })

  it('names all sixteen colours in both palettes rather than leaving them to a default', () => {
    for (const theme of [TERMINAL_DARK_THEME, TERMINAL_LIGHT_THEME]) {
      for (const name of ANSI) {
        expect(theme[name], name).toMatch(/^#[0-9a-f]{6}$/i)
      }
    }
  })

  // Yellow, white, bright white and bright black are the ones a light theme
  // usually gets wrong, so every colour is measured rather than eyeballed.
  it('keeps every light colour readable on the light background', () => {
    const bg = TERMINAL_LIGHT_THEME.background
    for (const name of [...ANSI, 'foreground', 'cursor']) {
      expect(contrast(TERMINAL_LIGHT_THEME[name], bg), name).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('uses the light palette and enforces contrast only in light mode', () => {
    expect(terminalAppearance(true)).toEqual({ theme: TERMINAL_DARK_THEME, minimumContrastRatio: 1 })
    expect(terminalAppearance(false)).toEqual({
      theme: TERMINAL_LIGHT_THEME,
      minimumContrastRatio: LIGHT_MINIMUM_CONTRAST,
    })
  })

  // The daemon sends exactly the bytes the program produced; a terminal that
  // converted line endings would be changing them.
  it('never converts line endings', () => {
    expect(TERMINAL_OPTIONS.convertEol).toBe(false)
  })

  it('keeps enough scrollback to be useful and not enough to be a leak', () => {
    expect(TERMINAL_OPTIONS.scrollback).toBeGreaterThan(1000)
    expect(TERMINAL_OPTIONS.scrollback).toBeLessThanOrEqual(10000)
  })

  // Anything above 1 puts a gap between rows, and a vertical box-drawing
  // character cannot reach the row above it across a gap — so an agent's
  // framed interface renders as loose fragments. This was 1.2, and it looked
  // like a readability setting rather than the rendering bug it was.
  it('gives a row exactly the height of its glyphs, so box drawing joins up', () => {
    expect(TERMINAL_OPTIONS.lineHeight).toBe(1)
  })

  // The point of shipping a font is that two people see the same terminal.
  // A family named here and loaded nowhere undoes that on its own: it is used
  // by whoever happens to have it installed and by nobody else, so a rendering
  // report stops being reproducible. Everything after the shipped face is a
  // generic or a platform font that needs no loading.
  it('names no font it does not ship', () => {
    const named = TERMINAL_OPTIONS.fontFamily
      .split(',')
      .map((f) => f.trim().replace(/^"|"$/g, ''))
    expect(named[0]).toBe(TERMINAL_FONT)
    expect(named.slice(1)).toEqual([
      'ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace',
    ])
  })

  // Asked for by `refitWhenFontsLoad`, which is what closes the race between
  // the cell xterm measures at open and the face that arrives after it. A spec
  // for a size the terminal does not use would wait on the wrong face.
  it('asks for both faces at the size the terminal renders', () => {
    expect(TERMINAL_FONT_SPECS).toEqual([
      `${TERMINAL_OPTIONS.fontSize}px "${TERMINAL_FONT}"`,
      `bold ${TERMINAL_OPTIONS.fontSize}px "${TERMINAL_FONT}"`,
    ])
  })
})

/**
 * The bit that looks unnecessary and is the whole fix.
 *
 * xterm caches the character cell it measured when the terminal opened and
 * re-measures for nothing less than a *changed* `fontFamily` or `fontSize`. Its
 * options service drops an assignment equal to the current value, so the
 * obvious `term.options.fontFamily = term.options.fontFamily` is silently
 * nothing, the fit that follows divides the box by the stale cell, and the
 * terminal keeps the columns it measured against the fallback.
 */
describe('remeasureCell', () => {
  /** A terminal whose options behave the way xterm's really do. */
  function fakeTerm(fontFamily = TERMINAL_OPTIONS.fontFamily) {
    const seen = []
    return {
      cleared: 0,
      options: {
        get fontFamily() {
          return fontFamily
        },
        set fontFamily(v) {
          // The rule this function exists to work around.
          if (v === fontFamily) return
          fontFamily = v
          seen.push(v)
        },
      },
      clearTextureAtlas() {
        this.cleared += 1
      },
      seen,
    }
  }

  it('moves the stack off the shipped face and back, so the cell is measured again', () => {
    const term = fakeTerm()

    expect(remeasureCell(term)).toBe(true)
    expect(term.seen).toEqual([TERMINAL_FALLBACK_FAMILY, TERMINAL_OPTIONS.fontFamily])
    // Ends where it started: the poke is a means, not a change.
    expect(term.options.fontFamily).toBe(TERMINAL_OPTIONS.fontFamily)
    expect(term.cleared).toBe(1)
  })

  it('says so when there is nothing to move it off', () => {
    const term = fakeTerm(TERMINAL_FALLBACK_FAMILY)

    // Both assignments would equal the current value, so xterm would drop both
    // and no cell would be measured. Reporting that beats pretending.
    expect(remeasureCell(term)).toBe(false)
    expect(term.seen).toEqual([])
    expect(term.cleared).toBe(0)
  })

  it('does nothing to a terminal that is not there', () => {
    // The font can land after unmount; this is the second guard behind that.
    expect(remeasureCell(null)).toBe(false)
  })

  it('does not need a renderer that has an atlas', () => {
    // No renderer addon is loaded today, so this is xterm's own no-op — but a
    // fake terminal without it must not take the re-measure down with it.
    const term = fakeTerm()
    delete term.clearTextureAtlas

    expect(remeasureCell(term)).toBe(true)
    expect(term.seen).toEqual([TERMINAL_FALLBACK_FAMILY, TERMINAL_OPTIONS.fontFamily])
  })
})

describe('defaults', () => {
  it('reaches for the real API when nothing is injected', async () => {
    const v = useTerminalView({ sessionId: 's1' })
    await v.load()
    expect(v.ended.value).toBe(true)
  })
})

describe('following the app theme', () => {
  const html = document.documentElement
  beforeEach(() => {
    html.classList.remove('dark')
  })

  it('reads the .dark class on <html>', () => {
    expect(isDarkDocument()).toBe(false)
    html.classList.add('dark')
    expect(isDarkDocument()).toBe(true)
  })

  it('re-themes a running terminal in place and drops its cached glyphs', () => {
    const term = { options: { fontSize: 13 }, clearTextureAtlas: vi.fn() }

    applyTerminalAppearance(term, false)
    expect(term.options).toEqual({
      fontSize: 13,
      theme: TERMINAL_LIGHT_THEME,
      minimumContrastRatio: LIGHT_MINIMUM_CONTRAST,
    })
    expect(term.clearTextureAtlas).toHaveBeenCalledTimes(1)

    applyTerminalAppearance(term, true)
    expect(term.options.theme).toBe(TERMINAL_DARK_THEME)
    expect(term.options.minimumContrastRatio).toBe(1)
  })

  it('copes with a terminal that has no texture atlas', () => {
    const term = { options: {} }
    applyTerminalAppearance(term, true)
    expect(term.options.theme).toBe(TERMINAL_DARK_THEME)
  })

  it('reports each flip of the class, and nothing else', async () => {
    const seen = []
    const watch = followDarkClass({ onChange: (d) => seen.push(d) })

    html.classList.add('dark')
    await Promise.resolve()
    html.classList.add('other') // a class change that is not a theme change
    await Promise.resolve()
    html.classList.remove('dark')
    await Promise.resolve()
    expect(seen).toEqual([true, false])

    watch.stop()
    html.classList.add('dark')
    await Promise.resolve()
    expect(seen).toEqual([true, false])
    html.classList.remove('other')
  })

  it('watches any root it is given', async () => {
    const root = document.createElement('div')
    const seen = []
    const watch = followDarkClass({ root, onChange: (d) => seen.push(d) })
    root.className = 'dark'
    await Promise.resolve()
    expect(seen).toEqual([true])
    watch.stop()
  })
})
