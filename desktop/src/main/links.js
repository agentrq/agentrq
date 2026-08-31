/**
 * Where a link should open.
 *
 * The shell used to hand every link that was not the app itself to the system
 * browser. Reading the docs, the terms, or a URL attached to a message threw
 * the user out of AgentRQ entirely, with nothing to come back to — the PWA
 * opens the same links in a window of its own that can simply be closed, and
 * this is what restores that.
 *
 * Not everything belongs in a window, which is why this is a classification
 * rather than a boolean:
 *
 * - `app://` is the application; the router already handles those in place.
 * - http(s) is web content, and an in-app window can show it.
 * - `mailto:`, `tel:` and their siblings address a program that is not a
 *   browser. A BrowserWindow cannot render them, so they go to the OS.
 * - `javascript:`, `data:` and `file:` are never opened from a link at all.
 *   Handing any of those to `shell.openExternal` is the well-worn way for
 *   injected markup in a message body to reach the machine it is running on.
 *
 * Kept apart from index.js because that module needs a live Electron to import,
 * and this decision is the part worth testing.
 */

export const LinkTarget = {
  /** The app itself; let the renderer's router handle it. */
  App: 'app',
  /** Web content, shown in a closable window belonging to the app. */
  Window: 'window',
  /** Hand to the operating system: mail client, dialler, and so on. */
  System: 'system',
  /** Refuse. */
  Blocked: 'blocked',
}

/**
 * Schemes that address something other than a browser, and that are safe to
 * pass to the OS. An allowlist, because the interesting failure is the scheme
 * nobody thought about.
 */
const SYSTEM_SCHEMES = new Set(['mailto:', 'tel:', 'sms:', 'facetime:'])

/**
 * `URL.origin` is useless here: Node returns the string "null" for any scheme
 * it does not consider special, and `app://` is not special. Protocol and host
 * are populated for every scheme, so they are what the comparison uses.
 */
function originOf(url) {
  return `${url.protocol}//${url.host}`
}

/**
 * @param {string} rawUrl
 * @param {{ appOrigin?: string }} [options]
 * @returns {typeof LinkTarget[keyof typeof LinkTarget]}
 */
export function classifyLink(rawUrl, { appOrigin = '' } = {}) {
  let url
  try {
    url = new URL(String(rawUrl ?? ''))
  } catch {
    // Not a URL at all — a relative href that reached here, or junk.
    return LinkTarget.Blocked
  }

  if (appOrigin && originOf(url) === appOrigin) return LinkTarget.App
  if (url.protocol === 'http:' || url.protocol === 'https:') return LinkTarget.Window
  if (SYSTEM_SCHEMES.has(url.protocol)) return LinkTarget.System
  return LinkTarget.Blocked
}

/**
 * Size for a link window, derived from the parent so the result is usable on a
 * laptop and not comically small beside a large main window.
 *
 * @param {{ width: number, height: number } | null | undefined} parentBounds
 */
export function linkWindowBounds(parentBounds) {
  const width = Math.round(Math.min(Math.max((parentBounds?.width ?? 1024) * 0.8, 640), 1200))
  const height = Math.round(Math.min(Math.max((parentBounds?.height ?? 768) * 0.85, 480), 900))
  return { width, height }
}
