<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Confirms giving one of your skills to a team workspace, the workspace's
     only one or the one picked here. The skill moves: it stops being yours.
     A refusal (a name the team workspace has, a full one) stays on screen in
     the server's words, so another can be picked. -->
<template>
  <Teleport to="body">
    <div v-if="show" data-test="give-skill-modal" class="fixed inset-0 z-[100] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="give-skill-title">
      <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" aria-hidden="true" @click="$emit('close')"></div>
      <div class="relative z-[110] w-full max-w-md bg-white dark:bg-zinc-900 rounded-sm border border-gray-100 dark:border-zinc-800 shadow-2xl">
        <div class="p-6 space-y-3">
          <h3 id="give-skill-title" class="text-lg font-bold text-gray-900 dark:text-zinc-50 tracking-tight">Give to team</h3>
          <p class="text-[13px] leading-relaxed text-gray-500 dark:text-zinc-400">
            <span class="font-mono font-semibold text-gray-900 dark:text-white">{{ name }}</span>
            moves out of your skills and belongs to the team workspace<template v-if="teamWorkspaces.length === 1">
            <span class="font-semibold text-gray-900 dark:text-white">{{ teamWorkspaces[0].name }}</span> in {{ teamWorkspaces[0].teamName }}</template>.
            Every teammate can then read and change it.
          </p>
          <label v-if="teamWorkspaces.length > 1" class="block">
            <span class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">Team workspace</span>
            <select v-model="picked" data-test="give-skill-target"
                    class="w-full px-3 py-2 text-xs bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500">
              <option v-for="t in teamWorkspaces" :key="t.id" :value="t.id">{{ t.name }} · {{ t.teamName }}</option>
            </select>
          </label>
          <p v-if="error" data-test="give-skill-error" role="alert" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ error }}</p>
        </div>
        <div class="px-6 py-4 bg-gray-50/50 dark:bg-zinc-800/50 border-t border-gray-100 dark:border-zinc-800 flex flex-row-reverse gap-3">
          <button type="button" data-test="give-skill-confirm" :disabled="busy || !picked" @click="$emit('confirm', picked)"
                  class="px-5 py-2 rounded-sm bg-black dark:bg-white text-white dark:text-black text-[10px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">
            {{ busy ? 'Giving…' : 'Give' }}
          </button>
          <button type="button" data-test="give-skill-cancel" @click="$emit('close')"
                  class="px-5 py-2 rounded-sm border border-gray-200 dark:border-zinc-700 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800">
            Cancel
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue';

const props = defineProps({
  show: { type: Boolean, default: false },
  name: { type: String, default: '' },
  // [{ id, name, teamId, teamName }]
  teamWorkspaces: { type: Array, default: () => [] },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' },
});
defineEmits(['close', 'confirm']);

// The first one to start with, each time it opens.
const picked = ref('');
watch(() => [props.show, props.teamWorkspaces], () => {
  if (props.show && !props.teamWorkspaces.some((t) => t.id === picked.value)) picked.value = props.teamWorkspaces[0]?.id ?? '';
}, { immediate: true });
</script>
