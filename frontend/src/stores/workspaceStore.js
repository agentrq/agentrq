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
      workspaces.value = res.workspaces || [];
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
