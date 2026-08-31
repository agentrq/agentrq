/**
 * Which AgentRQ server the desktop app talks to, and how that choice is stored.
 *
 * The desktop app is a client: the server lives wherever the user runs it, so
 * the app has to be told once and remember. Everything here takes its I/O as
 * arguments so the rules can be tested without Electron or a filesystem.
 *
 * Since profiles, the file holds a list of them rather than one server. Every
 * method that used to read or write "the" server or mute list now reads or
 * writes the *active* profile's, which is what those calls always meant — there
 * was simply only ever one account to mean it about.
 */
import {
  activeProfile,
  addProfile as addProfileToState,
  activateProfile as activateProfileInState,
  migrateProfiles,
  removeProfile as removeProfileFromState,
  renameProfile as renameProfileInState,
  updateProfile,
} from './profiles.js'

// The hosted instance, so someone who installs the app and has no server of
// their own still gets somewhere useful. Self-hosters type their own address
// once on the connection screen, or set AGENTRQ_SERVER_URL.
export const DEFAULT_SERVER_URL = 'https://app.agentrq.com'

/** Filename under Electron's userData directory. */
export const CONFIG_FILENAME = 'agentrq-desktop.json'

/**
 * Shape version of the stored config.
 *
 * v3: { profiles: [{ id, label, partition, serverUrl, mutedWorkspaces }],
 *       activeProfileId } — several signed-in accounts, each with its own
 *       session partition. A v2 file becomes a single profile carrying the
 *       settings it already had.
 *
 * v1: { serverUrl }
 * v2: adds { mutedWorkspaces } — workspaces the desktop app must not raise
 *     notifications for. A v1 file migrates forward by defaulting it to empty,
 *     which is the same behaviour it had.
 */
export const CONFIG_VERSION = 3

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
  const state = migrateProfiles(raw)
  return {
    version: CONFIG_VERSION,
    activeProfileId: state.activeProfileId,
    // Each profile's server is re-checked here rather than trusted from disk.
    // The profile model knows nothing about URLs, and something has to: a
    // stored `file:///x` reaching the fetch base would be a hole, and an
    // unparseable one would boot the app pointed at nothing. Either becomes
    // "not configured", which is the connection screen.
    profiles: state.profiles.map((profile) => {
      const normalized = normalizeServerUrl(profile.serverUrl)
      return { ...profile, serverUrl: normalized.ok ? normalized.url : '' }
    }),
  }
}

/**
 * Persisted server choice.
 *
 * @param {object} deps
 * @param {() => Promise<string>} deps.readFile   Rejects when the file is absent.
 * @param {(contents: string) => Promise<void>} deps.writeFile
 */
export function createServerConfigStore({ readFile, writeFile, newProfileId }) {
  const write = (state) =>
    writeFile(JSON.stringify({ ...state, version: CONFIG_VERSION }, null, 2))

  return {
    /**
     * @returns {Promise<{ version: number, profiles: object[], activeProfileId: string }>}
     *          always a valid shape with at least one profile — a missing or
     *          corrupt file reads as a first run rather than as an error.
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

    /** The profile in use, with its own server and mute list. */
    async active() {
      return activeProfile(await this.load())
    },

    /**
     * Point the active profile at a server.
     *
     * @returns {Promise<{ ok: true, url: string } | { ok: false, reason: string }>}
     */
    async save(input) {
      const normalized = normalizeServerUrl(input)
      if (!normalized.ok) return normalized

      const current = await this.load()
      await write(updateProfile(current, current.activeProfileId, { serverUrl: normalized.url }))
      return normalized
    },

    /** Forget the active profile's server, sending it back to the first-run screen. */
    async clear() {
      const current = await this.load()
      await write(updateProfile(current, current.activeProfileId, { serverUrl: '' }))
    },

    /** Replace the active profile's muted-workspace list. */
    async setMutedWorkspaces(ids) {
      const current = await this.load()
      const next = updateProfile(current, current.activeProfileId, { mutedWorkspaces: ids ?? [] })
      await write(next)
      return activeProfile(next).mutedWorkspaces
    },

    /** Turn notifications for one workspace on or off, for the active profile. */
    async setWorkspaceMuted(workspaceId, muted) {
      const { mutedWorkspaces } = await this.active()
      const next = muted
        ? [...mutedWorkspaces, workspaceId]
        : mutedWorkspaces.filter((id) => id !== workspaceId)
      return this.setMutedWorkspaces(next)
    },

    /**
     * Add a profile and make it the active one.
     *
     * The id is generated here rather than by the caller so it is guaranteed
     * unique against what is already stored — a collision would mean two
     * profiles sharing a session, which is the same account twice.
     */
    async addProfile(label = '') {
      const current = await this.load()
      let id = newProfileId()
      while (current.profiles.some((p) => p.id === id)) id = newProfileId()

      const next = addProfileToState(current, { id, label })
      await write(next)
      return next
    },

    /** Switch profiles. An unknown id changes nothing. */
    async activateProfile(id) {
      const next = activateProfileInState(await this.load(), id)
      await write(next)
      return next
    },

    /** Remove a profile. The last one is kept, since the app needs a session. */
    async removeProfile(id) {
      const next = removeProfileFromState(await this.load(), id)
      await write(next)
      return next
    },

    /** Rename a profile. A blank name keeps the old one. */
    async renameProfile(id, label) {
      const next = renameProfileInState(await this.load(), id, label)
      await write(next)
      return next
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
