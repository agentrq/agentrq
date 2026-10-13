<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A small button that turns a doc the person wrote into a task for one of
     their workspaces' agents, picked from a list it opens. -->
<template>
  <div class="relative inline-flex" v-click-outside="() => (open = false)" @click.stop.prevent>
    <button type="button" data-test="pano-to-task" title="Make it a task" aria-label="Make it a task" :aria-expanded="open" :disabled="busy"
      class="p-1 rounded-sm text-gray-600 hover:text-black hover:bg-black/10 disabled:opacity-40" :class="buttonClass" @click="open = !open">
      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2M9 5a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2M9 5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2m-6 9 2 2 4-4" />
      </svg>
    </button>
    <div v-if="open" data-test="pano-to-task-menu" role="menu"
      class="absolute right-0 top-full mt-1 z-30 w-56 max-h-64 overflow-y-auto custom-scrollbar bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg py-1 text-left">
      <p class="px-3 py-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Make a task in</p>
      <p v-if="!workspaces.length" class="px-3 py-1.5 text-[11px] text-gray-500 dark:text-zinc-400">No workspaces yet.</p>
      <button v-for="w in workspaces" :key="w.id" type="button" role="menuitem" data-test="pano-to-task-workspace" @click="convert(w)"
        class="block w-full truncate px-3 py-1.5 text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800">{{ w.name }}</button>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { storeToRefs } from 'pinia'
import * as api from '../api'
import { convertPanoDocToTask } from '../composables/usePano'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useToasts } from '../composables/useToasts'

const props = defineProps({
  doc: { type: Object, required: true },
  buttonClass: { type: String, default: '' },
})

const { workspaces } = storeToRefs(useWorkspaceStore())
const { notifySuccess, notifyError } = useToasts()
const open = ref(false)
const busy = ref(false)

async function convert(workspace) {
  open.value = false
  busy.value = true
  try {
    const task = await convertPanoDocToTask(props.doc, workspace.id, { api })
    notifySuccess(`Task created in ${workspace.name}`, null, { taskId: task.id, workspaceId: workspace.id })
  } catch (err) {
    notifyError(err.message || 'Failed to create the task')
  } finally {
    busy.value = false
  }
}
</script>
