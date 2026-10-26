// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The Extensions screen, mounted, where Node.js is not there yet (Tauri): the
 * banner that says so, and the prompt that downloads it.
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import { createApp, nextTick } from 'vue';

vi.mock('../src/stores/workspaceStore', () => ({ useWorkspaceStore: () => ({ workspaces: [] }) }));

import ExtensionsView from '../src/desktop/ExtensionsView.vue';

const NODE = { installed: false, update: false, version: 'v24.22.1', size: 30978868 };

/** Lets the bridge's promises settle and the screen redraw. */
const settle = async () => {
  for (let i = 0; i < 5; i += 1) await Promise.resolve();
  await nextTick();
};

let app;
let el;

function bridge(over = {}) {
  return {
    state: vi.fn(async () => ({ index: { entries: [] }, installed: [], node: { ...NODE } })),
    supervisor: vi.fn(async () => ({ authorized: true })),
    downloadNode: vi.fn(async () => ({ ok: true })),
    cancelNodeDownload: vi.fn(async () => true),
    onNodeProgress: vi.fn(() => () => {}),
    chooseFolder: vi.fn(async () => null),
    refresh: vi.fn(async () => ({ ok: true })),
    ...over,
  };
}

async function mount(extensions) {
  window.agentrq = { extensions };
  el = document.createElement('div');
  document.body.appendChild(el);
  app = createApp(ExtensionsView);
  app.mount(el);
  await settle();
}

const find = (test) => el.querySelector(`[data-test="${test}"]`);

afterEach(() => {
  app?.unmount();
  el?.remove();
  delete window.agentrq;
});

describe('ExtensionsView without Node.js', () => {
  it('says up front what it will download, and how big it is', async () => {
    await mount(bridge());
    expect(find('node-banner').textContent).toContain('Extensions run on Node.js v24.22.1');
    expect(find('node-banner').textContent).toContain('31 MB');
    expect(find('node-banner').querySelector('button').textContent.trim()).toBe('Download');
    expect(find('node-prompt')).toBeNull();
  });

  it('calls a newer pin an update', async () => {
    await mount(bridge({ state: vi.fn(async () => ({ installed: [], node: { ...NODE, update: true } })) }));
    expect(find('node-banner').textContent).toContain('need a Node.js update');
    find('node-banner').querySelector('button').click();
    await settle();
    expect(find('node-prompt').textContent).toContain('Update Node.js?');
  });

  it('shows no banner once Node is there, or where the app bundles it', async () => {
    await mount(bridge({ state: vi.fn(async () => ({ installed: [], node: { ...NODE, installed: true } })) }));
    expect(find('node-banner')).toBeNull();
    app.unmount();
    el.remove();
    await mount(bridge({ state: vi.fn(async () => ({ installed: [] })) }));
    expect(find('node-banner')).toBeNull();
  });

  it('downloads only when asked, and closes when it is done', async () => {
    const extensions = bridge();
    extensions.downloadNode.mockImplementation(async () => {
      extensions.state.mockImplementation(async () => ({ installed: [], node: { ...NODE, installed: true } }));
      return { ok: true };
    });
    await mount(extensions);
    find('node-banner').querySelector('button').click();
    await settle();
    expect(find('node-prompt').textContent).toContain('Download Node.js?');
    expect(extensions.downloadNode).not.toHaveBeenCalled();

    find('node-download').click();
    await settle();
    expect(extensions.downloadNode).toHaveBeenCalledTimes(1);
    expect(find('node-prompt')).toBeNull();
    expect(find('node-banner')).toBeNull();
  });

  it('shows progress, ignores the backdrop while downloading, and cancels from the button', async () => {
    let deliver;
    let finish;
    const extensions = bridge({
      onNodeProgress: vi.fn((callback) => {
        deliver = callback;
        return () => {};
      }),
      downloadNode: vi.fn(() => new Promise((resolve) => (finish = resolve))),
    });
    await mount(extensions);
    find('node-banner').querySelector('button').click();
    await settle();
    find('node-download').click();
    await settle();
    expect(find('node-download').disabled).toBe(true);
    expect(find('node-download').textContent.trim()).toBe('Downloading…');

    deliver({ downloaded: 15, total: 30, percent: 50 });
    await nextTick();
    expect(find('node-prompt').textContent).toContain('50%');

    find('node-prompt').click();
    await settle();
    expect(find('node-prompt')).not.toBeNull();
    expect(extensions.cancelNodeDownload).not.toHaveBeenCalled();

    find('node-cancel').click();
    expect(extensions.cancelNodeDownload).toHaveBeenCalledTimes(1);
    finish({ ok: false, cancelled: true });
    await settle();
    expect(find('node-download').disabled).toBe(false);
    expect(find('node-error')).toBeNull();

    // Idle, the backdrop puts the question away.
    find('node-prompt').click();
    await settle();
    expect(find('node-prompt')).toBeNull();
  });

  it('says inside the prompt why the download failed, and offers it again', async () => {
    const extensions = bridge({
      downloadNode: vi.fn(async () => ({ ok: false, reason: 'The Node.js download stalled. Try again.' })),
    });
    await mount(extensions);
    find('node-banner').querySelector('button').click();
    await settle();
    find('node-download').click();
    await settle();
    expect(find('node-prompt').contains(find('node-error'))).toBe(true);
    expect(find('node-error').textContent.trim()).toBe('The Node.js download stalled. Try again.');
    expect(find('node-download').textContent.trim()).toBe('Try again');

    find('node-cancel').click();
    await settle();
    expect(find('node-prompt')).toBeNull();
    find('node-banner').querySelector('button').click();
    await settle();
    expect(find('node-error')).toBeNull();
  });
});
