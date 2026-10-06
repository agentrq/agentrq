// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A skill's own page, mounted, in a workspace's settings and on the Skills
 * page. The rules of references are tested in `skills.test.js`; this is the
 * wiring that makes them the whole way in: a skill's files open from the list
 * beside the reader and by following references from the file on screen, Back
 * retraces them, and the page turns the skill on and off per workspace and for
 * the account, and deletes it. The coverage gate ignores `.vue`, so none of
 * this is counted there.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';

const FILES = {
  'systematic-debugging': [
    { path: 'SKILL.md', sizeBytes: 8192 },
    { path: 'root-cause-tracing.md', sizeBytes: 40 },
    { path: 'scripts/find-polluter.sh', sizeBytes: 20 },
  ],
  review: [{ path: 'SKILL.md', sizeBytes: 10 }],
};
const META = {
  'systematic-debugging': { description: 'Use when debugging.', totalBytes: 8252, fileCount: 3, sourceType: 'github', sourceRepo: 'obra/superpowers', sourceCommit: 'abcdef1234', locallyModified: true, workspaceIds: ['ws3'] },
  review: { description: 'Use when reviewing.', totalBytes: 10, fileCount: 1, sourceType: 'manual' },
};
const CONTENT = {
  'systematic-debugging/SKILL.md':
    '---\nname: systematic-debugging\n---\n# Debugging\n\nStart with `root-cause-tracing.md`, and run `npm test` first. Also [a missing one](gone.md). And [elsewhere](skill://nowhere).',
  'systematic-debugging/root-cause-tracing.md': 'Trace it. Then [run the script](scripts/find-polluter.sh). See [review](skill://review).',
  'systematic-debugging/scripts/find-polluter.sh': '#!/bin/sh\necho find',
  'review/SKILL.md': 'Review body',
};
const notFound = () => Promise.reject(Object.assign(new Error('not found'), { status: 404 }));

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(() =>
    Promise.resolve({
      workspaces: [
        { id: 'ws1', name: 'Home' },
        { id: 'ws2', name: 'Platform' },
        { id: 'ws3', name: 'Ops' },
        // A fork uses its parent's skills, so it has no box of its own.
        { id: 'ws4', name: 'Home-fork', forkOfId: 'ws1' },
      ],
    }),
  ),
  getSkill: vi.fn(),
  getSkillFile: vi.fn(),
  getWorkspaceSkill: vi.fn(),
  getWorkspaceSkillFile: vi.fn(),
  deleteSkill: vi.fn(() => Promise.resolve(true)),
  setSkillEnabled: vi.fn((name, enabled) => Promise.resolve({ skill: { name, enabled } })),
  setWorkspaceSkillEnabled: vi.fn(),
}));
vi.mock('../src/api', () => api);

const { default: SkillPage } = await import('../src/components/SkillPage.vue');
const { default: SkillsView } = await import('../src/views/SkillsView.vue');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

const settle = () => new Promise((r) => setTimeout(r, 30));
const apps = [];

// In a workspace's settings by default; `at` opens another path, such as the
// Skills page's /skills/<name>.
async function mount(name, at = `/workspaces/ws1/settings/skills/${name}`) {
  const el = document.createElement('div');
  document.body.append(el);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/workspaces/:id/settings/skills/:name', component: SkillPage },
      { path: '/skills/:name', component: SkillsView },
      { path: '/:any(.*)*', component: { render: () => h('p', { 'data-test': 'elsewhere' }, 'elsewhere') } },
    ],
  });
  await router.push(at);
  // The settings screen hands the page its workspace and skill from the route.
  const app = createApp({ render: () => h(RouterView, null, { default: ({ Component, route }) => Component && h(Component, { workspaceId: route.params.id, name: route.params.name }) }) });
  app.use(createPinia());
  app.use(router);
  app.mount(el);
  apps.push(app);
  await settle();
  return { el, router };
}

const text = (el) => el.textContent.replace(/\s+/g, ' ');
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  await settle();
};
const skillLink = (el, uri) => el.querySelector(`[data-skill-link="${uri}"]`);
const crumb = (el) => text(el.querySelector('[data-test="skill-breadcrumb"]'));

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.clearAllMocks();
  const read = (name) => (FILES[name] ? Promise.resolve({ skill: { name, ...META[name], files: FILES[name] } }) : notFound());
  const readFile = (name, path) => Promise.resolve({ file: { path, content: CONTENT[`${name}/${path}`] } });
  api.getWorkspaceSkill.mockImplementation((_ws, name) => read(name));
  api.getWorkspaceSkillFile.mockImplementation((_ws, name, path) => readFile(name, path));
  api.getSkill.mockImplementation(read);
  api.getSkillFile.mockImplementation(readFile);
  // The answer names the workspaces it is now on in.
  api.setWorkspaceSkillEnabled.mockImplementation((ws, name, enabled) =>
    Promise.resolve({ skill: { name, workspaceIds: enabled ? ['ws3', ws] : ['ws3'].filter((x) => x !== ws) } }),
  );
});

describe('a skill\'s page', () => {
  it('heads the page with what the card showed, and opens SKILL.md without its frontmatter', async () => {
    const { el } = await mount('systematic-debugging');
    expect(el.querySelector('[data-test="skill-page-name"]').textContent).toBe('systematic-debugging');
    expect(text(el)).toContain('Use when debugging.');
    expect(text(el)).toContain('Modified locally');
    expect(text(el.querySelector('[data-test="skill-page-meta"]'))).toContain('3 files · GitHub obra/superpowers@abcdef1');
    expect(el.querySelector('[data-test="skill-page-back-to-list"]').getAttribute('href')).toBe('/workspaces/ws1/settings?tab=skills');
    expect(crumb(el)).toBe('systematic-debugging/SKILL.md');
    expect(text(el)).toContain('8% of the 96 KB limit');
    expect(text(el.querySelector('[data-test="skill-body"]'))).not.toContain('name: systematic-debugging');
    // Plain code that is no file stays plain.
    expect(el.querySelector('[data-test="skill-body"]').innerHTML).toContain('<code>npm test</code>');
    // The skill is fetched once, not again for its first file.
    expect(api.getWorkspaceSkill).toHaveBeenCalledTimes(1);
  });

  it('opens a file named in inline code, follows links from it, and goes back', async () => {
    const { el } = await mount('systematic-debugging');
    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md').querySelector('code'));
    expect(api.getWorkspaceSkillFile).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', 'root-cause-tracing.md');
    expect(text(el)).toContain('Trace it.');

    await click(skillLink(el, 'skill://systematic-debugging/scripts/find-polluter.sh'));
    expect(crumb(el)).toBe('systematic-debugging/scripts/find-polluter.sh');
    expect(text(el)).toContain('echo find');

    await click(el.querySelector('[data-test="skill-back"]'));
    expect(text(el)).toContain('Trace it.');
    await click(el.querySelector('[data-test="skill-back"]'));
    expect(crumb(el)).toBe('systematic-debugging/SKILL.md');
    expect(el.querySelector('[data-test="skill-back"]')).toBeNull();
  });

  it('switches skills from a skill:// link, and says so when a file or skill is not there', async () => {
    const { el } = await mount('systematic-debugging');
    await click(skillLink(el, 'skill://systematic-debugging/gone.md'));
    expect(text(el.querySelector('[data-test="skill-missing"]'))).toContain('There is no gone.md in the skill systematic-debugging.');
    await click(el.querySelector('[data-test="skill-back"]'));

    await click(skillLink(el, 'skill://nowhere/SKILL.md'));
    expect(text(el.querySelector('[data-test="skill-missing"]'))).toContain('There is no skill called nowhere');
    await click(el.querySelector('[data-test="skill-back"]'));

    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md'));
    await click(skillLink(el, 'skill://review/SKILL.md'));
    expect(crumb(el)).toBe('review/SKILL.md');
    expect(text(el)).toContain('Review body');
  });

  it('lists the skill\'s files as a tree, and opens one from it', async () => {
    const { el } = await mount('systematic-debugging');
    const files = () => [...el.querySelectorAll('[data-test="skill-file"]')];
    const rows = () => [...el.querySelectorAll('[data-test="skill-files"] li')].map((li) => li.querySelector('span').textContent);
    expect(rows()).toEqual(['SKILL.md', 'scripts/', 'find-polluter.sh', 'root-cause-tracing.md']);
    // Folded on a phone until asked for; the md: classes show it wider up.
    const toggle = el.querySelector('[data-test="skill-files-toggle"]');
    expect(toggle.textContent).toContain('Files (3)');
    expect(el.querySelector('[data-test="skill-files"] ul').classList.contains('hidden')).toBe(true);
    await click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(el.querySelector('[data-test="skill-files"] ul').classList.contains('hidden')).toBe(false);
    expect(files()[0].getAttribute('aria-current')).toBe('page');

    // A folder folds away and opens again.
    const dir = el.querySelector('[data-test="skill-dir"]');
    expect(dir.getAttribute('aria-expanded')).toBe('true');
    await click(dir);
    expect(rows()).toEqual(['SKILL.md', 'scripts/', 'root-cause-tracing.md']);
    expect(el.querySelector('[data-test="skill-dir"]').getAttribute('aria-expanded')).toBe('false');
    await click(el.querySelector('[data-test="skill-dir"]'));

    await click(files()[1]);
    expect(crumb(el)).toBe('systematic-debugging/scripts/find-polluter.sh');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(files()[1].getAttribute('aria-current')).toBe('page');
    expect(files()[0].getAttribute('aria-current')).toBeNull();
    // The open file again does nothing, so Back is one step.
    await click(files()[1]);
    await click(el.querySelector('[data-test="skill-back"]'));
    expect(crumb(el)).toBe('systematic-debugging/SKILL.md');
    expect(el.querySelector('[data-test="skill-back"]')).toBeNull();

    // After a link into another skill the tree still belongs to this page, and
    // none of it is open.
    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md'));
    await click(skillLink(el, 'skill://review/SKILL.md'));
    expect(files().some((b) => b.getAttribute('aria-current'))).toBe(false);
    await click(files()[0]);
    expect(crumb(el)).toBe('systematic-debugging/SKILL.md');
    expect(api.getWorkspaceSkill).toHaveBeenLastCalledWith('ws1', 'systematic-debugging');
  });

  it('shows the raw file on request', async () => {
    const { el } = await mount('systematic-debugging');
    const raw = [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Raw');
    await click(raw);
    expect(el.querySelector('[data-test="skill-body"]')).toBeNull();
    expect(text(el)).toContain('name: systematic-debugging');
  });

  it('ticks the workspaces it is on in, turns it on and off in each by the box, and deletes it', async () => {
    const { el, router } = await mount('systematic-debugging');
    const options = () => [...el.querySelectorAll('[data-test="skill-workspace-option"]')];
    const box = (name) => options().find((o) => o.textContent.trim() === name).querySelector('input');
    // Every workspace but forks, this one included, ticked where it is on.
    expect(options().map((o) => o.textContent.trim())).toEqual(['Home', 'Ops', 'Platform']);
    expect(box('Ops').checked).toBe(true);
    expect(box('Platform').checked).toBe(false);

    await click(box('Platform'));
    expect(api.setWorkspaceSkillEnabled).toHaveBeenCalledWith('ws2', 'systematic-debugging', true);
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is on in Platform');
    expect(box('Platform').checked).toBe(true);

    await click(box('Ops'));
    expect(api.setWorkspaceSkillEnabled).toHaveBeenLastCalledWith('ws3', 'systematic-debugging', false);
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is off in Ops');
    expect(box('Ops').checked).toBe(false);

    await click(el.querySelector('[data-test="skill-delete"]'));
    const dialog = document.querySelector('[role="dialog"]');
    expect(text(dialog)).toContain("Delete the skill 'systematic-debugging' and all its files? Every workspace loses it. This cannot be undone.");
    const confirm = [...document.querySelectorAll('button')].find((b) => /delete|confirm/i.test(b.textContent) && b.closest('[role="dialog"]'));
    await click(confirm);
    expect(api.deleteSkill).toHaveBeenCalledWith('systematic-debugging');
    expect(router.currentRoute.value.fullPath).toBe('/workspaces/ws1/settings?tab=skills');
  });

  it('turns the skill off and on for the whole account, and says what that means', async () => {
    const { el } = await mount('systematic-debugging');
    const toggle = () => el.querySelector('[data-test="skill-enabled"]');
    const hint = () => text(el.querySelector('[data-test="skill-enabled-hint"]'));
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(hint()).toContain('On: agents find it with searchSkills in the workspaces it is on in');

    await click(toggle());
    expect(api.setSkillEnabled).toHaveBeenLastCalledWith('systematic-debugging', false);
    expect(api.setWorkspaceSkillEnabled).not.toHaveBeenCalled();
    expect(toggle().getAttribute('aria-checked')).toBe('false');
    expect(hint()).toContain('Off: kept, but no agent finds or loads it');
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is now hidden from agents');

    let answer;
    api.setSkillEnabled.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    await click(toggle());
    expect(toggle().disabled).toBe(true);
    answer({ skill: { name: 'systematic-debugging', enabled: true } });
    await settle();
    expect(toggle().disabled).toBe(false);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is available to agents again');

    api.setSkillEnabled.mockRejectedValueOnce(new Error('skill systematic-debugging not found'));
    await click(toggle());
    expect(toasts.value.at(-1).message).toBe('skill systematic-debugging not found');
    expect(toggle().getAttribute('aria-checked')).toBe('true');
  });

  it('holds a box while its answer is out', async () => {
    const { el } = await mount('systematic-debugging');
    let answer;
    api.setWorkspaceSkillEnabled.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    const platform = () => [...el.querySelectorAll('[data-test="skill-workspace-option"]')].find((o) => o.textContent.trim() === 'Platform').querySelector('input');
    await click(platform());
    expect(platform().disabled).toBe(true);
    answer({ skill: { name: 'systematic-debugging', workspaceIds: ['ws3', 'ws2'] } });
    await settle();
    expect(platform().disabled).toBe(false);
    expect(platform().checked).toBe(true);
  });

  it('reports a failed switch or delete by toast, and puts the box back', async () => {
    const { el, router } = await mount('systematic-debugging');
    const box = (name) => [...el.querySelectorAll('[data-test="skill-workspace-option"]')].find((o) => o.textContent.trim() === name).querySelector('input');

    api.setWorkspaceSkillEnabled.mockRejectedValueOnce(new Error('workspace not found'));
    await click(box('Platform'));
    expect(toasts.value.at(-1).message).toBe('workspace not found');
    expect(box('Platform').checked).toBe(false);
    expect(box('Ops').checked).toBe(true);

    api.setWorkspaceSkillEnabled.mockRejectedValueOnce(new Error('workspace not found'));
    await click(box('Ops'));
    expect(box('Ops').checked).toBe(true);

    // An answer naming no workspaces means it is on in none.
    api.setWorkspaceSkillEnabled.mockResolvedValueOnce({ skill: { name: 'systematic-debugging' } });
    await click(box('Ops'));
    expect(box('Ops').checked).toBe(false);

    api.deleteSkill.mockRejectedValueOnce(new Error('database unavailable'));
    await click(el.querySelector('[data-test="skill-delete"]'));
    await click([...document.querySelectorAll('button')].find((b) => /delete|confirm/i.test(b.textContent) && b.closest('[role="dialog"]')));
    expect(toasts.value.at(-1).message).toBe('database unavailable');
    expect(router.currentRoute.value.path).toBe('/workspaces/ws1/settings/skills/systematic-debugging');
  });

  it('is the Skills page\'s too: read from the account, leading back to /skills', async () => {
    const { el, router } = await mount('systematic-debugging', '/skills/systematic-debugging');
    expect(el.querySelector('h1').textContent.trim()).toBe('Skills');
    expect(el.querySelector('[data-test="skill-page-name"]').textContent).toBe('systematic-debugging');
    expect(api.getSkill).toHaveBeenCalledWith('systematic-debugging');
    expect(api.getSkillFile).toHaveBeenCalledWith('systematic-debugging', 'SKILL.md');
    expect(api.getWorkspaceSkill).not.toHaveBeenCalled();
    expect(el.querySelector('[data-test="skill-page-back-to-list"]').getAttribute('href')).toBe('/skills');

    await click(skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md'));
    expect(api.getSkillFile).toHaveBeenLastCalledWith('systematic-debugging', 'root-cause-tracing.md');

    await click(el.querySelector('[data-test="skill-delete"]'));
    await click([...document.querySelectorAll('button')].find((b) => /delete|confirm/i.test(b.textContent) && b.closest('[role="dialog"]')));
    expect(api.deleteSkill).toHaveBeenCalledWith('systematic-debugging');
    expect(router.currentRoute.value.fullPath).toBe('/skills');
  });

  it('says a skill is missing only when the server says so, and retries a failure', async () => {
    let { el } = await mount('nowhere');
    expect(text(el.querySelector('[data-test="skill-page-missing"]')).trim()).toBe('There is no skill called nowhere.');

    api.getWorkspaceSkill.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('Failed to fetch skill'), { status: 500 })));
    ({ el } = await mount('review'));
    expect(el.querySelector('[data-test="skill-page-missing"]')).toBeNull();
    expect(text(el)).toContain('Failed to fetch skill');
    await click([...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Try again'));
    expect(text(el)).toContain('Review body');
  });

  it('shows a file that failed to load as a failure, not as missing', async () => {
    api.getWorkspaceSkillFile.mockImplementationOnce(() => Promise.reject(Object.assign(new Error('storage down'), { status: 500 })));
    const { el } = await mount('review');
    expect(el.querySelector('[data-test="skill-missing"]')).toBeNull();
    expect(text(el)).toContain('storage down');
  });

  it('loads the next skill when the route changes under it', async () => {
    const { el, router } = await mount('systematic-debugging');
    await router.push('/workspaces/ws1/settings/skills/review');
    await settle();
    expect(el.querySelector('[data-test="skill-page-name"]').textContent).toBe('review');
    expect(crumb(el)).toBe('review/SKILL.md');
  });

  it('shows only the file it was last asked for, whichever answer arrives last', async () => {
    const { el } = await mount('systematic-debugging');
    // The first file answers after the second.
    api.getWorkspaceSkillFile.mockImplementationOnce(
      (_ws, name, path) => new Promise((r) => setTimeout(() => r({ file: { path, content: CONTENT[`${name}/${path}`] } }), 60)),
    );
    skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await nextTick();
    skillLink(el, 'skill://systematic-debugging/root-cause-tracing.md')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 120));
    expect(crumb(el)).toBe('systematic-debugging/root-cause-tracing.md');
    expect(text(el)).toContain('Trace it.');
  });
});
