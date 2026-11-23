// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Framed in the AgentRQ Chrome extension's popup, the app tells the popup each
 * page the person opens, so it reopens there: the popup cannot read the
 * address of a frame from another origin.
 *
 * Only to an extension parent, addressed by its own origin: posting to any page
 * that frames the app would tell it where the person is.
 */
export const ROUTE_MESSAGE = 'agentrq-route';

export function reportRouteToExtensionPopup(router, win = window) {
  if (win.parent === win) return;
  const parent = win.location.ancestorOrigins?.[0];
  if (!parent?.startsWith('chrome-extension://')) return;
  router.afterEach((to) => win.parent.postMessage({ type: ROUTE_MESSAGE, path: to.fullPath }, parent));
}
