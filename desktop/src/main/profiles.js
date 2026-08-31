/**
 * Signed-in profiles.
 *
 * A profile is an account. Electron keeps cookies per *session partition*, so
 * giving each profile its own partition gives it its own `at` cookie, and
 * therefore its own signed-in user — the same idea as a Chrome profile. The
 * work here is not the isolation, which Electron provides, but keeping the list
 * coherent: never empty, never pointing at a profile that is gone, and never
 * producing a partition name that is not safe as a directory.
 *
 * Each profile carries its own server URL and muted-workspace list, because an
 * account exists on one server: "which account" and "which server" are the same
 * question, and splitting them would let you sign in as one user and then point
 * that session at a server where the cookie means nothing.
 *
 * Pure, so the rules are testable without Electron. The main process supplies
 * ids and turns partitions into real sessions.
 */

/** Shown when a profile has no name of its own. */
export const DEFAULT_PROFILE_LABEL = 'Default'

/** Longest label kept; anything more is a paste accident, not a name. */
const MAX_LABEL = 40

/**
 * Partition name for a new profile.
 *
 * `persist:` is what makes the jar survive a restart — without it, signing in
 * would last only as long as the window. The id is embedded directly, which is
 * why ids are restricted to characters that are safe in a directory name: the
 * partition becomes a folder under the app's Partitions directory.
 */
export function partitionFor(id) {
  return `persist:profile-${id}`
}

/**
 * Every profile gets its own partition, including the one migrated from a
 * pre-profiles install.
 *
 * That migrated profile's `at` cookie lives in Electron's default session, so
 * moving it onto a partition signs the user out once, on the upgrade. That is a
 * deliberate trade, agreed with the owner: the session is only valid for 24
 * hours anyway, so the cost is one sign-in, and the alternative is a profile
 * that is permanently special-cased everywhere a session is resolved.
 */

/** Ids are used in a filesystem path, so they are deliberately narrow. */
export function isValidProfileId(id) {
  return typeof id === 'string' && /^[a-z0-9][a-z0-9-]{0,63}$/.test(id)
}

function cleanLabel(label, fallback = DEFAULT_PROFILE_LABEL) {
  const trimmed = typeof label === 'string' ? label.replace(/\s+/g, ' ').trim() : ''
  if (trimmed === '') return fallback
  return trimmed.slice(0, MAX_LABEL)
}

function cleanMuted(ids) {
  if (!Array.isArray(ids)) return []
  return [...new Set(ids.filter((id) => typeof id === 'string' && id !== ''))]
}

/**
 * One profile, with everything absent or malformed filled in.
 *
 * @param {{ id: string, label?: string, serverUrl?: string, mutedWorkspaces?: string[] }} raw
 * @returns {object | null} null when the id is unusable, since a profile
 *          without a valid id has no partition and cannot be stored
 */
export function makeProfile(raw) {
  if (!raw || typeof raw !== 'object' || !isValidProfileId(raw.id)) return null
  return {
    id: raw.id,
    label: cleanLabel(raw.label),
    // Stored rather than derived so the name is stable if the scheme ever
    // changes: a partition that moved would be an empty jar, i.e. a sign-out.
    partition: typeof raw.partition === 'string' && raw.partition !== '' ? raw.partition : partitionFor(raw.id),
    serverUrl: typeof raw.serverUrl === 'string' ? raw.serverUrl : '',
    mutedWorkspaces: cleanMuted(raw.mutedWorkspaces),
  }
}

/**
 * Bring stored profile state forward, from any shape including none.
 *
 * A config from before profiles existed becomes a single profile carrying the
 * settings that were already there, so upgrading does not look like being
 * signed out — the cookie jar is separate from this, and is handled by the
 * caller adopting the default partition for that first profile.
 *
 * @param {object|null} raw the stored config
 * @param {string} firstId  id to give the migrated profile when there is none
 */
export function migrateProfiles(raw, firstId = 'default') {
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {}

  const profiles = Array.isArray(source.profiles)
    ? source.profiles.map(makeProfile).filter(Boolean)
    : []

  if (profiles.length === 0) {
    // Either a first run or a pre-profiles config. Both become one profile;
    // the second keeps the server and mutes that were already configured.
    profiles.push(
      makeProfile({
        id: isValidProfileId(firstId) ? firstId : 'default',
        label: DEFAULT_PROFILE_LABEL,
        partition: partitionFor(isValidProfileId(firstId) ? firstId : 'default'),
        serverUrl: typeof source.serverUrl === 'string' ? source.serverUrl : '',
        mutedWorkspaces: source.mutedWorkspaces,
      })
    )
  }

  // Two profiles sharing an id would share a partition, which is the one thing
  // this must never allow: they would be the same account wearing two names.
  const seen = new Set()
  const unique = profiles.filter((p) => (seen.has(p.id) ? false : seen.add(p.id)))

  const activeId = unique.some((p) => p.id === source.activeProfileId)
    ? source.activeProfileId
    : unique[0].id

  return { profiles: unique, activeProfileId: activeId }
}

/** The profile in use. Never null: migrateProfiles guarantees one exists. */
export function activeProfile(state) {
  return state.profiles.find((p) => p.id === state.activeProfileId) ?? state.profiles[0]
}

/** @returns {object} new state; the input is not modified */
export function addProfile(state, { id, label, serverUrl = '' }) {
  // Always its own jar: a second profile sharing the first's session would be
  // the same signed-in account under a different name.
  const profile = makeProfile({ id, label, serverUrl, partition: partitionFor(id) })
  if (!profile) return state
  if (state.profiles.some((p) => p.id === profile.id)) return state

  // A new profile is switched to: adding one you then have to go and select is
  // a step nobody wants.
  return { profiles: [...state.profiles, profile], activeProfileId: profile.id }
}

/**
 * Remove a profile.
 *
 * The last one cannot be removed — an app with no profile has no session to
 * run in. Removing the active one falls back to the first remaining.
 */
export function removeProfile(state, id) {
  if (state.profiles.length <= 1) return state
  const profiles = state.profiles.filter((p) => p.id !== id)
  if (profiles.length === state.profiles.length) return state

  const activeProfileId = profiles.some((p) => p.id === state.activeProfileId)
    ? state.activeProfileId
    : profiles[0].id
  return { profiles, activeProfileId }
}

/** @returns {object} new state */
export function renameProfile(state, id, label) {
  if (!state.profiles.some((p) => p.id === id)) return state
  return {
    ...state,
    profiles: state.profiles.map((p) => (p.id === id ? { ...p, label: cleanLabel(label, p.label) } : p)),
  }
}

/** @returns {object} new state; an unknown id leaves the active profile alone */
export function activateProfile(state, id) {
  if (!state.profiles.some((p) => p.id === id)) return state
  return { ...state, activeProfileId: id }
}

/** @returns {object} new state, with one profile's fields updated */
export function updateProfile(state, id, patch) {
  if (!state.profiles.some((p) => p.id === id)) return state
  return {
    ...state,
    profiles: state.profiles.map((p) => {
      if (p.id !== id) return p
      const next = { ...p }
      if (typeof patch?.serverUrl === 'string') next.serverUrl = patch.serverUrl
      if (patch && 'mutedWorkspaces' in patch) next.mutedWorkspaces = cleanMuted(patch.mutedWorkspaces)
      if (typeof patch?.label === 'string') next.label = cleanLabel(patch.label, p.label)
      return next
    }),
  }
}
