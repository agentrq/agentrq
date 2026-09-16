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
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMachines } from '../composables/useMachines'
import {
  formatBytes,
  formatPercent,
  memoryUsedPercent,
} from '../composables/useMachineFormat'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import {
  detectPlatform,
  platformLabel,
  installGuide,
  PLATFORMS,
  DAEMON_DOCS_URL,
} from '../composables/useDaemonInstall'
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

// The browser's OS picks which tab opens and nothing else. You are usually
// setting up a machine other than the one you are looking at — a build box, a
// server, a spare laptop — so every platform stays one click away.
const platform = ref(detectPlatform(navigator.userAgent))
const guide = computed(() => installGuide(platform.value, enrolCommand.value))

onMounted(() => {
  load()
  // onEvent rather than a watcher on the buffer: a machine going offline is a
  // transition, and two events landing in one flush would collapse into one.
  onEvent(handleEvent)
  connect()
})
onUnmounted(disconnect)

async function copy(text, what) {
  if (!text) return
  try {
    // The shell first: the browser's Clipboard API refuses to write from a
    // document that is not focused, which the desktop window often is not.
    await writeClipboard(text, {
      bridge: window.agentrq?.clipboard,
      clipboard: navigator.clipboard,
    })
    notifySuccess(`${what} copied`)
  } catch {
    // A clipboard that refuses is not worth an error: it is all on screen.
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
      <!-- Install, then enrol. The enrol command alone was the whole panel
           once, which is a command you can only run if the thing it names is
           already there — so it answered the second question and not the
           first. The code is shown once; it is stored only as a hash. -->
      <div
        v-if="enrolmentCode"
        class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Add a machine</h2>
            <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">
              Run these on the machine itself. The code is single-use, expires shortly, and is not
              shown again.
            </p>
          </div>
          <button
            @click="dismissCode"
            class="shrink-0 px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95"
          >
            Done
          </button>
        </div>

        <!-- Before the commands, because enrolling is a grant rather than a
             setup step, and this is the last moment it is easy to stop. -->
        <p class="text-[11px] text-amber-700 dark:text-amber-400 border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 rounded-lg px-3 py-2">
          Enrolling lets anyone who can sign in to this AgentRQ account run commands on that
          machine, as the user who starts the daemon.
          <a :href="DAEMON_DOCS_URL" target="_blank" rel="noopener noreferrer" class="underline"
            >What this means</a
          >.
        </p>

        <div class="flex items-center gap-1">
          <button
            v-for="p in PLATFORMS"
            :key="p"
            @click="platform = p"
            class="px-3 py-1.5 text-[11px] font-black uppercase tracking-widest rounded-lg transition-colors"
            :class="
              platform === p
                ? 'bg-black dark:bg-white text-white dark:text-black'
                : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'
            "
          >
            {{ platformLabel(p) }}
          </button>
        </div>

        <ol class="space-y-3">
          <li v-for="(step, i) in guide.steps" :key="step.title" class="flex gap-3">
            <span
              class="shrink-0 mt-0.5 h-5 w-5 rounded-full bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300 text-[11px] font-black grid place-items-center tabular-nums"
              >{{ i + 1 }}</span
            >
            <div class="min-w-0 flex-1">
              <p class="text-xs font-bold text-gray-800 dark:text-zinc-200">{{ step.title }}</p>
              <a
                v-if="step.link"
                :href="step.link"
                target="_blank"
                rel="noopener noreferrer"
                class="text-xs text-gray-600 dark:text-zinc-400 underline break-all"
                >Download agentrqd for {{ guide.label }}</a
              >
              <div v-if="step.lines.length" class="mt-1 flex items-start gap-2">
                <!-- A foreground for every background. Without the text
                     colours this inherits the document's, which is the browser
                     default black — unreadable on the dark surface, and the
                     one combination nobody sees while building in light mode.
                     The pairing matches .md-body pre in style.css. -->
                <pre
                  class="flex-1 min-w-0 text-xs font-mono text-gray-800 dark:text-zinc-200 bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg p-3 overflow-x-auto"
                >{{ step.lines.join('\n') }}</pre>
                <button
                  @click="copy(step.lines.join('\n'), step.title)"
                  class="shrink-0 px-3 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95"
                >
                  Copy
                </button>
              </div>
            </div>
          </li>
        </ol>
      </div>

      <p v-if="error" class="text-xs text-red-500">{{ error }}</p>

      <div v-if="loading" class="text-xs text-gray-400 dark:text-zinc-500">Loading machines…</div>

      <div
        v-else-if="machines.length === 0"
        class="border border-gray-100 dark:border-zinc-800 rounded-xl p-8 text-center"
      >
        <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">No machines yet</p>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">
          A machine is a computer running <span class="font-mono">agentrqd</span>. Enrol one and you
          can run agents on it, and watch their terminals from here.
        </p>
        <!-- The empty state is the other moment somebody needs to know where
             the daemon comes from: they have arrived at this page and there is
             nothing on it to explain what one even is. -->
        <button
          @click="requestCode"
          class="mt-4 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95"
        >
          Add your first machine
        </button>
        <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-3">
          <a :href="DAEMON_DOCS_URL" target="_blank" rel="noopener noreferrer" class="underline"
            >What enrolling a machine means</a
          >
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
