// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it, vi } from 'vitest';

import { ROUTE_MESSAGE, reportRouteToExtensionPopup } from '../src/utils/extensionPopup.js';

// A router that remembers its afterEach hook, and a window framed by `ancestor`.
function setup(ancestor, { framed = true } = {}) {
  const hooks = [];
  const router = { afterEach: (fn) => hooks.push(fn) };
  const parent = { postMessage: vi.fn() };
  const win = { location: { ancestorOrigins: ancestor ? [ancestor] : [] } };
  win.parent = framed ? parent : win;
  reportRouteToExtensionPopup(router, win);
  const go = (fullPath) => hooks.forEach((fn) => fn({ fullPath }));
  return { hooks, parent, go };
}

describe('reportRouteToExtensionPopup', () => {
  it('tells the extension popup framing the app each page, addressed to it alone', () => {
    const { parent, go } = setup('chrome-extension://abc');

    go('/workspaces/w1/board?x=1');

    expect(parent.postMessage).toHaveBeenCalledWith(
      { type: ROUTE_MESSAGE, path: '/workspaces/w1/board?x=1' },
      'chrome-extension://abc',
    );
  });

  it('tells nobody when the app is not framed, or framed by anything but an extension', () => {
    expect(setup(null, { framed: false }).hooks).toHaveLength(0);
    expect(setup('https://evil.example').hooks).toHaveLength(0);
    expect(setup(null).hooks).toHaveLength(0);
  });

  it('tells nobody in a browser with no ancestorOrigins', () => {
    const hooks = [];
    const win = { location: {}, parent: {} };
    reportRouteToExtensionPopup({ afterEach: (fn) => hooks.push(fn) }, win);
    expect(hooks).toHaveLength(0);
  });

  it('reads the real window by default', () => {
    const hooks = [];
    reportRouteToExtensionPopup({ afterEach: (fn) => hooks.push(fn) });
    expect(hooks).toHaveLength(0);
  });
});
