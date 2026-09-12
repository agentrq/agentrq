// Copyright 2026 Contextual, Inc. https://agentrq.com

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

  /**
   * Record the slash commands a workspace's agent is offering.
   *
   * Lives here for the same reason `agentConnected` does: it changes from
   * outside the app — an agent advertises its commands once its session is up,
   * long after any page load — and the composer is not the only surface that
   * will want them. IDs are compared as strings for the reason `findIndex`
   * gives.
   */
  function updateAgentCommands(workspaceId, commands) {
    const idx = findIndex(workspaceId);
    if (idx !== -1) {
      const agentCommands = Array.isArray(commands) && commands.length > 0 ? { commands } : undefined;
      workspaces.value[idx] = { ...workspaces.value[idx], agentCommands };
    }
  }

  /**
   * Record the models a workspace's agent is offering.
   *
   * Lives here for the same reason `agentCommands` does: it changes from
   * outside the app — an agent reports its models when its session comes up,
   * and again every time one is switched — and the name on the Overview card
   * was otherwise fixed at whatever the last page load fetched. IDs are
   * compared as strings for the reason `findIndex` gives.
   *
   * An empty list clears the field rather than storing an empty object, so
   * `agentModels` keeps meaning "there is a choice here" — which is what the
   * REST payload means by omitting it, and what every reader already assumes.
   */
  function updateAgentModels(workspaceId, models) {
    const idx = findIndex(workspaceId);
    if (idx !== -1) {
      const list = Array.isArray(models?.models) ? models.models : [];
      const agentModels = list.length > 0
        ? {
            configId: models.configId,
            currentModel: models.currentModel,
            // Carried through explicitly, and defaulted to false rather than
            // left undefined: this is what decides whether a picker is offered
            // at all, and an event that dropped it would take the picker away
            // from an agent that can still be told to switch.
            canSet: models.canSet === true,
            models: list,
          }
        : undefined;
      workspaces.value[idx] = { ...workspaces.value[idx], agentModels };
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
    updateAgentCommands,
    updateAgentModels,
    getWorkspace,
    isAgentConnected
  };
});
