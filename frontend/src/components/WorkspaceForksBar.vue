<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  A parent workspace's forks, one chip each: what its agent is doing, how many
  of its tasks are done, and Merge once none is left unfinished. So the forks
  can be watched and merged from the parent without opening each one.
-->
<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import ForkIcon from './ForkIcon.vue'
import MergeForkModal from './MergeForkModal.vue'
import { mergeBlockedReason, parentName, useForkActions } from '../composables/useWorkspaceForks'
import { activityOf, forkProgressLabel, forkStatusLabel } from '../composables/useWorkspaceActivity'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useToasts } from '../composables/useToasts'
import { useFormat } from '../composables/useFormat'

const props = defineProps({
  workspaceId: { type: String, required: true },
})

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const { toKebabCase } = useFormat()
const forks = computed(() => workspaceStore.forksOf(props.workspaceId))
const forkActions = useForkActions({ router, store: workspaceStore, toasts: useToasts() })
</script>

<template>
  <div v-if="forks.length > 0" class="flex items-center gap-2 overflow-x-auto custom-scrollbar pb-1" data-test="forks-bar">
    <span class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Forks</span>
    <div v-for="fork in forks" :key="fork.id"
         class="shrink-0 flex items-center gap-2 max-w-xs pl-2.5 pr-1 py-1 rounded-sm border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:border-gray-400 dark:hover:border-zinc-600 transition-colors"
         data-test="fork-chip">
      <router-link :to="`/workspaces/${fork.id}`" class="flex items-center gap-2 min-w-0">
        <span class="relative flex items-center justify-center w-1.5 h-1.5 shrink-0">
          <span class="w-1.5 h-1.5 rounded-full" :class="fork.agentConnected ? 'bg-green-500 dark:bg-green-400' : 'bg-gray-300 dark:bg-zinc-600'"></span>
          <span v-if="activityOf(fork).working" class="absolute inset-0 rounded-full bg-green-500 animate-ping opacity-75"></span>
        </span>
        <ForkIcon class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500" />
        <span class="shrink-0 text-[11px] font-black text-gray-800 dark:text-zinc-200">{{ toKebabCase(fork.name) }}</span>
        <span class="truncate text-[10px] font-medium"
              :class="activityOf(fork).needsYou > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-gray-500 dark:text-zinc-400'"
              :title="forkStatusLabel(fork)" data-test="fork-status">{{ forkStatusLabel(fork) }}</span>
        <span v-if="forkProgressLabel(fork)" class="shrink-0 text-[10px] tabular-nums text-gray-400 dark:text-zinc-500" data-test="fork-progress">{{ forkProgressLabel(fork) }}</span>
      </router-link>
      <button v-if="!mergeBlockedReason(fork)" type="button" @click="forkActions.startMerge(fork)"
              class="shrink-0 px-2 py-0.5 rounded-sm text-[9px] font-black uppercase tracking-wider bg-black text-white dark:bg-white dark:text-black hover:opacity-80">
        Merge
      </button>
    </div>
    <Teleport to="body">
      <MergeForkModal :fork="forkActions.state.merging" :parent-name="parentName(forkActions.state.merging, workspaceStore.workspaces)"
                      :busy="forkActions.state.busy"
                      @close="forkActions.cancelMerge()" @confirm="forkActions.confirmMerge($event)" />
    </Teleport>
  </div>
</template>
