// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect, vi } from 'vitest';

import {
  createHiddenTabNotifier,
  notificationUrl,
  showSystemNotification,
  systemNotificationFor,
} from '../src/composables/useHiddenTabNotifications';

/**
 * A toast is drawn inside the page; with the tab in the background it is news
 * nobody sees. These are the rules for raising a system notification instead.
 */

const reply = (over = {}) => ({
  type: 'reply.received',
  payload: { id: 't1', workspaceId: 'w1', title: 'Ship it', status: 'ongoing', messages: [{ id: 'm1', sender: 'agent', text: 'on it' }], ...over },
});
const moved = { type: 'task.status', payload: { taskId: 't1', workspaceId: 'w1', title: 'Ship it', from: 'ongoing', to: 'blocked' } };
const sessionFailed = { type: 'session.updated', payload: { status: 'failed', error: 'claude: not found' } };
const names = { workspaceName: (id) => (id === 'w1' ? 'agentrq-code' : '') };

describe('notificationUrl', () => {
  it('opens the task, else its workspace, else the app, under the base path', () => {
    expect(notificationUrl({ taskId: 't1', workspaceId: 'w1' })).toBe('/workspaces/w1/tasks/t1');
    expect(notificationUrl({ taskId: '', workspaceId: 'w1' }, '/arq/')).toBe('/arq/workspaces/w1');
    expect(notificationUrl({ taskId: '', workspaceId: '' }, '/arq')).toBe('/arq/');
  });
});

describe('systemNotificationFor', () => {
  it("says what the toast says, naming the workspace", () => {
    expect(systemNotificationFor(moved, names)).toEqual({
      title: 'Ship it',
      body: 'agentrq-code: The agent needs your input.',
      tag: 'agentrq-t1',
      url: '/workspaces/w1/tasks/t1',
      workspaceId: 'w1',
    });
  });

  // The toast skips a reply on the task on screen, which renders itself there.
  // Hidden, that task is as unseen as any other.
  it('announces a reply even on the task that was open', () => {
    expect(systemNotificationFor(reply())).toMatchObject({ title: 'Ship it', body: 'on it', url: '/workspaces/w1/tasks/t1' });
  });

  it('names the app when there is no task or workspace to name', () => {
    expect(systemNotificationFor(sessionFailed)).toMatchObject({
      title: 'Agent could not start',
      body: 'claude: not found',
      tag: 'agentrq-failed',
      url: '/',
      workspaceId: '',
    });
    expect(systemNotificationFor({ ...moved, payload: { ...moved.payload, title: '' } }, names))
      .toMatchObject({ title: 'agentrq-code', body: 'The agent needs your input.' });
    expect(systemNotificationFor({ ...moved, payload: { ...moved.payload, title: '' } }).title).toBe('AgentRQ');
  });

  it('says nothing the toast would not', () => {
    expect(systemNotificationFor(reply({ messages: [{ id: 'm1', sender: 'human', text: 'any news?' }] }))).toBeNull();
    expect(systemNotificationFor({ type: 'agent.connected', payload: {} })).toBeNull();
  });
});

describe('showSystemNotification', () => {
  const notification = { title: 'Ship it', body: 'on it', tag: 'agentrq-t1', url: '/arq/workspaces/w1/tasks/t1' };
  const options = {
    body: 'on it',
    tag: 'agentrq-t1',
    renotify: true,
    icon: '/arq/pwa-192x192.png',
    badge: '/arq/pwa-192x192.png',
    data: { url: '/arq/workspaces/w1/tasks/t1' },
  };

  afterEach(() => vi.restoreAllMocks());

  // Through the worker, so its click handler focuses the tab or opens the
  // task, as it does for a push notification.
  it('shows it through the service worker when there is one', async () => {
    const showNotification = vi.fn().mockResolvedValue(undefined);
    const serviceWorker = { getRegistration: vi.fn().mockResolvedValue({ showNotification }) };

    await showSystemNotification(notification, { serviceWorker, basePath: '/arq/' });

    expect(showNotification).toHaveBeenCalledWith('Ship it', options);
  });

  it('falls back to a plain notification that opens the task on a click', async () => {
    const made = [];
    class Notify {
      constructor(title, opts) { made.push([title, opts]); made.push(this); }
    }
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => {});
    const assign = vi.fn();
    vi.spyOn(window, 'location', 'get').mockReturnValue({ assign });

    await showSystemNotification(notification, { serviceWorker: { getRegistration: async () => undefined }, Notify, basePath: '/arq' });
    expect(made[0]).toEqual(['Ship it', options]);

    made[1].onclick();
    expect(focus).toHaveBeenCalled();
    expect(assign).toHaveBeenCalledWith('/arq/workspaces/w1/tasks/t1');
  });

  it('copes with a browser that has no service worker at all', async () => {
    const Notify = vi.fn();
    await showSystemNotification(notification, { serviceWorker: undefined, Notify });
    expect(Notify).toHaveBeenCalledWith('Ship it', expect.objectContaining({ icon: '/pwa-192x192.png' }));
  });

  it('reads the browser itself by default', async () => {
    const Notify = vi.fn();
    vi.stubGlobal('Notification', Notify);
    await showSystemNotification(notification);
    expect(Notify).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
  });
});

describe('createHiddenTabNotifier', () => {
  const notifier = (over = {}) => {
    const deps = {
      isHidden: () => true,
      permission: () => 'granted',
      isPushActive: vi.fn().mockResolvedValue(false),
      show: vi.fn().mockResolvedValue(undefined),
      ...names,
      ...over,
    };
    return { notify: createHiddenTabNotifier(deps), deps };
  };

  it('raises the notification for a hidden tab with permission and no Web Push', async () => {
    const { notify, deps } = notifier();
    expect(await notify(moved)).toBe(true);
    expect(deps.isPushActive).toHaveBeenCalledWith('w1');
    expect(deps.show).toHaveBeenCalledWith(expect.objectContaining({ title: 'Ship it', url: '/workspaces/w1/tasks/t1' }));
  });

  it('leaves a visible tab to its toast', async () => {
    const { notify, deps } = notifier({ isHidden: () => false });
    expect(await notify(moved)).toBe(false);
    expect(deps.show).not.toHaveBeenCalled();
  });

  // It never asks: a permission prompt out of nowhere, in a background tab, is
  // exactly the interruption nobody agreed to.
  it('uses only a permission already given', async () => {
    for (const permission of ['default', 'denied']) {
      const { notify, deps } = notifier({ permission: () => permission });
      expect(await notify(moved)).toBe(false);
      expect(deps.show).not.toHaveBeenCalled();
    }
  });

  it('stays out of the way of Web Push, which brings the same news', async () => {
    const { notify, deps } = notifier({ isPushActive: vi.fn().mockResolvedValue(true) });
    expect(await notify(moved)).toBe(false);
    expect(deps.show).not.toHaveBeenCalled();
  });

  it('does not ask about Web Push for news with no workspace, or for no news', async () => {
    const { notify, deps } = notifier();
    expect(await notify(sessionFailed)).toBe(true);
    expect(await notify({ type: 'agent.connected', payload: {} })).toBe(false);
    expect(deps.isPushActive).not.toHaveBeenCalled();
  });
});
