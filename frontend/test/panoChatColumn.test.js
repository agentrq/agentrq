// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The Pano chat's own card: it shows the route's `chat` view, and is resized
 * from its right edge as the desktop side panel is from its left.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';
import PanoChatColumn from '../src/components/PanoChatColumn.vue';
import { PANO_CHAT_WIDTH_KEY } from '../src/composables/usePano';

const settle = () => new Promise((r) => setTimeout(r, 0));
const apps = [];
let observed = [];

class FakeResizeObserver {
  constructor(callback) { this.callback = callback; observed.push(this); }
  observe(element) { this.element = element; }
  disconnect() { this.disconnected = true; }
}

async function mount({ page = true } = {}) {
  const el = document.createElement('div');
  document.body.append(el);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/pano/chat/:workspaceId/:taskId', components: { default: { render: () => null }, chat: { render: () => h('p', { 'data-test': 'chat' }, 'the task') } } }],
  });
  await router.push('/pano/chat/sup/t1');
  // The sidebar before the column, as in App.vue.
  const app = createApp({ render: () => [h('nav', { 'data-test': 'sidebar' }), h(PanoChatColumn), page ? h('main') : null] });
  app.use(router);
  app.mount(el);
  apps.push(app);
  await settle();
  return { el, aside: el.querySelector('[data-pano-chat]'), handle: el.querySelector('[data-pano-chat-handle]') };
}

const widthOf = (aside) => aside.style.getPropertyValue('--pano-chat-width');
const pointer = (target, type, init) => target.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, ...init }));

beforeEach(() => {
  localStorage.clear();
  observed = [];
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
});

afterEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.unstubAllGlobals();
});

describe('the Pano chat card', () => {
  it('shows the chat view at a phone\'s width, with the side panel\'s grip', async () => {
    const { el, aside, handle } = await mount();
    expect(el.querySelector('[data-test="chat"]').textContent).toBe('the task');
    expect(widthOf(aside)).toBe('480px');
    expect(handle.getAttribute('title')).toBe('Drag to resize · double-click to reset');
    expect(el.querySelector('[data-resize-grip]')).not.toBeNull();
    expect(observed[0].element.dataset.test).toBe('sidebar');
  });

  it('widens as its right edge is dragged right, within the page\'s minimum, and remembers it', async () => {
    const { aside, handle } = await mount();
    pointer(handle, 'pointerdown', { button: 0, clientX: 500 });
    pointer(globalThis, 'pointermove', { clientX: 540 });
    await settle();
    expect(widthOf(aside)).toBe('520px');
    expect(handle.firstElementChild.className).toContain('bg-gray-400');
    pointer(globalThis, 'pointermove', { clientX: 900 });
    await settle();
    // jsdom's window is 1024 wide, and the page keeps 480 of it.
    expect(widthOf(aside)).toBe('544px');
    pointer(globalThis, 'pointerup', {});
    pointer(globalThis, 'pointermove', { clientX: 100 });
    await settle();
    expect(widthOf(aside)).toBe('544px');
    expect(localStorage.getItem(PANO_CHAT_WIDTH_KEY)).toBe('544');
  });

  it('ignores any button but the main one', async () => {
    const { aside, handle } = await mount();
    pointer(handle, 'pointerdown', { button: 2, clientX: 500 });
    pointer(globalThis, 'pointermove', { clientX: 540 });
    await settle();
    expect(widthOf(aside)).toBe('480px');
  });

  it('goes back to a phone\'s width on a double-click', async () => {
    localStorage.setItem(PANO_CHAT_WIDTH_KEY, '400');
    const { aside, handle } = await mount();
    expect(widthOf(aside)).toBe('400px');
    handle.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    await settle();
    expect(widthOf(aside)).toBe('480px');
    expect(localStorage.getItem(PANO_CHAT_WIDTH_KEY)).toBeNull();
  });

  it('measures its room again when the window or the sidebar changes, and stops when gone', async () => {
    localStorage.setItem(PANO_CHAT_WIDTH_KEY, '600');
    const { aside } = await mount();
    expect(widthOf(aside)).toBe('544px');
    vi.stubGlobal('innerWidth', 1400);
    globalThis.dispatchEvent(new Event('resize'));
    await settle();
    expect(widthOf(aside)).toBe('600px');
    vi.stubGlobal('innerWidth', 1100);
    observed[0].callback();
    await settle();
    expect(widthOf(aside)).toBe('600px');
    apps.pop().unmount();
    expect(observed[0].disconnected).toBe(true);
  });

  it('works without a ResizeObserver', async () => {
    vi.stubGlobal('ResizeObserver', undefined);
    const { aside } = await mount({ page: false });
    expect(widthOf(aside)).toBe('480px');
    expect(observed).toHaveLength(0);
  });
});
