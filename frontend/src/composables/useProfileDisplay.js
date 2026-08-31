/**
 * How one profile reads in the switcher.
 *
 * A list of profiles named "Default" and "Work" does not tell you which account
 * you are about to switch to, so the account comes first and the profile's own
 * name is the fallback. What is available varies: a profile may be signed in
 * with a full name and email, with only one of them, or not signed in at all.
 *
 * Kept out of the component so the precedence is one rule with one set of
 * tests, rather than three expressions in a template.
 */

/**
 * @param {{ label?: string, serverUrl?: string, identity?: {name?: string, email?: string} | null }} profile
 * @returns {{ title: string, subtitle: string, initial: string }}
 */
export function profileDisplay(profile) {
  const label = typeof profile?.label === 'string' ? profile.label.trim() : ''
  const name = profile?.identity?.name?.trim() ?? ''
  const email = profile?.identity?.email?.trim() ?? ''
  const serverUrl = typeof profile?.serverUrl === 'string' ? profile.serverUrl.trim() : ''

  // Who, then where. The account identifies the profile; the label rarely does.
  const title = name || email || label || 'Profile'

  // Never repeat the title underneath it: an email shown twice reads as a bug.
  let subtitle
  if (name && email) subtitle = email
  else if (serverUrl) subtitle = serverUrl
  else subtitle = 'Not signed in'

  // title always falls back to 'Profile', so there is always a first letter.
  return { title, subtitle, initial: title.charAt(0).toUpperCase() }
}
