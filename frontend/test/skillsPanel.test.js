// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The skills list, mounted: on a workspace's Skills tab and, given no
 * workspace, on the Skills page. A grid of cards, each leading to the skill's
 * own page (`skillPage.test.js`) beside its switch, and an import that turns
 * what it brings on in the workspaces ticked and reports what it left out.
 * The coverage gate ignores `.vue`, so none of this is counted there.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

const SKILLS = [
  { name: 'systematic-debugging', description: 'Use when debugging.', totalBytes: 8252, sourceType: 'github', sourceRepo: 'obra/superpowers', sourceCommit: 'abcdef1234', locallyModified: true, enabled: true, workspaceIds: ['ws1', 'ws2'], workspaceEnabled: true },
  { name: 'review', description: 'Use when reviewing.', totalBytes: 10, sourceType: 'manual', enabled: false, workspaceIds: [] },
];
// What the account's route sends: workspaceEnabled only comes through a workspace.
const accountView = () => SKILLS.map(({ workspaceEnabled: _, ...s }) => s);

const WORKSPACES = [
  { id: 'ws1', name: 'Home' },
  { id: 'ws2', name: 'Platform' },
  { id: 'ws3', name: 'Ops' },
  { id: 'ws4', name: 'Home-fork', forkOfId: 'ws1' },
];
const IMPORTED = {
  imported: [{ name: 'tdd', fileCount: 2, totalBytes: 300 }],
  skipped: [{ path: 'skills/brainstorming', reason: 'SKILL.md is 100000 bytes; the limit is 98304 bytes (96 KiB)' }],
  sourceRepo: 'obra/superpowers',
  sourceRef: 'main',
  sourceCommit: '0123456789',
};

// What an import's listing offers: two to import, one that cannot be.
const LISTED = {
  imported: [],
  skipped: [],
  candidates: [
    { name: 'tdd', path: 'skills/tdd', sizeBytes: 300 },
    { name: 'brainstorming', path: 'skills/brainstorming', sizeBytes: 100000, reason: 'SKILL.md is 100000 bytes; the limit is 98304 bytes (96 KiB)' },
    { name: 'writing-plans', path: 'skills/writing-plans', sizeBytes: 50 },
  ],
  sourceRepo: 'obra/superpowers',
  sourceRef: 'main',
  sourceCommit: '0123456789',
};

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(),
  searchSkills: vi.fn(),
  searchWorkspaceSkills: vi.fn(),
  importSkills: vi.fn(),
  setSkillEnabled: vi.fn((name, enabled) => Promise.resolve({ skill: { name, enabled } })),
  // Read through a workspace, an answer that is off leaves workspaceEnabled out.
  setWorkspaceSkillEnabled: vi.fn((_ws, name, enabled) => Promise.resolve({ skill: enabled ? { name, workspaceEnabled: true } : { name } })),
  // Shared into no team workspace: no team skills group, no "Give to team".
  listWorkspaceTeamSkills: vi.fn(() => Promise.resolve({ skills: [], teamWorkspaces: [] })),
  setWorkspaceTeamSkill: vi.fn(),
  giveSkillToTeam: vi.fn(),
  // The server has no GitHub App: the import offers public repositories only.
  getGitLabConnection: vi.fn(() => Promise.resolve({ host: 'gitlab.com', enabled: false })),
  getGitHubConnection: vi.fn(() => Promise.resolve({ enabled: false, installations: [] })),
  disconnectGitHub: vi.fn(),
}));
vi.mock('../src/api', () => api);
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect: () => {}, disconnect: () => {}, onEvent: () => () => {} }),
}));

const { default: SkillsPanel } = await import('../src/components/SkillsPanel.vue');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges');

const settle = () => new Promise((r) => setTimeout(r, 30));
const apps = [];

async function mount(workspaceId = 'ws1') {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(SkillsPanel, workspaceId ? { workspaceId } : {}) });
  app.use(createPinia());
  app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/:any(.*)*', component: { render: () => null } }] }));
  app.mount(el);
  apps.push(app);
  await settle();
  return el;
}

const text = (el) => el.textContent.replace(/\s+/g, ' ').trim();
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  await settle();
};
const card = (el, name) => [...el.querySelectorAll('[data-test="skill-card"]')].find((c) => c.querySelector('[data-test="skill-card-name"]').textContent === name);
const toggleOf = (el, name) => card(el, name).parentElement.querySelector('[data-test="skill-card-enabled"]');
const switchOf = (row) => row.querySelector('[data-test="skill-candidate-switch"]');
const workspaceBoxes = (el) => [...el.querySelectorAll('[data-test="skill-import-workspace"]')];
const ticked = (el) => workspaceBoxes(el).filter((o) => o.querySelector('input').checked).map((o) => o.textContent.trim());

async function submitImport(el, url) {
  const input = el.querySelector('#skill-import-url');
  input.value = url;
  input.dispatchEvent(new Event('input'));
  await nextTick();
  el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  await settle();
}

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.clearAllMocks();
  api.fetchWorkspaces.mockImplementation(() => Promise.resolve({ workspaces: WORKSPACES }));
  api.searchSkills.mockImplementation(() => Promise.resolve({ skills: accountView() }));
  api.searchWorkspaceSkills.mockImplementation(() => Promise.resolve({ skills: SKILLS }));
  api.importSkills.mockImplementation((_url, _overwrite, _skills, _ws, list) => Promise.resolve(list ? LISTED : IMPORTED));
});

describe('the Skills tab', () => {
  it('lays every skill of the account out as cards, each leading to its own page', async () => {
    const el = await mount();
    expect(api.searchWorkspaceSkills).toHaveBeenCalledWith('ws1');
    expect(api.searchSkills).not.toHaveBeenCalled();
    expect(el.querySelector('[data-test="skill-grid"]').className).toContain('xl:grid-cols-3');
    const cards = el.querySelectorAll('[data-test="skill-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0].querySelector('[data-test="skill-card-name"]').textContent).toBe('review');
    expect(cards[0].getAttribute('href')).toBe('/workspaces/ws1/settings/skills/review');
    expect(cards[1].querySelector('[data-test="skill-card-name"]').textContent).toBe('systematic-debugging');
    expect(text(cards[1])).toContain('Use when debugging.');
    expect(text(cards[1])).toContain('Modified');
    expect(cards[1].getAttribute('href')).toBe('/workspaces/ws1/settings/skills/systematic-debugging');
    // Nothing is read until a card is opened.
    expect(text(el)).not.toContain('root-cause-tracing');
    // A workspace's card does not count the others; the tab says where they all are.
    expect(el.querySelector('[data-test="skill-card-workspaces"]')).toBeNull();
    // There is no account-wide Skills page to link to.
    expect(el.querySelector('[data-test="skills-page-link"]')).toBeNull();
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

  it('imports into this workspace alone, with no choice of others, and reports what it left out', async () => {
    const el = await mount();
    expect(el.querySelector('[data-test="skill-import-workspaces"]')).toBeNull();
    // The form's own button discovers; the list's imports.
    expect(text(el.querySelector('[data-test="skill-import"]'))).toBe('Discover');
    let answer;
    api.importSkills.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    await submitImport(el, 'https://github.com/obra/superpowers');
    expect(text(el.querySelector('[data-test="skill-import"]'))).toBe('Discovering…');
    answer(LISTED);
    await settle();
    expect(text(el.querySelector('[data-test="skill-import"]'))).toBe('Discover');

    // The repository's skills are listed first, every one that can be on.
    expect(api.importSkills).toHaveBeenCalledWith('https://github.com/obra/superpowers', false, undefined, ['ws1'], true);
    const choice = el.querySelector('[data-test="skill-import-choice"]');
    expect(text(choice)).toContain('Choose the skills to import from obra/superpowers');
    expect(text(choice)).not.toContain('too large');
    expect(text(choice)).toContain('2 of 2 on');
    expect(el.querySelector('[data-test="skill-import-report"]')).toBeNull();
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(1);

    // All on imports the repository whole, naming no skill.
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/obra/superpowers', false, undefined, ['ws1'], false);
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
    const report = text(el.querySelector('[data-test="skill-import-report"]'));
    expect(report).toContain('Imported 1 skill from obra/superpowers@0123456');
    expect(report).toContain('skills/brainstorming — SKILL.md is 100000 bytes');
    // The list is fetched again, so what arrived shows up, on here.
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
  });

  it('imports from a fork into its parent, whose skills the fork uses', async () => {
    const el = await mount('ws4');
    await submitImport(el, 'https://github.com/obra/superpowers');
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/obra/superpowers', false, undefined, ['ws1'], false);
  });

  it('imports only the skills left switched on, with the skill cards\' switch', async () => {
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/superpowers');
    const rows = [...el.querySelectorAll('[data-test="skill-candidate"]')];
    expect(rows.map((r) => r.querySelector('[data-test="skill-candidate-name"]').textContent)).toEqual(['tdd', 'writing-plans', 'brainstorming']);
    expect(rows.map((r) => switchOf(r).getAttribute('aria-checked'))).toEqual(['true', 'true', 'false']);
    expect(switchOf(rows[2]).disabled).toBe(true);
    expect(switchOf(rows[0]).getAttribute('role')).toBe('switch');
    expect(switchOf(rows[0]).getAttribute('aria-label')).toBe('Import tdd');
    // The skill cards' look: dark when on, grey when off.
    expect(switchOf(rows[0]).className).toContain('aria-checked:bg-gray-900');

    await click(switchOf(rows[0]));
    expect(switchOf(rows[0]).getAttribute('aria-checked')).toBe('false');
    expect(switchOf(rows[0]).getAttribute('aria-checked')).toBe('false');
    const importChosen = el.querySelector('[data-test="skill-import-chosen"]');
    expect(text(importChosen)).toBe('Import 1');
    // A disabled switch stays off.
    switchOf(rows[2]).disabled = false;
    await click(switchOf(rows[2]));
    switchOf(rows[2]).disabled = true;
    expect(switchOf(rows[2]).getAttribute('aria-checked')).toBe('false');

    await click(importChosen);
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/obra/superpowers', false, ['skills/writing-plans'], ['ws1'], false);
    expect(text(el.querySelector('[data-test="skill-import-report"]'))).toContain('Imported 1 skill');
  });

  it('switches every skill off and on again with one switch, importing nothing with all off', async () => {
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/superpowers');
    const choice = el.querySelector('[data-test="skill-import-choice"]');
    const all = el.querySelector('[data-test="skill-choice-all"]');
    expect(all.getAttribute('role')).toBe('switch');
    expect(all.getAttribute('aria-checked')).toBe('true');
    await click(all);
    expect(all.getAttribute('aria-checked')).toBe('false');
    expect(text(choice)).toContain('0 of 2 on');
    expect(el.querySelector('[data-test="skill-import-chosen"]').disabled).toBe(true);
    const row = el.querySelector('[data-test="skill-candidate"]');
    await click(switchOf(row));
    expect(text(choice)).toContain('1 of 2 on');
    // Some on is not all on: the switch turns the rest on.
    expect(all.getAttribute('aria-checked')).toBe('false');
    await click(all);
    expect(text(choice)).toContain('2 of 2 on');
    expect(all.getAttribute('aria-checked')).toBe('true');
  });

  it('has nothing for the one switch to turn on when no skill can be imported', async () => {
    api.importSkills.mockImplementationOnce(() =>
      Promise.resolve({ imported: [], skipped: [], candidates: [{ name: 'ship', path: 'ship', reason: 'too big' }], sourceRepo: 'garrytan/gstack' }),
    );
    const el = await mount();
    await submitImport(el, 'https://github.com/garrytan/gstack');
    const all = el.querySelector('[data-test="skill-choice-all"]');
    expect(all.disabled).toBe(true);
    expect(all.getAttribute('aria-checked')).toBe('false');
  });

  it('reports a repository with no skills to choose from', async () => {
    api.importSkills.mockImplementationOnce(() => Promise.resolve({ imported: [], skipped: [], candidates: [], sourceRepo: 'a/empty', sourceRef: 'main' }));
    const el = await mount();
    await submitImport(el, 'https://github.com/a/empty');
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
    expect(text(el.querySelector('[data-test="skill-import-report"]'))).toContain('Imported 0 skills from a/empty');
  });

  it('shows why an import failed', async () => {
    api.importSkills.mockRejectedValueOnce(new Error('not a GitHub repository'));
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/superpowers');
    expect(text(el)).toContain('not a GitHub repository');
    expect(el.querySelector('[data-test="skill-import-report"]')).toBeNull();
  });

  it('offers the skills of a repository too large to import whole, and imports only those chosen', async () => {
    api.importSkills.mockImplementationOnce(() =>
      Promise.resolve({
        imported: [],
        skipped: [],
        candidates: [
          { name: 'ship', path: 'ship', sizeBytes: 131838, reason: 'SKILL.md is 131838 bytes; the limit is 96 KiB' },
          { name: 'guard', path: 'guard', sizeBytes: 3401 },
          { name: 'careful', path: 'tools/careful', sizeBytes: 3516 },
        ],
        tooLarge: true,
        sourceRepo: 'garrytan/gstack',
        sourceRef: 'main',
      }),
    );
    const el = await mount();
    await submitImport(el, 'https://github.com/garrytan/gstack');
    const input = el.querySelector('#skill-import-url');

    const choice = el.querySelector('[data-test="skill-import-choice"]');
    expect(text(choice)).toContain('garrytan/gstack is too large to import whole');
    expect(el.querySelector('[data-test="skill-import-report"]')).toBeNull();
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(1);
    // Those that can be chosen come first; one that cannot says why.
    const rows = [...el.querySelectorAll('[data-test="skill-candidate"]')];
    expect(rows.map((r) => r.querySelector('[data-test="skill-candidate-name"]').textContent)).toEqual(['careful', 'guard', 'ship']);
    expect(text(rows[0])).toContain('tools/careful');
    expect(text(rows[2])).toContain('the limit is 96 KiB');
    expect(switchOf(rows[2]).disabled).toBe(true);
    // Too large to import whole, every skill starts off.
    const importChosen = el.querySelector('[data-test="skill-import-chosen"]');
    expect(importChosen.disabled).toBe(true);

    await click(el.querySelector('[data-test="skill-choice-all"]'));
    expect(text(choice)).toContain('2 of 2 on');
    await click(el.querySelector('[data-test="skill-choice-all"]'));
    expect(text(choice)).toContain('0 of 2 on');
    await click(switchOf(rows[1]));
    expect(text(importChosen)).toContain('Import 1');

    // The link it came from is imported, whatever is typed since.
    input.value = 'https://github.com/other/repo';
    input.dispatchEvent(new Event('input'));
    await click(importChosen);
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/garrytan/gstack', false, ['guard'], ['ws1'], false);
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
    expect(text(el.querySelector('[data-test="skill-import-report"]'))).toContain('Imported 1 skill');
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
  });

  it('says what is wrong with the link under its field when Discover is pressed, not while typing', async () => {
    const el = await mount();
    const input = el.querySelector('#skill-import-url');
    const discover = el.querySelector('[data-test="skill-import"]');
    expect(discover.disabled).toBe(false);
    await click(discover);
    expect(api.importSkills).not.toHaveBeenCalled();
    expect(text(el.querySelector('[data-test="field-error"]'))).toBe('Enter a link to a GitHub or GitLab repository.');
    expect(input.getAttribute('aria-invalid')).toBe('true');
    expect(input.getAttribute('aria-describedby')).toContain('skill-import-url-error');
    expect(document.activeElement).toBe(input);

    // Typing puts the error away; a link the importer cannot take says why.
    input.value = 'https://example.com/a/b';
    input.dispatchEvent(new Event('input'));
    await settle();
    expect(el.querySelector('[data-test="field-error"]')).toBeNull();
    expect(input.getAttribute('aria-invalid')).toBeNull();
    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();
    expect(text(el.querySelector('[data-test="field-error"]'))).toContain('Expected https://github.com/owner/repo or https://gitlab.com/group/project');
    expect(api.importSkills).not.toHaveBeenCalled();
  });

  it('says a link the server refused under its field, and an import that failed below the list', async () => {
    api.importSkills.mockRejectedValueOnce(new Error('obra/nothing was not found'));
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/nothing');
    expect(text(el.querySelector('[data-test="field-error"]'))).toBe('obra/nothing was not found');
    expect(document.activeElement).toBe(el.querySelector('#skill-import-url'));
    expect(el.querySelector('[data-test="skill-import-error"]')).toBeNull();

    await submitImport(el, 'https://github.com/obra/superpowers');
    expect(el.querySelector('[data-test="field-error"]')).toBeNull();
    api.importSkills.mockRejectedValueOnce(new Error('the chosen skills are larger than 16 MB; choose fewer'));
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    const failed = el.querySelector('[data-test="skill-import-error"]');
    expect(failed.getAttribute('role')).toBe('alert');
    expect(text(failed)).toBe('the chosen skills are larger than 16 MB; choose fewer');
    // The list stays, to choose fewer.
    expect(el.querySelector('[data-test="skill-import-choice"]')).not.toBeNull();
  });

  it('lands on the list when it appears, and puts it away when the link changes', async () => {
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/superpowers');
    const choice = el.querySelector('[data-test="skill-import-choice"]');
    expect(document.activeElement).toBe(choice.querySelector('p'));
    const input = el.querySelector('#skill-import-url');
    input.value = 'https://github.com/obra/other';
    input.dispatchEvent(new Event('input'));
    await settle();
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
  });

  it('switches nothing while an import runs', async () => {
    const el = await mount();
    await submitImport(el, 'https://github.com/obra/superpowers');
    let answer;
    api.importSkills.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    const choice = el.querySelector('[data-test="skill-import-choice"]');
    expect([...choice.querySelectorAll('[role="switch"]')].every((b) => b.disabled)).toBe(true);
    expect(text(el.querySelector('[data-test="skill-import-chosen"]'))).toBe('Importing…');
    // The form's own button still says Discover while the list imports.
    expect(text(el.querySelector('[data-test="skill-import"]'))).toBe('Discover');
    answer(IMPORTED);
    await settle();
    expect(text(el.querySelector('[data-test="skill-import-report"]'))).toContain('Imported 1 skill');
    expect(el.querySelector('[data-test="skill-import-report"]').getAttribute('role')).toBe('status');
  });

  it('puts a choice away on cancel', async () => {
    api.importSkills.mockImplementationOnce(() =>
      Promise.resolve({ imported: [], skipped: [], candidates: [{ name: 'guard', path: 'guard', sizeBytes: 1 }], sourceRepo: 'garrytan/gstack' }),
    );
    const el = await mount();
    await submitImport(el, 'https://github.com/garrytan/gstack');
    const cancel = [...el.querySelectorAll('[data-test="skill-import-choice"] button')].find((b) => b.textContent.trim() === 'Cancel');
    await click(cancel);
    expect(el.querySelector('[data-test="skill-import-choice"]')).toBeNull();
  });

  it('turns a skill on and off in this workspace from its card, without leaving the tab', async () => {
    const el = await mount();
    const toggle = () => toggleOf(el, 'systematic-debugging');
    // On here, and the switch is beside the link, not inside it.
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toggle().getAttribute('aria-label')).toBe('On in this workspace: systematic-debugging');
    expect(card(el, 'systematic-debugging').contains(toggle())).toBe(false);
    expect(card(el, 'systematic-debugging').querySelector('[data-test="skill-card-off"]')).toBeNull();

    await click(toggle());
    expect(api.setWorkspaceSkillEnabled).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', false);
    expect(api.setSkillEnabled).not.toHaveBeenCalled();
    // The answer leaves workspaceEnabled out, which is off.
    expect(toggle().getAttribute('aria-checked')).toBe('false');
    expect(card(el, 'systematic-debugging').dataset.enabled).toBe('false');
    expect(toggle().title).toBe("Off: this workspace's agents do not see it");
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is off in this workspace');

    await click(toggle());
    expect(api.setWorkspaceSkillEnabled).toHaveBeenLastCalledWith('ws1', 'systematic-debugging', true);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toggle().title).toBe("On: this workspace's agents can find and load it");
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is on in this workspace');
  });

  it('says when a skill is off for the whole account, whatever this workspace\'s switch says', async () => {
    api.searchWorkspaceSkills.mockResolvedValueOnce({ skills: [{ ...SKILLS[0], enabled: false }] });
    const el = await mount();
    expect(toggleOf(el, 'systematic-debugging').getAttribute('aria-checked')).toBe('true');
    expect(text(card(el, 'systematic-debugging').querySelector('[data-test="skill-card-off"]'))).toBe('Off for all workspaces');
    // Dimmed: no agent sees it.
    expect(card(el, 'systematic-debugging').querySelector('[data-test="skill-card-name"]').className).toContain('text-gray-400');
  });

  it('holds a switch while its answer is out, and reports a failure without flipping it', async () => {
    api.searchWorkspaceSkills.mockResolvedValueOnce({ skills: [{ name: 'tdd', description: 'Test first.', totalBytes: 10, sourceType: 'manual' }] });
    const el = await mount();
    const toggle = () => el.querySelector('[data-test="skill-card-enabled"]');
    expect(toggle().getAttribute('aria-checked')).toBe('false');

    let answer;
    api.setWorkspaceSkillEnabled.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    await click(toggle());
    expect(toggle().disabled).toBe(true);
    answer({ skill: { name: 'tdd', workspaceEnabled: true } });
    await settle();
    expect(toggle().disabled).toBe(false);
    expect(toggle().getAttribute('aria-checked')).toBe('true');

    api.setWorkspaceSkillEnabled.mockRejectedValueOnce(new Error('skill tdd not found'));
    await click(toggle());
    expect(toasts.value.at(-1).message).toBe('skill tdd not found');
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toggle().disabled).toBe(false);
  });

  it('reloads quietly after a browser agent changes something', async () => {
    const el = await mount();
    let answer;
    api.searchWorkspaceSkills.mockImplementationOnce(() => new Promise((r) => { answer = r; }));
    notifyWebMCPChange();
    await settle();
    // The old list stays up while the new one is fetched.
    expect(text(el)).not.toContain('Loading skills');
    expect(el.querySelectorAll('[data-test="skill-card"]')).toHaveLength(2);
    answer({ skills: [SKILLS[1]] });
    await settle();
    expect(el.querySelectorAll('[data-test="skill-card"]')).toHaveLength(1);
  });

  it('keeps the new workspace\'s list when the previous one\'s answers last', async () => {
    const answers = {};
    api.searchWorkspaceSkills.mockImplementation((id) => new Promise((r) => { answers[id] = r; }));
    const workspaceId = ref('ws1');
    const el = document.createElement('div');
    document.body.append(el);
    const app = createApp({ render: () => h(SkillsPanel, { workspaceId: workspaceId.value }) });
    app.use(createPinia());
    app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/:any(.*)*', component: { render: () => null } }] }));
    app.mount(el);
    apps.push(app);
    await settle();

    workspaceId.value = 'ws2';
    await settle();
    answers.ws2({ skills: [SKILLS[1]] });
    await settle();
    answers.ws1({ skills: SKILLS });
    await settle();

    const cards = el.querySelectorAll('[data-test="skill-card"]');
    expect(cards).toHaveLength(1);
    expect(cards[0].getAttribute('href')).toBe('/workspaces/ws2/settings/skills/review');
  });
});

describe('the Skills page\'s list', () => {
  it('reads the account\'s skills, links each to its own page and counts its workspaces', async () => {
    const el = await mount('');
    expect(api.searchSkills).toHaveBeenCalledWith();
    expect(api.searchWorkspaceSkills).not.toHaveBeenCalled();
    // The page's own header introduces it.
    expect(el.querySelector('[data-test="skills-page-link"]')).toBeNull();
    expect(card(el, 'review').getAttribute('href')).toBe('/skills/review');
    expect(text(card(el, 'systematic-debugging').querySelector('[data-test="skill-card-workspaces"]'))).toBe('· On in 2 workspaces');
    expect(text(card(el, 'review').querySelector('[data-test="skill-card-workspaces"]'))).toBe('· Not on in any workspace');
    expect(text(card(el, 'review').querySelector('[data-test="skill-card-off"]'))).toBe('Off');
  });

  it('turns a skill off and on for the whole account', async () => {
    const el = await mount('');
    const toggle = () => toggleOf(el, 'systematic-debugging');
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toggle().getAttribute('aria-label')).toBe('Available to agents: systematic-debugging');
    expect(toggle().title).toBe('On: agents can find and load it where it is on');

    await click(toggle());
    expect(api.setSkillEnabled).toHaveBeenLastCalledWith('systematic-debugging', false);
    expect(api.setWorkspaceSkillEnabled).not.toHaveBeenCalled();
    expect(toggle().getAttribute('aria-checked')).toBe('false');
    expect(toggle().title).toBe('Off: hidden from agents in every workspace');
    expect(text(card(el, 'systematic-debugging').querySelector('[data-test="skill-card-off"]'))).toBe('Off');
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is now hidden from agents');
    // The workspaces it is on in are untouched.
    expect(text(card(el, 'systematic-debugging'))).toContain('On in 2 workspaces');

    await click(toggle());
    expect(api.setSkillEnabled).toHaveBeenLastCalledWith('systematic-debugging', true);
    expect(toasts.value.at(-1).message).toBe('systematic-debugging is available to agents again');
  });

  it('imports into the workspaces ticked, none to start with', async () => {
    const el = await mount('');
    // Where to turn them on and overwriting are asked beside the import they
    // apply to, once there is one.
    expect(el.querySelector('[data-test="skill-import-workspaces"]')).toBeNull();
    expect(el.querySelector('[data-test="skill-import-overwrite"]')).toBeNull();
    await submitImport(el, 'https://github.com/obra/superpowers');
    expect(ticked(el)).toEqual([]);
    expect(text(el.querySelector('[data-test="skill-import-choice"]'))).toContain('Overwrite skills you already have');
    await click(el.querySelector('[data-test="skill-import-overwrite"]'));
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/obra/superpowers', true, undefined, [], false);

    el.querySelector('form').dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();
    workspaceBoxes(el)[2].querySelector('input').click();
    await settle();
    await click(el.querySelector('[data-test="skill-import-chosen"]'));
    expect(api.importSkills).toHaveBeenLastCalledWith('https://github.com/obra/superpowers', true, undefined, ['ws2'], false);
    expect(api.searchSkills).toHaveBeenCalledTimes(3);
  });

  it('says it could not load the account\'s skills', async () => {
    api.searchSkills.mockRejectedValueOnce(new Error('network unreachable'));
    const el = await mount('');
    expect(text(el)).toContain('Could not load your skills.');
  });
});
