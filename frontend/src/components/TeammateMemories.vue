<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Workspace settings › Sharing: the teammates in this workspace's team
     workspace, and whether its agent's memory index lists each one's memory.
     Everyone starts included; turning one off changes only your agent's
     index, never what the teammate sees. Nothing shows until the workspace
     has a teammate. -->
<template>
  <div v-if="teammates.length > 0" class="space-y-4" data-test="teammate-memories">
    <div class="space-y-1">
      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Teammates' memory your agent reads</h3>
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium ml-1">Your agent's memory index lists each teammate's memory in this team workspace. Turn one off to leave it out; they are not told.</p>
    </div>
    <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="t in teammates" :key="t.workspaceId" data-test="teammate-memory-row" class="py-3 flex items-center gap-3">
        <OwnerAvatar :owner="t" size="md" />
        <div class="flex-1 min-w-0">
          <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ t.name || 'A member' }}</p>
          <p data-test="teammate-memory-updated" class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium" :title="t.updatedAt || ''">
            {{ t.updatedAt ? `Updated ${timeAgo(t.updatedAt)}` : 'No memory yet' }}
          </p>
        </div>
        <button type="button" role="switch" data-test="teammate-memory-switch" :aria-checked="t.included"
                :aria-label="`Your agent reads ${t.name || 'this member'}'s memory`" :disabled="pending.has(t.userId)"
                @click="toggle(t)"
                class="shrink-0 w-11 h-6 rounded-full border transition-colors relative disabled:opacity-50"
                :class="t.included ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
          <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                :class="t.included ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
        </button>
      </li>
    </ul>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue';
import { listTeammateMemories, setTeammateMemory } from '../api';
import { useToasts } from '../composables/useToasts';
import { timeAgo } from '../composables/useTeamMode';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import OwnerAvatar from './OwnerAvatar.vue';

const props = defineProps({
  workspaceId: { type: String, required: true },
});

const { notifyError } = useToasts();

const teammates = ref([]);
const pending = ref(new Set());

/** The teammates again; quiet, so a failure leaves the list as it was. */
async function reload() {
  try {
    const res = await listTeammateMemories(props.workspaceId);
    teammates.value = res?.teammates || [];
  } catch (err) {
    notifyError(err.message);
  }
}

/** A teammate's rows: one per workspace of theirs in the team workspace. */
function setIncluded(userId, included) {
  for (const row of teammates.value) if (row.userId === userId) row.included = included;
}

/** Flips one teammate at once, every row of theirs, and back if the server refuses. */
async function toggle(t) {
  const want = !t.included;
  setIncluded(t.userId, want);
  pending.value = new Set(pending.value).add(t.userId);
  try {
    await setTeammateMemory(props.workspaceId, t.userId, want);
  } catch (err) {
    setIncluded(t.userId, !want);
    notifyError(err.message);
  } finally {
    const next = new Set(pending.value);
    next.delete(t.userId);
    pending.value = next;
  }
}

defineExpose({ reload });
onMounted(reload);
watch(() => props.workspaceId, reload);
onWebMCPChange(reload);
</script>
