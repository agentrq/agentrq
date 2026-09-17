// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * How one profile reads in the switcher.
 *
 * A list of profiles named "Default" and "Work" does not tell you which account
 * you are about to switch to, so the account comes first and the profile's own
 * name is the fallback. What is available varies: a profile may be signed in
 * with a full name and email, with only one of them, or not signed in at all.
 *
 * There are two accounts in play and the difference is the point. `identity` is
 * the live answer — who this profile is signed in as *right now* — and it is
 * absent whenever the server cannot be asked, which is often: the session
 * lasts a day, the lookup gives up after four seconds, and the app starts
 * knowing nothing. `account` is who the profile belongs to, written down when
 * it was last known and not forgotten because a request failed.
 *
 * Reading the live answer alone is what turned the switcher into a list of
 * profiles called "Default": the moment it could not reach the server, every
 * row fell back to a label that was the same word on all of them.
 *
 * Kept out of the component so the precedence is one rule with one set of
 * tests, rather than three expressions in a template.
 */

/**
 * @param {{ label?: string, serverUrl?: string,
 *           identity?: {name?: string, email?: string} | null,
 *           account?: {name?: string, email?: string} | null }} profile
 * @returns {{ title: string, subtitle: string, initial: string, signedIn: boolean }}
 */
export function profileDisplay(profile) {
  const label = typeof profile?.label === 'string' ? profile.label.trim() : ''
  const serverUrl = typeof profile?.serverUrl === 'string' ? profile.serverUrl.trim() : ''

  const signedIn = Boolean(profile?.identity?.name?.trim() || profile?.identity?.email?.trim())
  // The live answer when there is one, the remembered one otherwise. They are
  // the same account in every ordinary case; when they differ, the live one is
  // the truth and the record is about to be corrected.
  const known = signedIn ? profile.identity : profile?.account
  const name = known?.name?.trim() ?? ''
  const email = known?.email?.trim() ?? ''

  // Who, then where. The account identifies the profile; the label rarely does.
  const title = name || email || label || 'Profile'

  // Never repeat the title underneath it: an email shown twice reads as a bug.
  let subtitle
  if (!signedIn && (name || email)) {
    // Still named, and worth saying why switching to it will ask for a
    // password: a profile that has simply been away for a day looks broken
    // otherwise.
    subtitle = 'Signed out'
  } else if (name && email) subtitle = email
  else if (serverUrl) subtitle = serverUrl
  else subtitle = 'Not signed in'

  // title always falls back to 'Profile', so there is always a first letter.
  return { title, subtitle, initial: title.charAt(0).toUpperCase(), signedIn }
}

/**
 * What to say about a profile that is a second window onto an account another
 * profile already holds.
 *
 * Nothing stops this at the moment a profile is added — it has no account yet,
 * and the sign-in happens on a web page the shell does not drive — so it is
 * caught once the account is known and said where the profiles are listed,
 * beside the one thing that fixes it.
 *
 * @param {object} profile the profile being described
 * @param {Array} all      every profile, so the original can be named
 * @returns {string} '' when this is not a duplicate
 */
export function duplicateNotice(profile, all) {
  const otherId = typeof profile?.duplicateOf === 'string' ? profile.duplicateOf : ''
  if (!otherId) return ''

  const other = Array.isArray(all) ? all.find((p) => p?.id === otherId) : null
  if (!other) return 'Same account as another profile'
  return `Same account as ${profileDisplay(other).title}`
}
