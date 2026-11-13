<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- "Clone to my workspace" on a teammate's task: pick one of your workspaces
     (the one named like the task's workspace or group comes first) and how
     the copy starts. The copy is yours; the original is not touched. -->
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
          <label class="block space-y-1">
            <span class="text-[10px] font-bold uppercase tracking-widest text-gray-500 dark:text-zinc-400">Workspace</span>
            <select v-model="workspaceId" data-test="clone-workspace"
                    class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 outline-none">
              <option v-for="w in mine" :key="w.id" :value="String(w.id)">{{ w.name }}</option>
            </select>
          </label>
          <label class="block space-y-1">
            <span class="text-[10px] font-bold uppercase tracking-widest text-gray-500 dark:text-zinc-400">Start as</span>
            <select v-model="status" data-test="clone-status"
                    class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 outline-none">
              <option value="notstarted">Not started</option>
              <option value="ongoing">Ongoing</option>
            </select>
          </label>
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

const props = defineProps({
  show: { type: Boolean, default: false },
  teamId: { type: String, default: '' },
  task: { type: Object, default: null },
  // The task's workspace as the team sees it: { name, group }.
  workspace: { type: Object, default: null },
});
const emit = defineEmits(['close']);

const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();

const mine = computed(() => workspaceStore.workspaces.filter((w) => !w.archivedAt));
const workspaceId = ref('');
const status = ref('notstarted');
const busy = ref(false);

watch(() => props.show, async (open) => {
  if (!open) return;
  status.value = 'notstarted';
  // The store reports its own failures: the picker then offers to make one.
  if (workspaceStore.workspaces.length === 0) await workspaceStore.fetchWorkspaces();
  workspaceId.value = preferredWorkspace(mine.value, { workspaceName: props.workspace?.name, group: props.workspace?.group });
}, { immediate: true });

async function clone() {
  busy.value = true;
  try {
    const res = await cloneTeamTask(props.teamId, props.task.id, workspaceId.value, status.value);
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
