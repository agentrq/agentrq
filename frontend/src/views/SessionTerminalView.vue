<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
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
const { session, loading, others, ended, shownStatus, title, subtitle, titleIsWorkspaceName, machineLabel } =
  view

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
  muted: 'bg-zinc-400 dark:bg-zinc-600',
}

const machineHref = computed(() =>
  session.value?.machineId ? `/machines/${session.value.machineId}` : '/machines'
)

/**
 * The way back to the work this agent is doing.
 *
 * Empty when the session names no workspace, which is the one case where there
 * is nowhere to go — and an id is what decides it, not the name: naming is
 * best-effort on the server, so a session can be headed by its kind and still
 * belong to a workspace.
 */
const workspaceHref = computed(() =>
  session.value?.workspaceId ? `/workspaces/${session.value.workspaceId}` : ''
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
       scrolls, and it scrolls inside itself. The phone padding lifts its key
       row clear of the floating Menu pill. -->
  <div class="flex flex-col h-full min-h-0 gap-3 pb-6 md:pb-0">
    <div class="shrink-0 px-4 pt-2 flex items-center justify-between gap-4">
      <div class="min-w-0">
        <!-- Both ways out, because a terminal is reached from either: from the
             machine that is running it, and from the workspace it is working
             in. Arriving from one and being offered only the other is how
             somebody loses their place. -->
        <!-- The machine's link is named after it, which is also how the page
             says where this terminal is running. -->
        <div class="flex items-center gap-2 min-w-0">
          <button
            @click="router.push(machineHref)"
            class="min-w-0 truncate text-[11px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300"
            :title="`Running on ${machineLabel}`"
          >
            ← {{ machineLabel }}
          </button>
          <!-- Only when the heading cannot carry the link itself. A session
               can have a workspace id while its heading is the kind — naming
               is best-effort on the server — and dropping this outright would
               lose the way out in exactly that case. -->
          <template v-if="workspaceHref && !titleIsWorkspaceName">
            <span class="text-gray-300 dark:text-zinc-700 text-[11px]">·</span>
            <button
              @click="router.push(workspaceHref)"
              class="text-[11px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 truncate"
            >
              Workspace
            </button>
          </template>
        </div>
        <!-- The heading is the link when it is the workspace's own name: the
             name is already on screen, and a second control saying "Workspace"
             beside it was two things for one destination. -->
        <h1
          class="text-lg md:text-xl font-black tracking-tight text-gray-900 dark:text-zinc-100 truncate"
        >
          <button
            v-if="workspaceHref && titleIsWorkspaceName"
            @click="router.push(workspaceHref)"
            class="max-w-full truncate hover:underline underline-offset-4 decoration-2 decoration-gray-300 dark:decoration-zinc-600 transition-all"
            :title="`Open ${title}`"
          >
            {{ title }}
          </button>
          <template v-else>{{ title }}</template>
        </h1>
        <p v-if="subtitle" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">
          {{ subtitle }}
        </p>
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
        <!-- The session is gone, so both offers are about where to go next —
             and the workspace is usually the more useful of the two, since
             whatever this agent was doing is still there. -->
        <div class="mt-4 flex items-center justify-center gap-2">
          <button
            @click="router.push(machineHref)"
            class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95"
          >
            Back to the machine
          </button>
          <button
            v-if="workspaceHref"
            @click="router.push(workspaceHref)"
            class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95"
          >
            Open workspace
          </button>
        </div>
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
          <span class="text-[11px] font-black uppercase tracking-widest text-zinc-500 dark:text-zinc-400">
            {{ statusLabel(shownStatus) }}
          </span>
        </span>
      </template>

      <template #actions>
        <!-- Two browsers on one terminal is allowed — it is how one person
             shows another what is happening — but never left implicit. -->
        <span
          v-if="others.length"
          class="text-[11px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-400 truncate"
        >
          Shared with {{ others.join(', ') }}
        </span>
      </template>
    </SessionTerminal>
  </div>
</template>
