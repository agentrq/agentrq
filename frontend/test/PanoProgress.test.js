// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * How much of a Pano doc's checklist is done, mounted: a bar in the note's ink
 * and "N of M done" under it, and nothing at all for a doc with no checklist.
 * The percentage itself is tested in `usePano.test.js`.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { createApp, h, nextTick, reactive } from 'vue';

import PanoProgress from '../src/components/PanoProgress.vue';
import { PANO_COLORS } from '../src/composables/usePano';

const apps = [];

async function mount(doc) {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(PanoProgress, { doc }) });
  app.mount(el);
  apps.push(app);
  await nextTick();
  return el;
}

const bar = (el) => el.querySelector('[role="progressbar"]');

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
});

describe('a doc\'s checklist progress', () => {
  it('shows nothing for a doc without a checklist', async () => {
    expect((await mount({ id: 'd1' })).querySelector('[data-test="pano-progress"]')).toBeNull();
    expect((await mount({ id: 'd2', checklistDone: 0, checklistTotal: 0 })).querySelector('[data-test="pano-progress"]')).toBeNull();
  });

  it('fills the bar to the share done, and says how many', async () => {
    const el = await mount({ id: 'd1', color: 'pink', checklistDone: 2, checklistTotal: 3 });
    expect(bar(el).getAttribute('aria-valuenow')).toBe('67');
    expect(bar(el).getAttribute('aria-label')).toBe('2 of 3 done');
    expect(bar(el).firstElementChild.style.width).toBe('67%');
    expect(el.textContent.replace(/\s+/g, ' ').trim()).toBe('2 of 3 done');
  });

  it('draws the bar and its dot in the note\'s ink, yellow for an unknown color', async () => {
    const pink = await mount({ id: 'd1', color: 'pink', checklistDone: 0, checklistTotal: 4 });
    expect(bar(pink).firstElementChild.classList.contains(PANO_COLORS[1].inkBg)).toBe(true);
    expect(pink.querySelector('p span').classList.contains(PANO_COLORS[1].inkBg)).toBe(true);
    expect(bar(pink).firstElementChild.style.width).toBe('0%');
    const odd = await mount({ id: 'd2', color: 'magenta', checklistDone: 4, checklistTotal: 4 });
    expect(bar(odd).firstElementChild.classList.contains(PANO_COLORS[0].inkBg)).toBe(true);
    expect(bar(odd).getAttribute('aria-valuenow')).toBe('100');
  });

  it('follows the doc as its items are ticked', async () => {
    const doc = reactive({ id: 'd1', checklistDone: 1, checklistTotal: 2 });
    const el = await mount(doc);
    expect(bar(el).getAttribute('aria-valuenow')).toBe('50');
    doc.checklistDone = 2;
    await nextTick();
    expect(bar(el).getAttribute('aria-valuenow')).toBe('100');
    expect(el.textContent).toContain('2 of 2 done');
  });
});
