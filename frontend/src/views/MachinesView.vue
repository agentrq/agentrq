<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<script setup>
/**
 * The machines enrolled against this account.
 *
 * Thin on purpose: every rule — what a live update may overwrite, how a
 * session count moves, what an absent reading means — is in
 * `useMachines` and `useMachineFormat`, where it is tested.
 */
import { onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMachines } from '../composables/useMachines'
import {
  formatBytes,
  formatPercent,
  memoryUsedPercent,
} from '../composables/useMachineFormat'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import { writeClipboard } from '../composables/useMarkdownLinks'

const router = useRouter()
const { notifySuccess } = useToasts()
const {
  machines,
  loading,
  error,
  enrolmentCode,
  enrolCommand,
  load,
  requestCode,
  dismissCode,
  handleEvent,
} = useMachines()

const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

onMounted(() => {
  load()
  // onEvent rather than a watcher on the buffer: a machine going offline is a
  // transition, and two events landing in one flush would collapse into one.
  onEvent(handleEvent)
  connect()
})
onUnmounted(disconnect)

async function copyCommand() {
  if (!enrolCommand.value) return
  try {
    // The shell first: the browser's Clipboard API refuses to write from a
    // document that is not focused, which the desktop window often is not.
    await writeClipboard(enrolCommand.value, {
      bridge: window.agentrq?.clipboard,
      clipboard: navigator.clipboard,
    })
    notifySuccess('Enrolment command copied')
  } catch {
    // A clipboard that refuses is not worth an error: the command is on screen.
  }
}
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">
          Machines
        </h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          Computers running agentrqd that can host agents for your workspaces.
        </p>
      </div>
      <button
        @click="requestCode"
        class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0"
      >
        + Add machine
      </button>
    </div>

    <div class="px-4 space-y-6 pb-10">
      <!-- The code is shown once; it is stored only as a hash. -->
      <div
        v-if="enrolmentCode"
        class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-3"
      >
        <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Enrol a machine</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400">
          Run this on the machine itself. The code is single-use and expires shortly, and it is not
          shown again.
        </p>
        <pre class="text-xs font-mono bg-gray-50 dark:bg-zinc-800 rounded-lg p-3 overflow-x-auto">{{ enrolCommand }}</pre>
        <div class="flex items-center gap-2">
          <button
            @click="copyCommand"
            class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95"
          >
            Copy command
          </button>
          <button
            @click="dismissCode"
            class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95"
          >
            Done
          </button>
        </div>
      </div>

      <p v-if="error" class="text-xs text-red-500">{{ error }}</p>

      <div v-if="loading" class="text-xs text-gray-400 dark:text-zinc-500">Loading machines…</div>

      <div
        v-else-if="machines.length === 0"
        class="border border-gray-100 dark:border-zinc-800 rounded-xl p-8 text-center"
      >
        <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">No machines yet</p>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">
          Add a machine to run agents somewhere other than this browser.
        </p>
      </div>

      <div v-else class="grid gap-3 md:grid-cols-2">
        <button
          v-for="m in machines"
          :key="m.id"
          @click="router.push(`/machines/${m.id}`)"
          class="text-left border border-gray-100 dark:border-zinc-800 hover:border-gray-200 dark:hover:border-zinc-700 rounded-xl p-4 bg-white dark:bg-zinc-900 transition-colors"
        >
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span
                  class="h-2 w-2 rounded-full shrink-0"
                  :class="m.online ? 'bg-emerald-500' : 'bg-gray-300 dark:bg-zinc-600'"
                  :title="m.online ? 'Online' : 'Offline'"
                />
                <span class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ m.name || m.hostname }}</span>
                <span
                  v-if="!m.enabled"
                  class="text-[10px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-400"
                  >Disabled</span
                >
              </div>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1 truncate">
                {{ m.os }}/{{ m.arch }} · agentrqd {{ m.version || '—' }}
              </p>
            </div>
            <div class="text-right shrink-0">
              <p class="text-lg font-black text-gray-900 dark:text-zinc-100 tabular-nums">{{ m.sessions ?? 0 }}</p>
              <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
                {{ (m.sessions ?? 0) === 1 ? 'agent' : 'agents' }}
              </p>
            </div>
          </div>

          <div v-if="m.metrics" class="mt-3 pt-3 border-t border-gray-100 dark:border-zinc-800 flex gap-5">
            <div>
              <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">CPU</p>
              <p class="text-xs font-bold text-gray-800 dark:text-zinc-200 tabular-nums">
                {{ formatPercent(m.metrics.cpuPercent) }}
              </p>
            </div>
            <div>
              <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Memory</p>
              <p class="text-xs font-bold text-gray-800 dark:text-zinc-200 tabular-nums">
                {{ formatPercent(memoryUsedPercent(m.metrics)) }}
                <span class="font-medium text-gray-400 dark:text-zinc-500"
                  >of {{ formatBytes(m.metrics.memTotal) }}</span
                >
              </p>
            </div>
          </div>
          <!-- A machine that has never reported says so, rather than showing zeros. -->
          <p v-else class="mt-3 pt-3 border-t border-gray-100 dark:border-zinc-800 text-[11px] text-gray-400 dark:text-zinc-500">
            No readings yet
          </p>
        </button>
      </div>
    </div>
  </div>
</template>
