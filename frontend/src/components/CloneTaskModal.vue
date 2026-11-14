<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- "Clone to my workspace" on a teammate's task: pick one of your workspaces
     (the one named like the task's workspace, or the team workspace it is in,
     comes first) and who the copy is assigned to, a human unless you say an
     agent. The copy is yours; the original is not touched. -->
<template>
  <Teleport to="body">
    <div v-if="show" class="fixed inset-0 z-[120] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="clone-task-title" data-test="clone-modal">
      <div class="absolute inset-0 bg-gray-900/60 backdrop-blur-sm" @click="$emit('close')"></div>
      <form @submit.prevent="clone" class="relative w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-2xl p-6 sm:p-8 space-y-4">
        <h3 id="clone-task-title" class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight">Clone to my workspace</h3>
        <p class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
          A copy of <span class="font-semibold text-gray-900 dark:text-white">{{ task?.title }}</span>, with its attachments, in a workspace of yours.
        </p>
        <p v-if="mine.length === 0" data-test="clone-no-workspace" class="text-[12px] text-gray-500 dark:text-zinc-400">
          You have no workspace to clone into. <router-link to="/workspaces/new" class="font-semibold underline text-gray-900 dark:text-white" @click="$emit('close')">Create one</router-link> first.
        </p>
        <template v-else>
          <div class="space-y-1">
            <span class="block text-[10px] font-bold uppercase tracking-widest text-gray-500 dark:text-zinc-400">Workspace</span>
            <ItemPicker v-model="workspaceId" :items="workspaceItems" block data-test="clone-workspace" aria-label="Workspace" />
          </div>
          <div class="space-y-1">
            <span id="clone-assignee-label" class="block text-[10px] font-bold uppercase tracking-widest text-gray-500 dark:text-zinc-400">Assign to</span>
            <div role="radiogroup" aria-labelledby="clone-assignee-label" data-test="clone-assignee"
                 class="flex p-0.5 bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700/50 rounded-lg">
              <button v-for="a in ASSIGNEES" :key="a.value" type="button" role="radio" :aria-checked="assignee === a.value"
                      :data-test="`clone-assignee-${a.value}`" @click="assignee = a.value"
                      :class="assignee === a.value ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-200'"
                      class="flex-1 py-1.5 rounded-md text-[10px] font-black uppercase tracking-widest transition-all">
                {{ a.label }}
              </button>
            </div>
          </div>
        </template>
        <div class="flex justify-end gap-2">
          <button type="button" @click="$emit('close')"
                  class="rounded-sm border border-gray-200 dark:border-zinc-700 px-6 py-2.5 bg-white dark:bg-zinc-900 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800">
            Cancel
          </button>
          <button type="submit" data-test="clone-confirm" :disabled="busy || !workspaceId"
                  class="rounded-sm px-6 py-2.5 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 text-[10px] font-bold uppercase tracking-widest hover:opacity-90 disabled:opacity-40">
            {{ busy ? 'Cloning…' : 'Clone' }}
          </button>
        </div>
      </form>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, ref, watch } from 'vue';
import { cloneTeamTask } from '../api';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import { useEscapeKey } from '../composables/useEscapeKey';
import { preferredWorkspace } from '../composables/useTeamMode';
import ItemPicker from './ItemPicker.vue';

const ASSIGNEES = [{ value: 'human', label: 'Human' }, { value: 'agent', label: 'Agent' }];

const props = defineProps({
  show: { type: Boolean, default: false },
  teamId: { type: String, default: '' },
  task: { type: Object, default: null },
  // The task's workspace as the team sees it: { name, sharedName }.
  workspace: { type: Object, default: null },
});
const emit = defineEmits(['close']);

const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();

const mine = computed(() => workspaceStore.workspaces.filter((w) => !w.archivedAt));
const workspaceItems = computed(() => mine.value.map((w) => ({ value: String(w.id), label: w.name })));
const workspaceId = ref('');
const assignee = ref('human');
const busy = ref(false);

watch(() => props.show, async (open) => {
  if (!open) return;
  assignee.value = 'human';
  // The store reports its own failures: the picker then offers to make one.
  if (workspaceStore.workspaces.length === 0) await workspaceStore.fetchWorkspaces();
  workspaceId.value = preferredWorkspace(mine.value, { workspaceName: props.workspace?.name, sharedName: props.workspace?.sharedName });
}, { immediate: true });

async function clone() {
  busy.value = true;
  try {
    const res = await cloneTeamTask(props.teamId, props.task.id, workspaceId.value, assignee.value);
    const copy = res?.task ?? res ?? {};
    const wsId = String(copy.workspaceId ?? workspaceId.value);
    notifySuccess('Task cloned', null, copy.id ? { taskId: String(copy.id), workspaceId: wsId, label: 'Open' } : null);
    emit('close');
  } catch (err) {
    notifyError(err.message);
  } finally {
    busy.value = false;
  }
}

useEscapeKey(() => {
  if (!props.show) return false;
  emit('close');
});
</script>
