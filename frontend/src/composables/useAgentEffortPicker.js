// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref, watch } from 'vue';

import { CONFIRMATION_TIMEOUT_MS } from './useAgentModelPicker';

/**
 * Choosing the reasoning level the workspace's agent runs at, the agent's
 * `thought_level` option. It follows the model picker's rules: the agent's own
 * report ends a pending choice, and the levels are only the ones it offers for
 * the model it is running now.
 */

/** Whether the connected agent offers reasoning levels it will act on. */
export function canChooseEffort(workspace) {
  if (!workspace?.agentConnected) return false;
  const effort = workspace?.agentEffort;
  return effort?.canSet === true && Array.isArray(effort.efforts) && effort.efforts.length > 0;
}

/** The display name of a level, falling back to its id. */
export function effortName(agentEffort, id) {
  const level = agentEffort?.efforts?.find((e) => e?.id === id);
  return level?.name || id || '';
}

export function useAgentEffortPicker({ workspace, selectEffort, now = () => Date.now(), timeoutMs = CONFIRMATION_TIMEOUT_MS }) {
  const pending = ref('');
  const requestedAt = ref(0);
  const error = ref('');
  const sending = ref(false);

  const agentEffort = computed(() => workspace()?.agentEffort);
  const available = computed(() => canChooseEffort(workspace()));
  const efforts = computed(() => (agentEffort.value?.efforts || []).filter((e) => e?.id));
  const selectedId = computed(() => pending.value || agentEffort.value?.currentEffort || '');
  const selectedName = computed(() => effortName(agentEffort.value, selectedId.value));
  const isPending = computed(() => pending.value !== '');

  // Any report settles a pending choice: one that names it confirms it, and
  // one that names another level is the agent saying what it runs at now.
  watch(agentEffort, () => {
    if (!pending.value) return;
    const current = agentEffort.value?.currentEffort;
    if (current && current !== pending.value) {
      error.value = `The agent is running at ${effortName(agentEffort.value, current)}`;
    }
    pending.value = '';
  });

  function expirePending() {
    if (!pending.value || now() - requestedAt.value < timeoutMs) return;
    pending.value = '';
    error.value = 'The agent did not confirm the change';
  }

  async function choose(id) {
    error.value = '';
    if (!id) return;
    if (id === agentEffort.value?.currentEffort) {
      pending.value = '';
      return;
    }
    pending.value = id;
    requestedAt.value = now();
    sending.value = true;
    try {
      await selectEffort(id);
    } catch (err) {
      pending.value = '';
      error.value = err?.message || 'Could not change the reasoning level';
    } finally {
      sending.value = false;
    }
  }

  return { available, efforts, selectedId, selectedName, isPending, sending, error, choose, expirePending };
}
