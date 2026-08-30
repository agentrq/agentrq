/**
 * Which AgentRQ server the desktop app talks to.
 *
 * Phase 1 only needs a resolved URL; the first-run connection screen, live
 * validation and the switch-server flow land in phase 3 (task 0hua8EL8awL).
 * The normalisation rules live here now so that work can build on them rather
 * than reinvent them.
 */

export const DEFAULT_SERVER_URL = 'http://localhost:3000'

/**
 * Turn whatever the user typed into a URL we can safely resolve against.
 *
 * A bare host is assumed to be http:// — the common case is a self-hosted
 * instance on a LAN address or localhost, where demanding a scheme would just
 * be friction. Anything that is not http(s) is rejected outright, so a stored
 * value cannot smuggle in file:// or javascript:.
 *
 * @returns {{ ok: true, url: string } | { ok: false, reason: string }}
 */
export function normalizeServerUrl(input) {
  const raw = String(input ?? '').trim()
  if (!raw) return { ok: false, reason: 'Server URL is required' }

  const withScheme = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(raw) ? raw : `http://${raw}`

  let parsed
  try {
    parsed = new URL(withScheme)
  } catch {
    return { ok: false, reason: 'Not a valid URL' }
  }

  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    return { ok: false, reason: 'Server URL must use http or https' }
  }
  // No host check is needed: http and https are "special" schemes, and the URL
  // parser already rejects an empty host for those (`http://` throws above).

  // Only the origin and any base path matter; a query or fragment on the server
  // URL would be silently dropped when resolving /api/v1 against it, so drop it
  // here where it is visible instead.
  parsed.search = ''
  parsed.hash = ''
  // Trailing slash removed so `new URL('/api/v1', base)` behaves predictably.
  const normalized = parsed.toString().replace(/\/$/, '')
  return { ok: true, url: normalized }
}

/**
 * Resolve the server URL for this launch. The environment variable exists so a
 * developer (or the dev script) can point at a scratch backend without touching
 * stored settings.
 */
export function resolveServerUrl({ env = {}, stored = '' } = {}) {
  for (const candidate of [env.AGENTRQ_SERVER_URL, stored]) {
    if (!candidate) continue
    const result = normalizeServerUrl(candidate)
    if (result.ok) return result.url
  }
  return DEFAULT_SERVER_URL
}
