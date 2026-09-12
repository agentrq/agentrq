// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { checkCompatibility } from './manifest.js'

/**
 * The catalogue as the interface receives it.
 *
 * Discovery finds repositories and validates their manifests; this decides
 * whether *this* install can run them, and it belongs here rather than in the
 * renderer for the same reason the search does — the app version and the live
 * tool lists are main-process facts, and the manifest rules are already here.
 *
 * The renderer is left with grouping and ordering, and never has to learn what
 * an SPDX identifier or a semver range is.
 */

/**
 * Marks every entry with whether it can be used, and why not.
 *
 * A broken entry passes through untouched: there is no manifest to judge, and
 * its parse failure is already the reason it cannot be used.
 */
export function decorate(index, { appVersion = '', workspaceTools = [], supervisorTools = [] } = {}) {
  const entries = (index?.entries ?? []).map((entry) => {
    if (!entry.ok) return entry
    const { compatible, reasons } = checkCompatibility(entry.manifest, {
      appVersion,
      workspaceTools,
      supervisorTools,
    })
    return { ...entry, compatible, reasons }
  })
  return { ...index, entries }
}
