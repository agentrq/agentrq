<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A workspace's presets: messages the composer puts in place of #name.
     Adding and removing apply at once; there is no Save. -->
<template>
  <div data-test="presets" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Presets</h3>
        <span class="text-[9px] font-bold text-gray-400 bg-gray-100 dark:bg-zinc-800 px-2 py-0.5 rounded border border-gray-200 dark:border-zinc-700 uppercase">{{ presets.length }} Saved</span>
      </div>
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 leading-relaxed px-1 font-medium">
        Type <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">#</code> and a preset's name in a task or reply, and its message takes the name's place.
      </p>

      <p v-if="error" data-test="presets-error" class="text-[11px] font-bold text-red-600 dark:text-red-400 ml-1">{{ error }}</p>

      <p v-if="loading && !presets.length" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading presets…</p>
      <div v-else-if="presets.length" class="grid grid-cols-1 gap-2">
        <div v-for="preset in presets" :key="preset.name" data-test="preset-row"
             class="flex items-start justify-between gap-3 p-4 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 hover:border-gray-900 dark:hover:border-white transition-all shadow-sm min-w-0">
          <div class="flex flex-col gap-1 min-w-0">
            <span class="text-xs font-bold text-gray-800 dark:text-zinc-200 font-mono">#{{ preset.name }}</span>
            <span class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium whitespace-pre-wrap break-words line-clamp-3">{{ preset.body }}</span>
          </div>
          <button type="button" data-test="preset-remove" :aria-label="`Remove #${preset.name}`" @click="remove(preset.name)"
                  class="shrink-0 text-gray-300 hover:text-red-500 transition-colors p-2 -m-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
          </button>
        </div>
      </div>
      <div v-else data-test="presets-empty" class="py-10 border-2 border-dashed border-gray-100 dark:border-zinc-800 rounded-sm flex flex-col items-center justify-center text-center px-8 bg-gray-50/30 dark:bg-zinc-900/30">
        <p class="text-[10px] font-bold text-gray-400 dark:text-zinc-600 uppercase tracking-widest">No presets yet</p>
        <p class="text-[11px] text-gray-500 dark:text-zinc-500 mt-2 font-medium">Add one below, like #pull-n-push for "pull latest main, tag and push".</p>
      </div>
    </div>

    <div class="space-y-4 pt-8 border-t border-gray-100 dark:border-zinc-800">
      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Add a Preset</h3>
      <div class="space-y-2">
        <label for="presetName" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Name</label>
        <input id="presetName" v-model="name" data-test="preset-name" type="text" spellcheck="false" autocapitalize="off" autocorrect="off"
               @blur="name = toKebabCase(name)" @keydown.enter.prevent="submit"
               class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-bold font-mono text-gray-900 dark:text-zinc-100 transition-all shadow-sm"
               placeholder="pull-n-push" />
        <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1">Lowercase letters and digits joined by hyphens, like skills.</p>
      </div>
      <div class="space-y-2">
        <label for="presetBody" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Message</label>
        <textarea id="presetBody" ref="bodyInput" v-model="body" data-test="preset-body" rows="4"
                  @keydown="skillMenu.handleKeydown"
                  class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none shadow-sm"
                  placeholder="pull latest main, tag and push"></textarea>
        <!-- The workspace's skills, offered as a word starts with `/`. -->
        <SlashCommandMenu :menu="skillMenu" @accept="skillMenu.accept" />
      </div>
      <button type="button" data-test="preset-add" :disabled="!canAdd" @click="submit"
              class="px-5 py-2.5 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 rounded-sm text-[10px] font-bold uppercase tracking-widest hover:bg-black dark:hover:bg-zinc-100 transition-all shadow-sm disabled:opacity-40 disabled:cursor-not-allowed">
        {{ saving ? 'Saving…' : 'Add Preset' }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { useWorkspacePresets } from '../composables/usePresets';
import { useInlineSkillMenu } from '../composables/useSlashCommands';
import SlashCommandMenu from './SlashCommandMenu.vue';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import { useFormat } from '../composables/useFormat';

const { toKebabCase } = useFormat();

const props = defineProps({
  workspaceId: { type: String, required: true },
});

const { presets, loading, error, saving, load, add, remove } = useWorkspacePresets(() => props.workspaceId);

const name = ref('');
const body = ref('');
const bodyInput = ref(null);
const skillMenu = useInlineSkillMenu({ text: body, workspaceId: () => props.workspaceId, el: bodyInput });
const canAdd = computed(() => !saving.value && name.value.trim() !== '' && body.value.trim() !== '');

async function submit() {
  name.value = toKebabCase(name.value);
  if (!canAdd.value) return;
  if (await add(name.value, body.value)) {
    name.value = '';
    body.value = '';
  }
}

onMounted(load);
onWebMCPChange(() => load());
</script>
