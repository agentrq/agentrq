// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * System notifications from the live stream, for a tab nobody is looking at.
 *
 * A toast is drawn inside the page, so with the tab in the background it is
 * news nobody sees. Web Push covers that, but only for a workspace this browser
 * has subscribed to, on a server with VAPID keys. Otherwise, with the
 * permission already given, the page raises the notification itself.
 *
 * What counts as news is `toastFor`'s decision, so a toast and a notification
 * can never disagree about it. Web only: the desktop shell raises its own.
 */

import { toastFor } from './useStreamToasts';

/**
 * Where a notification opens: the task, else its workspace, else the app.
 * Under the base path, since the service worker opens it, not the router.
 */
export function notificationUrl({ taskId, workspaceId }, basePath = '') {
  const base = String(basePath).replace(/\/$/, '');
  if (workspaceId && taskId) return `${base}/workspaces/${workspaceId}/tasks/${taskId}`;
  if (workspaceId) return `${base}/workspaces/${workspaceId}`;
  return `${base}/`;
}

/**
 * The system notification an event deserves, or null.
 *
 * No open task is passed on: with the tab hidden, a reply on the task on
 * screen is as unseen as any other.
 */
export function systemNotificationFor(event, { workspaceName = () => '', basePath = '' } = {}) {
  const toast = toastFor(event, { platform: 'web' });
  if (!toast) return null;
  const workspace = workspaceName(toast.workspaceId);
  return {
    title: toast.title || workspace || 'AgentRQ',
    body: workspace && toast.title ? `${workspace}: ${toast.message}` : toast.message,
    // One per task: a newer reply replaces the last rather than piling up.
    tag: `agentrq-${toast.taskId || toast.workspaceId || toast.kind}`,
    url: notificationUrl(toast, basePath),
    workspaceId: toast.workspaceId,
  };
}

/**
 * Shows a notification through the service worker, so a click is handled the
 * way a push notification's is; without one, as a plain browser notification.
 */
export async function showSystemNotification(notification, { serviceWorker = navigator.serviceWorker, Notify = window.Notification, basePath = '' } = {}) {
  const icon = `${String(basePath).replace(/\/$/, '')}/pwa-192x192.png`;
  const options = { body: notification.body, tag: notification.tag, renotify: true, icon, badge: icon, data: { url: notification.url } };
  const registration = await serviceWorker?.getRegistration?.();
  if (registration) return registration.showNotification(notification.title, options);
  const shown = new Notify(notification.title, options);
  shown.onclick = () => {
    window.focus();
    window.location.assign(notification.url);
  };
}

/**
 * The stream handler. Resolves true when it raised a notification.
 *
 * @param {object} deps
 * @param {() => boolean} deps.isHidden            the tab is in the background
 * @param {() => string} deps.permission           `Notification.permission`
 * @param {(workspaceId: string) => Promise<boolean>} deps.isPushActive
 *        Web Push already delivers this workspace's news to this browser
 * @param {(n: object) => Promise<unknown>} deps.show
 * @param {(id: string) => string} [deps.workspaceName]
 * @param {string} [deps.basePath]
 */
export function createHiddenTabNotifier({ isHidden, permission, isPushActive, show, workspaceName, basePath }) {
  return async (event) => {
    if (!isHidden() || permission() !== 'granted') return false;
    const notification = systemNotificationFor(event, { workspaceName, basePath });
    if (!notification) return false;
    if (notification.workspaceId && (await isPushActive(notification.workspaceId))) return false;
    await show(notification);
    return true;
  };
}
