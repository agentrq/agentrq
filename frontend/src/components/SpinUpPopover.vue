<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  Spin up's one question: which machine, and what to run. Everything else —
  the fork's name, where the task goes — follows from the task. On a wide
  screen it opens in the middle, like a dialog, rather than beside a button
  that may sit in a corner; on a phone it stays beside the button. It opens on
  the parent's last launch, so the common case is one more click.
-->
<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import AcpAgentList from './AcpAgentList.vue'
import AgentKindPicker from './AgentKindPicker.vue'
import ClaudeOptionsPicker from './ClaudeOptionsPicker.vue'
import { terminalPath } from '../composables/useTerminalView'
import { spinUpName } from '../composables/useSpinUp'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useViewport } from '../composables/useViewport'

const props = defineProps({
  // The object `useSpinUp()` returns; the view that owns the button owns it.
  spin: { type: Object, required: true },
})

const emit = defineEmits(['done'])

const router = useRouter()
const workspaceStore = useWorkspaceStore()

const task = computed(() => props.spin.state.task)
const { isMobile } = useViewport()
const width = 320
const style = computed(() => {
  if (!isMobile.value) {
    return { top: '50%', left: '50%', transform: 'translate(-50%, -50%)', width: '380px', maxHeight: 'calc(100vh - 32px)' }
  }
  const vw = window.innerWidth
  const left = Math.max(8, Math.min(props.spin.state.x - width + 24, vw - width - 8))
  return { top: `${props.spin.state.y + 8}px`, left: `${left}px`, width: `${Math.min(width, vw - 16)}px` }
})

const runLabel = computed(() => {
  if (!props.spin.running.value) return 'Spin up'
  return { fork: 'Forking…', move: 'Moving…', launch: 'Starting…' }[props.spin.step.value] ?? 'Spin up'
})

async function start() {
  const result = await props.spin.run()
  if (!result) return
  // The fork exists either way, so the sidebar should show it either way.
  workspaceStore.fetchWorkspaces()
  emit('done', result)
  if (result.session) {
    const to = terminalPath(result.session)
    if (to) router.push(to)
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="task" class="fixed inset-0 z-[140]" :class="isMobile ? '' : 'bg-black/20 dark:bg-black/40'" data-test="spin-up-backdrop" @click="spin.close()" @contextmenu.prevent="spin.close()"></div>
    <div v-if="task" role="dialog" aria-labelledby="spin-up-title" data-test="spin-up"
         class="fixed z-[150] p-4 overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl shadow-2xl space-y-3 text-left"
         :style="style" @click.stop @keydown.esc="spin.close()">
      <div class="min-w-0">
        <h2 id="spin-up-title" class="text-sm font-bold text-gray-800 dark:text-zinc-200">Spin up</h2>
        <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5 break-words">
          Forks {{ spin.state.workspace?.name }} as
          <span class="font-semibold text-gray-900 dark:text-zinc-100">{{ spinUpName(task) }}</span>,
          moves this task into it and starts an agent there.
        </p>
      </div>

      <div v-if="spin.available.value.length > 1">
        <label for="spin-up-machine" class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">Machine</label>
        <select id="spin-up-machine" :value="spin.machineId.value" @change="spin.machineId.value = $event.target.value"
                class="w-full px-3 py-2 text-xs font-semibold border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white">
          <option value="" disabled>Pick a machine</option>
          <option v-for="m in spin.available.value" :key="m.id" :value="m.id">{{ m.name }}</option>
        </select>
      </div>
      <p v-else-if="spin.available.value.length === 1" class="text-[11px] text-gray-500 dark:text-zinc-400">
        On {{ spin.available.value[0].name }}, your only machine that is online.
      </p>

      <AgentKindPicker id-prefix="spin-up-kind" :model-value="spin.kind.value" @update:model-value="spin.kind.value = $event" />

      <ClaudeOptionsPicker v-if="spin.kind.value === 'claude-code'" id-prefix="spin-up" :model-value="spin.params.value" :models="spin.claudeModels.value" @update:model-value="spin.params.value = $event" />

      <div v-if="spin.kind.value === 'acp-gateway'" class="grid gap-2 grid-cols-2">
        <div class="min-w-0">
          <label for="spin-up-agent" class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">Agent</label>
          <input id="spin-up-agent" v-model="spin.params.value.agent" type="text"
                 spellcheck="false" autocapitalize="off" autocorrect="off"
                 class="w-full px-2 py-1.5 text-xs font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white" />
        </div>
        <div class="min-w-0">
          <label for="spin-up-model" class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">Model</label>
          <input id="spin-up-model" v-model="spin.params.value.model" type="text" list="spin-up-model-options" placeholder="Gateway default"
                 spellcheck="false" autocapitalize="off" autocorrect="off"
                 class="w-full px-2 py-1.5 text-xs font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white" />
          <datalist id="spin-up-model-options">
            <option v-for="m in spin.acpModels.value" :key="m.id" :value="m.id">{{ m.name }}</option>
          </datalist>
        </div>
      </div>

      <AcpAgentList v-if="spin.kind.value === 'acp-gateway'" id-prefix="spin-up" :agents="spin.acpAgents.value" :query="spin.params.value.agent ?? ''" @pick="spin.pickAcpAgent" />

      <ul v-if="spin.blockers.value.length" class="space-y-1">
        <li v-for="b in spin.blockers.value" :key="b.reason" class="text-[11px] text-gray-500 dark:text-zinc-400">
          {{ b.reason }}
          <router-link v-if="b.fix" :to="b.fix.to" class="underline hover:text-gray-700 dark:hover:text-zinc-200" @click="spin.close()">{{ b.fix.label }}</router-link>
        </li>
      </ul>

      <p v-if="spin.error.value" data-test="spin-up-error" class="text-[11px] text-red-600 dark:text-red-400 font-medium break-words">{{ spin.error.value }}</p>
      <router-link v-if="spin.forked.value" :to="`/workspaces/${spin.forked.value.id}`" @click="spin.close()"
                   class="block text-[11px] font-semibold underline text-gray-700 dark:text-zinc-200">Open {{ spin.forked.value.name }}</router-link>

      <div class="flex items-center gap-2">
        <button type="button" :disabled="!spin.canRun.value" @click="start"
                class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-40 disabled:cursor-not-allowed">
          {{ runLabel }}
        </button>
        <button type="button" @click="spin.close()"
                class="px-4 py-2 text-[11px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200 transition-all">
          Cancel
        </button>
      </div>
    </div>
  </Teleport>
</template>
