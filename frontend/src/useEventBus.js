import { ref, onUnmounted, unref } from 'vue';

export function useEventBus(workspaceId) {
  const events = ref([]);
  const isConnected = ref(false);
  let eventSource = null;

  function connect() {
    if (eventSource) return;
    
    const wsId = unref(workspaceId);
    const url = wsId ? `/api/v1/workspaces/${wsId}/events` : `/api/v1/events`;
    eventSource = new EventSource(url);
    
    eventSource.onopen = () => {
      isConnected.value = true;
    };
    
    eventSource.onerror = (error) => {
      console.error('EventSource failed:', error);
      isConnected.value = false;
      eventSource.close();
      eventSource = null;
      // Reconnect with backoff in real apps; simple 3s timeout here
      setTimeout(connect, 3000);
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
