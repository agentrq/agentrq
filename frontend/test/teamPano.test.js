// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Pano docs shared with a team: the owner's switch on a doc, and the team's
// Pano a teammate reads them on, read-only. Mounted for real, against a
// mocked API.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, defineComponent, h, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory, RouterView } from 'vue-router';

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  listTeams: vi.fn(),
  getTeam: vi.fn(),
  listPanoShares: vi.fn(),
  sharePanoDoc: vi.fn(),
  unsharePanoDoc: vi.fn(),
  listTeamPanoDocs: vi.fn(),
  getTeamPanoDoc: vi.fn(),
}));

import * as api from '../src/api';
import { useTeamStore } from '../src/stores/teamStore';
import { useToasts } from '../src/composables/useToasts';
import { notifyWebMCPChange } from '../src/composables/useWebMCPChanges';
import PanoSharing from '../src/components/PanoSharing.vue';
import TeamPanoView from '../src/views/TeamPanoView.vue';
import TeamPanoDocView from '../src/views/TeamPanoDocView.vue';

const flush = async () => {
  for (let i = 0; i < 3; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
};

const failure = (status, message = 'nope') => Object.assign(new Error(message), { status });
const conflict = () => Object.assign(new Error('someone changed this team'), { status: 409, conflict: true, version: 9 });

let app;
let router;

async function mount(component, { path = '/', teams = [], props = {} } = {}) {
  const el = document.createElement('div');
  document.body.appendChild(el);
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/team/pano/:docId', component },
      { path: '/:rest(.*)*', component },
    ],
  });
  await router.push(path);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useTeamStore().teams = teams.map((t) => ({ ...t }));
  const Host = defineComponent({ render: () => h(RouterView, null, { default: ({ Component }) => Component && h(Component, props) }) });
  app = createApp(Host).use(pinia).use(router);
  app.mount(el);
  await flush();
  return el;
}

const $ = (sel) => document.body.querySelector(sel);
const $$ = (sel) => [...document.body.querySelectorAll(sel)];
const text = (node) => node?.textContent.replace(/\s+/g, ' ').trim();
const click = async (node) => {
  node.click();
  await flush();
};
const here = () => router.currentRoute.value;
const toasts = () => useToasts().toasts.value.map((t) => t.message);

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  useToasts().toasts.value = [];
  api.listTeams.mockResolvedValue({ teams: [] });
});

afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = '';
});

describe('sharing a Pano doc', () => {
  const core = { id: 'tm1', name: 'Core', version: 3 };
  const docs = { id: 'tm2', name: 'Docs', version: 1 };

  const share = (shares, teams = [core, docs]) => {
    api.listPanoShares.mockResolvedValue(shares);
    return mount(PanoSharing, { teams, props: { docId: 'd1', title: 'Plan' } });
  };
  const row = (label) => $$('[data-test=pano-share-row]').find((r) => text(r).startsWith(label));
  const toggle = (label) => row(label).querySelector('[data-test=pano-share-switch]');

  it('lists your teams, the ones it is shared with on', async () => {
    await share({ shares: [{ teamId: 'tm1' }] });
    expect(api.listPanoShares).toHaveBeenCalledWith('d1');
    expect($$('[data-test=pano-share-row]').length).toBe(2);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
    expect(toggle('Docs').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Core').getAttribute('aria-label')).toBe('Share Plan with Core');
  });

  it('reads an answer with no shares as none, and names a team it does not know by nothing', async () => {
    await share(null, [{ id: 'tm3' }]);
    const only = $('[data-test=pano-share-switch]');
    expect(only.getAttribute('aria-checked')).toBe('false');
    expect(only.getAttribute('aria-label')).toBe('Share Plan with ');
  });

  it('shares against the team version, and moves to the one the answer names', async () => {
    api.sharePanoDoc.mockResolvedValue({ teamId: 'tm1', version: 4 });
    await share({ shares: [] });
    await click(toggle('Core'));
    expect(api.sharePanoDoc).toHaveBeenCalledWith('d1', 'tm1', 3);
    expect(useTeamStore().getTeam('tm1').version).toBe(4);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
    expect(toasts()).toContain('Shared Plan with Core');
  });

  it('says why a share failed, and stays off', async () => {
    api.sharePanoDoc.mockRejectedValue(failure(500, 'Share failed'));
    await share({ shares: [] });
    await click(toggle('Core'));
    expect(toasts()).toEqual(['Share failed']);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Core').disabled).toBe(false);
  });

  it('reads the team and the shares again when someone changed the team first', async () => {
    api.sharePanoDoc.mockRejectedValue(conflict());
    api.getTeam.mockResolvedValue({ ...core, version: 9 });
    await share({ shares: [] });
    api.listPanoShares.mockResolvedValue({ shares: [{ teamId: 'tm1' }] });
    await click(toggle('Core'));
    expect(toasts()).toEqual(['Someone changed this team, refreshed']);
    expect(useTeamStore().getTeam('tm1').version).toBe(9);
    expect(api.listPanoShares).toHaveBeenCalledTimes(2);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
  });

  it('asks before it stops sharing, and stops at once on yes', async () => {
    api.unsharePanoDoc.mockResolvedValue({ version: 5 });
    await share({ shares: [{ teamId: 'tm1' }] });
    await click(toggle('Core'));
    expect(document.body.textContent).toContain('Teammates in Core will stop seeing Plan immediately.');
    await click($$('button').find((b) => text(b) === 'Cancel'));
    expect(api.unsharePanoDoc).not.toHaveBeenCalled();

    await click(toggle('Core'));
    await click($$('button').filter((b) => text(b) === 'Stop sharing').pop());
    expect(api.unsharePanoDoc).toHaveBeenCalledWith('d1', 'tm1', 3);
    expect(useTeamStore().getTeam('tm1').version).toBe(5);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('false');
    expect(toasts()).toContain('Stopped sharing Plan with Core');
  });

  it('says so when the shares cannot be read', async () => {
    api.listPanoShares.mockRejectedValue(failure(500, 'Could not read'));
    await mount(PanoSharing, { teams: [core], props: { docId: 'd1', title: 'Plan' } });
    expect(toasts()).toEqual(['Could not read']);
    expect(toggle('Core').disabled).toBe(false);
  });
});

describe('the team Pano', () => {
  const alice = { ownerId: 'u1', ownerName: 'Alice', ownerPicture: 'https://x/a.png' };
  const now = new Date().toISOString();
  const DOCS = [
    { id: 'd1', title: 'Plan', excerpt: '**ship** it', color: 'pink', updatedAt: now, checklistDone: 1, checklistTotal: 2, ...alice },
    { id: 'd2', title: 'Notes', excerpt: '- [ ] [step](/workspaces/w1/tasks/t1 "ongoing")', color: 'blue', updatedAt: now, ownerId: 'u2' },
  ];

  it('lists the docs members shared, each with its owner, opening one in the team', async () => {
    api.listTeamPanoDocs.mockResolvedValue({ docs: DOCS });
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    expect(api.listTeamPanoDocs).toHaveBeenCalledWith('tm1');
    const cards = $$('[data-test=team-pano-card]');
    expect(cards.map((c) => c.getAttribute('href'))).toEqual(['/team/pano/d1?team=tm1', '/team/pano/d2?team=tm1']);
    expect(cards[0].querySelector('[data-test=team-pano-excerpt]').innerHTML).toContain('<strong>ship</strong>');
    expect(text(cards[0])).toContain('Alice · just now');
    expect(cards[0].querySelector('[data-test=owner-avatar] img').getAttribute('src')).toBe('https://x/a.png');
    expect(cards[0].querySelector('[data-test=pano-progress]')).not.toBe(null);
    // An owner with no name yet, and an assigned item whose task is the owner's: no link to it.
    expect(text(cards[1])).toContain('A teammate · just now');
    expect(cards[1].querySelector('a.pano-assignee')).toBe(null);
    // Read-only: no new note, no edit, color or delete.
    expect($('[data-test=pano-new-card]')).toBe(null);
    expect($('[data-test=pano-card-edit]')).toBe(null);
  });

  it('says when nothing is shared, and reads an empty answer as none', async () => {
    api.listTeamPanoDocs.mockResolvedValue({});
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    expect(text($('[data-test=team-pano-empty]'))).toContain('Nothing shared yet.');
  });

  it('reads the other team when the switch picks one, and none in Me', async () => {
    api.listTeamPanoDocs.mockResolvedValue({ docs: [] });
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    await router.push('/team/pano?team=tm2');
    await flush();
    expect(api.listTeamPanoDocs).toHaveBeenLastCalledWith('tm2');
    await router.push('/team/pano');
    await flush();
    expect(api.listTeamPanoDocs).toHaveBeenCalledTimes(2);
  });

  it('reads again quietly on a browser agent change', async () => {
    api.listTeamPanoDocs.mockResolvedValue({ docs: [] });
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    api.listTeamPanoDocs.mockResolvedValue({ docs: DOCS });
    notifyWebMCPChange();
    await flush();
    expect($$('[data-test=team-pano-card]').length).toBe(2);
  });

  it('goes back to Me without the team, and says other failures', async () => {
    api.listTeamPanoDocs.mockRejectedValueOnce(failure(500, 'Pano down'));
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    expect(toasts()).toContain('Pano down');
    app.unmount();
    api.listTeamPanoDocs.mockRejectedValueOnce(failure(404));
    await mount(TeamPanoView, { path: '/team/pano?team=tm1' });
    expect(here().path).toBe('/');
  });
});

describe('a doc shared with the team', () => {
  const DOC = {
    id: 'd1',
    title: 'Plan',
    markdown: '# Goal\n\n- [ ] [step](/workspaces/w1/tasks/t1 "ongoing")\n\n![chart](chart.png)',
    color: 'green',
    updatedAt: '2026-10-13T12:00:00Z',
    expiresAt: new Date(Date.now() + 2 * 24 * 3600 * 1000 + 60000).toISOString(),
    checklistDone: 0,
    checklistTotal: 1,
    ownerId: 'u1',
    ownerName: 'Alice',
    files: [{ id: 'f1', filename: 'chart.png', url: '/api/v1/teams/tm1/pano/d1/files/f1', size: 2048 }],
    attachments: [{ id: 'a1', filename: 'log.txt', url: '/api/v1/teams/tm1/pano/d1/files/a1', size: 10 }],
  };

  it('shows the note, whose it is, and its files by their team links, read-only', async () => {
    api.getTeamPanoDoc.mockResolvedValue({ doc: DOC });
    await mount(TeamPanoDocView, { path: '/team/pano/d1?team=tm1' });
    expect(api.getTeamPanoDoc).toHaveBeenCalledWith('tm1', 'd1');
    expect(text($('[data-test=team-pano-title]'))).toBe('Plan');
    expect(text($('[data-test=team-pano-meta]'))).toMatch(/^AAlice · updated \w{3} \d+, \d\d:\d\d$/);
    const body = $('[data-test=team-pano-body]');
    expect(body.querySelector('h1').textContent).toBe('Goal');
    expect(body.querySelector('img').getAttribute('src')).toBe('/api/v1/teams/tm1/pano/d1/files/f1');
    expect(body.querySelector('span.pano-assignee')).not.toBe(null);
    expect(body.querySelector('a.pano-assignee')).toBe(null);
    expect(text($('[data-test=team-pano-files]'))).toBe('Linked fileschart.png2.0 KB');
    expect($('[data-test=team-pano-attachments] a').getAttribute('href')).toBe('/api/v1/teams/tm1/pano/d1/files/a1');
    expect(document.body.textContent).toContain('Expires in 2 days');
    expect($('[data-test=team-pano-back]').getAttribute('href')).toBe('/team/pano?team=tm1');
    // No editing, comments or tasks: those stay the owner's.
    for (const test of ['pano-edit', 'pano-delete', 'pano-comment-input', 'pano-attach', 'pano-to-task', 'pano-chat']) {
      expect($(`[data-test=${test}]`), test).toBe(null);
    }
  });

  it('shows a doc with no files, by a teammate with no name yet', async () => {
    api.getTeamPanoDoc.mockResolvedValue({ doc: { id: 'd1', title: 'Bare', markdown: 'text', color: 'pink', updatedAt: DOC.updatedAt, expiresAt: DOC.expiresAt } });
    await mount(TeamPanoDocView, { path: '/team/pano/d1?team=tm1' });
    expect(text($('[data-test=team-pano-meta]'))).toContain('A teammate');
    expect($('[data-test=team-pano-files]')).toBe(null);
    expect($('[data-test=team-pano-attachments]')).toBe(null);
  });

  it('reads it again on a browser agent change', async () => {
    api.getTeamPanoDoc.mockResolvedValue({ doc: DOC });
    await mount(TeamPanoDocView, { path: '/team/pano/d1?team=tm1' });
    api.getTeamPanoDoc.mockResolvedValue({ doc: { ...DOC, title: 'Plan v2' } });
    notifyWebMCPChange();
    await flush();
    expect(text($('[data-test=team-pano-title]'))).toBe('Plan v2');
  });

  it('goes back to the team Pano once it is no longer shared', async () => {
    api.getTeamPanoDoc.mockRejectedValue(failure(404));
    await mount(TeamPanoDocView, { path: '/team/pano/d1?team=tm1' });
    expect(toasts()).toContain('That doc is no longer shared with the team');
    expect(here().fullPath).toBe('/team/pano?team=tm1');
  });

  it('says any other failure where the note would be', async () => {
    api.getTeamPanoDoc.mockRejectedValue(failure(500, 'Pano down'));
    await mount(TeamPanoDocView, { path: '/team/pano/d1?team=tm1' });
    expect(text($('[data-test=team-pano-error]'))).toBe('Pano down');
  });
});
