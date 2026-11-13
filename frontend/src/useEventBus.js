// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, onUnmounted, unref, getCurrentInstance } from 'vue';
import { API_BASE_URL } from './api';
import { apiBreaker } from './utils/circuitBreaker';

// Shared across all useEventBus instances — ensures only one auth check fires at a time
// regardless of how many SSE connections error simultaneously.
let sharedAuthCheckPromise = null;

async function checkAuth() {
  if (sharedAuthCheckPromise) return sharedAuthCheckPromise;
  sharedAuthCheckPromise = fetch(`${API_BASE_URL}/auth/user`)
    .then(res => res.status)
    .catch(() => null)
    .finally(() => { sharedAuthCheckPromise = null; });
  return sharedAuthCheckPromise;
}

/**
 * Subscribe to the server's event stream.
 *
 * Two ways to read it, and they are not interchangeable:
 *
 * - `onEvent(fn)` delivers every event exactly once, as it arrives. Anything
 *   that reacts to a *transition* — an agent connecting, a task being deleted —
 *   has to use this. A Vue watcher on the array below is scheduled, so two
 *   events landing in one flush collapse into a single callback and a handler
 *   reading `events[events.length - 1]` silently loses the earlier one.
 * - `events` is the running buffer, for views that re-render from the whole
 *   list rather than acting on each item.
 *
 * `buffer: false` skips the buffer for long-lived subscribers. The shell holds
 * its stream open for the life of the app — days, on the desktop build — and an
 * array nobody reads would grow without bound, making every deep watcher over
 * it a little slower for every event that had ever arrived.
 *
 * `isPublicPage` keeps a signed-out visitor on a page that offers its own
 * sign-in (an invitation link) instead of sending them to the login page.
 *
 * @param {string | import('vue').Ref<string>} [workspaceId]  scope to one workspace; omit for the user-wide stream
 * @param {{ buffer?: boolean, isPublicPage?: () => boolean }} [options]
 */
export function useEventBus(workspaceId, { buffer = true, isPublicPage = () => false } = {}) {
  const events = ref([]);
  const isConnected = ref(false);
  const handlers = new Set();
  let eventSource = null;
  let reconnectDelay = 1000;
  // Set between connect() and disconnect(): a reconnect is only scheduled,
  // and a scheduled one only kept, while the caller still wants the stream.
  let wanted = false;
  let reconnectTimer = null;

  function reconnectIn(ms) {
    if (!wanted) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, ms);
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
  }

  /**
   * Call `fn` for each event from now on. Returns an unsubscribe function; the
   * handler is also dropped when the owning component unmounts.
   */
  function onEvent(fn) {
    handlers.add(fn);
    const off = () => handlers.delete(fn);
    if (getCurrentInstance()) onUnmounted(off);
    return off;
  }

  function connect() {
    wanted = true;
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
    if (eventSource) return;

    events.value = [];
    const wsId = unref(workspaceId);
    const cleanBase = (window.__AGENTRQ_BASE_PATH__ || '').replace(/\/$/, '');
    const url = wsId ? `${cleanBase}/api/v1/workspaces/${wsId}/events` : `${cleanBase}/api/v1/events/stream`;
    eventSource = new EventSource(url);

    eventSource.onopen = () => {
      isConnected.value = true;
      reconnectDelay = 1000;
    };

    eventSource.onerror = async (error) => {
      console.error('EventSource failed:', error);
      isConnected.value = false;
      eventSource.close();
      eventSource = null;

      // While the API is backing off, so does the stream: no auth check, and
      // no reconnect before the cooldown is over.
      const busyFor = apiBreaker.retryIn();
      if (busyFor > 0) {
        reconnectIn(Math.max(reconnectDelay, busyFor));
        return;
      }

      const status = await checkAuth();
      if (status === 401) {
        console.warn('Not authenticated. Stopping EventSource reconnection and redirecting to login.');
        const cleanBase = (window.__AGENTRQ_BASE_PATH__ || '').replace(/\/$/, '');
        const loginPath = cleanBase ? `${cleanBase}/login` : '/login';
        if (window.location.pathname !== loginPath && !isPublicPage()) {
          window.location.href = loginPath;
        }
        return;
      }

      reconnectIn(reconnectDelay);
    };

    eventSource.onmessage = (e) => {
      let payload;
      try {
        payload = JSON.parse(e.data);
      } catch (err) {
        console.error('Error parsing SSE data', err, e.data);
        return;
      }
      if (buffer) events.value.push(payload);
      // One bad handler must not stop the others from seeing the event.
      for (const fn of handlers) {
        try {
          fn(payload);
        } catch (err) {
          console.error('SSE handler failed', err, payload);
        }
      }
    };
  }

  function disconnect() {
    wanted = false;
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
    if (eventSource) {
      eventSource.close();
      eventSource = null;
      isConnected.value = false;
    }
  }

  onUnmounted(() => {
    disconnect();
  });

  return { connect, disconnect, events, isConnected, onEvent };
}
