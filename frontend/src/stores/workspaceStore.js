import { defineStore } from 'pinia';
import { ref } from 'vue';
import { fetchWorkspaces as apiFetchWorkspaces } from '../api';

/**
 * The one copy of the workspace list every view reads.
 *
 * `agentConnected` in particular has to live in exactly one place. It changes
 * from outside the app — an agent attaches or drops its MCP stream — and five
 * separate surfaces render it: the sidebar dot, the Overview cards, the task
 * list rows, the workspace header, and the reply box's disabled state. When a
 * view kept its own copy, the copy it fetched on mount was the only value it
 * ever showed, so the indicator went stale the moment an agent connected and
 * stayed stale until that view was mounted again.
 */
export const useWorkspaceStore = defineStore('workspace', () => {
  const workspaces = ref([]);
  const loading = ref(false);

  const byName = (a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });

  async function fetchWorkspaces() {
    loading.value = true;
    try {
      const res = await apiFetchWorkspaces();
      const list = res.workspaces || [];
      workspaces.value = list.sort(byName);
    } catch (err) {
      console.error('Failed to fetch workspaces:', err);
    } finally {
      loading.value = false;
    }
  }

  function updateWorkspaceMetadata(updatedWs) {
    const idx = findIndex(updatedWs?.id);
    if (idx !== -1) {
      workspaces.value[idx] = { ...workspaces.value[idx], ...updatedWs };
      workspaces.value.sort(byName);
    }
  }

  /**
   * Record whether a workspace's agent is attached.
   *
   * IDs are compared as strings because they arrive from two directions: a
   * route parameter is always a string, while a payload field is whatever the
   * backend serialised. Coercing both sides is what keeps a mismatch from
   * silently dropping the update, which is how this indicator broke before.
   */
  function updateAgentStatus(workspaceId, connected) {
    const idx = findIndex(workspaceId);
    if (idx !== -1) {
      workspaces.value[idx] = { ...workspaces.value[idx], agentConnected: connected };
    }
  }

  /** The workspace with this ID, or undefined. */
  function getWorkspace(workspaceId) {
    const idx = findIndex(workspaceId);
    return idx === -1 ? undefined : workspaces.value[idx];
  }

  /** Whether this workspace's agent is attached right now. Unknown reads false. */
  function isAgentConnected(workspaceId) {
    return getWorkspace(workspaceId)?.agentConnected === true;
  }

  function findIndex(workspaceId) {
    if (workspaceId === undefined || workspaceId === null) return -1;
    const wanted = String(workspaceId);
    return workspaces.value.findIndex(w => String(w.id) === wanted);
  }

  return {
    workspaces,
    loading,
    fetchWorkspaces,
    updateWorkspaceMetadata,
    updateAgentStatus,
    getWorkspace,
    isAgentConnected
  };
});
