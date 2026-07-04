import { ref, onUnmounted, unref } from 'vue';

// Shared across all useEventBus instances — ensures only one auth check fires at a time
// regardless of how many SSE connections error simultaneously.
let sharedAuthCheckPromise = null;

async function checkAuth() {
  if (sharedAuthCheckPromise) return sharedAuthCheckPromise;
  sharedAuthCheckPromise = fetch('/api/v1/auth/user')
    .then(res => res.status)
    .catch(() => null)
    .finally(() => { sharedAuthCheckPromise = null; });
  return sharedAuthCheckPromise;
}

export function useEventBus(workspaceId) {
  const events = ref([]);
  const isConnected = ref(false);
  let eventSource = null;
  let reconnectDelay = 1000;

  function connect() {
    if (eventSource) return;

    events.value = [];
    const wsId = unref(workspaceId);
    const url = wsId ? `/api/v1/workspaces/${wsId}/events` : `/api/v1/events/stream`;
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

      const status = await checkAuth();
      if (status === 401) {
        console.warn('Not authenticated. Stopping EventSource reconnection and redirecting to login.');
        if (window.location.pathname !== '/login') {
          window.location.href = '/login';
        }
        return;
      }

      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    };

    eventSource.onmessage = (e) => {
      try {
        const payload = JSON.parse(e.data);
        events.value.push(payload);
      } catch (err) {
        console.error('Error parsing SSE data', err, e.data);
      }
    };
  }

  function disconnect() {
    if (eventSource) {
      eventSource.close();
      eventSource = null;
      isConnected.value = false;
    }
  }

  onUnmounted(() => {
    disconnect();
  });

  return { connect, disconnect, events, isConnected };
}
