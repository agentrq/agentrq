import { defineStore } from 'pinia';
import { ref } from 'vue';
import { fetchWorkspaces as apiFetchWorkspaces } from '../api';

export const useWorkspaceStore = defineStore('workspace', () => {
  const workspaces = ref([]);
  const loading = ref(false);

  async function fetchWorkspaces() {
    loading.value = true;
    try {
      const res = await apiFetchWorkspaces();
      const list = res.workspaces || [];
      workspaces.value = list.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }));
    } catch (err) {
      console.error('Failed to fetch workspaces:', err);
    } finally {
      loading.value = false;
    }
  }

  function updateWorkspaceMetadata(updatedWs) {
    const idx = workspaces.value.findIndex(w => w.id == updatedWs.id);
    if (idx !== -1) {
      workspaces.value[idx] = { ...workspaces.value[idx], ...updatedWs };
      workspaces.value.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }));
    }
  }

  function updateAgentStatus(workspaceId, connected) {
    const ws = workspaces.value.find(w => w.id == workspaceId);
    if (ws) {
      ws.agentConnected = connected;
    }
  }

  return {
    workspaces,
    loading,
    fetchWorkspaces,
    updateWorkspaceMetadata,
    updateAgentStatus
  };
});
