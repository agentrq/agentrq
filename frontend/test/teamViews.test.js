// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The team pages a person manages a team from: creating one, its settings
 * (members, the invite form and its daily cap, invitations, rename, leave,
 * delete), the invitations inbox, and the invitation link's accept page.
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
  setTeamMemberRole: vi.fn(),
  removeTeamMember: vi.fn(),
  listTeamInvitations: vi.fn(),
  createTeamInvitation: vi.fn(),
  revokeTeamInvitation: vi.fn(),
  previewInvitation: vi.fn(),
  acceptInvitation: vi.fn(),
  acceptInvitationById: vi.fn(),
  declineInvitation: vi.fn(),
}));

import * as api from '../src/api';
import { useTeamStore, rememberedTeam, rememberTeam } from '../src/stores/teamStore';
import { useToasts } from '../src/composables/useToasts';
import TeamNewView from '../src/views/TeamNewView.vue';
import TeamSettingsView from '../src/views/TeamSettingsView.vue';
import InvitationsView from '../src/views/InvitationsView.vue';
import InvitationAcceptView from '../src/views/InvitationAcceptView.vue';
import InviteLinkModal from '../src/components/InviteLinkModal.vue';

const Blank = { render: () => h('div', { 'data-test': 'blank' }) };

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

async function mount(path) {
  const pinia = createPinia();
  setActivePinia(pinia);
  store = useTeamStore();
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/login', component: Blank },
      { path: '/tasks/active', component: Blank },
      { path: '/team/new', component: TeamNewView },
      { path: '/team/settings', component: TeamSettingsView },
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

  it('makes you its owner and opens its members, with the invite box', async () => {
    api.createTeam.mockResolvedValue({ id: 'tm1', name: 'Core', role: 'owner', memberCount: 1 });
    await mount('/team/new');
    store.upsert({ id: 'tm0', name: 'Alpha', role: 'member' });
    await nextTick();
    expect(text($('h1'))).toBe('Create a team');
    expect($('h2')).toBe(null);
    await type($('#team-name'), '  Core ');
    await submit($('form'));
    expect(api.createTeam).toHaveBeenCalledWith('Core');
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', role: 'owner', memberCount: 1 });
    expect(rememberedTeam()).toBe('tm1');
    expect(router.currentRoute.value.fullPath).toBe('/team/settings?team=tm1&tab=members&welcome=1');
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
  { userId: 'u1', name: 'Me', email: 'me@acme.com', role: 'owner' },
  { userId: 'u2', name: 'Bob', email: 'bob@acme.com', role: 'member' },
  { userId: 'u3', name: '', email: 'cy@acme.com', role: 'owner' },
];
const soon = () => new Date(Date.now() + 3 * 86400000 + 60000).toISOString();
const invitations = () => [
  { id: 'i1', email: 'dee@acme.com', role: 'member', invitedBy: 'Me', expiresAt: soon() },
  { id: 'i2', email: 'eve@acme.com', role: 'admin', invitedBy: '', expiresAt: soon() },
];

function asTeam(role = 'owner', name = 'Core') {
  api.fetchUser.mockResolvedValue(ME);
  api.getTeam.mockResolvedValue({ id: 'tm1', name, role, memberCount: 2 });
  api.listTeamMembers.mockResolvedValue({ members: members() });
  api.listTeamInvitations.mockResolvedValue({ invitations: invitations() });
}

describe('a team\'s settings', () => {
  it('lists its members and pending invitations for its owner', async () => {
    asTeam('owner');
    await mount('/team/settings?team=tm1');
    expect(text($('h1'))).toBe('Core');
    expect(text($('h1 + p'))).toBe('Team settings · you are an owner');
    const rows = $$('[data-test=member-row]');
    expect(rows.map(text)).toEqual([
      'M Me (you) me@acme.com owner',
      'B Bob bob@acme.com member admin owner Remove',
      'C cy@acme.com member admin owner Remove',
    ]);
    expect(rows[0].querySelector('[data-test=member-role]')).toBe(null);
    expect(rows[1].querySelector('[data-test=member-role]')).not.toBe(null);
    // An owner can change another owner.
    expect(rows[2].querySelector('[data-test=member-role]')).not.toBe(null);
    expect([...$('[data-test=invite-role]').options].map((o) => o.value)).toEqual(['member', 'admin', 'owner']);
    const pending = $$('[data-test=invitation-row]').map(text);
    expect(pending[0]).toBe('dee@acme.com member · sent by Me · expires in 3 days Resend Revoke');
    expect(pending[1]).toBe('eve@acme.com admin · expires in 3 days Resend Revoke');
  });

  it('lets an admin manage members but not owners, and offers no owner role', async () => {
    asTeam('admin');
    await mount('/team/settings?team=tm1&tab=nonsense');
    expect(text($('h1 + p'))).toBe('Team settings · you are an admin');
    const rows = $$('[data-test=member-row]');
    expect(rows[1].querySelector('[data-test=member-role]')).not.toBe(null);
    expect(rows[2].querySelector('[data-test=member-role]')).toBe(null);
    expect([...$('[data-test=invite-role]').options].map((o) => o.value)).toEqual(['member', 'admin']);
  });

  it('shows a member the lists, with nothing to change', async () => {
    asTeam('member');
    api.listTeamMembers.mockResolvedValue({});
    await mount('/team/settings?team=tm1&welcome=1');
    expect(text($('h1 + p'))).toBe('Team settings · you are a member');
    expect($('[data-test=invite-form]')).toBe(null);
    expect($$('[data-test=member-row]')).toEqual([]);
    expect(api.listTeamInvitations).not.toHaveBeenCalled();
    expect(text(el)).not.toContain('Pending invitations');
    await click($('[data-test=tab-general]'));
    expect($('#team-rename').disabled).toBe(true);
    expect(button('Rename')).toBe(undefined);
    await click($('[data-test=tab-danger]'));
    expect($('[data-test=leave]')).not.toBe(null);
    expect($('[data-test=delete]')).toBe(null);
  });

  it('names the page "Team" until the team is known, with no role', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.getTeam.mockResolvedValue({ id: 'tm1', name: 'Core' });
    api.listTeamMembers.mockResolvedValue({ members: [] });
    api.listTeamInvitations.mockResolvedValue({});
    await mount('/team/settings?team=tm1');
    expect(text($('h1'))).toBe('Core');
    expect(text($('h1 + p'))).toBe('Team settings');
    // Not loaded yet: the name falls back, and the rename box is empty.
    store.drop('tm1');
    await nextTick();
    expect(text($('h1'))).toBe('Team');
    await click($('[data-test=tab-general]'));
    expect($('#team-rename').value).toBe('');
  });

  it('has none pending when there are none', async () => {
    asTeam('owner');
    api.listTeamInvitations.mockResolvedValue({});
    await mount('/team/settings?team=tm1');
    expect(text(el)).toContain('Pending invitations · 0 None.');
  });

  it('sends you back to Me when the team is gone', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.getTeam.mockRejectedValue(fail(404));
    await mount('/team/settings?team=tm1');
    expect(router.currentRoute.value.fullPath).toBe('/');
    expect(toasts()).toContain("You're no longer in that team");
  });

  it('says any other failure to load where it happened', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.getTeam.mockRejectedValue(fail(500, 'Server down'));
    await mount('/team/settings?team=tm1');
    expect(router.currentRoute.value.path).toBe('/team/settings');
    expect(toasts()).toContain('Server down');
  });

  describe('right after creating it', () => {
    it('offers to share your workspaces and focuses the invite box', async () => {
      asTeam('owner');
      await mount('/team/settings?team=tm1&tab=members&welcome=1');
      expect($('[data-test=share-hint]')).not.toBe(null);
      expect(document.activeElement).toBe($('#invite-email'));
      await click(button('Choose', $('[data-test=share-hint]')));
      expect(router.currentRoute.value.fullPath).toBe('/team/settings?team=tm1&tab=workspaces');
      expect($('[data-test=workspace-sharing]')).not.toBe(null);
      expect(text(el)).toContain('Teammates in Core see the workspaces you share here');
    });
  });

  describe('the invite form', () => {
    it('sends an invitation, says so, and lists it in place of any older one to that address', async () => {
      asTeam('owner');
      api.createTeamInvitation.mockResolvedValue({
        invitation: { id: 'i9', email: 'dee@acme.com', role: 'admin', expiresAt: soon() },
        emailSent: true,
      });
      await mount('/team/settings?team=tm1');
      await type($('#invite-email'), ' dee@acme.com ');
      await choose($('[data-test=invite-role]'), 'admin');
      await submit($('[data-test=invite-form]'));
      expect(api.createTeamInvitation).toHaveBeenCalledWith('tm1', 'dee@acme.com', 'admin');
      expect(toasts()).toEqual(['Invitation sent']);
      expect($$('[data-test=invitation-row]').map((r) => r.querySelector('p').textContent)).toEqual(['dee@acme.com', 'eve@acme.com']);
      expect($('#invite-email').value).toBe('');
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
    });

    it('hands over the link when no email carried it', async () => {
      asTeam('owner');
      api.createTeamInvitation.mockResolvedValue({ emailSent: false, link: 'https://agentrq.test/invite#t1' });
      await mount('/team/settings?team=tm1');
      await type($('#invite-email'), 'new@acme.com');
      await submit($('[data-test=invite-form]'));
      const modal = document.querySelector('[data-test=invite-link-modal]');
      expect(modal.querySelector('[data-test=invite-link]').value).toBe('https://agentrq.test/invite#t1');
      expect(text(modal)).toContain('Send this to new@acme.com. It works once, for that email, for 7 days.');
      expect($$('[data-test=invitation-row]')).toHaveLength(2);
      await click(button('Done', modal));
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
    });

    it('takes an answer with nothing in it', async () => {
      asTeam('owner');
      api.createTeamInvitation.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1');
      await type($('#invite-email'), 'new@acme.com');
      await submit($('[data-test=invite-form]'));
      expect(toasts()).toEqual(['Invitation sent']);
      expect(document.querySelector('[data-test=invite-link-modal]')).toBe(null);
    });

    it.each([
      ['the team\'s', 'this team has reached its limit of 10 invitations per day'],
      ['the inviter\'s', 'you have reached your limit of 20 invitations per day across all your teams'],
    ])('shows %s daily cap in the server\'s words, in the form, and keeps what was typed', async (_, message) => {
      asTeam('owner');
      api.createTeamInvitation.mockRejectedValue(fail(429, message));
      await mount('/team/settings?team=tm1');
      await type($('#invite-email'), 'new@acme.com');
      await submit($('[data-test=invite-form]'));
      const err = $('[data-test=invite-error]');
      expect(err.getAttribute('role')).toBe('alert');
      expect(text(err)).toBe(message);
      expect($('#invite-email').value).toBe('new@acme.com');
      expect(toasts()).toEqual([]);
    });
  });

  describe('pending invitations', () => {
    it('revokes one', async () => {
      asTeam('owner');
      api.revokeTeamInvitation.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1');
      await click($('[data-test=invitation-revoke]'));
      expect(api.revokeTeamInvitation).toHaveBeenCalledWith('tm1', 'i1');
      expect($$('[data-test=invitation-row]')).toHaveLength(1);
      expect(toasts()).toEqual(['Invitation to dee@acme.com revoked']);
    });

    it('says when a revoke fails', async () => {
      asTeam('owner');
      api.revokeTeamInvitation.mockRejectedValue(fail(500, 'No'));
      await mount('/team/settings?team=tm1');
      await click($('[data-test=invitation-revoke]'));
      expect($$('[data-test=invitation-row]')).toHaveLength(2);
      expect(toasts()).toEqual(['No']);
    });

    it('resends one as a new invitation in its place', async () => {
      asTeam('owner');
      api.revokeTeamInvitation.mockResolvedValue(undefined);
      api.createTeamInvitation.mockResolvedValue({ invitation: { id: 'i7', email: 'eve@acme.com', role: 'admin', expiresAt: soon() }, emailSent: true });
      await mount('/team/settings?team=tm1');
      await click($$('[data-test=invitation-resend]')[1]);
      expect(api.revokeTeamInvitation).toHaveBeenCalledWith('tm1', 'i2');
      expect(api.createTeamInvitation).toHaveBeenCalledWith('tm1', 'eve@acme.com', 'admin');
      expect($$('[data-test=invitation-row]').map((r) => r.querySelector('p').textContent)).toEqual(['eve@acme.com', 'dee@acme.com']);
    });

    it('sends nothing when the old one cannot be revoked', async () => {
      asTeam('owner');
      api.revokeTeamInvitation.mockRejectedValue(fail(403));
      await mount('/team/settings?team=tm1');
      await click($('[data-test=invitation-resend]'));
      expect(api.createTeamInvitation).not.toHaveBeenCalled();
      expect(router.currentRoute.value.fullPath).toBe('/');
      expect(toasts()).toContain("You're no longer in Core");
    });
  });

  describe('members', () => {
    it('changes a role', async () => {
      asTeam('owner');
      api.setTeamMemberRole.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1');
      await choose($$('[data-test=member-role]')[0], 'admin');
      expect(api.setTeamMemberRole).toHaveBeenCalledWith('tm1', 'u2', 'admin');
      expect(toasts()).toEqual(['Bob is now an admin']);
      await choose($$('[data-test=member-role]')[1], 'member');
      expect(toasts()).toEqual(['Bob is now an admin', 'cy@acme.com is now a member']);
    });

    it('puts the role back when the change is refused', async () => {
      asTeam('owner');
      api.setTeamMemberRole.mockRejectedValue(fail(400, 'The last owner stays'));
      await mount('/team/settings?team=tm1');
      const select = $$('[data-test=member-role]')[1];
      await choose(select, 'member');
      expect($$('[data-test=member-role]')[1].value).toBe('owner');
      expect(toasts()).toEqual(['The last owner stays']);
    });

    it('removes one, after asking', async () => {
      asTeam('owner');
      api.removeTeamMember.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1');
      await click($$('[data-test=member-remove]')[0]);
      const modal = dialog('Remove member');
      expect(text(modal)).toContain('Remove Bob from Core? Their shared workspaces leave Core too.');
      await click(button('Remove', modal));
      expect(api.removeTeamMember).toHaveBeenCalledWith('tm1', 'u2');
      expect($$('[data-test=member-row]')).toHaveLength(2);
      expect(toasts()).toEqual(['Bob removed']);
    });

    it('names one with no name by email, and says when removing fails', async () => {
      asTeam('owner');
      api.removeTeamMember.mockRejectedValue(fail(500, 'Nope'));
      await mount('/team/settings?team=tm1');
      await click($$('[data-test=member-remove]')[1]);
      const modal = dialog('Remove member');
      expect(text(modal)).toContain('Remove cy@acme.com from Core?');
      await click(button('Remove', modal));
      expect($$('[data-test=member-row]')).toHaveLength(3);
      expect(toasts()).toEqual(['Nope']);
    });

    it('removes one with no name, naming them by email', async () => {
      asTeam('owner');
      api.removeTeamMember.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1');
      await click($$('[data-test=member-remove]')[1]);
      await click(button('Remove', dialog('Remove member')));
      expect(toasts()).toEqual(['cy@acme.com removed']);
    });

    it('cancels a removal', async () => {
      asTeam('owner');
      await mount('/team/settings?team=tm1');
      await click($$('[data-test=member-remove]')[0]);
      await click(button('Cancel', dialog('Remove member')));
      expect(api.removeTeamMember).not.toHaveBeenCalled();
    });
  });

  describe('its name', () => {
    it('renames it', async () => {
      asTeam('owner');
      api.renameTeam.mockResolvedValue({ id: 'tm1', name: 'Platform' });
      await mount('/team/settings?team=tm1&tab=general');
      expect($('#team-rename').value).toBe('Core');
      expect(button('Rename').disabled).toBe(true);
      await type($('#team-rename'), ' Platform ');
      expect(button('Rename').disabled).toBe(false);
      await submit($('form'));
      expect(api.renameTeam).toHaveBeenCalledWith('tm1', 'Platform');
      expect(text($('h1'))).toBe('Platform');
      expect(toasts()).toEqual(['Team renamed']);
    });

    it('says when renaming fails', async () => {
      asTeam('owner');
      api.renameTeam.mockRejectedValue(fail(409, 'Taken'));
      await mount('/team/settings?team=tm1&tab=general');
      await type($('#team-rename'), 'Platform');
      await submit($('form'));
      expect(toasts()).toEqual(['Taken']);
    });
  });

  describe('leaving and deleting', () => {
    it('leaves, and goes back to Me', async () => {
      asTeam('member');
      rememberTeam('tm1');
      api.leaveTeam.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1&tab=danger');
      await click($('[data-test=leave]'));
      const modal = dialog('Leave team');
      expect(text(modal)).toContain('Leave Core? Your shares to it are removed.');
      await click(button('Leave', modal));
      expect(api.leaveTeam).toHaveBeenCalledWith('tm1');
      expect(store.getTeam('tm1')).toBe(null);
      expect(rememberedTeam()).toBe('');
      expect(router.currentRoute.value.fullPath).toBe('/');
      expect(toasts()).toEqual(['You left Core']);
    });

    it('stays when leaving fails', async () => {
      asTeam('owner');
      api.leaveTeam.mockRejectedValue(fail(400, 'Hand over ownership first'));
      await mount('/team/settings?team=tm1&tab=danger');
      await click($('[data-test=leave]'));
      await click(button('Leave', dialog('Leave team')));
      expect(router.currentRoute.value.path).toBe('/team/settings');
      expect(toasts()).toEqual(['Hand over ownership first']);
    });

    it('cancels leaving', async () => {
      asTeam('owner');
      await mount('/team/settings?team=tm1&tab=danger');
      await click($('[data-test=leave]'));
      await click(button('Cancel', dialog('Leave team')));
      expect(api.leaveTeam).not.toHaveBeenCalled();
    });

    it('deletes only once its name is typed', async () => {
      asTeam('owner');
      api.deleteTeam.mockResolvedValue(undefined);
      await mount('/team/settings?team=tm1&tab=danger');
      await click($('[data-test=delete]'));
      const modal = dialog('Delete team');
      expect(text(modal)).toContain('Delete Core for everyone in it? This cannot be undone.');
      expect(text(modal)).toContain('Type Core to confirm.');
      const confirm = button('Delete team', modal);
      expect(confirm.disabled).toBe(true);
      // A click on the disabled button's handler still refuses.
      confirm.disabled = false;
      await click(confirm);
      expect(api.deleteTeam).not.toHaveBeenCalled();
      confirm.disabled = true;
      await type(modal.querySelector('[data-test=confirm-text]'), 'core');
      expect(button('Delete team', modal).disabled).toBe(true);
      await type(modal.querySelector('[data-test=confirm-text]'), 'Core');
      expect(button('Delete team', modal).disabled).toBe(false);
      await click(button('Delete team', modal));
      expect(api.deleteTeam).toHaveBeenCalledWith('tm1');
      expect(router.currentRoute.value.fullPath).toBe('/');
      expect(toasts()).toEqual(['Core deleted']);
    });

    it('empties the typed name each time it opens', async () => {
      asTeam('owner');
      await mount('/team/settings?team=tm1&tab=danger');
      await click($('[data-test=delete]'));
      await type(dialog('Delete team').querySelector('[data-test=confirm-text]'), 'Core');
      await click(button('Cancel', dialog('Delete team')));
      await click($('[data-test=delete]'));
      const inputs = [...document.querySelectorAll('[data-test=confirm-text]')];
      expect(inputs.at(-1).value).toBe('');
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
  { id: 'i1', teamId: 'tm1', teamName: 'Core', role: 'member', invitedBy: 'u9', invitedByName: 'Ann', expiresAt: soon() },
  { id: 'i2', teamId: 'tm2', teamName: 'Ops', role: 'admin', invitedBy: 'u8', invitedByName: '', expiresAt: soon() },
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
      'Ann invited you to Core as member Expires in 3 days',
      'Someone invited you to Ops as admin Expires in 3 days',
    ]);
  });

  it('accepts one and switches to that team, with its welcome', async () => {
    api.listMyInvitations.mockResolvedValue({ invitations: inbox() });
    api.acceptInvitationById.mockResolvedValue({ id: 'tm1', name: 'Core', role: 'member', memberCount: 3 });
    await mount('/invitations');
    await click($('[data-test=invitation-accept]'));
    expect(api.acceptInvitationById).toHaveBeenCalledWith('i1');
    expect(api.acceptInvitation).not.toHaveBeenCalled();
    expect(store.invitations.map((i) => i.id)).toEqual(['i2']);
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', role: 'member', memberCount: 3 });
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
  const preview = (over = {}) => ({ teamId: 'tm1', teamName: 'Core', invitedByName: 'Ann', role: 'admin', email: 'm***@acme.com', emailMatches: true, ...over });
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
    api.acceptInvitation.mockResolvedValue({ id: 'tm1', name: 'Core', role: 'admin', memberCount: 2 });
    await mount('/invite#t1');
    expect(text(card())).toContain('Ann invited you to Core');
    expect(text(card())).toContain('As admin.');
    await click($('[data-test=accept-button]'));
    expect(api.acceptInvitation).toHaveBeenCalledWith('t1');
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Core', role: 'admin', memberCount: 2 });
    expect(rememberedTeam()).toBe('tm1');
    expect(kept()).toBe(null);
    expect(router.currentRoute.value.fullPath).toBe('/tasks/active?team=tm1&joined=1');
  });

  it('accepts an invitation that names no sender or role', async () => {
    api.fetchUser.mockResolvedValue(ME);
    api.previewInvitation.mockResolvedValue(preview({ invitedByName: '', role: '' }));
    api.acceptInvitation.mockResolvedValue({ id: 'tm1', name: 'Core', role: 'member', memberCount: 2 });
    await mount('/invite#t1');
    expect(text(card())).toContain('Someone invited you to Core');
    expect(text(card())).toContain('As member.');
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
