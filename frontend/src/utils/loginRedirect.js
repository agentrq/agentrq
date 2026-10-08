// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Where the login page sends a person back to once they are signed in: the
 * `redirect_url` an MCP client's authorize step put on it, so a sign-in with
 * any provider returns to the consent page.
 *
 * Only an address on this same origin is kept; anything else is null, so the
 * page cannot be used to bounce someone to another site. The backend checks
 * the provider logins' copy again.
 *
 * @param {unknown} raw the `redirect_url` query value
 * @param {string} origin this page's origin
 * @returns {string|null}
 */
export function loginRedirect(raw, origin) {
  if (typeof raw !== 'string' || raw === '') return null
  let target
  try {
    target = new URL(raw, origin)
  } catch {
    return null
  }
  return target.origin === origin ? target.href : null
}

/**
 * A provider's login route, carrying the return address when there is one.
 *
 * @param {string} route e.g. `/api/v1/auth/github/login`
 * @param {string|null} redirect from {@link loginRedirect}
 */
export function providerLoginUrl(route, redirect) {
  return redirect ? `${route}?redirect_url=${encodeURIComponent(redirect)}` : route
}
