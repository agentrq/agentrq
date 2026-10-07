// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
 * The palette xterm renders with in dark mode.
 *
 * Set explicitly rather than left to xterm's defaults: an agent's output is
 * full of ANSI colours chosen on the assumption of a dark background, and this
 * is the background they were chosen for.
 *
 * The sixteen are given in full because a program that asks for "bright black"
 * expects something it can read; leaving them to a default that has never seen
 * this background is where unreadable output comes from.
 */
export const TERMINAL_DARK_THEME = {
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

/**
 * The palette in light mode.
 *
 * Every one of the sixteen is re-picked to read at 4.5:1 on this background,
 * and a test holds them there. "White" and "bright white" are dark greys:
 * programs print them as text expecting it to be readable, and white on
 * white is the yellow-on-white bug again.
 */
export const TERMINAL_LIGHT_THEME = {
  background: '#fafafa',
  foreground: '#27272a',
  cursor: '#27272a',
  cursorAccent: '#fafafa',
  selectionBackground: 'rgba(39, 39, 42, 0.18)',

  black: '#3f3f46',
  red: '#b91c1c',
  green: '#15803d',
  yellow: '#b45309',
  blue: '#1d4ed8',
  magenta: '#7e22ce',
  cyan: '#0e7490',
  white: '#52525b',

  brightBlack: '#71717a',
  brightRed: '#dc2626',
  brightGreen: '#166534',
  brightYellow: '#92400e',
  brightBlue: '#2563eb',
  brightMagenta: '#9333ea',
  brightCyan: '#155e75',
  brightWhite: '#18181b',
}

/**
 * The contrast xterm enforces on glyphs in light mode.
 *
 * The palette covers the sixteen ANSI colours only. Agents such as Claude Code
 * also print 24-bit colours picked for a dark background, which no palette can
 * remap, so xterm darkens any glyph below this ratio against its own cell.
 * xterm halves it for dim text, which therefore stays dimmer and still reads.
 * Dark mode keeps xterm's default of 1, i.e. off, so it is exactly as before.
 */
export const LIGHT_MINIMUM_CONTRAST = 4.5

/** Whether the app is in dark mode: the `.dark` class, never the OS setting. */
export function isDarkDocument(root = document.documentElement) {
  return root.classList.contains('dark')
}

/** The xterm options that differ between the app's two themes. */
export function terminalAppearance(dark) {
  return dark
    ? { theme: TERMINAL_DARK_THEME, minimumContrastRatio: 1 }
    : { theme: TERMINAL_LIGHT_THEME, minimumContrastRatio: LIGHT_MINIMUM_CONTRAST }
}

/**
 * Re-theme a running terminal, keeping its connection and scrollback.
 *
 * The atlas is cleared because the WebGL renderer caches glyphs already
 * painted in the old colours.
 */
export function applyTerminalAppearance(term, dark) {
  Object.assign(term.options, terminalAppearance(dark))
  term.clearTextureAtlas?.()
}

/**
 * Call `onChange(dark)` whenever the `.dark` class on `root` flips.
 *
 * The class rather than the theme store, because "system" follows the OS and
 * the store only re-applies the class; the class is what every other surface
 * on the page is already painted from.
 */
export function followDarkClass({ onChange, root = document.documentElement }) {
  let dark = isDarkDocument(root)
  const observer = new MutationObserver(() => {
    const now = isDarkDocument(root)
    if (now === dark) return
    dark = now
    onChange(now)
  })
  observer.observe(root, { attributes: true, attributeFilter: ['class'] })
  return { stop: () => observer.disconnect() }
}

/**
 * The font the terminal is shipped with.
 *
 * Self-hosted through `@fontsource/jetbrains-mono` (imported by `style.css`),
 * never a font CDN: the desktop build serves the app from `app://` under
 * `font-src 'self' data:`, so an external font URL works in the browser and is
 * blocked there — the trap `docs/agents/desktop.md` exists for.
 *
 * Its subsets carry Google's `unicode-range`s, which stop short of box drawing
 * (U+2500-257F). Those glyphs come from the fallback below, exactly as they do
 * today; what shipping this fixes is the *cell*, which is measured from the
 * primary font and was therefore whatever each person happened to have.
 */
export const TERMINAL_FONT = 'JetBrains Mono'

/**
 * The stack under it, and a value in its own right.
 *
 * Named separately because `remeasureCell` needs a font stack that is real,
 * differs from the full one, and does not depend on anything being loaded.
 */
export const TERMINAL_FALLBACK_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'

/**
 * What the terminal asks for.
 *
 * Nothing is named here that the app does not ship. `"Fira Code"` used to sit
 * second and was never loaded either, which meant it could only ever be reached
 * for the glyphs the shipped subset lacks — i.e. box drawing — so whoever had
 * Fira Code installed got different box characters from everybody else. Naming
 * a font nobody loads is how a terminal stops looking the same on two machines,
 * which is the whole reason this file now ships one.
 */
export const TERMINAL_FONT_FAMILY = `"${TERMINAL_FONT}", ${TERMINAL_FALLBACK_FAMILY}`

/** How xterm itself is set up. */
export const TERMINAL_OPTIONS = {
  // The agent controls the cursor; a blink the agent did not ask for is a lie
  // about what the program is doing.
  cursorBlink: true,
  cursorStyle: 'bar',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: 13,
  // Exactly 1, and this is not a style preference. A row taller than its
  // glyphs cannot join the row above it, so every vertical box-drawing
  // character — │ ├ └ ╰ — is left hanging in space and an agent's framed
  // interface renders as loose fragments. It was 1.2, which is comfortable
  // for prose and wrong for a terminal, where most of what arrives is drawn
  // rather than written. Verified by rendering the same output at both
  // values; see the note in docs/agents/machines-and-daemon.md.
  lineHeight: 1,
  letterSpacing: 0,
  // Enough that scrolling back through a build is useful, bounded so a
  // runaway process cannot fill the tab's memory.
  scrollback: 5000,
  // Never: the daemon sends exactly the bytes the program produced, and a
  // terminal that converted line endings would be changing them.
  convertEol: false,
  allowProposedApi: true,
}

/**
 * What to hand `document.fonts.load()`, as CSS font shorthand.
 *
 * Both weights, because xterm renders bold text in the bold face. Built from
 * `TERMINAL_OPTIONS.fontSize` rather than written out, so a change of size
 * cannot leave this waiting on a face the terminal does not use.
 */
export const TERMINAL_FONT_SPECS = [
  `${TERMINAL_OPTIONS.fontSize}px "${TERMINAL_FONT}"`,
  `bold ${TERMINAL_OPTIONS.fontSize}px "${TERMINAL_FONT}"`,
]

/**
 * Make xterm measure a character cell again.
 *
 * Needed because **a terminal does not re-measure when its font arrives, and a
 * re-fit on its own changes nothing.** `FitAddon` divides the host box by the
 * cell xterm cached when the terminal opened, so fitting after the webfont
 * lands returns the same columns it already had, computed from the fallback.
 * Measured: without this the terminal stayed at 74 columns of a 6.5px cell
 * while rendering a 7.8px font, and its frame ran off the right of the box.
 *
 * The only public lever is `term.options`, and xterm re-measures on a *change*
 * of `fontFamily` or `fontSize` — its options service drops an assignment equal
 * to the current value, so setting the same stack back does nothing at all. So
 * the option is moved off the shipped face and back onto it, both assignments
 * in the same tick, before anything is painted.
 *
 * The atlas goes with it: a canvas or WebGL renderer caches glyphs drawn in the
 * old face and would otherwise keep painting them at the new cell size. There
 * is no renderer addon today, so `clearTextureAtlas` is xterm's own no-op —
 * and already correct for the day there is one.
 *
 * @param {import('@xterm/xterm').Terminal} term
 * @returns {boolean} whether a re-measure was actually provoked.
 */
export function remeasureCell(term) {
  const wanted = term?.options?.fontFamily
  // Nothing to move it off: the stack is already the one the poke would use, so
  // both assignments would be dropped and this would quietly do nothing.
  if (!wanted || wanted === TERMINAL_FALLBACK_FAMILY) return false

  term.options.fontFamily = TERMINAL_FALLBACK_FAMILY
  term.options.fontFamily = wanted
  term.clearTextureAtlas?.()
  return true
}

/**
 * Where a session's terminal lives.
 *
 * Empty for anything that is not a session with an id, and that empty is the
 * case worth having rather than an oversight. A launch that resolved to `{}`
 * — a server shaping the body differently, an error page parsed as JSON —
 * would otherwise send the browser to `/sessions/undefined`, which loads,
 * finds no session and reports that the agent has ended. A caller handed ''
 * can stay where it is and say nothing, which is the truthful answer.
 */
export function terminalPath(session) {
  return session?.id ? `/sessions/${session.id}` : ''
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
  /** The workspace this agent is working in, when the server named one. */
  const workspaceName = computed(() => (session.value?.workspaceName ?? '').trim())

  const others = computed(() => viewers.value.filter((_, i) => i !== self.value))

  const ended = computed(() => !loading.value && hasEnded(session.value))

  /** The status the page shows, which an ended session overrides. */
  const shownStatus = computed(() => (ended.value ? 'ended' : status.value))

  /**
   * What the page is called.
   *
   * The workspace, for the same reason the machine's list uses it: "claude-code"
   * names the kind of agent and not which one you are looking at, and somebody
   * arriving here from a link deserves to see what it is working on.
   */
  const title = computed(() => workspaceName.value || session.value?.kind || 'Terminal')

  /**
   * The kind, said underneath when the heading is the workspace.
   *
   * No optional chaining on the session: a workspace name can only have come
   * from one, so reaching here with nothing is not a case, and writing it as
   * though it were leaves a branch no test can reach.
   */
  const subtitle = computed(() => {
    if (!workspaceName.value) return ''
    return session.value.kind || ''
  })

  /**
   * Whether the heading is the workspace's own name.
   *
   * Asked because the heading is what links to the workspace, and it may only
   * do that when it is actually naming one. `title` falls back to the kind and
   * then to "Terminal", and **a session can have a workspace id while its
   * heading is the kind** — naming is best-effort on the server, so the two
   * questions genuinely come apart. Linking a heading that reads "claude-code"
   * to a workspace would be a link whose text is not what it opens.
   *
   * Written as the comparison rather than as `!!workspaceName` so that it
   * stays true to `title` if the fallbacks above ever change.
   */
  const titleIsWorkspaceName = computed(
    () => !!workspaceName.value && title.value === workspaceName.value
  )

  async function load() {
    loading.value = true
    try {
      const data = await getSession(sessionId)
      session.value = data?.session ?? session.value
    } catch {
      // Deliberately nothing. A session that cannot be read is one that is not
      // there, which is already what `session` says — and if the stream got
      // here first, what it reported is the better answer.
      //
      // That second case is the one this is written for. A launch the daemon
      // refuses is deleted the instant it fails, so this read races a 404
      // against the `session.updated` event carrying the reason. Clearing the
      // session here replaced "this agent failed to start: <reason>" with the
      // generic "no longer on the machine", and the reason was then only in
      // the daemon's log.
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
    subtitle,
    titleIsWorkspaceName,
    load,
    handleEvent,
    handleControl,
  }
}
