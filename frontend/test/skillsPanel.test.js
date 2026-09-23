// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The Skills tab, mounted. The rules of references are tested in
 * `skills.test.js`; this is the wiring that makes them the whole way in: only
 * skills are listed, a skill's other files open by following references from
 * the file on screen, Back retraces them, and an import reports what it left
 * out. The coverage gate ignores `.vue`, so none of this is counted there.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';

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
      skipped: [{ path: 'skills/brainstorming', reason: 'SKILL.md is 17548 bytes; the limit is 16384 bytes (16 KiB)' }],
      sourceRepo: 'obra/superpowers',
      sourceRef: 'main',
      sourceCommit: '0123456789',
    }),
  ),
  deleteWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
  fetchWorkspaceSkillShares: vi.fn(() => Promise.resolve({ shares: [{ targetWorkspaceId: 'ws3' }] })),
  shareWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
  unshareWorkspaceSkill: vi.fn(() => Promise.resolve(true)),
}));
vi.mock('../src/api', () => api);

const { default: WorkspaceSkillsPanel } = await import('../src/components/WorkspaceSkillsPanel.vue');

const settle = () => new Promise((r) => setTimeout(r, 30));
const apps = [];

async function mount() {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(WorkspaceSkillsPanel, { workspaceId: 'ws1' }) });
  app.use(createPinia());
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
const openFirst = async (el) => click(el.querySelector('[data-test="skill-row"] > button'));
const skillLink = (el, uri) => el.querySelector(`[data-skill-link="${uri}"]`);

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

describe('the Skills tab', () => {
  it('lists the skills and none of their other files', async () => {
    const el = await mount();
    const rows = el.querySelectorAll('[data-test="skill-row"]');
    expect(rows).toHaveLength(2);
    expect(text(rows[0])).toContain('review');
    expect(text(rows[0])).toContain('Shared from Platform');
    expect(text(rows[1])).toContain('systematic-debugging');
    expect(text(rows[1])).toContain('Modified locally');
    expect(text(rows[1])).toContain('GitHub obra/superpowers@abcdef1');
    expect(text(el)).not.toContain('root-cause-tracing');
  });

  it('opens a file named in inline code, follows links from it, and goes back', async () => {
    const el = await mount();
    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('systematic-debugging/SKILL.md');
    expect(text(el)).toContain('50% of the 16 KB limit');
    // Plain code that is no file stays plain.
    expect(el.querySelector('[data-test="skill-body"]').innerHTML).toContain('<code>npm test</code>');

    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md').querySelector('code'));
    expect(api.getWorkspaceSkillFile).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', 'root-cause-tracing.md');
    expect(text(el)).toContain('Trace it.');

    await click(skillLink(el, 'skill://systematic-debugging/scripts/find-polluter.sh'));
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('systematic-debugging/scripts/find-polluter.sh');
    expect(text(el)).toContain('echo find');

    await click(el.querySelector('[data-test="skill-back"]'));
    expect(text(el)).toContain('Trace it.');
    await click(el.querySelector('[data-test="skill-back"]'));
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('systematic-debugging/SKILL.md');
    expect(el.querySelector('[data-test="skill-back"]')).toBeNull();
  });

  it('switches skills from a skill:// link, and says so when a file is not there', async () => {
    const el = await mount();
    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);

    await click(skillLink(el, 'skill://systematic-debugging/gone.md'));
    expect(text(el.querySelector('[data-test="skill-missing"]'))).toContain('There is no gone.md in the skill systematic-debugging.');
    await click(el.querySelector('[data-test="skill-back"]'));

    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md'));
    await click(skillLink(el, 'skill://review/SKILL.md'));
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('review/SKILL.md');
    await click(el.querySelector('[data-test="skill-back"]'));
    await click(el.querySelector('[data-test="skill-back"]'));
    // A skill:// link to a skill this workspace cannot see reads as missing.
    CONTENT['systematic-debugging/SKILL.md'] += ' And [elsewhere](skill://nowhere).';
    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);
    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);
    await click(skillLink(el, 'skill://nowhere/SKILL.md'));
    expect(text(el.querySelector('[data-test="skill-missing"]'))).toContain('There is no skill called nowhere');
    await click(el.querySelector('[data-test="skill-back"]'));
    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md'));
    await click(skillLink(el, 'skill://review/SKILL.md'));
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('review/SKILL.md');
    expect(text(el)).toContain('Review body');
  });

  it('imports and reports what it left out', async () => {
    const el = await mount();
    const input = el.querySelector('#skill-import-url');
    input.value = 'https://github.com/obra/superpowers';
    input.dispatchEvent(new Event('input'));
    await nextTick();
    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();

    expect(api.importWorkspaceSkills).toHaveBeenCalledWith('ws1', 'https://github.com/obra/superpowers', false);
    const report = text(el.querySelector('[data-test="skill-import-report"]'));
    expect(report).toContain('Imported 1 skill from obra/superpowers@0123456');
    expect(report).toContain('skills/brainstorming — SKILL.md is 17548 bytes');
    // The list is fetched again, so what arrived shows up.
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
  });

  it('offers delete and share only on the workspace\'s own skills', async () => {
    const el = await mount();
    await openFirst(el); // review, shared in
    expect(el.querySelector('[data-test="skill-delete"]')).toBeNull();
    expect(el.querySelector('[data-test="skill-share"]')).toBeNull();
    expect(api.fetchWorkspaceSkillShares).not.toHaveBeenCalled();

    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);
    expect(text(el.querySelector('[data-test="skill-shares"]'))).toContain('Shared with Ops');
    // Neither this workspace nor one it is already shared with is offered.
    const options = [...el.querySelectorAll('[data-test="skill-share-target"] option')].map((o) => o.textContent.trim());
    expect(options).toEqual(['Share with a workspace…', 'Platform']);

    const select = el.querySelector('[data-test="skill-share-target"]');
    select.value = 'ws2';
    select.dispatchEvent(new Event('change'));
    await click(el.querySelector('[data-test="skill-share"]'));
    expect(api.shareWorkspaceSkill).toHaveBeenCalledWith('ws1', 'systematic-debugging', 'ws2');

    await click(el.querySelector('[data-test="skill-delete"]'));
    const confirm = [...document.querySelectorAll('button')].find((b) => /delete|confirm/i.test(b.textContent) && b.closest('[role="dialog"]'));
    await click(confirm);
    expect(api.deleteWorkspaceSkill).toHaveBeenCalledWith('ws1', 'systematic-debugging');
  });

  it('shows only the file it was last asked for, whichever answer arrives last', async () => {
    // The first skill's file answers after the second's.
    api.getWorkspaceSkillFile.mockImplementationOnce(
      (_ws, name, path) => new Promise((r) => setTimeout(() => r({ file: { path, content: CONTENT[`${name}/${path}`] } }), 60)),
    );
    const el = await mount();
    const rows = () => el.querySelectorAll('[data-test="skill-row"] > button');
    rows()[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await nextTick();
    rows()[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 120));
    expect(text(el.querySelector('[data-test="skill-breadcrumb"]'))).toBe('review/SKILL.md');
    expect(text(el)).toContain('Review body');
    expect(text(el)).not.toContain('Debugging');
  });

  it('says a skill is missing only when the server says so', async () => {
    const el = await mount();
    api.getWorkspaceSkill.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('Failed to fetch skill'), { status: 500 })));
    await click(el.querySelectorAll('[data-test="skill-row"] > button')[1]);
    expect(el.querySelector('[data-test="skill-missing"]')).toBeNull();
    expect(text(el)).toContain('Failed to fetch skill');
  });
});
