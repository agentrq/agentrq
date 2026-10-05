// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The Skills tab, mounted: a grid of cards, each leading to the skill's own
 * page (`skillDetailView.test.js`), and an import that reports what it left
 * out. The coverage gate ignores `.vue`, so none of this is counted there.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

const FILES = {
  'systematic-debugging': [
    { path: 'SKILL.md', sizeBytes: 8192 },
    { path: 'root-cause-tracing.md', sizeBytes: 40 },
    { path: 'scripts/find-polluter.sh', sizeBytes: 20 },
  ],
  review: [{ path: 'SKILL.md', sizeBytes: 10 }],
};
const CONTENT = {
  'systematic-debugging/SKILL.md':
    '# Debugging\n\nStart with `root-cause-tracing.md`, and run `npm test` first. Also [a missing one](gone.md).',
  'systematic-debugging/root-cause-tracing.md': 'Trace it. Then [run the script](scripts/find-polluter.sh). See [review](skill://review).',
  'systematic-debugging/scripts/find-polluter.sh': '#!/bin/sh\necho find',
  'review/SKILL.md': 'Review body',
};

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(() => Promise.resolve({ workspaces: [{ id: 'ws1', name: 'Home' }, { id: 'ws2', name: 'Platform' }, { id: 'ws3', name: 'Ops' }] })),
  searchWorkspaceSkills: vi.fn(() =>
    Promise.resolve({
      skills: [
        { name: 'systematic-debugging', description: 'Use when debugging.', totalBytes: 8252, sourceType: 'github', sourceRepo: 'obra/superpowers', sourceCommit: 'abcdef1234', locallyModified: true },
        { name: 'review', description: 'Use when reviewing.', totalBytes: 10, sourceType: 'manual', sharedFromWorkspaceId: 'ws2' },
      ],
    }),
  ),
  getWorkspaceSkill: vi.fn((_ws, name) => (FILES[name] ? Promise.resolve({ skill: { name, files: FILES[name] } }) : Promise.reject(Object.assign(new Error('not found'), { status: 404 })))),
  getWorkspaceSkillFile: vi.fn((_ws, name, path) => Promise.resolve({ file: { path, content: CONTENT[`${name}/${path}`] } })),
  importWorkspaceSkills: vi.fn(() =>
    Promise.resolve({
      imported: [{ name: 'tdd', fileCount: 2, totalBytes: 300 }],
      skipped: [{ path: 'skills/brainstorming', reason: 'SKILL.md is 100000 bytes; the limit is 98304 bytes (96 KiB)' }],
      sourceRepo: 'obra/superpowers',
      sourceRef: 'main',
      sourceCommit: '0123456789',
    }),
  ),
  deleteWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
  fetchWorkspaceSkillShares: vi.fn(() => Promise.resolve({ shares: [{ targetWorkspaceId: 'ws3' }] })),
  shareWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
  unshareWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
  setWorkspaceSkillEnabled: vi.fn((_ws, name, enabled) => Promise.resolve({ skill: { name, enabled } })),
}));
vi.mock('../src/api', () => api);

const { default: WorkspaceSkillsPanel } = await import('../src/components/WorkspaceSkillsPanel.vue');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

const settle = () => new Promise((r) => setTimeout(r, 30));
const apps = [];

async function mount() {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(WorkspaceSkillsPanel, { workspaceId: 'ws1' }) });
  app.use(createPinia());
  app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/:any(.*)*', component: { render: () => null } }] }));
  app.mount(el);
  apps.push(app);
  await settle();
  return el;
}

const text = (el) => el.textContent.replace(/\s+/g, ' ');
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  await settle();
};

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

describe('the Skills tab', () => {
  it('lays the skills out as cards, each leading to its own page', async () => {
    const el = await mount();
    expect(el.querySelector('[data-test="skill-grid"]').className).toContain('xl:grid-cols-3');
    const cards = el.querySelectorAll('[data-test="skill-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0].querySelector('[data-test="skill-card-name"]').textContent).toBe('review');
    expect(text(cards[0])).toContain('Shared from Platform');
    expect(cards[0].getAttribute('href')).toBe('/workspaces/ws1/settings/skills/review');
    expect(cards[1].querySelector('[data-test="skill-card-name"]').textContent).toBe('systematic-debugging');
    expect(text(cards[1])).toContain('Use when debugging.');
    expect(text(cards[1])).toContain('Modified');
    expect(cards[1].getAttribute('href')).toBe('/workspaces/ws1/settings/skills/systematic-debugging');
    // Nothing is read until a card is opened.
    expect(api.getWorkspaceSkill).not.toHaveBeenCalled();
    expect(text(el)).not.toContain('root-cause-tracing');
  });

  it('names where a plain skill came from', async () => {
    api.searchWorkspaceSkills.mockResolvedValueOnce({
      skills: [{ name: 'tdd', description: 'Use when testing.', totalBytes: 10, sourceType: 'github', sourceRepo: 'obra/superpowers', sourceCommit: 'abcdef1234' }],
    });
    const el = await mount();
    expect(text(el.querySelector('[data-test="skill-card"]'))).toContain('GitHub obra/superpowers@abcdef1');
  });

  it('says there are none, and says so differently when the list failed', async () => {
    api.searchWorkspaceSkills.mockResolvedValueOnce({});
    let el = await mount();
    expect(text(el)).toContain('No skills yet.');

    api.searchWorkspaceSkills.mockRejectedValueOnce(new Error('network unreachable'));
    el = await mount();
    expect(text(el)).toContain("Could not load this workspace's skills.");
    const retry = [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Try again');
    await click(retry);
    expect(el.querySelectorAll('[data-test="skill-card"]')).toHaveLength(2);
  });

  it('imports and reports what it left out', async () => {
    const el = await mount();
    const input = el.querySelector('#skill-import-url');
    input.value = 'https://github.com/obra/superpowers';
    input.dispatchEvent(new Event('input'));
    await nextTick();
    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();

    expect(api.importWorkspaceSkills).toHaveBeenCalledWith('ws1', 'https://github.com/obra/superpowers', false, undefined);
    const report = text(el.querySelector('[data-test="skill-import-report"]'));
    expect(report).toContain('Imported 1 skill from obra/superpowers@0123456');
    expect(report).toContain('skills/brainstorming — SKILL.md is 100000 bytes');
    // The list is fetched again, so what arrived shows up.
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
  });

  it('offers the skills of a repository too large to import whole, and imports only those chosen', async () => {
    api.importWorkspaceSkills.mockImplementationOnce(() =>
      Promise.resolve({
        imported: [],
        skipped: [],
        candidates: [
          { name: 'ship', path: 'ship', sizeBytes: 131838, reason: 'SKILL.md is 131838 bytes; the limit is 96 KiB' },
          { name: 'guard', path: 'guard', sizeBytes: 3401 },
          { name: 'careful', path: 'tools/careful', sizeBytes: 3516 },
        ],
        sourceRepo: 'garrytan/gstack',
        sourceRef: 'main',
      }),
    );
    const el = await mount();
    const input = el.querySelector('#skill-import-url');
    input.value = 'https://github.com/garrytan/gstack';
    input.dispatchEvent(new Event('input'));
    await nextTick();
    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();

    const choice = el.querySelector('[data-test="skill-import-choice"]');
    expect(text(choice)).toContain('garrytan/gstack is too large to import whole');
    expect(el.querySelector('[data-test="skill-import-report"]')).toBeNull();
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(1);
    // Those that can be chosen come first; one that cannot says why.
    const rows = [...el.querySelectorAll('[data-test="skill-candidate"]')];
    expect(rows.map((r) => r.querySelector('[data-test="skill-candidate-name"]').textContent)).toEqual(['careful', 'guard', 'ship']);
    expect(text(rows[0])).toContain('tools/careful');
    expect(text(rows[2])).toContain('the limit is 96 KiB');
    expect(rows[2].querySelector('input').disabled).toBe(true);
    const importChosen = el.querySelector('[data-test="skill-import-chosen"]');
    expect(importChosen.disabled).toBe(true);

    await click(el.querySelector('[data-test="skill-choice-all"]'));
    expect(text(choice)).toContain('2 of 2 selected');
    await click(el.querySelector('[data-test="skill-choice-none"]'));
    expect(text(choice)).toContain('0 of 2 selected');
    rows[1].querySelector('input').click();
    await settle();
    expect(text(importChosen)).toContain('Import 1 selected');

    // The link it came from is imported, whatever is typed since.
    input.value = 'https://github.com/other/repo';
    input.dispatchEvent(new Event('input'));
    await click(importChosen);
    expect(api.importWorkspaceSkills).toHaveBeenLastCalledWith('ws1', 'https://github.com/garrytan/gstack', false, ['guard']);
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
    expect(text(el.querySelector('[data-test="skill-import-report"]'))).toContain('Imported 1 skill');
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
  });

  it('puts a choice away on cancel', async () => {
    api.importWorkspaceSkills.mockImplementationOnce(() =>
      Promise.resolve({ imported: [], skipped: [], candidates: [{ name: 'guard', path: 'guard', sizeBytes: 1 }], sourceRepo: 'garrytan/gstack' }),
    );
    const el = await mount();
    const input = el.querySelector('#skill-import-url');
    input.value = 'https://github.com/garrytan/gstack';
    input.dispatchEvent(new Event('input'));
    await nextTick();
    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();
    const cancel = [...el.querySelectorAll('[data-test="skill-import-choice"] button')].find((b) => b.textContent.trim() === 'Cancel');
    await click(cancel);
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
  });

  it('turns an own skill off and on from its card, without leaving the tab', async () => {
    const el = await mount();
    const card = (name) => [...el.querySelectorAll('[data-test="skill-card"]')].find((c) => c.querySelector('[data-test="skill-card-name"]').textContent === name);
    const toggle = () => card('systematic-debugging').parentElement.querySelector('[data-test="skill-card-enabled"]');
    // A skill shared in is the owner's to switch, not this workspace's.
    expect(card('review').parentElement.querySelector('[data-test="skill-card-enabled"]')).toBeNull();
    // On by default, and the switch is beside the link, not inside it.
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(card('systematic-debugging').contains(toggle())).toBe(false);
    expect(card('systematic-debugging').querySelector('[data-test="skill-card-off"]')).toBeNull();

    await click(toggle());
    expect(api.setWorkspaceSkillEnabled).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', false);
    expect(toggle().getAttribute('aria-checked')).toBe('false');
    expect(card('systematic-debugging').dataset.enabled).toBe('false');
    expect(text(card('systematic-debugging').querySelector('[data-test="skill-card-off"]'))).toContain('Off');
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is now hidden from agents');

    await click(toggle());
    expect(api.setWorkspaceSkillEnabled).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', true);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is available to agents again');
  });

  it('holds a switch while its answer is out, and reports a failure without flipping it', async () => {
    api.searchWorkspaceSkills.mockResolvedValueOnce({ skills: [{ name: 'tdd', description: 'Test first.', totalBytes: 10, sourceType: 'manual', enabled: false }] });
    const el = await mount();
    const toggle = () => el.querySelector('[data-test="skill-card-enabled"]');
    expect(toggle().getAttribute('aria-checked')).toBe('false');

    let answer;
    api.setWorkspaceSkillEnabled.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    await click(toggle());
    expect(toggle().disabled).toBe(true);
    answer({ skill: { name: 'tdd', enabled: true } });
    await settle();
    expect(toggle().disabled).toBe(false);
    expect(toggle().getAttribute('aria-checked')).toBe('true');

    api.setWorkspaceSkillEnabled.mockRejectedValueOnce(new Error('read-only here'));
    await click(toggle());
    expect(toasts.value.at(-1).message).toBe('read-only here');
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toggle().disabled).toBe(false);
  });
});
