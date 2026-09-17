<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<script setup>
/**
 * A full-height terminal for one agent session.
 *
 * The page never scrolls: the terminal fills whatever is left after the header
 * and scrolls its own contents, which is what a terminal is supposed to do.
 * Before this it sized itself to its output and pushed the page down forever.
 */
import { computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SessionTerminal from '../components/SessionTerminal.vue'
import {
  useTerminalView,
  statusLabel,
  statusTone,
  endedReason,
} from '../composables/useTerminalView'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import { killSession } from '../api'

const route = useRoute()
const router = useRouter()
const { notifySuccess, notifyError } = useToasts()

const sessionId = String(route.params.id ?? '')
const view = useTerminalView({ sessionId })
const { session, loading, others, ended, shownStatus, title } = view

// An explicit setter rather than an inline assignment in the template: `status`
// here is a ref destructured out of a composable, and the compiler cannot know
// that, so `status = s` would quietly rebind a local instead of the ref.
function onStatus(next) {
  view.status.value = next
}

const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

const TONES = {
  good: 'bg-emerald-500',
  pending: 'bg-amber-400',
  bad: 'bg-red-500',
  muted: 'bg-zinc-600',
}

const machineHref = computed(() =>
  session.value?.machineId ? `/machines/${session.value.machineId}` : '/machines'
)

onMounted(() => {
  view.load()
  onEvent(view.handleEvent)
  connect()
})
onUnmounted(disconnect)

async function stop() {
  try {
    await killSession(sessionId)
    notifySuccess('Asked the machine to stop this session')
  } catch (e) {
    notifyError(e?.message || 'Failed to stop the session')
  }
}
</script>

<template>
  <!-- h-full and min-h-0 all the way down: the terminal is the only thing that
       scrolls, and it scrolls inside itself. -->
  <div class="flex flex-col h-full min-h-0 gap-3">
    <div class="shrink-0 flex items-center justify-between gap-4">
      <div class="min-w-0">
        <button
          @click="router.push(machineHref)"
          class="text-[11px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300"
        >
          ← Machine
        </button>
        <h1
          class="text-lg md:text-xl font-black tracking-tight text-gray-900 dark:text-zinc-100 truncate"
        >
          {{ title }}
        </h1>
      </div>

      <button
        v-if="!ended && !loading"
        @click="stop"
        class="shrink-0 px-4 py-2 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-red-50 dark:hover:bg-red-500/10 transition-all active:scale-95"
      >
        Stop
      </button>
    </div>

    <!-- A terminal that has ended says so. A blank rectangle is the worst
         version of this: it looks exactly like one that is simply quiet. -->
    <div
      v-if="ended"
      class="flex-1 min-h-0 grid place-items-center border border-gray-100 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900"
    >
      <div class="text-center px-6">
        <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">{{ endedReason(session) }}</p>
        <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">
          Its terminal is gone with it — what an agent had on screen is not kept.
        </p>
        <button
          @click="router.push(machineHref)"
          class="mt-4 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95"
        >
          Back to the machine
        </button>
      </div>
    </div>

    <SessionTerminal
      v-else
      :session-id="sessionId"
      :ended="ended"
      @status="onStatus"
      @control="view.handleControl"
    >
      <template #title>
        <span class="flex items-center gap-2 min-w-0">
          <span class="h-2 w-2 rounded-full shrink-0" :class="TONES[statusTone(shownStatus)]" />
          <span class="text-[11px] font-black uppercase tracking-widest text-zinc-400">
            {{ statusLabel(shownStatus) }}
          </span>
        </span>
      </template>

      <template #actions>
        <!-- Two browsers on one terminal is allowed — it is how one person
             shows another what is happening — but never left implicit. -->
        <span
          v-if="others.length"
          class="text-[11px] font-black uppercase tracking-widest text-amber-400 truncate"
        >
          Shared with {{ others.join(', ') }}
        </span>
      </template>
    </SessionTerminal>
  </div>
</template>
