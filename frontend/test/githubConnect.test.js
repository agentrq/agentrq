// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Importing skills from private repositories: the GitHub accounts connected
 * beside the import, and the page GitHub returns to after an install, which
 * finishes the connect with the session cookie and goes back where it began.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

const api = vi.hoisted(() => ({
  getGitHubConnection: vi.fn(),
  connectGitHub: vi.fn(),
  disconnectGitHub: vi.fn(),
}));
vi.mock('../src/api', () => api);

const { default: GitHubConnection } = await import('../src/components/GitHubConnection.vue');
const { default: GitHubCallbackView } = await import('../src/views/GitHubCallbackView.vue');
const { GITHUB_RETURN_KEY, takeGitHubReturn } = await import('../src/utils/githubConnect');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

const settle = () => new Promise((r) => setTimeout(r, 20));
const q = (el, t) => el.querySelector(`[data-test="${t}"]`);
const text = (el) => el.textContent.replace(/\s+/g, ' ').trim();
const apps = [];
// DeleteModal's buttons, by their label.
const modalButton = (label) => [...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === label);

async function mount(component, path) {
  const el = document.createElement('div');
  document.body.append(el);
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:any(.*)*', component: { render: () => null } }] });
  await router.push(path);
  const app = createApp({ render: () => h(component) });
  app.use(createPinia());
  app.use(router);
  app.mount(el);
  apps.push(app);
  await settle();
  return { el, router };
}

const CONNECTED = {
  enabled: true,
  installUrl: 'https://github.com/apps/agentrq/installations/new?state=s',
  installations: [{ installationId: 9, account: 'acme', accountType: 'Organization' }, { installationId: 10, account: 'me', accountType: 'User' }],
};

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  sessionStorage.clear();
  vi.resetAllMocks();
});

describe('the GitHub connection beside an import', () => {
  it('shows nothing when the server has no GitHub App, or cannot say', async () => {
    api.getGitHubConnection.mockResolvedValueOnce({ enabled: false, installations: [] });
    expect(q((await mount(GitHubConnection, '/')).el, 'github-connection')).toBeNull();
    api.getGitHubConnection.mockRejectedValueOnce(new Error('down'));
    expect(q((await mount(GitHubConnection, '/')).el, 'github-connection')).toBeNull();
  });

  it('copes without storage when leaving for GitHub', async () => {
    api.getGitHubConnection.mockResolvedValueOnce({ ...CONNECTED, installations: [] });
    const { el } = await mount(GitHubConnection, '/');
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    const link = q(el, 'github-connect');
    link.addEventListener('click', (e) => e.preventDefault());
    expect(() => link.click()).not.toThrow();
    set.mockRestore();
  });

  it('offers to connect, remembering where it started', async () => {
    api.getGitHubConnection.mockResolvedValueOnce({ ...CONNECTED, installations: [] });
    const { el } = await mount(GitHubConnection, '/workspaces/ws1/settings?tab=skills');
    expect(text(q(el, 'github-empty'))).toBe('Connect GitHub to import from repositories you choose.');
    const link = q(el, 'github-connect');
    expect(link.textContent.trim()).toBe('Connect GitHub');
    expect(link.getAttribute('href')).toBe(CONNECTED.installUrl);
    link.addEventListener('click', (e) => e.preventDefault());
    link.click();
    expect(sessionStorage.getItem(GITHUB_RETURN_KEY)).toBe('/workspaces/ws1/settings?tab=skills');
  });

  it('lists the connected accounts, and disconnects one', async () => {
    api.getGitHubConnection.mockResolvedValueOnce(structuredClone(CONNECTED));
    api.disconnectGitHub.mockResolvedValueOnce(true);
    const { el } = await mount(GitHubConnection, '/');
    const chips = [...el.querySelectorAll('[data-test="github-installation"]')];
    expect(chips.map((c) => c.querySelector('.font-mono').textContent)).toEqual(['acme', 'me']);
    // An organization says so.
    expect(chips.map((c) => text(c).includes('Org'))).toEqual([true, false]);
    expect(q(el, 'github-connect').textContent.trim()).toBe('Manage on GitHub');

    // Asked first; cancelling keeps it.
    q(el, 'github-disconnect-9').click();
    await settle();
    expect(document.body.textContent).toContain('Private repositories of acme can no longer be imported');
    modalButton('Cancel').click();
    await settle();
    expect(api.disconnectGitHub).not.toHaveBeenCalled();

    q(el, 'github-disconnect-9').click();
    await settle();
    modalButton('Disconnect').click();
    await settle();
    expect(api.disconnectGitHub).toHaveBeenCalledWith(9);
    expect(el.querySelectorAll('[data-test="github-installation"]')).toHaveLength(1);
    expect(toasts.value.at(-1).message).toBe('Disconnected acme.');
  });

  it('says why a disconnect failed, and keeps the account', async () => {
    api.getGitHubConnection.mockResolvedValueOnce(structuredClone(CONNECTED));
    api.disconnectGitHub.mockRejectedValueOnce(new Error('not found'));
    const { el } = await mount(GitHubConnection, '/');
    q(el, 'github-disconnect-10').click();
    await settle();
    modalButton('Disconnect').click();
    await settle();
    expect(el.querySelectorAll('[data-test="github-installation"]')).toHaveLength(2);
    expect(toasts.value.at(-1).message).toBe('not found');
  });
});

describe('the page GitHub returns to', () => {
  it('finishes the connect and goes back where it started', async () => {
    sessionStorage.setItem(GITHUB_RETURN_KEY, '/workspaces/ws1/settings?tab=skills');
    api.connectGitHub.mockResolvedValueOnce(CONNECTED);
    const { router } = await mount(GitHubCallbackView, '/github/callback?code=c1&installation_id=9&setup_action=install&state=s1');
    expect(api.connectGitHub).toHaveBeenCalledWith('c1', 's1');
    expect(router.currentRoute.value.fullPath).toBe('/workspaces/ws1/settings?tab=skills');
    expect(toasts.value.at(-1).message).toBe('GitHub connected: acme, me.');
    expect(sessionStorage.getItem(GITHUB_RETURN_KEY)).toBeNull();
  });

  it('says when the app reached no account', async () => {
    api.connectGitHub.mockResolvedValueOnce({ ...CONNECTED, installations: [] });
    const { router } = await mount(GitHubCallbackView, '/github/callback?code=c1&state=s1');
    expect(router.currentRoute.value.fullPath).toBe('/');
    expect(toasts.value.at(-1).message).toContain('not installed on any account');
  });

  it('shows why a connect was refused', async () => {
    api.connectGitHub.mockRejectedValueOnce(new Error('this GitHub connection link has expired'));
    const { el } = await mount(GitHubCallbackView, '/github/callback?code=c1&state=s1');
    expect(q(el, 'github-callback').dataset.state).toBe('failed');
    expect(text(q(el, 'github-callback-message'))).toBe('This GitHub connection link has expired');
    expect(q(el, 'github-callback-back').getAttribute('href')).toBe('/');
  });

  it('explains a return with no code, and an install waiting for approval', async () => {
    let { el } = await mount(GitHubCallbackView, '/github/callback?installation_id=9&setup_action=install');
    expect(text(q(el, 'github-callback-message'))).toContain('Request user authorization');
    ({ el } = await mount(GitHubCallbackView, '/github/callback?setup_action=request&state=s'));
    expect(q(el, 'github-callback').dataset.state).toBe('requested');
    expect(api.connectGitHub).not.toHaveBeenCalled();
  });
});

describe('takeGitHubReturn', () => {
  it('returns a path of this app once, and home for anything else', () => {
    expect(takeGitHubReturn()).toBe('/');
    sessionStorage.setItem(GITHUB_RETURN_KEY, '/kanban');
    expect(takeGitHubReturn()).toBe('/kanban');
    expect(takeGitHubReturn()).toBe('/');
    for (const bad of ['//evil.example', 'https://evil.example']) {
      sessionStorage.setItem(GITHUB_RETURN_KEY, bad);
      expect(takeGitHubReturn()).toBe('/');
    }
  });

  it('copes without storage', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(takeGitHubReturn()).toBe('/');
    get.mockRestore();
  });
});
