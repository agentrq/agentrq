/**
 * Which AgentRQ server the desktop app talks to, and how that choice is stored.
 *
 * The desktop app is a client: the server lives wherever the user runs it, so
 * the app has to be told once and remember. Everything here takes its I/O as
 * arguments so the rules can be tested without Electron or a filesystem.
 */

export const DEFAULT_SERVER_URL = 'http://localhost:3000'

/** Filename under Electron's userData directory. */
export const CONFIG_FILENAME = 'agentrq-desktop.json'

/**
 * Shape version of the stored config.
 *
 * v1: { serverUrl }
 * v2: adds { mutedWorkspaces } — workspaces the desktop app must not raise
 *     notifications for. A v1 file migrates forward by defaulting it to empty,
 *     which is the same behaviour it had.
 */
export const CONFIG_VERSION = 2

/** Endpoint used to decide whether a URL is really an AgentRQ server. */
export const CONFIG_PROBE_PATH = '/api/v1/auth/config'

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

/**
 * Bring a stored config forward to the current shape.
 *
 * Anything unrecognised degrades to "no server configured", which sends the
 * user to the connection screen. That is a mildly annoying outcome and a
 * completely safe one — far better than booting the app pointed at a URL we
 * could not actually parse.
 *
 * A file written by a *newer* build is treated the same way: its `serverUrl` is
 * kept if it still looks like a URL, and every other key is dropped rather than
 * guessed at.
 */
export function migrateConfig(raw) {
  const empty = { version: CONFIG_VERSION, serverUrl: '', mutedWorkspaces: [] }
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return empty

  const normalized = normalizeServerUrl(raw.serverUrl)
  return {
    version: CONFIG_VERSION,
    serverUrl: normalized.ok ? normalized.url : '',
    // Absent in v1, and anything that is not a list of ids is not something to
    // guess at. Empty means "notify for everything", which is what v1 did.
    mutedWorkspaces: Array.isArray(raw.mutedWorkspaces)
      ? raw.mutedWorkspaces.filter((id) => typeof id === 'string' && id !== '')
      : [],
  }
}

/**
 * Persisted server choice.
 *
 * @param {object} deps
 * @param {() => Promise<string>} deps.readFile   Rejects when the file is absent.
 * @param {(contents: string) => Promise<void>} deps.writeFile
 */
export function createServerConfigStore({ readFile, writeFile }) {
  const write = (config) =>
    writeFile(JSON.stringify({ ...config, version: CONFIG_VERSION }, null, 2))

  return {
    /**
     * @returns {Promise<{ version: number, serverUrl: string }>} always a valid
     *          shape — a missing or corrupt file reads as "not configured".
     */
    async load() {
      let contents
      try {
        contents = await readFile()
      } catch {
        // No file yet: first run.
        return migrateConfig(null)
      }

      try {
        return migrateConfig(JSON.parse(contents))
      } catch {
        // Corrupt JSON — a half-written file, say. Same answer as no file.
        return migrateConfig(null)
      }
    },

    /**
     * @returns {Promise<{ ok: true, url: string } | { ok: false, reason: string }>}
     */
    async save(input) {
      const normalized = normalizeServerUrl(input)
      if (!normalized.ok) return normalized

      // Read-modify-write: switching server must not silently discard the
      // user's mute choices.
      const current = await this.load()
      await write({ ...current, serverUrl: normalized.url })
      return normalized
    },

    /** Forget the configured server, sending the app back to the first-run screen. */
    async clear() {
      const current = await this.load()
      await write({ ...current, serverUrl: '' })
    },

    /** Replace the muted-workspace list. */
    async setMutedWorkspaces(ids) {
      const current = await this.load()
      const mutedWorkspaces = [...new Set((ids ?? []).filter((id) => typeof id === 'string' && id !== ''))]
      await write({ ...current, mutedWorkspaces })
      return mutedWorkspaces
    },

    /** Turn notifications for one workspace on or off. */
    async setWorkspaceMuted(workspaceId, muted) {
      const { mutedWorkspaces } = await this.load()
      const next = muted
        ? [...mutedWorkspaces, workspaceId]
        : mutedWorkspaces.filter((id) => id !== workspaceId)
      return this.setMutedWorkspaces(next)
    },
  }
}

/**
 * Check that a URL is actually an AgentRQ server before it is stored.
 *
 * `/api/v1/auth/config` is the right probe: it is unauthenticated, it is cheap,
 * and its response tells the login screen which sign-in methods exist — so a
 * URL that answers it is a server this app can really use, not merely a host
 * that happens to be up.
 *
 * @param {string} input raw user entry; normalised here so callers need not.
 * @param {typeof fetch} fetchImpl
 */
export async function validateServerUrl(input, fetchImpl) {
  const normalized = normalizeServerUrl(input)
  if (!normalized.ok) return normalized

  let res
  try {
    res = await fetchImpl(`${normalized.url}${CONFIG_PROBE_PATH}`)
  } catch (err) {
    return { ok: false, reason: `Could not reach ${normalized.url}`, detail: String(err?.message ?? err) }
  }

  if (!res.ok) {
    return { ok: false, reason: `Server answered with ${res.status}` }
  }

  let config
  try {
    config = await res.json()
  } catch {
    return { ok: false, reason: 'That URL is not an AgentRQ server' }
  }

  // A JSON body alone is not proof: any API might return one. The auth config
  // always carries these flags, so their absence means we are talking to
  // something else.
  if (typeof config?.rootLoginEnabled !== 'boolean' && typeof config?.githubLoginEnabled !== 'boolean') {
    return { ok: false, reason: 'That URL is not an AgentRQ server' }
  }

  return { ok: true, url: normalized.url, config }
}
