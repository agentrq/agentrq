// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The key App.vue gives the page, so a route marked `meta.remount` is rebuilt
 * when its params change. Other pages keep one instance and follow the params
 * themselves; keying them would reload a workspace on every task opened in it.
 * A route may name its own key (`meta.remountKey`) when its path holds more
 * than the page's identity.
 */
export function pageKey(route) {
  if (!route.meta?.remount) return undefined
  return route.meta.remountKey?.(route) ?? route.path
}
