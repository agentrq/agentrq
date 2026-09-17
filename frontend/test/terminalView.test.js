// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'
import {
  useTerminalView,
  statusLabel,
  statusTone,
  hasEnded,
  endedReason,
  TERMINAL_THEME,
  TERMINAL_OPTIONS,
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

// An agent's output assumes a dark background; rendering it on white is how
// you get yellow on white.
describe('the terminal itself', () => {
  it('sets a dark background and a readable foreground', () => {
    expect(TERMINAL_THEME.background).toMatch(/^#0/)
    expect(TERMINAL_THEME.foreground).toMatch(/^#[de]/)
  })

  it('names all sixteen colours rather than leaving them to a default', () => {
    for (const name of [
      'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
      'brightBlack', 'brightRed', 'brightGreen', 'brightYellow',
      'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
    ]) {
      expect(TERMINAL_THEME[name], name).toMatch(/^#[0-9a-f]{6}$/i)
    }
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
})

describe('defaults', () => {
  it('reaches for the real API when nothing is injected', async () => {
    const v = useTerminalView({ sessionId: 's1' })
    await v.load()
    expect(v.ended.value).toBe(true)
  })
})
