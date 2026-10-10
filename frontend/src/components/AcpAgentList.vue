<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  The agents acp-gateway can run, listed under its Agent field rather than
  hidden behind a datalist that shows nothing until somebody types — the
  agent's id is exactly what a person does not know. Typing narrows the list;
  a click fills the field.
-->
<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { filterAcpAgents } from '../composables/useAgentLaunch'

const props = defineProps({
  agents: { type: Array, required: true },
  // What the Agent field holds.
  query: { type: String, default: '' },
  idPrefix: { type: String, required: true },
})

const emit = defineEmits(['pick'])

const matched = computed(() => filterAcpAgents(props.agents, props.query))
// An id nothing here matches — a remembered one, or one only some machine
// knows — still leaves every agent on offer rather than an empty box.
const shown = computed(() => (matched.value.length ? matched.value : props.agents))
const selected = computed(() => (props.query ?? '').trim())

// The chosen agent is scrolled to within the list — never the page — so a
// remembered one is visible when the form opens.
const list = ref(null)
watch(
  [selected, shown],
  async () => {
    await nextTick()
    const row = list.value?.querySelector('[aria-pressed="true"]')?.closest('li')
    if (row) list.value.scrollTop = row.offsetTop - list.value.offsetTop
  },
  { immediate: true }
)
</script>

<template>
  <div v-if="agents.length" class="min-w-0" :data-test="`${idPrefix}-acp-agents`">
    <p :id="`${idPrefix}-acp-agents-label`" class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">
      Available agents
      <span class="normal-case tracking-normal font-semibold">· {{ shown.length === agents.length ? agents.length : `${shown.length} of ${agents.length}` }}</span>
    </p>
    <p v-if="!matched.length" class="text-[11px] text-gray-500 dark:text-zinc-400 mb-1 break-words">
      None of these is “{{ selected }}”. It is still sent as typed.
    </p>
    <ul ref="list" :aria-labelledby="`${idPrefix}-acp-agents-label`"
        class="max-h-44 overflow-y-auto border border-gray-200 dark:border-zinc-700 rounded-lg divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="a in shown" :key="a.id">
        <button type="button" :aria-pressed="a.id === selected" :title="a.description || a.name || a.id"
                class="w-full min-w-0 text-left px-2 py-1.5 transition-colors"
                :class="a.id === selected ? 'bg-gray-100 dark:bg-zinc-800' : 'hover:bg-gray-50 dark:hover:bg-zinc-800/60'"
                @click="emit('pick', a.id)">
          <span class="flex items-baseline gap-1.5 min-w-0">
            <span class="text-xs font-semibold text-gray-900 dark:text-zinc-100 truncate">{{ a.name || a.id }}</span>
            <span v-if="a.name" class="text-[10px] font-mono text-gray-500 dark:text-zinc-400 truncate">{{ a.id }}</span>
            <span class="ml-auto flex items-center gap-1 shrink-0">
              <span v-for="r in a.runtimes ?? []" :key="r"
                    class="inline-flex items-center h-4 px-1 leading-none rounded text-[9px] font-mono text-gray-500 dark:text-zinc-400 bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700">{{ r }}</span>
            </span>
          </span>
          <span v-if="a.description" class="block text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ a.description }}</span>
        </button>
      </li>
    </ul>
  </div>
</template>
