// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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

import { accountKey, describeIdentity } from './identity.js'

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
    // Who this profile belongs to, as last seen. Stored, not looked up, so a
    // server that is slow, unreachable, or has expired the session cannot make
    // a profile anonymous — see [rememberAccount].
    account: describeIdentity(raw.account),
  }
}

/**
 * A name for a new profile that is not the name every other one already has.
 *
 * "Default" for all of them is how a switcher becomes unreadable the moment it
 * cannot reach the server: every row falls back to its label and they are all
 * the same word. The first profile keeps the name it has always had; the ones
 * after it are numbered, and the number skips anything already taken so
 * renaming one to "Profile 3" does not produce a second.
 */
export function nextProfileLabel(state) {
  const profiles = Array.isArray(state?.profiles) ? state.profiles : []
  if (profiles.length === 0) return DEFAULT_PROFILE_LABEL

  // Every stored profile has a string label: makeProfile guarantees it.
  const taken = new Set(profiles.map((p) => p.label))
  let n = profiles.length + 1
  while (taken.has(`Profile ${n}`)) n += 1
  return `Profile ${n}`
}

/**
 * Record who a profile is signed in as.
 *
 * The rule that matters is what it does with nothing: a lookup that failed
 * leaves the profile as it was. Overwriting the account with "could not say"
 * is how every profile in the switcher turned back into its label — the
 * session is good for a day, the lookup gives up after four seconds, and the
 * app has no memory across a restart, so all three ordinary conditions ended
 * with a list of profiles called "Default".
 *
 * What is remembered is who a profile *belongs to*, which does not stop being
 * true when the session expires. Whether it is signed in right now is a
 * separate question, asked live and answered in the subtitle.
 */
export function rememberAccount(state, id, identity) {
  const account = describeIdentity(identity)
  if (!account) return state
  if (!state.profiles.some((p) => p.id === id)) return state
  return {
    ...state,
    profiles: state.profiles.map((p) => (p.id === id ? { ...p, account } : p)),
  }
}

/**
 * The profile that already holds this one's account, if any.
 *
 * Two profiles signed into the same account are two windows onto one thing:
 * the same tasks, the same workspaces, the same notifications twice. Nothing
 * can stop it at the moment a profile is added — it has no account yet, and
 * the sign-in happens on a web page the shell does not drive — so it is caught
 * when the account becomes known, and said out loud where the profiles are
 * listed.
 *
 * The *earlier* profile wins, always: whichever was added first is the
 * original, and the answer does not change depending on which one you ask
 * about.
 *
 * @returns {string} the other profile's id, or '' when this one is not a
 *          duplicate
 */
export function duplicateOf(state, id) {
  const profiles = Array.isArray(state?.profiles) ? state.profiles : []
  const self = profiles.find((p) => p.id === id)
  const key = accountKey(self?.account)
  if (!key) return ''

  // Only what was added before this one, which is what makes the answer
  // stable: asked about either half of a pair, it names the same original.
  const earlier = profiles.slice(0, profiles.indexOf(self))
  return earlier.find((p) => accountKey(p.account) === key)?.id ?? ''
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
  //
  // An unnamed profile is numbered rather than called "Default" like the first
  // one: until it has signed in, its label is the only thing distinguishing it
  // in the switcher, and two identical labels distinguish nothing.
  const named = typeof label === 'string' && label.trim() !== '' ? label : nextProfileLabel(state)
  const profile = makeProfile({ id, label: named, serverUrl, partition: partitionFor(id) })
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
 * run in. Removing the active one falls back to `fallbackId` when that profile
 * is still there, and to the first remaining otherwise.
 *
 * The fallback exists for abandoning a profile that was just added: the one to
 * land on is the one it was added *from*, which with three or more profiles is
 * not the first in the list.
 */
export function removeProfile(state, id, fallbackId = '') {
  if (state.profiles.length <= 1) return state
  const profiles = state.profiles.filter((p) => p.id !== id)
  if (profiles.length === state.profiles.length) return state

  if (profiles.some((p) => p.id === state.activeProfileId)) {
    return { profiles, activeProfileId: state.activeProfileId }
  }
  const preferred = profiles.some((p) => p.id === fallbackId) ? fallbackId : profiles[0].id
  return { profiles, activeProfileId: preferred }
}

/**
 * Whether the profile on screen can be thrown away instead of finished.
 *
 * A profile with no server has never been used for anything: it was added and
 * then abandoned at the connection screen. Discarding it is only offered when
 * another profile remains to go back to — during a genuine first run there is
 * nowhere to go, and the app needs a server before it can do anything at all.
 *
 * One rule in one place because two callers ask it: the connection screen, to
 * decide whether to offer a way out, and the shell, before taking one.
 */
export function canDiscardActiveProfile(state) {
  if (!state || !Array.isArray(state.profiles) || state.profiles.length <= 1) return false
  return !activeProfile(state).serverUrl
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
