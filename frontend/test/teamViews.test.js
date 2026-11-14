// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The team pages a person manages a team from: creating one, the Manage team
 * dialog (members, the invite form and its daily cap, invitations, the
 * activity log, leave, delete), the invitations inbox, and the invitation
 * link's accept page.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory, RouterView } from 'vue-router';

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchUser: vi.fn(),
  fetchWorkspaces: vi.fn(),
  listTeams: vi.fn(),
  listMyInvitations: vi.fn(),
  fetchTeamWorkspaces: vi.fn(),
  createTeam: vi.fn(),
  getTeam: vi.fn(),
  renameTeam: vi.fn(),
  deleteTeam: vi.fn(),
  leaveTeam: vi.fn(),
  listTeamMembers: vi.fn(),
  removeTeamMember: vi.fn(),
  listTeamInvitations: vi.fn(),
  createTeamInvitation: vi.fn(),
  revokeTeamInvitation: vi.fn(),
  fetchTeamActivities: vi.fn(),
  previewInvitation: vi.fn(),
  acceptInvitation: vi.fn(),
  acceptInvitationById: vi.fn(),
  declineInvitation: vi.fn(),
}));

import * as api from '../src/api';
import { useTeamStore, rememberedTeam, rememberTeam } from '../src/stores/teamStore';
import { useToasts } from '../src/composables/useToasts';
import TeamNewView from '../src/views/TeamNewView.vue';
import TeamManageDialog from '../src/components/TeamManageDialog.vue';
import InvitationsView from '../src/views/InvitationsView.vue';
import InvitationAcceptView from '../src/views/InvitationAcceptView.vue';
import InviteLinkModal from '../src/components/InviteLinkModal.vue';

const Blank = { render: () => h('div', { 'data-test': 'blank' }) };
// A team-mode page, with the dialog App.vue puts over it.
const TeamPage = { render: () => h('div', [h(TeamManageDialog)]) };

const settle = () => new Promise((r) => setTimeout(r, 0));
async function flush() {
  for (let i = 0; i < 10; i++) await settle();
  await nextTick();
}

const fail = (status, message = `failed ${status}`) => Object.assign(new Error(message), { status });

let app;
let el;
let router;
let store;

async function mount(path, setup = () => {}) {
  const pinia = createPinia();
  setActivePinia(pinia);
  store = useTeamStore();
  setup(store);
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/login', component: Blank },
      { path: '/tasks/active', component: TeamPage },
      { path: '/kanban', component: TeamPage },
      { path: '/team/new', component: TeamNewView },
      { path: '/invitations', component: InvitationsView },
      { path: '/invite', component: InvitationAcceptView },
    ],
  });
  await router.push(path);
  await router.isReady();
  el = document.createElement('div');
  document.body.append(el);
  app = createApp({ render: () => h(RouterView) });
  app.use(pinia);
  app.use(router);
  app.mount(el);
  await flush();
  return el;
}

const $ = (sel) => el.querySelector(sel);
const $$ = (sel) => [...el.querySelectorAll(sel)];
// Each text node apart, as a reader sees them, not run together.
function text(node) {
  if (!node) return undefined;
  const parts = [];
  const walk = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
  while (walk.nextNode()) parts.push(walk.currentNode.nodeValue);
  return parts.join(' ').replace(/\s+/g, ' ').replace(/ ([.,)])/g, '$1').trim();
}
const button = (label, root = el) => [...root.querySelectorAll('button')].find((b) => text(b) === label);
const toasts = () => useToasts().toasts.value.map((t) => t.message);

async function type(input, value) {
  input.value = value;
  input.dispatchEvent(new Event('input'));
  await nextTick();
}
async function choose(select, value) {
  select.value = value;
  select.dispatchEvent(new Event('change'));
  await flush();
}
async function click(node) {
  node.click();
  await flush();
}
async function submit(form) {
  form.dispatchEvent(new Event('submit', { cancelable: true }));
  await flush();
}

// The dialog DeleteModal draws, by its title.
const dialog = (title) => [...document.querySelectorAll('[role=dialog]')].find((d) => text(d.querySelector('#modal-title')) === title);

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.clearAllMocks();
  useToasts().toasts.value = [];
  api.listTeams.mockResolvedValue({ teams: [] });
  api.listMyInvitations.mockResolvedValue({ invitations: [] });
  api.fetchWorkspaces.mockResolvedValue({ workspaces: [] });
  api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [] });
});

afterEach(() => {
  app?.unmount();
  app = null;
  el?.remove();
  document.body.innerHTML = '';
});

describe('creating a team', () => {
  it('starts with "your first team" and no invitations', async () => {
    await mount('/team/new');
    expect(text($('h1'))).toBe('Teams');
    expect(text($('h2'))).toBe('Create your first team');
    expect(text($('[data-test=invitation-hint]'))).toBe('You have no invitations.');
    expect(document.activeElement).toBe($('#team-name'));
  });

  it('counts the invitations waiting, once or more', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: [{ id: 'i1' }] });
    await mount('/team/new');
    await store.load();
    await flush();
    expect(text($('[data-test=invitation-hint]'))).toBe('You have 1 invitation');
    store.invitations = [{ id: 'i1' }, { id: 'i2' }];
    await nextTick();
    expect(text($('[data-test=invitation-hint]'))).toBe('You have 2 invitations');
  });

  it('puts you in it and opens Manage team on its invitations', async () => {
    api.fetchUser.mockResolvedValue({ id: 'u1', name: 'Me' });
    api.listTeamMembers.mockResolvedValue({ members: [], version: 1 });
    api.listTeamInvitations.mockResolvedValue({ invitations: [], version: 1 });
    api.createTeam.mockResolvedValue({ id: 'tm1', name: 'Core', version: 1, memberCount: 1 });
    await mount('/team/new');
    store.upsert({ id: 'tm0', name: 'Alpha', version: 1 });
    await nextTick();
    expect(text($('h1'))).toBe('Create a team');
    expect($('h2')).toBe(null);
    await type($('#team-name'), '  Core ');
    await submit($('form'));
    expect(api.createTeam).toHaveBeenCalledWith('Core');
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', version: 1, memberCount: 1 });
    expect(rememberedTeam()).toBe('tm1');
    expect(router.currentRoute.value.fullPath).toBe('/tasks/active?team=tm1&manage=invitations');
    expect(document.querySelector('[data-test=team-manage]')).not.toBe(null);
  });

  it('says why it could not', async () => {
    api.createTeam.mockRejectedValue(new Error('A team needs a name'));
    await mount('/team/new');
    await type($('#team-name'), 'Core');
    await submit($('form'));
    expect(text($('[data-test=create-error]'))).toBe('A team needs a name');
    expect(router.currentRoute.value.path).toBe('/team/new');
  });
});

const ME = { id: 'u1', name: 'Me' };
const members = () => [
  { userId: 'u1', name: 'Me', email: 'me@acme.com' },
  { userId: 'u2', name: 'Bob', email: 'bob@acme.com' },
  { userId: 'u3', name: '', email: 'cy@acme.com' },
];
const soon = () => new Date(Date.now() + 3 * 86400000 + 60000).toISOString();
const invitations = () => [
  { id: 'i1', email: 'dee@acme.com', invitedBy: 'u1', expiresAt: soon() },
  { id: 'i2', email: 'eve@acme.com', invitedBy: '', expiresAt: soon() },
];
const activity = (n, from = 0) => Array.from({ length: n }, (_, k) => ({
  id: `a${from + k}`, action: 'member.left', actor: { userId: `u${from + k}`, name: `P${from + k}` }, createdAt: new Date().toISOString(),
}));

// The dialog, over a page of the team, with the team's version in the store as
// the app's own load leaves it.
function asTeam({ version = 3, name = 'Core' } = {}) {
  api.fetchUser.mockResolvedValue(ME);
  api.listTeamMembers.mockResolvedValue({ members: members(), version });
  api.listTeamInvitations.mockResolvedValue({ invitations: invitations(), version });
  api.fetchTeamActivities.mockResolvedValue({ activities: [], version });
  return (s) => { s.teams = [{ id: 'tm1', name, version, memberCount: 3 }]; };
}

// Teleported to the body, as every dialog is.
const d$ = (sel) => document.body.querySelector(sel);
const d$$ = (sel) => [...document.body.querySelectorAll(sel)];
const here = () => router.currentRoute.value.fullPath;

describe('the Manage team dialog', () => {
  it('is shut without ?manage=, and in Me mode', async () => {
    await mount('/tasks/active?team=tm1', asTeam());
    expect(d$('[data-test=team-manage]')).toBe(null);
    expect(api.listTeamMembers).not.toHaveBeenCalled();
    await router.push('/tasks/active?manage=members');
    await flush();
    expect(d$('[data-test=team-manage]')).toBe(null);
    await router.push('/tasks/active?team=tm1&manage=settings');
    await flush();
    expect(d$('[data-test=team-manage]')).toBe(null);
  });

  it('opens over the page on screen, on members, every one of whom can be removed but you, and no roles', async () => {
    await mount('/kanban?team=tm1&manage=members', asTeam());
    const dialog = d$('[data-test=team-manage]');
    expect(dialog.getAttribute('role')).toBe('dialog');
    expect(text(dialog.querySelector('h2'))).toBe('Core');
    expect(text(dialog.querySelector('header p'))).toBe('Manage team · 3 members');
    expect(d$$('[role=tab]').map(text)).toEqual(['Members', 'Invitations 2', 'Activity', 'Your workspaces']);
    expect(d$('[data-test=manage-tab-members]').getAttribute('aria-selected')).toBe('true');
    expect(d$$('[data-test=member-row]').map(text)).toEqual([
      'M Me (you) me@acme.com',
      'B Bob bob@acme.com Remove',
      'C cy@acme.com Remove',
    ]);
    expect(text(dialog)).not.toMatch(/owner|admin|role/i);
    expect(d$('[data-test=leave]')).not.toBe(null);
    expect(d$('[data-test=delete]')).not.toBe(null);
    expect(store.getTeam('tm1').version).toBe(3);
  });

  it('moves between tabs in the URL, and shuts on its close, its backdrop, or Escape', async () => {
    await mount('/kanban?team=tm1&manage=members', asTeam());
    await click(d$('[data-test=manage-tab-invitations]'));
    expect(here()).toBe('/kanban?team=tm1&manage=invitations');
    expect(document.activeElement).toBe(d$('#invite-email'));
    await click(d$('[data-test=manage-tab-workspaces]'));
    expect(text(d$('[data-test=team-manage]'))).toContain('Teammates in Core see the workspaces you share here');
    expect(d$('[data-test=workspace-sharing]')).not.toBe(null);

    await click(d$('[data-test=team-manage-close]'));
    expect(here()).toBe('/kanban?team=tm1');
    expect(d$('[data-test=team-manage]')).toBe(null);

    await router.push('/kanban?team=tm1&manage=members');
    await flush();
    await click(d$('[data-test=team-manage] > div'));
    expect(here()).toBe('/kanban?team=tm1');

    await router.push('/kanban?team=tm1&manage=members');
    await flush();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flush();
    expect(here()).toBe('/kanban?team=tm1');
    // Shut, Escape is someone else's.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flush();
    expect(here()).toBe('/kanban?team=tm1');
  });

  it('renames the team from its title', async () => {
    api.renameTeam.mockResolvedValue({ id: 'tm1', name: 'Platform', version: 4, memberCount: 3 });
    await mount('/tasks/active?team=tm1&manage=members', asTeam());
    await click(d$('[data-test=team-manage] [data-test=team-rename]'));
    await type(d$('[data-test=team-rename-input]'), 'Platform');
    await submit(d$('[data-test=team-rename-form]'));
    expect(api.renameTeam).toHaveBeenCalledWith('tm1', 'Platform', 3);
    expect(text(d$('[data-test=team-manage] h2'))).toBe('Platform');
  });

  it('reads lists with no members or invitations as empty, and one member as one', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.listTeamMembers.mockResolvedValue({ members: [members()[0]] });
    api.listTeamInvitations.mockResolvedValue({});
    await mount('/tasks/active?team=tm1&manage=invitations', (s) => { s.teams = [{ id: 'tm1', name: 'Core', version: 2 }]; });
    expect(text(d$('[data-test=team-manage] header p'))).toBe('Manage team · 1 member');
    expect(text(d$('[data-test=invitations-empty]'))).toBe('No invitation waiting for an answer.');
    expect(store.getTeam('tm1').version).toBe(2);
  });

  it('sends you back to Me when the team is gone, and says any other failure to load', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.listTeamMembers.mockRejectedValue(fail(404));
    api.listTeamInvitations.mockResolvedValue({});
    await mount('/tasks/active?team=tm1&manage=members');
    expect(here()).toBe('/');
    expect(toasts()).toContain("You're no longer in that team");
    app.unmount();

    api.listTeamMembers.mockRejectedValue(fail(500, 'Server down'));
    await mount('/tasks/active?team=tm1&manage=members');
    expect(here()).toBe('/tasks/active?team=tm1&manage=members');
    expect(toasts()).toContain('Server down');
  });

  describe('members', () => {
    it('removes one, after asking, against the team\'s version', async () => {
      api.removeTeamMember.mockResolvedValue({ version: 4 });
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$$('[data-test=member-remove]')[0]);
      const modal = dialog('Remove member');
      expect(text(modal)).toContain('Remove Bob from Core? Their shared workspaces leave Core too.');
      await click(button('Remove', modal));
      expect(api.removeTeamMember).toHaveBeenCalledWith('tm1', 'u2', 3);
      expect(store.getTeam('tm1').version).toBe(4);
      expect(d$$('[data-test=member-row]')).toHaveLength(2);
      expect(toasts()).toEqual(['Bob removed']);
    });

    it('names one with no name by email, and says when removing fails', async () => {
      api.removeTeamMember.mockRejectedValueOnce(fail(500, 'Nope')).mockResolvedValueOnce(null);
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$$('[data-test=member-remove]')[1]);
      expect(text(dialog('Remove member'))).toContain('Remove cy@acme.com from Core?');
      await click(button('Remove', dialog('Remove member')));
      expect(d$$('[data-test=member-row]')).toHaveLength(3);
      expect(toasts()).toEqual(['Nope']);

      await click(d$$('[data-test=member-remove]')[1]);
      await click(button('Remove', dialog('Remove member')));
      expect(toasts()).toEqual(['Nope', 'cy@acme.com removed']);
      expect(store.getTeam('tm1').version).toBe(3);
    });

    it('cancels a removal', async () => {
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$$('[data-test=member-remove]')[0]);
      await click(button('Cancel', dialog('Remove member')));
      expect(api.removeTeamMember).not.toHaveBeenCalled();
      // Escape belongs to the open confirmation, not the dialog under it.
      await click(d$$('[data-test=member-remove]')[0]);
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      await flush();
      expect(d$('[data-test=team-manage]')).not.toBe(null);
    });

    it('reads the team and its lists again when someone changed it first, and says so', async () => {
      api.removeTeamMember.mockRejectedValue(Object.assign(fail(409, 'someone changed this team'), { conflict: true, version: 7 }));
      api.getTeam.mockResolvedValue({ id: 'tm1', name: 'Core', version: 7, memberCount: 2 });
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      api.listTeamMembers.mockResolvedValue({ members: members().slice(0, 2), version: 7 });
      api.listTeamInvitations.mockResolvedValue({ invitations: invitations(), version: 7 });
      await click(d$$('[data-test=member-remove]')[1]);
      await click(button('Remove', dialog('Remove member')));
      expect(api.getTeam).toHaveBeenCalledWith('tm1');
      expect(toasts()).toEqual(['Someone changed this team, refreshed']);
      expect(d$$('[data-test=member-row]')).toHaveLength(2);
      expect(store.getTeam('tm1').version).toBe(7);
    });
  });

  describe('the invite form', () => {
    it('sends an invitation against the version, says so, and lists it in place of any older one', async () => {
      api.createTeamInvitation.mockResolvedValue({
        invitation: { id: 'i9', email: 'dee@acme.com', invitedBy: 'u1', expiresAt: soon() },
        emailSent: true,
        version: 4,
      });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      expect(d$('[data-test=invite-role]')).toBe(null);
      await type(d$('#invite-email'), ' dee@acme.com ');
      await submit(d$('[data-test=invite-form]'));
      expect(api.createTeamInvitation).toHaveBeenCalledWith('tm1', 'dee@acme.com', 3);
      expect(store.getTeam('tm1').version).toBe(4);
      expect(toasts()).toEqual(['Invitation sent']);
      expect(d$$('[data-test=invitation-row]').map((r) => r.querySelector('p').textContent)).toEqual(['dee@acme.com', 'eve@acme.com']);
      expect(d$('#invite-email').value).toBe('');
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
    });

    it('lists who sent each and when it expires', async () => {
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      expect(d$$('[data-test=invitation-row]').map(text)).toEqual([
        'dee@acme.com Sent by Me · expires in 3 days Resend Revoke',
        'eve@acme.com expires in 3 days Resend Revoke',
      ]);
    });

    it('hands over the link when no email carried it, and Escape shuts the link first', async () => {
      api.createTeamInvitation.mockResolvedValue({ emailSent: false, link: 'https://agentrq.test/invite#t1', version: 4 });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await type(d$('#invite-email'), 'new@acme.com');
      await submit(d$('[data-test=invite-form]'));
      const modal = document.querySelector('[data-test=invite-link-modal]');
      expect(modal.querySelector('[data-test=invite-link]').value).toBe('https://agentrq.test/invite#t1');
      expect(text(modal)).toContain('Send this to new@acme.com. It works once, for that email, for 7 days.');
      expect(d$$('[data-test=invitation-row]')).toHaveLength(2);
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      await flush();
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
      expect(d$('[data-test=team-manage]')).not.toBe(null);
    });

    it('closes the link with Done', async () => {
      api.createTeamInvitation.mockResolvedValue({ emailSent: false, link: 'https://agentrq.test/invite#t2' });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await type(d$('#invite-email'), 'new@acme.com');
      await submit(d$('[data-test=invite-form]'));
      const modal = document.querySelector('[data-test=invite-link-modal]');
      await click(button('Done', modal));
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
    });

    it('takes an answer with nothing in it', async () => {
      api.createTeamInvitation.mockResolvedValue(undefined);
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await type(d$('#invite-email'), 'new@acme.com');
      await submit(d$('[data-test=invite-form]'));
      expect(toasts()).toEqual(['Invitation sent']);
      expect(store.getTeam('tm1').version).toBe(3);
    });

    it.each([
      ['the team\'s', 'this team has reached its limit of 10 invitations per day'],
      ['the inviter\'s', 'you have reached your limit of 20 invitations per day across all your teams'],
    ])('shows %s daily cap in the server\'s words, in the form, and keeps what was typed', async (_, message) => {
      api.createTeamInvitation.mockRejectedValue(fail(429, message));
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await type(d$('#invite-email'), 'new@acme.com');
      await submit(d$('[data-test=invite-form]'));
      const err = d$('[data-test=invite-error]');
      expect(err.getAttribute('role')).toBe('alert');
      expect(text(err)).toBe(message);
      expect(d$('#invite-email').value).toBe('new@acme.com');
      expect(toasts()).toEqual([]);
    });

    it('reads the team again when someone changed it first, keeping what was typed', async () => {
      api.createTeamInvitation.mockRejectedValue(Object.assign(fail(409), { conflict: true, version: 5 }));
      api.getTeam.mockResolvedValue({ id: 'tm1', name: 'Core', version: 5 });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await type(d$('#invite-email'), 'new@acme.com');
      await submit(d$('[data-test=invite-form]'));
      expect(toasts()).toEqual(['Someone changed this team, refreshed']);
      expect(d$('[data-test=invite-error]')).toBe(null);
      expect(d$('#invite-email').value).toBe('new@acme.com');
      expect(api.listTeamInvitations).toHaveBeenCalledTimes(2);
    });
  });

  describe('pending invitations', () => {
    it('revokes one against the version', async () => {
      api.revokeTeamInvitation.mockResolvedValue({ version: 4 });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await click(d$('[data-test=invitation-revoke]'));
      expect(api.revokeTeamInvitation).toHaveBeenCalledWith('tm1', 'i1', 3);
      expect(d$$('[data-test=invitation-row]')).toHaveLength(1);
      expect(toasts()).toEqual(['Invitation to dee@acme.com revoked']);
      expect(store.getTeam('tm1').version).toBe(4);
    });

    it('says when a revoke fails', async () => {
      api.revokeTeamInvitation.mockRejectedValue(fail(500, 'No'));
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await click(d$('[data-test=invitation-revoke]'));
      expect(d$$('[data-test=invitation-row]')).toHaveLength(2);
      expect(toasts()).toEqual(['No']);
    });

    it('resends one as a new invitation in its place, each at the version the last left', async () => {
      api.revokeTeamInvitation.mockResolvedValue({ version: 4 });
      api.createTeamInvitation.mockResolvedValue({ invitation: { id: 'i7', email: 'eve@acme.com', expiresAt: soon() }, emailSent: true, version: 5 });
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await click(d$$('[data-test=invitation-resend]')[1]);
      expect(api.revokeTeamInvitation).toHaveBeenCalledWith('tm1', 'i2', 3);
      expect(api.createTeamInvitation).toHaveBeenCalledWith('tm1', 'eve@acme.com', 4);
      expect(d$$('[data-test=invitation-row]').map((r) => r.querySelector('p').textContent)).toEqual(['eve@acme.com', 'dee@acme.com']);
      expect(store.getTeam('tm1').version).toBe(5);
    });

    it('sends nothing when the old one cannot be revoked', async () => {
      api.revokeTeamInvitation.mockRejectedValue(fail(403));
      await mount('/tasks/active?team=tm1&manage=invitations', asTeam());
      await click(d$('[data-test=invitation-resend]'));
      expect(api.createTeamInvitation).not.toHaveBeenCalled();
      expect(here()).toBe('/');
      expect(toasts()).toContain("You're no longer in Core");
    });
  });

  describe('the activity log', () => {
    it('reads the newest twenty, in sentences, and the next twenty before the last', async () => {
      await mount('/tasks/active?team=tm1&manage=activity', asTeam());
      api.fetchTeamActivities.mockResolvedValueOnce({ activities: activity(20), version: 3 }).mockResolvedValueOnce({ activities: activity(2, 20), version: 3 });
      await click(d$('[data-test=manage-tab-members]'));
      await click(d$('[data-test=manage-tab-activity]'));
      expect(api.fetchTeamActivities).toHaveBeenLastCalledWith('tm1', { limit: 20, before: '' });
      const rows = d$$('[data-test=activity-row]');
      expect(rows).toHaveLength(20);
      expect(text(rows[0])).toBe('P P0 left just now');

      await click(d$('[data-test=activity-more]'));
      expect(api.fetchTeamActivities).toHaveBeenLastCalledWith('tm1', { limit: 20, before: 'a19' });
      expect(d$$('[data-test=activity-row]')).toHaveLength(22);
      expect(d$('[data-test=activity-more]')).toBe(null);
    });

    it('says when there is none, says it is loading, and says why it could not be read', async () => {
      await mount('/tasks/active?team=tm1&manage=activity', asTeam());
      expect(api.fetchTeamActivities).toHaveBeenCalledWith('tm1', { limit: 20, before: '' });
      expect(text(d$('[data-test=activity-empty]'))).toBe('Nothing yet.');
      app.unmount();

      await mount('/tasks/active?team=tm1&manage=activity', (s) => { asTeam()(s); api.fetchTeamActivities.mockReturnValue(new Promise(() => {})); });
      expect(text(d$('[data-test=team-manage] section'))).toBe('Loading…');
      app.unmount();

      api.fetchTeamActivities.mockRejectedValue(fail(500, 'Log down'));
      await mount('/tasks/active?team=tm1&manage=activity', (s) => { asTeam()(s); api.fetchTeamActivities.mockRejectedValue(fail(500, 'Log down')); });
      expect(toasts()).toContain('Log down');
      expect(text(d$('[data-test=activity-empty]'))).toBe('Nothing yet.');
    });

    it('keeps what it read when an older page cannot be read', async () => {
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      api.fetchTeamActivities.mockResolvedValueOnce({ activities: activity(20) }).mockRejectedValueOnce(fail(500, 'Log down'));
      await click(d$('[data-test=manage-tab-activity]'));
      await click(d$('[data-test=activity-more]'));
      expect(d$$('[data-test=activity-row]')).toHaveLength(20);
      expect(toasts()).toContain('Log down');
    });
  });

  describe('leaving and deleting', () => {
    it('leaves against the version, and goes back to Me', async () => {
      rememberTeam('tm1');
      api.leaveTeam.mockResolvedValue(null);
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$('[data-test=leave]'));
      const modal = dialog('Leave team');
      expect(text(modal)).toContain('Leave Core? Your shares to it are removed.');
      await click(button('Leave', modal));
      expect(api.leaveTeam).toHaveBeenCalledWith('tm1', 3);
      expect(store.getTeam('tm1')).toBe(null);
      expect(rememberedTeam()).toBe('');
      expect(here()).toBe('/');
      expect(d$('[data-test=team-manage]')).toBe(null);
      expect(toasts()).toEqual(['You left Core']);
    });

    it('says the last member leaving deletes the team', async () => {
      await mount('/tasks/active?team=tm1&manage=members', (s) => { asTeam()(s); api.listTeamMembers.mockResolvedValue({ members: [members()[0]] }); });
      await click(d$('[data-test=leave]'));
      expect(text(dialog('Leave team'))).toContain('You are the last member: leaving deletes Core.');
    });

    it('stays when leaving fails, and cancels leaving', async () => {
      api.leaveTeam.mockRejectedValue(fail(400, 'Not now'));
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$('[data-test=leave]'));
      await click(button('Cancel', dialog('Leave team')));
      expect(api.leaveTeam).not.toHaveBeenCalled();
      await click(d$('[data-test=leave]'));
      await click(button('Leave', dialog('Leave team')));
      expect(here()).toBe('/tasks/active?team=tm1&manage=members');
      expect(toasts()).toEqual(['Not now']);
    });

    it('deletes only once its name is typed, against the version', async () => {
      api.deleteTeam.mockResolvedValue(null);
      await mount('/tasks/active?team=tm1&manage=members', asTeam());
      await click(d$('[data-test=delete]'));
      const modal = dialog('Delete team');
      expect(text(modal)).toContain('Delete Core for everyone in it? This cannot be undone.');
      expect(text(modal)).toContain('Type Core to confirm.');
      expect(button('Delete team', modal).disabled).toBe(true);
      await type(modal.querySelector('[data-test=confirm-text]'), 'core');
      expect(button('Delete team', modal).disabled).toBe(true);
      await type(modal.querySelector('[data-test=confirm-text]'), 'Core');
      await click(button('Delete team', modal));
      expect(api.deleteTeam).toHaveBeenCalledWith('tm1', 3);
      expect(here()).toBe('/');
      expect(toasts()).toEqual(['Core deleted']);
    });
  });
});

describe('the invitation link dialog', () => {
  async function mountModal(props) {
    el = document.createElement('div');
    document.body.append(el);
    const onClose = vi.fn();
    app = createApp({ render: () => h(InviteLinkModal, { ...props, onClose }) });
    app.mount(el);
    await flush();
    return onClose;
  }

  it('copies the link', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    await mountModal({ link: 'https://x.test/l', email: 'a@b.c' });
    const copy = document.querySelector('[data-test=invite-copy]');
    expect(text(copy)).toBe('Copy');
    await click(copy);
    expect(writeText).toHaveBeenCalledWith('https://x.test/l');
    expect(text(copy)).toBe('Copied');
    vi.unstubAllGlobals();
  });

  it('copies through the desktop bridge when there is one', async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    window.agentrq = { clipboard: { write } };
    await mountModal({ link: 'https://x.test/l', email: 'a@b.c' });
    await click(document.querySelector('[data-test=invite-copy]'));
    expect(write).toHaveBeenCalledWith('https://x.test/l');
    delete window.agentrq;
  });

  it('leaves the link to copy by hand when the clipboard refuses', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    await mountModal({ link: 'https://x.test/l', email: 'a@b.c' });
    await click(document.querySelector('[data-test=invite-copy]'));
    expect(text(document.querySelector('[data-test=invite-copy]'))).toBe('Copy');
    const input = document.querySelector('[data-test=invite-link]');
    input.select = vi.fn();
    input.dispatchEvent(new Event('focus'));
    expect(input.select).toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('closes on its backdrop and on Escape, and lets Escape go when hidden', async () => {
    const onClose = await mountModal({ link: 'https://x.test/l', email: 'a@b.c' });
    document.querySelector('[data-test=invite-link-modal] > div').click();
    expect(onClose).toHaveBeenCalledTimes(1);
    const esc = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true });
    window.dispatchEvent(esc);
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(esc.defaultPrevented).toBe(true);
    app.unmount();
    el.remove();
    const closed = await mountModal({ link: '', email: '' });
    const esc2 = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true });
    window.dispatchEvent(esc2);
    expect(closed).not.toHaveBeenCalled();
    expect(esc2.defaultPrevented).toBe(false);
  });

  it('resets "Copied" for a new link', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    el = document.createElement('div');
    document.body.append(el);
    const { ref } = await import('vue');
    const link = ref('https://x.test/1');
    app = createApp({ render: () => h(InviteLinkModal, { link: link.value, email: 'a@b.c' }) });
    app.mount(el);
    await flush();
    await click(document.querySelector('[data-test=invite-copy]'));
    expect(text(document.querySelector('[data-test=invite-copy]'))).toBe('Copied');
    link.value = 'https://x.test/2';
    await flush();
    expect(text(document.querySelector('[data-test=invite-copy]'))).toBe('Copy');
    vi.unstubAllGlobals();
  });
});

const inbox = () => [
  { id: 'i1', teamId: 'tm1', teamName: 'Core', invitedBy: 'u9', invitedByName: 'Ann', expiresAt: soon() },
  { id: 'i2', teamId: 'tm2', teamName: 'Ops', invitedBy: 'u8', invitedByName: '', expiresAt: soon() },
];

describe('the invitations inbox', () => {
  it('says when there are none', async () => {
    await mount('/invitations');
    expect(api.listMyInvitations).toHaveBeenCalled();
    expect(text($('[data-test=invitations-empty]'))).toBe('You have no invitations.');
  });

  it('lists each, with who sent it and when it expires', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    await mount('/invitations');
    expect($$('[data-test=invitation-card]').map((c) => text(c.querySelector('div')))).toEqual([
      'Ann invited you to Core Expires in 3 days',
      'Someone invited you to Ops Expires in 3 days',
    ]);
  });

  it('accepts one and switches to that team, with its welcome', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    api.acceptInvitationById.mockResolvedValue({ id: 'tm1', name: 'Core', version: 2, memberCount: 3 });
    await mount('/invitations');
    await click($('[data-test=invitation-accept]'));
    expect(api.acceptInvitationById).toHaveBeenCalledWith('i1');
    expect(api.acceptInvitation).not.toHaveBeenCalled();
    expect(store.invitations.map((i) => i.id)).toEqual(['i2']);
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', version: 2, memberCount: 3 });
    expect(rememberedTeam()).toBe('tm1');
    expect(router.currentRoute.value.fullPath).toBe('/tasks/active?team=tm1&joined=1');
  });

  it('says why an accept failed, and reads the list again', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    api.acceptInvitationById.mockRejectedValue(fail(410, 'This invitation is no longer valid'));
    await mount('/invitations');
    api.listMyInvitations.mockResolvedValue({ invitations: [inbox()[1]] });
    await click($('[data-test=invitation-accept]'));
    expect(toasts()).toEqual(['This invitation is no longer valid']);
    expect($$('[data-test=invitation-card]')).toHaveLength(1);
    expect(router.currentRoute.value.path).toBe('/invitations');
  });

  it('declines one', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    api.declineInvitation.mockResolvedValue(undefined);
    await mount('/invitations');
    await click($$('[data-test=invitation-decline]')[1]);
    expect(api.declineInvitation).toHaveBeenCalledWith('i2');
    expect($$('[data-test=invitation-card]')).toHaveLength(1);
    expect(toasts()).toEqual(['Declined Ops']);
  });

  it('says why a decline failed', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    api.declineInvitation.mockRejectedValue(fail(500, 'Nope'));
    await mount('/invitations');
    await click($('[data-test=invitation-decline]'));
    expect(toasts()).toEqual(['Nope']);
    expect($$('[data-test=invitation-card]')).toHaveLength(2);
  });
});

describe('the invitation link', () => {
  const card = () => $('[data-test=accept-card]');
  const preview = (over = {}) => ({ teamId: 'tm1', teamName: 'Core', invitedByName: 'Ann', email: 'm***@acme.com', emailMatches: true, ...over });
  const kept = () => sessionStorage.getItem('agentrq:invite-token');

  it('reads the invitation, and says so meanwhile', async () => {
    api.fetchUser.mockResolvedValue(ME);
    let answer;
    api.previewInvitation.mockReturnValue(new Promise((r) => { answer = r; }));
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('loading');
    expect(text(card())).toContain('Reading the invitation…');
    answer(preview());
    await flush();
    expect(card().dataset.state).toBe('ready');
  });

  it('takes the token out of the address bar, keeping it for this tab', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    await mount('/invite?x=1#t1');
    expect(api.previewInvitation).toHaveBeenCalledWith('t1');
    expect(router.currentRoute.value.fullPath).toBe('/invite?x=1');
    expect(kept()).toBe('t1');
  });

  it('offers the sign-in, coming back here after it, when signed out', async () => {
    api.fetchUser.mockResolvedValue(null);
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('signedOut');
    // The token never rides on redirect_url, which the server sees.
    expect($('[data-test=accept-sign-in]').getAttribute('href')).toBe('/login?redirect_url=/invite');
    expect(api.previewInvitation).not.toHaveBeenCalled();
    expect(kept()).toBe('t1');
  });

  it('reads another link opened in the same tab', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    await mount('/invite#t1');
    api.previewInvitation.mockResolvedValue(preview({ teamName: 'Ops' }));
    await router.push('/invite#t2');
    await flush();
    expect(api.previewInvitation).toHaveBeenLastCalledWith('t2');
    expect(router.currentRoute.value.fullPath).toBe('/invite');
    expect(text(card())).toContain('Ann invited you to Ops');
  });

  it('picks the token up again after the sign-in', async () => {
    sessionStorage.setItem('agentrq:invite-token', 't1');
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    await mount('/invite');
    expect(api.previewInvitation).toHaveBeenCalledWith('t1');
    expect(card().dataset.state).toBe('ready');
  });

  it('works with no sessionStorage, from the link alone', async () => {
    const get = vi.spyOn(window, 'sessionStorage', 'get').mockImplementation(() => { throw new Error('denied'); });
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    await mount('/invite#t1');
    expect(api.previewInvitation).toHaveBeenCalledWith('t1');
    get.mockRestore();
  });

  it('treats a failed sign-in check as signed out', async () => {
    api.fetchUser.mockRejectedValue(fail(401));
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('signedOut');
  });

  it('accepts, and lands in the team with its welcome', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    api.acceptInvitation.mockResolvedValue({ id: 'tm1', name: 'Core', version: 2, memberCount: 2 });
    await mount('/invite#t1');
    expect(text(card())).toContain('Ann invited you to Core');
    expect(text(card())).not.toMatch(/admin|member\./i);
    await click($('[data-test=accept-button]'));
    expect(api.acceptInvitation).toHaveBeenCalledWith('t1');
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', version: 2, memberCount: 2 });
    expect(rememberedTeam()).toBe('tm1');
    expect(kept()).toBe(null);
    expect(router.currentRoute.value.fullPath).toBe('/tasks/active?team=tm1&joined=1');
  });

  it('accepts an invitation that names no sender', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview({ invitedByName: '' }));
    api.acceptInvitation.mockResolvedValue({ id: 'tm1', name: 'Core', version: 1, memberCount: 2 });
    await mount('/invite#t1');
    expect(text(card())).toContain('Someone invited you to Core');
    await click($('[data-test=accept-button]'));
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core' });
  });

  it('shows the server\'s words when the accept finds it used or expired', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    api.acceptInvitation.mockRejectedValue(fail(410, 'This invitation is no longer valid: ask for a new one'));
    await mount('/invite#t1');
    await click($('[data-test=accept-button]'));
    expect(card().dataset.state).toBe('invalid');
    expect(text($('[data-test=accept-invalid]'))).toBe('This invitation is no longer valid: ask for a new one');
    expect(kept()).toBe('t1');
  });

  it('says an invitation for another email is not yours, by the preview', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview({ emailMatches: false, email: 'b***@acme.com' }));
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('wrongEmail');
    expect(text($('[data-test=accept-wrong-email]'))).toBe('This invitation is for b***@acme.com. Sign in with that account.');
    expect($('[data-test=accept-button]')).toBe(null);
  });

  it('says "another email" when the preview names none', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview({ emailMatches: false, email: '' }));
    await mount('/invite#t1');
    expect(text($('[data-test=accept-wrong-email]'))).toBe('This invitation is for another email. Sign in with that account.');
  });

  it('shows the refusal, which names the masked address', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview());
    api.acceptInvitation.mockRejectedValue(fail(403, 'This invitation is for c***@acme.com: sign in with that account'));
    await mount('/invite#t1');
    await click($('[data-test=accept-button]'));
    expect(card().dataset.state).toBe('wrongEmail');
    expect(text($('[data-test=accept-wrong-email]'))).toBe('This invitation is for c***@acme.com: sign in with that account');
  });

  it('says an unknown invitation is no longer valid', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockRejectedValue(fail(404, 'Not found'));
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('invalid');
    expect(text($('[data-test=accept-invalid]'))).toBe('This invitation is no longer valid. Ask whoever invited you for a new one.');
  });

  it('says a link with no token is not valid, and reads nothing', async () => {
    api.fetchUser.mockResolvedValue(ME);
    await mount('/invite');
    expect(card().dataset.state).toBe('invalid');
    expect(api.previewInvitation).not.toHaveBeenCalled();
  });

  it('shows any other failure, and tries again', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockRejectedValueOnce(fail(500, 'Server down')).mockResolvedValueOnce(preview());
    await mount('/invite#t1');
    expect(card().dataset.state).toBe('error');
    expect(text($('[data-test=accept-error]'))).toBe('Server down');
    await click(button('Try again'));
    expect(card().dataset.state).toBe('ready');
  });
});
