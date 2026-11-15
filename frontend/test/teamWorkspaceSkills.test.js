// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Team workspace skills, mounted against a mocked API: the "Team skills"
 * group on a workspace's Skills tab, with its switches, a clash and "Use the
 * team's instead"; "Give to team" on your own skills; and the team's Skills
 * page, where any member creates, imports, edits and deletes the team
 * workspace's skills.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(() => Promise.resolve({ workspaces: [] })),
  searchSkills: vi.fn(),
  searchWorkspaceSkills: vi.fn(),
  importSkills: vi.fn(),
  setSkillEnabled: vi.fn(),
  setWorkspaceSkillEnabled: vi.fn(),
  listWorkspaceTeamSkills: vi.fn(),
  setWorkspaceTeamSkill: vi.fn(),
  giveSkillToTeam: vi.fn(),
  fetchTeamWorkspaces: vi.fn(),
  listTeamWorkspaceSkills: vi.fn(),
  createTeamWorkspaceSkill: vi.fn(),
  importTeamWorkspaceSkills: vi.fn(),
  getTeamWorkspaceSkill: vi.fn(),
  deleteTeamWorkspaceSkill: vi.fn(),
  getTeamWorkspaceSkillFile: vi.fn(),
  saveTeamWorkspaceSkillFile: vi.fn(),
  deleteTeamWorkspaceSkillFile: vi.fn(),
  // The teammates' listing under the team's page; drawn, not tested, here.
  fetchTeamSkills: vi.fn(() => Promise.resolve({ skills: [] })),
  getTeamSkill: vi.fn(),
  addTeamSkill: vi.fn(),
  getTeam: vi.fn(() => Promise.resolve({ id: 'tm1', name: 'Core', version: 1 })),
  listTeams: vi.fn(() => Promise.resolve({ teams: [] })),
}));
vi.mock('../src/api', () => api);

// The stream, by hand: each handler a component registers, to send events to.
const stream = vi.hoisted(() => ({ handlers: new Set(), connected: 0 }));
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({
    connect: () => { stream.connected += 1; },
    disconnect: () => {},
    onEvent: (fn) => { stream.handlers.add(fn); return () => stream.handlers.delete(fn); },
  }),
}));

const { default: SkillsPanel } = await import('../src/components/SkillsPanel.vue');
const { default: SkillsView } = await import('../src/views/SkillsView.vue');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

const settle = async () => {
  for (let i = 0; i < 3; i += 1) {
    await new Promise((r) => setTimeout(r, 0));
    await nextTick();
  }
};
const failure = (status, message) => Object.assign(new Error(message), { status });

let app;
let router;

async function mount(component, { path = '/', props = {} } = {}) {
  const el = document.createElement('div');
  document.body.append(el);
  router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/teams/:teamId/skills/:name', component: { render: () => null } },
    { path: '/:any(.*)*', component: { render: () => null } },
  ] });
  await router.push(path);
  await router.isReady();
  app = createApp({ render: () => h(component, props) });
  app.use(createPinia());
  app.use(router);
  app.directive('click-outside', {});
  app.mount(el);
  await settle();
  return el;
}

const $ = (sel) => document.body.querySelector(sel);
const $$ = (sel) => [...document.body.querySelectorAll(sel)];
// Every text node apart, as a reader sees adjacent elements.
const text = (node) => {
  if (!node) return undefined;
  const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
  const parts = [];
  while (walker.nextNode()) parts.push(walker.currentNode.nodeValue);
  return parts.join(' ').replace(/\s+/g, ' ').replace(/ \./g, '.').trim();
};
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  await settle();
};
const type = async (node, value) => {
  node.value = value;
  node.dispatchEvent(new Event('input'));
  node.dispatchEvent(new Event('change'));
  await settle();
};
const send = async (event) => {
  for (const fn of stream.handlers) fn(event);
  await settle();
};
const toastText = () => toasts.value.map((t) => t.message);

beforeEach(() => {
  vi.clearAllMocks();
  stream.handlers.clear();
  stream.connected = 0;
  toasts.value = [];
});
afterEach(() => {
  app?.unmount();
  document.body.innerHTML = '';
});

const MINE = [{ name: 'deploy', description: 'Ship it.', totalBytes: 10, enabled: true, workspaceEnabled: true, workspaceIds: ['ws1'] }];
const backend = { id: 'tw1', name: 'backend', teamId: 'tm1', teamName: 'Core' };
const frontend = { id: 'tw2', name: 'frontend', teamId: 'tm2', teamName: 'Web' };
const teamDeploy = { id: 's1', name: 'deploy', description: 'The team way.', teamId: 'tm1', teamWorkspaceId: 'tw1', teamWorkspaceName: 'backend', on: true, clash: true };
const teamReview = { id: 's2', name: 'review', description: 'Review code.', teamId: 'tm1', teamWorkspaceId: 'tw1', teamWorkspaceName: 'backend', on: false, clash: false };

describe("a workspace's Skills tab", () => {
  beforeEach(() => {
    api.searchWorkspaceSkills.mockResolvedValue({ skills: MINE });
  });

  it('shows no team skills and no "Give to team" when the workspace is in no team workspace', async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [] });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    expect(api.listWorkspaceTeamSkills).toHaveBeenCalledWith('ws1');
    expect($('[data-test=workspace-team-skills]')).toBe(null);
    expect($('[data-test=skill-card-give]')).toBe(null);
    expect($$('[data-test=skill-card]')).toHaveLength(1);
  });

  it('keeps your own skills on screen when the team skills cannot be read', async () => {
    api.listWorkspaceTeamSkills.mockRejectedValue(failure(500, 'down'));
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    expect($('[data-test=workspace-team-skills]')).toBe(null);
    expect($$('[data-test=skill-card]')).toHaveLength(1);
  });

  it('reads no team skills on the account\'s Skills page', async () => {
    api.searchSkills.mockResolvedValue({ skills: MINE });
    await mount(SkillsPanel);
    expect(api.listWorkspaceTeamSkills).not.toHaveBeenCalled();
    expect(stream.connected).toBe(0);
  });

  it('lists each team skill with its team workspace and switch, and flags a clash', async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [teamDeploy, teamReview], teamWorkspaces: [backend] });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    const items = $$('[data-test=team-skill-item]');
    expect(items.map((i) => text(i.querySelector('[data-test=team-skill-name]')))).toEqual(['deploy', 'review']);
    expect(text(items[0].querySelector('[data-test=team-skill-workspace]'))).toBe('backend');
    expect(text(items[0])).toContain('The team way.');
    expect(items.map((i) => i.querySelector('[data-test=team-skill-switch]').getAttribute('aria-checked'))).toEqual(['true', 'false']);
    expect(text(items[0].querySelector('[data-test=team-skill-clash]'))).toBe("Your own skill deploy is used instead. Use the team's instead");
    expect(items[1].querySelector('[data-test=team-skill-clash]')).toBe(null);
  });

  it('says so when the team workspace has no skills yet', async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [backend] });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    expect(text($('[data-test=team-skills-none]'))).toBe('No team skills yet.');
  });

  it('opts the workspace in, and out, with the switch', async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [teamReview], teamWorkspaces: [backend] });
    api.setWorkspaceTeamSkill.mockResolvedValue({ skill: { ...teamReview, on: true } });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    const toggle = () => $('[data-test=team-skill-switch]');
    await click(toggle());
    expect(api.setWorkspaceTeamSkill).toHaveBeenCalledWith('ws1', 's2', true);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toastText()).toContain('Turned review on in this workspace');

    api.setWorkspaceTeamSkill.mockRejectedValueOnce(failure(500, 'Could not'));
    await click(toggle());
    expect(api.setWorkspaceTeamSkill).toHaveBeenLastCalledWith('ws1', 's2', false);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    expect(toastText()).toContain('Could not');
  });

  it("uses the team's skill instead by turning your own off here, then reads both lists again", async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [teamDeploy], teamWorkspaces: [backend] });
    api.setWorkspaceSkillEnabled.mockResolvedValue({ skill: { name: 'deploy' } });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [{ ...teamDeploy, clash: false }], teamWorkspaces: [backend] });
    await click($('[data-test=team-skill-use-team]'));
    expect(api.setWorkspaceSkillEnabled).toHaveBeenCalledWith('ws1', 'deploy', false);
    expect(api.listWorkspaceTeamSkills).toHaveBeenCalledTimes(2);
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
    expect($('[data-test=team-skill-clash]')).toBe(null);
    expect(toastText()).toContain("This workspace now uses the team's deploy");
  });

  it('reads both lists again on a team skill event, and on no other', async () => {
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [backend] });
    await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
    expect(stream.connected).toBe(1);
    await send({ type: 'team.task.updated', teamId: 'tm1' });
    expect(api.listWorkspaceTeamSkills).toHaveBeenCalledTimes(1);
    api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [teamReview], teamWorkspaces: [backend] });
    await send({ type: 'team.skill.created', teamId: 'tm1', payload: { id: 's2' } });
    expect(api.listWorkspaceTeamSkills).toHaveBeenCalledTimes(2);
    expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
    expect($$('[data-test=team-skill-item]')).toHaveLength(1);
  });

  describe('giving one of your skills to the team', () => {
    it('confirms first, then moves it into the only team workspace and reads both lists again', async () => {
      api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [backend] });
      api.giveSkillToTeam.mockResolvedValue({ skill: { ...teamDeploy, clash: false } });
      await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
      await click($('[data-test=skill-card-give]'));
      expect(api.giveSkillToTeam).not.toHaveBeenCalled();
      expect(text($('[data-test=give-skill-modal]'))).toContain('deploy moves out of your skills and belongs to the team workspace backend in Core.');
      expect($('[data-test=give-skill-target]')).toBe(null);
      await click($('[data-test=give-skill-confirm]'));
      expect(api.giveSkillToTeam).toHaveBeenCalledWith('ws1', 'deploy', 'tw1');
      expect($('[data-test=give-skill-modal]')).toBe(null);
      expect(toastText()).toContain('Gave deploy to backend');
      expect(api.listWorkspaceTeamSkills).toHaveBeenCalledTimes(2);
      expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(2);
    });

    it('picks the team workspace when there are several, and gives nothing on cancel', async () => {
      api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [backend, frontend] });
      api.giveSkillToTeam.mockResolvedValue({});
      await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
      await click($('[data-test=skill-card-give]'));
      await click($('[data-test=give-skill-cancel]'));
      expect($('[data-test=give-skill-modal]')).toBe(null);
      expect(api.giveSkillToTeam).not.toHaveBeenCalled();

      await click($('[data-test=skill-card-give]'));
      const select = $('[data-test=give-skill-target]');
      expect([...select.options].map((o) => o.textContent.trim())).toEqual(['backend · Core', 'frontend · Web']);
      await type(select, 'tw2');
      await click($('[data-test=give-skill-confirm]'));
      expect(api.giveSkillToTeam).toHaveBeenCalledWith('ws1', 'deploy', 'tw2');
    });

    it.each([
      [409, 'backend already has a skill named deploy'],
      [422, 'backend has as many skills as it may hold'],
    ])('shows a %s in the server\'s words and stays open', async (status, message) => {
      api.listWorkspaceTeamSkills.mockResolvedValue({ skills: [], teamWorkspaces: [backend] });
      api.giveSkillToTeam.mockRejectedValue(failure(status, message));
      await mount(SkillsPanel, { props: { workspaceId: 'ws1' } });
      await click($('[data-test=skill-card-give]'));
      await click($('[data-test=give-skill-confirm]'));
      expect(text($('[data-test=give-skill-error]'))).toBe(message);
      expect($('[data-test=give-skill-modal]')).not.toBe(null);
      expect(api.searchWorkspaceSkills).toHaveBeenCalledTimes(1);
    });
  });
});

describe("the team's Skills page", () => {
  const rows = [
    { id: 'w1', name: 'api', sharedName: 'backend', teamWorkspaceId: 'tw1', owner: { userId: 'u1' } },
    { id: 'w2', name: 'api', sharedName: 'backend', teamWorkspaceId: 'tw1', owner: { userId: 'u2' } },
    { id: 'w3', name: 'web', sharedName: 'frontend', teamWorkspaceId: 'tw2', owner: { userId: 'u2' } },
  ];
  const deploy = { id: 's1', name: 'deploy', description: 'Ship it.', fileCount: 2, totalBytes: 2048 };
  const files = [
    { path: 'scripts/run.sh', sizeBytes: 20, version: 1 },
    { path: 'SKILL.md', sizeBytes: 100, version: 3 },
  ];

  beforeEach(() => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: rows });
    api.listTeamWorkspaceSkills.mockResolvedValue({ skills: [deploy] });
    api.getTeamWorkspaceSkill.mockResolvedValue({ skill: { ...deploy, files } });
    api.getTeamWorkspaceSkillFile.mockImplementation((_t, _tw, _s, path) => Promise.resolve({ file: { path, content: `content of ${path}`, version: path === 'SKILL.md' ? 3 : 1 } }));
  });

  it("puts the team workspace's skills first, and teammates' own skills below", async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills?workspace=backend' });
    const page = $('[data-test=team-workspace-skills]');
    const own = $('[data-test=teammates-own-skills]');
    expect(page).not.toBe(null);
    expect(text(own.querySelector('h2'))).toBe("Teammates' own skills");
    expect(page.compareDocumentPosition(own) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(api.fetchTeamSkills).toHaveBeenCalledWith('tm1', 'backend');
  });

  it("opens a teammate's own skill on its own, as before", async () => {
    api.getTeamSkill.mockResolvedValue({ skill: { name: 'deploy', content: 'x' } });
    api.searchSkills.mockResolvedValue({ skills: [] });
    await mount(SkillsView, { path: '/teams/tm1/skills/deploy?workspace=backend' });
    expect($('[data-test=team-workspace-skills]')).toBe(null);
    expect(api.getTeamSkill).toHaveBeenCalledWith('tm1', 'deploy', 'backend');
  });

  it('lists the skills of the team workspace the route names, one per team workspace to pick', async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills?workspace=Backend' });
    expect([...$('[data-test=tws-pick]').options].map((o) => o.textContent.trim())).toEqual(['backend', 'frontend']);
    expect($('[data-test=tws-pick]').value).toBe('tw1');
    expect(api.listTeamWorkspaceSkills).toHaveBeenCalledWith('tm1', 'tw1');
    expect(text($('[data-test=tws-row]'))).toBe('deploy Ship it. 2 files · 2.0 KB');

    await type($('[data-test=tws-pick]'), 'tw2');
    expect(router.currentRoute.value.fullPath).toBe('/teams/tm1/skills?workspace=frontend');
    expect(api.listTeamWorkspaceSkills).toHaveBeenLastCalledWith('tm1', 'tw2');
  });

  it('starts on the first team workspace, and names a lone one', async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    expect(api.listTeamWorkspaceSkills).toHaveBeenCalledWith('tm1', 'tw1');
    app.unmount();

    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [rows[2]] });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    expect(text($('[data-test=tws-name]'))).toBe('Team workspace · frontend');
  });

  it('says when there is no team workspace, or no skill in it', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [] });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    expect(text($('[data-test=tws-none]'))).toContain('No team workspaces yet.');
    app.unmount();

    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: rows });
    api.listTeamWorkspaceSkills.mockResolvedValue({ skills: [] });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    expect(text($('[data-test=tws-empty]'))).toContain('No team skills yet.');
  });

  it('creates a skill from a SKILL.md and opens it', async () => {
    api.createTeamWorkspaceSkill.mockResolvedValue({ skill: { id: 's9', name: 'lint' } });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-new]'));
    const md = '---\nname: lint\ndescription: Use when linting\n---\n\nRun it.';
    await type($('[data-test=tws-create-content]'), md);
    await click($('[data-test=tws-create-submit]'));
    expect(api.createTeamWorkspaceSkill).toHaveBeenCalledWith('tm1', 'tw1', md);
    expect(toastText()).toContain('Created lint in backend');
    expect(api.getTeamWorkspaceSkill).toHaveBeenCalledWith('tm1', 'tw1', 's9');
  });

  it("shows a refused create in the server's words", async () => {
    api.createTeamWorkspaceSkill.mockRejectedValue(failure(409, 'backend already has a skill named lint'));
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-new]'));
    await click($('[data-test=tws-create-submit]'));
    expect(text($('[data-test=tws-create-error]'))).toBe('backend already has a skill named lint');
  });

  it('imports from GitHub, choosing the skills of a repository too large to import whole', async () => {
    api.importTeamWorkspaceSkills
      .mockResolvedValueOnce({ sourceRepo: 'o/r', candidates: [{ name: 'a', path: 'skills/a', sizeBytes: 10 }, { name: 'b', path: 'skills/b', sizeBytes: 10, reason: 'too large' }] })
      .mockResolvedValueOnce({ imported: [{ id: 's5', name: 'a' }], skipped: [], sourceRepo: 'o/r', sourceCommit: '0123456789' });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await type($('[data-test=tws-import-url]'), 'https://github.com/o/r');
    await click($('[data-test=tws-import-overwrite]'));
    $('[data-test=tws-import]').closest('form').dispatchEvent(new Event('submit', { cancelable: true }));
    await settle();
    expect(api.importTeamWorkspaceSkills).toHaveBeenCalledWith('tm1', 'tw1', 'https://github.com/o/r', true, undefined);
    expect($$('[data-test=tws-candidate]')).toHaveLength(2);
    await click($('[data-test=tws-choice-all]'));
    await click($('[data-test=tws-import-chosen]'));
    expect(api.importTeamWorkspaceSkills).toHaveBeenLastCalledWith('tm1', 'tw1', 'https://github.com/o/r', true, ['skills/a']);
    expect(text($('[data-test=tws-import-report]'))).toBe('Imported 1 skill from o/r @0123456.');
  });

  it("opens a skill with its files, SKILL.md first and in the editor", async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    expect(api.getTeamWorkspaceSkill).toHaveBeenCalledWith('tm1', 'tw1', 's1');
    expect($$('[data-test=tws-file-open]').map(text)).toEqual(['SKILL.md', 'scripts/run.sh']);
    // SKILL.md cannot be deleted; another file can.
    expect($$('[data-test=tws-file]').map((f) => !!f.querySelector('[data-test=tws-file-delete]'))).toEqual([false, true]);
    expect(api.getTeamWorkspaceSkillFile).toHaveBeenCalledWith('tm1', 'tw1', 's1', 'SKILL.md');
    expect($('[data-test=tws-content]').value).toBe('content of SKILL.md');
    expect($('[data-test=tws-save]').disabled).toBe(true);

    await click($('[data-test=tws-back]'));
    expect($('[data-test=tws-skill]')).toBe(null);
    expect($$('[data-test=tws-row]')).toHaveLength(1);
  });

  it('saves a file with the version read', async () => {
    api.saveTeamWorkspaceSkillFile.mockResolvedValue({ skill: { ...deploy, files }, file: { path: 'SKILL.md', version: 4 } });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await type($('[data-test=tws-content]'), 'better');
    await click($('[data-test=tws-save]'));
    expect(api.saveTeamWorkspaceSkillFile).toHaveBeenCalledWith('tm1', 'tw1', 's1', 'SKILL.md', 'better', 3);
    expect(toastText()).toContain('Saved SKILL.md');

    await type($('[data-test=tws-content]'), 'better still');
    await click($('[data-test=tws-save]'));
    expect(api.saveTeamWorkspaceSkillFile).toHaveBeenLastCalledWith('tm1', 'tw1', 's1', 'SKILL.md', 'better still', 4);
  });

  it("shows a stale save's message, keeps the edit, and reloads the file on request", async () => {
    api.saveTeamWorkspaceSkillFile.mockRejectedValue(failure(409, 'SKILL.md was saved since you read it'));
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await type($('[data-test=tws-content]'), 'mine');
    await click($('[data-test=tws-save]'));
    expect(text($('[data-test=tws-save-error]'))).toContain('SKILL.md was saved since you read it');
    expect($('[data-test=tws-content]').value).toBe('mine');

    api.getTeamWorkspaceSkillFile.mockResolvedValueOnce({ file: { path: 'SKILL.md', content: 'theirs', version: 5 } });
    await click($('[data-test=tws-reload]'));
    expect($('[data-test=tws-content]').value).toBe('theirs');
    expect($('[data-test=tws-save-error]')).toBe(null);
  });

  it('offers no reload for a refusal that is not stale', async () => {
    api.saveTeamWorkspaceSkillFile.mockRejectedValue(failure(422, 'SKILL.md needs a name'));
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await type($('[data-test=tws-content]'), 'x');
    await click($('[data-test=tws-save]'));
    expect(text($('[data-test=tws-save-error]'))).toBe('SKILL.md needs a name');
    expect($('[data-test=tws-reload]')).toBe(null);
  });

  it('adds a file with no version, and deletes one once confirmed', async () => {
    api.saveTeamWorkspaceSkillFile.mockResolvedValue({ skill: { ...deploy, files: [...files, { path: 'notes.md', version: 1 }] }, file: { path: 'notes.md', version: 1 } });
    api.deleteTeamWorkspaceSkillFile.mockResolvedValue({ skill: { ...deploy, files: [files[1]] } });
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await click($('[data-test=tws-cancel]'));
    await click($('[data-test=tws-add-file]'));
    await type($('[data-test=tws-new-path]'), ' notes.md ');
    await type($('[data-test=tws-content]'), 'Notes');
    await click($('[data-test=tws-save]'));
    expect(api.saveTeamWorkspaceSkillFile).toHaveBeenCalledWith('tm1', 'tw1', 's1', 'notes.md', 'Notes', undefined);
    expect($$('[data-test=tws-file-open]').map(text)).toEqual(['SKILL.md', 'notes.md', 'scripts/run.sh']);

    const run = $$('[data-test=tws-file]').find((f) => text(f).startsWith('scripts/run.sh'));
    await click(run.querySelector('[data-test=tws-file-delete]'));
    expect(api.deleteTeamWorkspaceSkillFile).not.toHaveBeenCalled();
    const dialog = $('[role=dialog]');
    expect(text(dialog)).toContain('Deletes scripts/run.sh from deploy.');
    await click([...dialog.querySelectorAll('button')].find((b) => text(b) === 'Delete'));
    expect(api.deleteTeamWorkspaceSkillFile).toHaveBeenCalledWith('tm1', 'tw1', 's1', 'scripts/run.sh');
    expect($$('[data-test=tws-file-open]').map(text)).toEqual(['SKILL.md']);
  });

  it('deletes the skill for everyone once confirmed', async () => {
    api.deleteTeamWorkspaceSkill.mockResolvedValue(null);
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await click($('[data-test=tws-delete-skill]'));
    const dialog = $('[role=dialog]');
    expect(text(dialog)).toContain('Deletes deploy for every workspace in the team.');
    api.listTeamWorkspaceSkills.mockResolvedValue({ skills: [] });
    await click([...dialog.querySelectorAll('button')].find((b) => text(b) === 'Delete'));
    expect(api.deleteTeamWorkspaceSkill).toHaveBeenCalledWith('tm1', 'tw1', 's1');
    expect(toastText()).toContain('Deleted deploy for the team');
    expect($('[data-test=tws-skill]')).toBe(null);
    expect($('[data-test=tws-empty]')).not.toBe(null);
  });

  it("reads the list again on this team's skill events, and closes a skill deleted elsewhere", async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await send({ type: 'team.skill.created', teamId: 'tm2', payload: { id: 's7' } });
    expect(api.listTeamWorkspaceSkills).toHaveBeenCalledTimes(1);
    await send({ type: 'team.skill.created', teamId: 'tm1', payload: { id: 's7' } });
    expect(api.listTeamWorkspaceSkills).toHaveBeenCalledTimes(2);

    await click($('[data-test=tws-open]'));
    await send({ type: 'team.skill.deleted', teamId: 'tm1', payload: { id: 's1' } });
    expect($('[data-test=tws-skill]')).toBe(null);
    expect(toastText()).toContain('deploy was deleted');
  });

  it('keeps unsaved edits when a teammate changes the open skill', async () => {
    await mount(SkillsView, { path: '/teams/tm1/skills' });
    await click($('[data-test=tws-open]'));
    await type($('[data-test=tws-content]'), 'draft');
    await send({ type: 'team.skill.optedIn', teamId: 'tm1', payload: { id: 's1' } });
    expect(api.getTeamWorkspaceSkill).toHaveBeenCalledTimes(1);
    expect($('[data-test=tws-content]').value).toBe('draft');
  });
});
