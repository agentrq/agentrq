<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's memory: its own memory.md, which any member may edit (the last
     save wins, and a save over a newer one says so instead of overwriting
     it), and the memories of the members' shared workspaces, read-only. -->
<template>
  <div class="flex-1 min-w-0 flex flex-col h-full page-surface overflow-hidden px-4">
    <div class="flex-1 overflow-y-auto custom-scrollbar space-y-8 pb-6">
      <section class="space-y-3">
        <div class="flex items-center gap-3">
          <h1 class="text-lg font-black text-gray-900 dark:text-white">Team memory</h1>
          <span v-if="updatedAt" class="text-[11px] text-gray-400 dark:text-zinc-500">Saved {{ new Date(updatedAt).toLocaleString() }}</span>
        </div>
        <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">What the team's agents read first. Any member may edit it.</p>
        <p v-if="loadError" data-test="team-memory-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ loadError }}</p>
        <template v-else>
          <textarea v-model="draft" data-test="team-memory-editor" rows="14" aria-label="Team memory.md" :disabled="loading"
                    class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm p-3 text-[12px] font-mono text-gray-900 dark:text-zinc-100 outline-none focus:border-gray-900 dark:focus:border-white"></textarea>
          <div v-if="conflict" data-test="team-memory-conflict" role="alert"
               class="flex flex-wrap items-center gap-3 p-3 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/20 rounded-sm text-[11px] text-amber-800 dark:text-amber-200">
            <span class="flex-1 min-w-0">Someone saved it since you opened it. Load theirs to see it, or save again to replace it.</span>
            <button type="button" data-test="team-memory-reload" @click="load" class="font-black uppercase tracking-widest underline">Load theirs</button>
          </div>
          <div class="flex justify-end">
            <button type="button" data-test="team-memory-save" :disabled="saving || loading || draft === saved" @click="save"
                    class="px-6 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">
              {{ saving ? 'Saving…' : 'Save' }}
            </button>
          </div>
        </template>
      </section>

      <section class="space-y-3">
        <h2 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">Members' memories</h2>
        <p v-if="index.length === 0" data-test="team-memory-index-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">No shared workspace has memories yet.</p>
        <div v-for="ws in index" :key="ws.workspaceId" data-test="team-memory-workspace" class="space-y-1.5">
          <div class="flex items-center gap-2 text-xs font-bold text-gray-800 dark:text-zinc-100">
            <OwnerAvatar :owner="ws.owner" />
            <span class="truncate">{{ ws.group || ws.workspaceName }}</span>
            <span v-if="ws.group && ws.group !== ws.workspaceName" class="text-gray-400 dark:text-zinc-500 font-medium truncate">{{ ws.workspaceName }}</span>
          </div>
          <ul class="flex flex-wrap gap-x-4 gap-y-1 pl-7">
            <li v-for="name in ws.names || []" :key="name">
              <button type="button" data-test="team-memory-item" @click="open(ws, name)"
                      class="text-[11px] font-mono text-gray-600 dark:text-zinc-300 underline decoration-dotted underline-offset-2 hover:text-gray-900 dark:hover:text-zinc-100">{{ name }}</button>
            </li>
          </ul>
        </div>
        <div v-if="opened" data-test="team-memory-opened" class="mt-4 p-4 border border-gray-100 dark:border-zinc-800 rounded-sm space-y-2">
          <div class="flex items-center gap-2">
            <h3 class="flex-1 min-w-0 truncate text-xs font-bold font-mono text-gray-800 dark:text-zinc-100">{{ opened.name }}</h3>
            <button type="button" @click="opened = null" class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">Close</button>
          </div>
          <p v-if="opened.error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ opened.error }}</p>
          <div v-else class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(opened.content)"></div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue';
import { fetchTeamMemoryIndex, getTeamMemberMemory, getTeamMemory, saveTeamMemory } from '../api';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import { renderMarkdown } from '../utils/markdown';
import { useToasts } from '../composables/useToasts';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';

const { notifySuccess, notifyError } = useToasts();
const { teamId, leaveTeam } = useTeamMode();

const draft = ref('');
const saved = ref('');
const updatedAt = ref('');
const loading = ref(true);
const saving = ref(false);
const conflict = ref(false);
const loadError = ref('');
const index = ref([]);
const opened = ref(null);

function failed(err) {
  if (lostTeam(err)) return leaveTeam();
  notifyError(err.message);
}

async function load() {
  loading.value = true;
  conflict.value = false;
  loadError.value = '';
  try {
    const res = await getTeamMemory(teamId.value);
    draft.value = saved.value = res?.content ?? '';
    updatedAt.value = res?.updatedAt ?? '';
  } catch (err) {
    if (lostTeam(err)) return leaveTeam();
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
}

async function loadIndex() {
  try {
    index.value = (await fetchTeamMemoryIndex(teamId.value))?.memories ?? [];
  } catch (err) {
    failed(err);
  }
}

// Saving again over a conflict replaces theirs: the human has seen the notice.
async function save() {
  saving.value = true;
  try {
    const res = await saveTeamMemory(teamId.value, draft.value, conflict.value ? '' : updatedAt.value);
    saved.value = draft.value;
    updatedAt.value = res?.updatedAt ?? updatedAt.value;
    conflict.value = false;
    notifySuccess('Team memory saved');
  } catch (err) {
    if (err.status === 409) conflict.value = true;
    else failed(err);
  } finally {
    saving.value = false;
  }
}

async function open(ws, name) {
  opened.value = { name, content: '', error: '' };
  try {
    const res = await getTeamMemberMemory(teamId.value, ws.workspaceId, name);
    opened.value = { name, content: res?.content ?? '', error: '' };
  } catch (err) {
    opened.value = { name, content: '', error: err.message };
  }
}

onMounted(() => {
  load();
  loadIndex();
});
</script>
