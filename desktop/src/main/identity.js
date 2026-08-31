/**
 * Who is signed in to a profile.
 *
 * The switcher shows a name and email rather than only a label, because a list
 * of profiles named "Default" and "Work" does not tell you which account you
 * are about to switch to — which was the whole point of having profiles.
 *
 * Each lookup runs against that profile's own session, so it answers for that
 * profile rather than for whichever one happens to be on screen. Everything is
 * best effort: a profile that is signed out, pointed at a server that is down,
 * or simply not configured yet is a normal state, not an error, and shows as
 * "not signed in" rather than blocking the menu.
 */

/** How long to wait before deciding a profile cannot answer for itself. */
export const IDENTITY_TIMEOUT_MS = 4000

/** The user endpoint, unauthenticated-safe: it 401s rather than erroring. */
export const USER_PATH = '/api/v1/auth/user'

/**
 * Ask one profile who it is signed in as.
 *
 * @param {object} deps
 * @param {typeof fetch} deps.fetchImpl  that profile's session fetch
 * @param {string} deps.serverUrl        that profile's server; '' means unconfigured
 * @param {(ms: number) => AbortSignal} [deps.timeout]
 * @returns {Promise<{name: string, email: string, picture: string} | null>}
 */
export async function fetchProfileIdentity({ fetchImpl, serverUrl, timeout }) {
  if (!serverUrl) return null

  let res
  try {
    const signal = timeout ? timeout(IDENTITY_TIMEOUT_MS) : undefined
    res = await fetchImpl(`${serverUrl}${USER_PATH}`, signal ? { signal } : {})
  } catch {
    // Unreachable, refused, timed out: all "cannot say", none worth an error.
    return null
  }

  // 401 is the ordinary answer for a profile nobody has signed into yet.
  if (!res?.ok) return null

  let user
  try {
    user = await res.json()
  } catch {
    return null
  }

  return describeIdentity(user)
}

/**
 * Reduce a user payload to what the switcher shows.
 *
 * @returns {{name: string, email: string, picture: string} | null} null when
 *          there is nothing worth showing, so callers have one thing to check
 */
export function describeIdentity(user) {
  if (!user || typeof user !== 'object') return null

  const name = typeof user.name === 'string' ? user.name.trim() : ''
  const email = typeof user.email === 'string' ? user.email.trim() : ''
  const picture = typeof user.picture === 'string' ? user.picture : ''

  // A response with neither is indistinguishable from being signed out, as far
  // as anything the user can see goes.
  if (name === '' && email === '') return null
  return { name, email, picture }
}
