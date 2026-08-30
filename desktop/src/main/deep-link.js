/**
 * `agentrq://` deep links.
 *
 * A link from a Slack message, an email or another app should open the desktop
 * app at the right place. The URL maps onto the shared route table, so
 * `agentrq://workspaces/<id>/tasks/<id>` becomes the in-app route
 * `/workspaces/<id>/tasks/<id>`.
 *
 * A deep link is untrusted input from outside the app, so the result is checked
 * against the routes that actually exist rather than passed through. Anything
 * unrecognised opens the app at its default view — the user asked to see
 * AgentRQ, and showing them the wrong screen is worse than showing them home.
 *
 * Pure, so the parsing rules are testable without Electron.
 */

export const DEEP_LINK_SCHEME = 'agentrq'

/**
 * Top-level sections a link may address. These mirror the route table in
 * `frontend/src/app.js`; a link outside them has no destination.
 */
export const LINKABLE_SECTIONS = ['workspaces', 'tasks', 'events', 'workflows']

/**
 * Turn a deep-link URL into an in-app route.
 *
 * @param {string} url
 * @returns {string|null} the route, or null when the link is not one we serve.
 */
export function parseDeepLink(url) {
  let parsed
  try {
    parsed = new URL(String(url ?? ''))
  } catch {
    return null
  }

  if (parsed.protocol !== `${DEEP_LINK_SCHEME}:`) return null

  // `agentrq://workspaces/abc` parses with host 'workspaces' and pathname
  // '/abc', while `agentrq:///workspaces/abc` puts it all in the pathname.
  // Both are things a link can look like in the wild, so both are accepted.
  const raw = `${parsed.host}${parsed.pathname}`
  const segments = raw
    .split('/')
    .filter((segment) => segment !== '')
    .map((segment) => {
      try {
        return decodeURIComponent(segment)
      } catch {
        return segment
      }
    })

  if (segments.length === 0) return '/'
  if (!LINKABLE_SECTIONS.includes(segments[0])) return null

  // Every remaining segment must be a plain identifier. This is external input,
  // and a segment carrying a slash, a traversal or a control character has no
  // business reaching the router.
  const rest = segments.slice(1)
  if (rest.some((segment) => !/^[A-Za-z0-9_-]+$/.test(segment))) return null

  return `/${segments.join('/')}${parsed.search}`
}

/**
 * Find the deep link in a set of process arguments.
 *
 * Windows and Linux deliver the link as an argv entry on the second launch,
 * rather than through the `open-url` event macOS uses.
 */
export function deepLinkFromArgv(argv = []) {
  const match = argv.find((arg) => typeof arg === 'string' && arg.startsWith(`${DEEP_LINK_SCHEME}://`))
  return match ?? null
}
