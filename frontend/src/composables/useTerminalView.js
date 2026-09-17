// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The terminal page's own state: what it is showing, and how it looks.
 *
 * Separate from `useTerminalSession`, which is the wire. This is the page —
 * which session, whether it is still running, what the status line says — and
 * it is here rather than in the component so the decisions have tests on them.
 */

import { ref, computed } from 'vue'
import * as api from '../api'

/**
 * The palette xterm renders with.
 *
 * Set explicitly rather than left to xterm's defaults, and deliberately dark
 * in both themes: a terminal is a terminal, and an agent's output is full of
 * ANSI colours chosen on the assumption of a dark background. Rendering them
 * on white is how you get yellow on white.
 *
 * The sixteen are given in full because a program that asks for "bright black"
 * expects something it can read; leaving them to a default that has never seen
 * this background is where unreadable output comes from.
 */
export const TERMINAL_THEME = {
  background: '#09090b',
  foreground: '#e4e4e7',
  cursor: '#e4e4e7',
  cursorAccent: '#09090b',
  selectionBackground: 'rgba(228, 228, 231, 0.25)',

  black: '#27272a',
  red: '#f87171',
  green: '#4ade80',
  yellow: '#fbbf24',
  blue: '#60a5fa',
  magenta: '#c084fc',
  cyan: '#22d3ee',
  white: '#d4d4d8',

  brightBlack: '#52525b',
  brightRed: '#fca5a5',
  brightGreen: '#86efac',
  brightYellow: '#fcd34d',
  brightBlue: '#93c5fd',
  brightMagenta: '#d8b4fe',
  brightCyan: '#67e8f9',
  brightWhite: '#fafafa',
}

/** How xterm itself is set up. */
export const TERMINAL_OPTIONS = {
  // The agent controls the cursor; a blink the agent did not ask for is a lie
  // about what the program is doing.
  cursorBlink: true,
  cursorStyle: 'bar',
  fontFamily: '"JetBrains Mono", "Fira Code", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  fontSize: 13,
  lineHeight: 1.2,
  letterSpacing: 0,
  // Enough that scrolling back through a build is useful, bounded so a
  // runaway process cannot fill the tab's memory.
  scrollback: 5000,
  // Never: the daemon sends exactly the bytes the program produced, and a
  // terminal that converted line endings would be changing them.
  convertEol: false,
  allowProposedApi: true,
}

/** What the status line says, and how it reads. */
export function statusLabel(status) {
  return (
    {
      connecting: 'Connecting',
      connected: 'Live',
      reconnecting: 'Reconnecting',
      disconnected: 'Disconnected',
      closed: 'Closed',
      ended: 'Session ended',
    }[status] ?? status
  )
}

/**
 * The colour a status reads as.
 *
 * Returned as a token so the mapping is testable and the view owns how a token
 * looks. "Reconnecting" is deliberately the same tone as a warning rather than
 * an error: it is a state that usually fixes itself.
 */
export function statusTone(status) {
  if (status === 'connected') return 'good'
  if (status === 'connecting' || status === 'reconnecting') return 'pending'
  if (status === 'ended' || status === 'closed') return 'muted'
  return 'bad'
}

/**
 * Whether a session is over.
 *
 * A finished session's row is removed, so the page can learn this two ways:
 * the status it was last told, or the session simply not being there. Both
 * mean the same thing to somebody looking at the screen.
 */
export function hasEnded(session) {
  if (!session) return true
  return ['exited', 'killed', 'failed'].includes(session.status)
}

/**
 * How a terminal that has ended should explain itself.
 *
 * A blank rectangle is the worst version of this: it looks identical to a
 * terminal that is simply quiet, and somebody will sit waiting for output that
 * is never coming.
 */
export function endedReason(session) {
  if (!session) return 'This session has ended and is no longer on the machine.'
  if (session.status === 'failed') {
    return session.error
      ? `This agent failed to start: ${session.error}`
      : 'This agent failed to start.'
  }
  if (session.status === 'killed') return 'This session was stopped.'
  const code = session.exitCode
  if (typeof code === 'number' && code !== 0) {
    return `This agent exited with code ${code}.`
  }
  return 'This agent has exited.'
}

/**
 * The terminal page.
 *
 * @param {object} deps `sessionId` plus, in tests, the API functions
 */
export function useTerminalView(deps = {}) {
  const { sessionId, getSession = api.getSession } = deps

  const session = ref(null)
  const loading = ref(true)
  const status = ref('connecting')
  const viewers = ref([])
  const self = ref(0)

  /**
   * Which entry in the viewer list is us comes from the server rather than by
   * matching on name: the same person in two tabs is two identical names, and
   * the list alone cannot tell them apart.
   */
  const others = computed(() => viewers.value.filter((_, i) => i !== self.value))

  const ended = computed(() => !loading.value && hasEnded(session.value))

  /** The status the page shows, which an ended session overrides. */
  const shownStatus = computed(() => (ended.value ? 'ended' : status.value))

  const title = computed(() => session.value?.kind || 'Terminal')

  async function load() {
    loading.value = true
    try {
      const data = await getSession(sessionId)
      session.value = data?.session ?? null
    } catch {
      // A session that cannot be read is treated as one that is not there:
      // the page says it has ended rather than showing a terminal that will
      // never receive anything.
      session.value = null
    } finally {
      loading.value = false
    }
  }

  /** Fold a live update in. */
  function handleEvent(event) {
    if (event?.type !== 'session.updated') return
    const payload = event.payload
    if (!payload?.id || payload.id !== sessionId) return
    session.value = { ...(session.value ?? {}), ...payload }
  }

  /** A presence announcement, which arrives as a control frame. */
  function handleControl(payload) {
    try {
      const message = JSON.parse(new TextDecoder().decode(payload))
      if (message.op !== 'presence') return
      viewers.value = message.body?.viewers ?? []
      self.value = message.body?.you ?? 0
    } catch {
      // A control frame this build does not understand is not worth breaking
      // the terminal over.
    }
  }

  return {
    session,
    loading,
    status,
    viewers,
    others,
    ended,
    shownStatus,
    title,
    load,
    handleEvent,
    handleControl,
  }
}
