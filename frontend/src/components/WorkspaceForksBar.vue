<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  A parent workspace's forks, one chip each and busiest first: a dot that
  pulses while its agent works, a count of what needs you, and Merge once none
  of its tasks is left unfinished; its state and progress on hover. A few show,
  and the rest open in place, so ten forks still take one line.
-->
<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import ForkIcon from './ForkIcon.vue'
import MergeForkModal from './MergeForkModal.vue'
import { mergeBlockedReason, parentName, useForkActions } from '../composables/useWorkspaceForks'
import { activityOf, forkTooltip, sortForks } from '../composables/useWorkspaceActivity'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useTooltipStore } from '../stores/tooltipStore'
import { useToasts } from '../composables/useToasts'
import { useFormat } from '../composables/useFormat'

const props = defineProps({
  workspaceId: { type: String, required: true },
})

/** Chips shown before "+N more". */
const VISIBLE = 5

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const tooltipStore = useTooltipStore()
const { toKebabCase } = useFormat()
const forks = computed(() => sortForks(workspaceStore.forksOf(props.workspaceId)))
const expanded = ref(false)
const shown = computed(() => (expanded.value ? forks.value : forks.value.slice(0, VISIBLE)))
const hidden = computed(() => forks.value.length - shown.value.length)
const forkActions = useForkActions({ router, store: workspaceStore, toasts: useToasts() })
</script>

<template>
  <div v-if="forks.length > 0" class="flex items-center gap-2 overflow-x-auto custom-scrollbar pb-1" data-test="forks-bar">
    <span class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Forks</span>
    <div v-for="fork in shown" :key="fork.id"
         class="shrink-0 flex items-center gap-1.5 pl-2.5 pr-1 py-1 rounded-sm border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:border-gray-400 dark:hover:border-zinc-600 transition-colors"
         data-test="fork-chip"
         @mouseenter="tooltipStore.show($event, forkTooltip(fork), 'bottom')" @mouseleave="tooltipStore.hide()">
      <router-link :to="`/workspaces/${fork.id}`" class="flex items-center gap-1.5 min-w-0 pr-1.5">
        <span class="relative flex items-center justify-center w-1.5 h-1.5 shrink-0">
          <span class="w-1.5 h-1.5 rounded-full" :class="fork.agentConnected ? 'bg-green-500 dark:bg-green-400' : 'bg-gray-300 dark:bg-zinc-600'"></span>
          <span v-if="activityOf(fork).working" class="absolute inset-0 rounded-full bg-green-500 animate-ping opacity-75" data-test="fork-working"></span>
        </span>
        <ForkIcon class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500" />
        <span class="max-w-40 truncate text-[11px] font-black text-gray-800 dark:text-zinc-200">{{ toKebabCase(fork.name) }}</span>
        <span v-if="activityOf(fork).needsYou > 0"
              class="shrink-0 min-w-4 px-1 rounded-sm text-center text-[9px] font-black leading-4 bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-400"
              data-test="fork-needs-you">{{ activityOf(fork).needsYou }}</span>
      </router-link>
      <button v-if="!mergeBlockedReason(fork)" type="button" @click="forkActions.startMerge(fork)"
              class="shrink-0 px-2 py-0.5 rounded-sm text-[9px] font-black uppercase tracking-wider bg-black text-white dark:bg-white dark:text-black hover:opacity-80">
        Merge
      </button>
    </div>
    <button v-if="forks.length > VISIBLE" type="button" @click="expanded = !expanded"
            class="shrink-0 px-2 py-1 rounded-sm text-[10px] font-black text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50"
            data-test="forks-more">
      {{ expanded ? 'Show less' : `+${hidden} more` }}
    </button>
    <Teleport to="body">
      <MergeForkModal :fork="forkActions.state.merging" :parent-name="parentName(forkActions.state.merging, workspaceStore.workspaces)"
                      :busy="forkActions.state.busy"
                      @close="forkActions.cancelMerge()" @confirm="forkActions.confirmMerge($event)" />
    </Teleport>
  </div>
</template>
