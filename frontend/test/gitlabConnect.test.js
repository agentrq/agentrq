// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Connecting GitLab, for imports of private projects: the row beside an
 * import, the page GitLab returns to, where that page goes back to, and the
 * GitLab host the import box takes links to.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

const api = vi.hoisted(() => ({
  getGitLabConnection: vi.fn(),
  connectGitLab: vi.fn(),
  disconnectGitLab: vi.fn(),
}));
vi.mock('../src/api', () => api);

const { default: GitLabConnection } = await import('../src/components/GitLabConnection.vue');
const { default: GitLabCallbackView } = await import('../src/views/GitLabCallbackView.vue');
const { GITLAB_RETURN_KEY, takeGitLabReturn } = await import('../src/utils/gitlabConnect');
const { resetGitLabHost, useGitLabHost } = await import('../src/composables/useGitLabHost');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

const settle = () => new Promise((r) => setTimeout(r, 20));
const q = (el, t) => el.querySelector(`[data-test="${t}"]`);
const text = (el) => el.textContent.replace(/\s+/g, ' ').trim();
const apps = [];
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

const OFFERED = { host: 'gitlab.example', enabled: true, authorizeUrl: 'https://gitlab.example/oauth/authorize?state=s' };
const CONNECTED = { ...OFFERED, username: 'ann' };

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  sessionStorage.clear();
  vi.resetAllMocks();
});

describe('the GitLab connection beside an import', () => {
  it('shows nothing when the server cannot connect GitLab, or cannot say', async () => {
    api.getGitLabConnection.mockResolvedValueOnce({ host: 'gitlab.com', enabled: false });
    expect(q((await mount(GitLabConnection, '/')).el, 'gitlab-connection')).toBeNull();
    api.getGitLabConnection.mockRejectedValueOnce(new Error('down'));
    expect(q((await mount(GitLabConnection, '/')).el, 'gitlab-connection')).toBeNull();
  });

  it('offers to connect, remembering where it started', async () => {
    api.getGitLabConnection.mockResolvedValueOnce(OFFERED);
    const { el } = await mount(GitLabConnection, '/workspaces/ws1/settings?tab=skills');
    expect(text(q(el, 'gitlab-empty'))).toBe('Connect GitLab to import from your projects on gitlab.example.');
    const link = q(el, 'gitlab-connect');
    expect(link.textContent.trim()).toBe('Connect GitLab');
    expect(link.getAttribute('href')).toBe(OFFERED.authorizeUrl);
    link.addEventListener('click', (e) => e.preventDefault());
    link.click();
    expect(sessionStorage.getItem(GITLAB_RETURN_KEY)).toBe('/workspaces/ws1/settings?tab=skills');
  });

  it('copes without storage when leaving for GitLab', async () => {
    api.getGitLabConnection.mockResolvedValueOnce(OFFERED);
    const { el } = await mount(GitLabConnection, '/');
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    const link = q(el, 'gitlab-connect');
    link.addEventListener('click', (e) => e.preventDefault());
    expect(() => link.click()).not.toThrow();
    set.mockRestore();
  });

  it('names the account connected, and disconnects it once confirmed', async () => {
    api.getGitLabConnection.mockResolvedValueOnce(CONNECTED).mockResolvedValueOnce(OFFERED);
    api.disconnectGitLab.mockResolvedValueOnce(true);
    const { el } = await mount(GitLabConnection, '/');
    expect(q(el, 'gitlab-account').querySelector('.font-mono').textContent).toBe('ann');
    expect(text(q(el, 'gitlab-account'))).toContain('gitlab.example');
    expect(q(el, 'gitlab-connect')).toBeNull();

    q(el, 'gitlab-disconnect').click();
    await settle();
    expect(document.body.textContent).toContain('Private projects of ann can no longer be imported');
    modalButton('Cancel').click();
    await settle();
    expect(api.disconnectGitLab).not.toHaveBeenCalled();

    q(el, 'gitlab-disconnect').click();
    await settle();
    modalButton('Disconnect').click();
    await settle();
    expect(api.disconnectGitLab).toHaveBeenCalledTimes(1);
    expect(q(el, 'gitlab-account')).toBeNull();
    expect(q(el, 'gitlab-connect')).not.toBeNull();
    expect(toasts.value.at(-1).message).toBe('Disconnected ann.');
  });

  it('says why a disconnect failed, and keeps the account', async () => {
    api.getGitLabConnection.mockResolvedValueOnce(CONNECTED);
    api.disconnectGitLab.mockRejectedValueOnce(new Error('not found'));
    const { el } = await mount(GitLabConnection, '/');
    q(el, 'gitlab-disconnect').click();
    await settle();
    modalButton('Disconnect').click();
    await settle();
    expect(q(el, 'gitlab-account')).not.toBeNull();
    expect(toasts.value.at(-1).message).toBe('not found');
  });
});

describe('the page GitLab returns to', () => {
  it('finishes the connect and goes back where it started', async () => {
    sessionStorage.setItem(GITLAB_RETURN_KEY, '/workspaces/ws1/settings?tab=skills');
    api.connectGitLab.mockResolvedValueOnce(CONNECTED);
    const { router } = await mount(GitLabCallbackView, '/gitlab/callback?code=c1&state=s1');
    expect(api.connectGitLab).toHaveBeenCalledWith('c1', 's1');
    expect(router.currentRoute.value.fullPath).toBe('/workspaces/ws1/settings?tab=skills');
    expect(toasts.value.at(-1).message).toBe('GitLab connected: ann.');
    expect(sessionStorage.getItem(GITLAB_RETURN_KEY)).toBeNull();
  });

  it('shows why a connect was refused', async () => {
    api.connectGitLab.mockRejectedValueOnce(new Error('this GitLab connection link has expired'));
    const { el } = await mount(GitLabCallbackView, '/gitlab/callback?code=c1&state=s1');
    expect(q(el, 'gitlab-callback').dataset.state).toBe('failed');
    expect(text(q(el, 'gitlab-callback-message'))).toBe('This GitLab connection link has expired');
    expect(q(el, 'gitlab-callback-back').getAttribute('href')).toBe('/');
  });

  it('explains a denial, GitLab\'s own error, and a return with nothing', async () => {
    let { el } = await mount(GitLabCallbackView, '/gitlab/callback?error=access_denied&state=s');
    expect(text(q(el, 'gitlab-callback-message'))).toContain('You did not allow AgentRQ');
    ({ el } = await mount(GitLabCallbackView, '/gitlab/callback?error=invalid_scope&error_description=The+requested+scope+is+invalid'));
    expect(text(q(el, 'gitlab-callback-message'))).toBe('The requested scope is invalid');
    ({ el } = await mount(GitLabCallbackView, '/gitlab/callback?code=c1'));
    expect(text(q(el, 'gitlab-callback-message'))).toContain('did not send back what AgentRQ needs');
    expect(api.connectGitLab).not.toHaveBeenCalled();
  });
});

describe('takeGitLabReturn', () => {
  it('returns a path of this app once, and home for anything else', () => {
    expect(takeGitLabReturn()).toBe('/');
    sessionStorage.setItem(GITLAB_RETURN_KEY, '/kanban');
    expect(takeGitLabReturn()).toBe('/kanban');
    expect(takeGitLabReturn()).toBe('/');
    for (const bad of ['//evil.example', 'https://evil.example']) {
      sessionStorage.setItem(GITLAB_RETURN_KEY, bad);
      expect(takeGitLabReturn()).toBe('/');
    }
  });

  it('copes without storage', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(takeGitLabReturn()).toBe('/');
    get.mockRestore();
  });
});

describe('useGitLabHost', () => {
  beforeEach(() => resetGitLabHost());

  it('starts at gitlab.com and asks the server once', async () => {
    api.getGitLabConnection.mockResolvedValue({ host: 'git.example.com', enabled: false });
    const host = useGitLabHost();
    expect(host.value).toBe('gitlab.com');
    await settle();
    expect(host.value).toBe('git.example.com');
    expect(useGitLabHost()).toBe(host);
    expect(api.getGitLabConnection).toHaveBeenCalledTimes(1);
  });

  it('reads a missing answer as gitlab.com, and asks again after a failure', async () => {
    api.getGitLabConnection.mockRejectedValueOnce(new Error('down')).mockResolvedValueOnce(null);
    const host = useGitLabHost();
    await settle();
    useGitLabHost();
    await settle();
    expect(host.value).toBe('gitlab.com');
    expect(api.getGitLabConnection).toHaveBeenCalledTimes(2);
  });
});
