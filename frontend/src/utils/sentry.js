// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { API_BASE_URL } from '../api'

/**
 * Reports a signed-in user's errors to Sentry when the server says so:
 * GET /sentry names the DSN, or is null when off, and then no SDK is loaded. Events are posted to that
 * same route, which relays them: the desktop app's CSP lets the page reach no
 * other origin. Resolves whether it started; it never throws, as the app runs
 * the same without it.
 */
export async function startSentry(app, { load = () => import('@sentry/vue') } = {}) {
  try {
    const res = await fetch(`${API_BASE_URL}/sentry`)
    if (!res.ok) return false
    const cfg = await res.json()
    if (!cfg?.dsn) return false
    const Sentry = await load()
    Sentry.init({
      app,
      dsn: cfg.dsn,
      environment: cfg.environment,
      release: __APP_VERSION__,
      tunnel: `${API_BASE_URL}/sentry`,
    })
    return true
  } catch {
    return false
  }
}
