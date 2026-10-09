<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * One machine: what it has left, what is running on it, and what can be done
 * to it.
 *
 * The destructive parts read their wording from `useMachineDetail` rather than
 * inventing it here. A confirm dialog that does not name what it destroys is
 * not consent, and that sentence is worth testing.
 */
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  useMachineDetail,
  updateConsequence,
  deleteConsequence,
  REMOTE_CONTROL_MIN_VERSION,
} from '../composables/useMachineDetail'
import {
  formatBytes,
  formatPercent,
  formatUptime,
  formatLoadAvg,
  formatAge,
  diskUsedPercent,
  memoryUsedPercent,
  isSessionLive,
  sessionTone,
  sessionLabel,
  sessionSummary,
  daemonOutdated,
  DAEMON_OUTDATED_HINT,
} from '../composables/useMachineFormat'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import DeleteModal from '../components/DeleteModal.vue'
import AgentKindPicker from '../components/AgentKindPicker.vue'
import ClaudeOptionsPicker from '../components/ClaudeOptionsPicker.vue'
import { useAgentLaunch } from '../composables/useAgentLaunch'
import { terminalPath } from '../composables/useTerminalView'

const route = useRoute()
const router = useRouter()
const { notifySuccess, notifyError } = useToasts()

const machineId = String(route.params.id ?? '')
const detail = useMachineDetail({ machineId })
const { machine, sessions, liveSessions, canControl, updating, loading, error, busy } = detail

const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

const showDelete = ref(false)
const showRestart = ref(false)

// Destructured because refs keep their reactivity through it, and the
// alternative — reaching through `launcher.x.value` in every binding — is
// where a missing `.value` hides.
const launcher = useAgentLaunch({ machine, sessions })
const {
  choices: launchChoices,
  workspaceId: launchWorkspace,
  kind: launchKind,
  params: launchParams,
  blockers: launchBlockers,
  canLaunch,
  launching,
  error: launchError,
  acpAgents: launchAcpAgents,
  acpModels: launchAcpModels,
  claudeModels: launchClaudeModels,
} = launcher

const liveCount = computed(() => liveSessions.value.length)
const updateText = computed(() => updateConsequence(liveCount.value))
const handUpdateText = `Update agentrqd on this machine by hand to ${REMOTE_CONTROL_MIN_VERSION} or newer to restart or update it from here.`
const deleteText = computed(() => deleteConsequence(machine.value, liveCount.value))
const outdated = computed(() => daemonOutdated(machine.value?.version))

const TABS = [
  { id: 'sessions', label: 'Sessions', icon: 'M4 6h16M4 12h16M4 18h7' },
  { id: 'new-session', label: 'New Session', icon: 'M12 4v16m8-8H4' },
  {
    id: 'info',
    label: 'Machine Info',
    icon: 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
  },
  {
    id: 'settings',
    label: 'Settings',
    icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z',
  },
]

// Tracks an explicit click only. Until somebody clicks a tab, it follows
// whether this machine has ever run a session — so a machine with none opens
// straight on the form that starts one, and a machine that already has some
// opens on the list of them.
const chosenTab = ref('')
const activeTab = computed({
  get: () => chosenTab.value || (sessions.value.length === 0 ? 'new-session' : 'sessions'),
  set: (id) => {
    chosenTab.value = id
  },
})

const TONES = {
  good: 'text-emerald-600 dark:text-emerald-400',
  pending: 'text-amber-600 dark:text-amber-400',
  bad: 'text-red-600 dark:text-red-400',
  muted: 'text-gray-400 dark:text-zinc-500',
}

// The same tokens as a filled dot. Text colours, which is what TONES holds,
// are chosen to be readable as words and read as grey at eight pixels across.
// The update and restart button on a phone, where it is this icon alone: the
// row beside it already says which of the two it is.
const REFRESH_ICON =
  'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15'

const DOTS = {
  good: 'bg-emerald-500',
  pending: 'bg-amber-500',
  bad: 'bg-red-500',
  muted: 'bg-gray-300 dark:bg-zinc-600',
}

onMounted(() => {
  detail.load()
  launcher.load()
  onEvent(detail.handleEvent)
  connect()
})
onUnmounted(disconnect)
// A rename by a browser agent is not among the live events.
onWebMCPChange(() => detail.load({ quiet: true }))

async function toggleEnabled() {
  const next = !machine.value?.enabled
  if (await detail.setEnabled(next)) {
    notifySuccess(next ? 'Machine enabled' : 'Machine disabled')
  } else {
    notifyError(error.value)
  }
}

async function confirmDelete() {
  showDelete.value = false
  if (await detail.remove()) {
    notifySuccess('Machine deleted')
    router.push('/machines')
  } else {
    notifyError(error.value)
  }
}

async function confirmRestart() {
  showRestart.value = false
  // Read before the call: the daemon's next hello clears the offer.
  const doing = updating.value ? 'updating' : 'restarting'
  if (await detail.restart()) {
    notifySuccess(`The machine is ${doing}; its sessions will come back as new terminals`)
  } else {
    notifyError(error.value)
  }
}

async function startAgent() {
  const session = await launcher.launch()
  if (!session) {
    notifyError(launchError.value)
    return
  }
  // Asked, not running: the daemon reports its own state over the event
  // stream, and the session list is already listening for it.
  notifySuccess('Asked the machine to start the agent')
  // Added to the list straight away rather than waiting for the event: the
  // daemon's own report is what makes it accurate, and this is what makes it
  // visible. The two merge, because an unknown session is added and a known
  // one is updated in place.
  detail.handleEvent({
    type: 'session.updated',
    payload: { ...session, machineId: machine.value?.id },
  })
  // Cleared so the form does not sit there inviting the same launch again,
  // which the server would now refuse.
  launchWorkspace.value = ''

  // Then straight to the terminal, which is the ending the workspace's own
  // panel already gives this same launch.
  //
  // An agent's first minute is where it asks the questions that stop it dead
  // — trust this folder, allow this tool, paste a key — and a pseudo-terminal
  // is the only place those appear. Left on this page somebody watches a row
  // say "starting" while the agent sits waiting for an answer to a question
  // nobody can see it asking. The row above is still worth writing for the
  // case where there is nowhere to go.
  const to = terminalPath(session)
  if (to) router.push(to)
}

async function stopSession(id) {
  if (await detail.stop(id)) notifySuccess('Asked the machine to stop that session')
  else notifyError(error.value)
}
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <button
          @click="router.push('/machines')"
          class="text-[11px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 text-left"
        >
          ← Machines
        </button>
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">
          {{ machine?.name || machine?.hostname || 'Machine' }}
        </h1>
        <p v-if="machine" class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          {{ machine.os }}/{{ machine.arch }} ·
          <span :class="{ 'text-red-600 dark:text-red-400 font-bold': outdated }"
            >agentrqd {{ machine.version || '—' }}</span
          >
          · {{ machine.online ? 'online' : 'offline' }}
        </p>
        <!-- With an update on offer, the banner below says it next to the
             button that does it; without one, it is said here. -->
        <p v-if="outdated && !machine.availableVersion" class="text-xs text-red-600 dark:text-red-400 mt-0.5">
          {{ DAEMON_OUTDATED_HINT }}
        </p>
      </div>
    </div>

    <div class="px-4 space-y-6 pb-10">
      <p v-if="error" class="text-xs text-red-500">{{ error }}</p>
      <div v-if="loading" class="text-xs text-gray-400 dark:text-zinc-500">Loading…</div>

      <template v-else-if="machine">
        <!-- Update available.
             The button names what it destroys before it is pressed, and the
             confirm says it again: this is the one action in the product that
             deliberately ends work somebody else may be in the middle of. -->
        <div
          v-if="machine.availableVersion"
          class="border border-gray-100 dark:border-zinc-800 rounded-xl p-4 bg-white dark:bg-zinc-900 flex items-start justify-between gap-4"
        >
          <div class="min-w-0">
            <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">
              agentrqd {{ machine.availableVersion }} is available
            </p>
            <p v-if="outdated" class="text-[11px] text-red-600 dark:text-red-400 mt-1">{{ DAEMON_OUTDATED_HINT }}</p>
            <template v-if="canControl">
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">{{ updateText }}</p>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">
                Sessions come back as new terminals: same agent, same folder, empty scrollback. Claude Code
                agents resume their conversation.
              </p>
            </template>
            <p v-else class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">{{ handUpdateText }}</p>
          </div>
          <button
            v-if="canControl"
            :disabled="busy || !machine.online"
            @click="showRestart = true"
            aria-label="Update daemon"
            class="shrink-0 p-2.5 sm:px-4 sm:py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50"
          >
            <span class="hidden sm:inline">Update daemon</span>
            <svg class="sm:hidden w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" :d="REFRESH_ICON" />
            </svg>
          </button>
        </div>

        <!-- Tabs, vertical: the same sidebar-nav shape as workspace settings.
             Sessions leads because it is what the page is opened to check;
             New Session defaults to active on a machine that has never run
             one, since the list it would otherwise lead on is empty. -->
        <div class="w-full min-w-0 flex flex-col md:flex-row gap-6">
          <div class="w-full md:w-48 shrink-0">
            <nav class="flex flex-col gap-1 sticky top-0">
              <button
                v-for="tab in TABS"
                :key="tab.id"
                type="button"
                @click="activeTab = tab.id"
                :class="[
                  activeTab === tab.id
                    ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5'
                    : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800',
                ]"
                class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left"
              >
                <span class="w-4 h-4 flex items-center justify-center shrink-0">
                  <svg class="w-full h-full" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="tab.icon" />
                  </svg>
                </span>
                {{ tab.label }}
              </button>
            </nav>
          </div>

          <div class="flex-1 min-w-0">
            <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl shadow-sm overflow-hidden">
              <div class="p-6 md:p-8">
        <!-- Sessions: what is already running here.

             A card each rather than a full-width row: a busy machine runs
             several at once and the interesting part of one is three short
             lines, so rows spend the page's height on empty space and push
             the fourth session below the fold. -->
        <div v-if="activeTab === 'sessions'">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200 mb-4">
            Sessions
            <span class="text-gray-400 dark:text-zinc-500 font-medium">({{ liveCount }} running)</span>
          </h2>

          <p v-if="sessions.length === 0" class="text-xs text-gray-400 dark:text-zinc-500">
            No agents have run on this machine yet.
          </p>

          <div v-else class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            <div
              v-for="s in sessions"
              :key="s.id"
              class="border border-gray-100 dark:border-zinc-800 rounded-lg p-3 bg-gray-50/60 dark:bg-zinc-800/30 flex items-start gap-2"
            >
              <!-- The status as a dot: it is the thing being scanned for, and
                   a word in the corner of every card is not scannable. -->
              <span
                class="w-2 h-2 rounded-full shrink-0 mt-1.5"
                :class="DOTS[sessionTone(s.status)]"
                :title="s.status"
              />
              <div class="min-w-0 flex-1">
                <!-- The workspace is the heading, because it is what tells one
                     card from the next: the kind is the same on most of them.
                     And it is the way back to that workspace — this page says
                     what is running, and "what is it working on" is one click
                     away rather than a name to go and search for.

                     A link only where the session names a workspace id. The
                     name alone is not enough: naming is best-effort, so a card
                     can be headed "claude-code" and still belong somewhere
                     worth going. -->
                <button
                  v-if="s.workspaceId"
                  @click="router.push(`/workspaces/${s.workspaceId}`)"
                  class="block max-w-full text-xs font-bold text-gray-900 dark:text-zinc-100 truncate text-left hover:underline decoration-gray-300 dark:decoration-zinc-600 underline-offset-2"
                  title="Open this workspace"
                >
                  {{ sessionLabel(s) }}
                </button>
                <p v-else class="text-xs font-bold text-gray-900 dark:text-zinc-100 truncate">
                  {{ sessionLabel(s) }}
                </p>
                <p
                  class="text-[10px] mt-0.5 tabular-nums truncate"
                  :class="TONES[sessionTone(s.status)]"
                >
                  {{ sessionSummary(s) }}
                </p>
                <p v-if="s.error" class="text-[10px] text-red-500 mt-0.5">{{ s.error }}</p>
              </div>
              <!-- On the heading row rather than under it, and icons rather
                   than words: two more lines per card is what a list of five
                   costs, and these two actions are the same on every card, so
                   the words are read once and skipped after that. Named for a
                   screen reader and on hover, which a bare glyph is not. -->
              <div v-if="isSessionLive(s.status)" class="flex items-center gap-1 shrink-0">
                <button
                  @click="router.push(terminalPath(s))"
                  title="Open the terminal"
                  aria-label="Open the terminal"
                  class="w-7 h-7 flex items-center justify-center bg-black dark:bg-white text-white dark:text-black rounded-md hover:opacity-80 transition-all active:scale-95"
                >
                  <svg
                    class="w-3.5 h-3.5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2.5"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  >
                    <polyline points="4 17 10 11 4 5" />
                    <line x1="12" y1="19" x2="20" y2="19" />
                  </svg>
                </button>
                <button
                  :disabled="busy"
                  @click="stopSession(s.id)"
                  title="Stop this session"
                  aria-label="Stop this session"
                  class="w-7 h-7 flex items-center justify-center bg-white dark:bg-zinc-800 text-gray-600 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 rounded-md hover:text-red-600 dark:hover:text-red-400 hover:border-red-200 dark:hover:border-red-900/50 transition-all active:scale-95 disabled:opacity-50"
                >
                  <svg class="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
                    <rect x="5" y="5" width="14" height="14" rx="2" />
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- New Session.
             Every reason a launch would be refused is worked out before
             anything is sent and shown next to the button: the backend has
             five of them, and a form that fired and reported whichever it hit
             would make you press the button to find out whether you could
             press the button. -->
        <div v-if="activeTab === 'new-session'" class="space-y-4">
          <div>
            <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Run an agent here</h2>
            <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
              Starts it in the workspace's folder on this machine.
            </p>
          </div>

          <!-- The workspaces are listed, not folded into a dropdown.
               Which one to run in is the only real decision on this page, and
               a list is also the only place there is room to say, before it is
               picked, why one of them cannot take an agent. Real radios behind
               the card, so it is reachable from the keyboard. -->
          <div>
            <p
              id="launch-workspace-label"
              class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
            >
              Workspace
            </p>
            <p v-if="!launchChoices.length" class="text-[11px] text-gray-500 dark:text-zinc-400">
              No workspaces yet.
            </p>
            <div
              v-else
              role="radiogroup"
              aria-labelledby="launch-workspace-label"
              class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 max-h-56 overflow-y-auto custom-scrollbar"
            >
              <!-- `min-w-0` on the card itself, not just on the text inside
                   it: a grid item's minimum width is its content, so without
                   it a folder path too long for one column widens the card
                   past the panel instead of being truncated — visible on a
                   phone, where there is one column and paths are long. -->
              <label
                v-for="w in launchChoices"
                :key="w.id"
                class="flex items-center gap-2 px-3 py-2 rounded-lg border cursor-pointer transition-all min-w-0"
                :class="
                  launchWorkspace === w.id
                    ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800'
                    : 'border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600'
                "
              >
                <input
                  :id="`launch-workspace-${w.id}`"
                  type="radio"
                  name="launch-workspace"
                  class="sr-only"
                  :value="w.id"
                  :checked="launchWorkspace === w.id"
                  @change="launchWorkspace = w.id"
                />
                <span
                  class="w-3 h-3 rounded-full border flex items-center justify-center shrink-0"
                  :class="
                    launchWorkspace === w.id
                      ? 'border-gray-900 dark:border-white'
                      : 'border-gray-300 dark:border-zinc-600'
                  "
                >
                  <span
                    v-if="launchWorkspace === w.id"
                    class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white"
                  />
                </span>
                <span class="min-w-0">
                  <span class="block text-xs font-bold text-gray-900 dark:text-zinc-100 truncate">
                    {{ w.name }}
                  </span>
                  <!-- A ready workspace shows its folder, which is what tells
                       two similarly named ones apart; one that is not shows
                       why in the same place. Green for one already running an
                       agent elsewhere — that is not a problem, only a reason
                       this launch cannot start a second one — amber for
                       anything that actually needs fixing. -->
                  <span
                    v-if="w.note"
                    class="block text-[10px] truncate"
                    :class="
                      w.ready
                        ? 'font-mono text-gray-400 dark:text-zinc-500'
                        : w.tone === 'good'
                          ? 'font-bold uppercase tracking-wider text-emerald-600 dark:text-emerald-400'
                          : 'font-bold uppercase tracking-wider text-amber-600 dark:text-amber-400'
                    "
                  >
                    {{ w.note }}
                  </span>
                </span>
              </label>
            </div>
          </div>

          <AgentKindPicker id-prefix="launch-kind" v-model="launchKind" />

          <ClaudeOptionsPicker v-if="launchKind === 'claude-code'" id-prefix="launch" v-model="launchParams" :models="launchClaudeModels" />

          <!-- Only the gateway needs these, and only the agent is required.
               Agent first: the model list is per-agent, so there is nothing
               to suggest for the second field until the first is answered. -->
          <div v-if="launchKind === 'acp-gateway'" class="grid gap-3 md:grid-cols-2">
            <div>
              <label
                for="launch-agent"
                class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
                >Agent</label
              >
              <input
                id="launch-agent"
                v-model="launchParams.agent"
                type="text"
                list="launch-agent-options"
                spellcheck="false"
                autocapitalize="off"
                autocorrect="off"
                class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              />
              <!-- A datalist only ever suggests: typing anything else, including
                   while the list is empty or never arrives, is still accepted. -->
              <datalist id="launch-agent-options">
                <option v-for="a in launchAcpAgents" :key="a.id" :value="a.id">{{ a.name }}</option>
              </datalist>
            </div>
            <div>
              <label
                for="launch-model"
                class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
                >Model <span class="normal-case font-normal tracking-normal">(optional)</span></label
              >
              <input
                id="launch-model"
                v-model="launchParams.model"
                type="text"
                list="launch-model-options"
                placeholder="Gateway default"
                spellcheck="false"
                autocapitalize="off"
                autocorrect="off"
                class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              />
              <datalist id="launch-model-options">
                <option v-for="m in launchAcpModels" :key="m.id" :value="m.id">{{ m.name }}</option>
              </datalist>
            </div>
          </div>

          <!-- Said before the button rather than after it is pressed. -->
          <ul v-if="launchBlockers.length" class="space-y-1">
            <li
              v-for="b in launchBlockers"
              :key="b.reason"
              class="text-[11px] text-gray-500 dark:text-zinc-400"
            >
              {{ b.reason }}
              <router-link v-if="b.fix" :to="b.fix.to" class="underline">{{ b.fix.label }}</router-link>
            </li>
          </ul>

          <button
            :disabled="!canLaunch"
            @click="startAgent"
            class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {{ launching ? 'Starting…' : 'Start agent' }}
          </button>
        </div>

        <!-- Machine Info: what the machine has left -->
        <div v-if="activeTab === 'info'">
          <div class="flex items-baseline justify-between gap-3 mb-4">
            <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Resources</h2>
            <p v-if="machine.metrics" class="text-[11px] text-gray-400 dark:text-zinc-500">
              measured {{ formatAge(machine.metrics.reportedAt) }}
            </p>
          </div>

          <p v-if="!machine.metrics" class="text-xs text-gray-400 dark:text-zinc-500">
            This machine has not reported any readings yet.
          </p>

          <div v-else class="space-y-4">
            <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div>
                <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">CPU</p>
                <p class="text-base font-bold text-gray-900 dark:text-zinc-100 tabular-nums">
                  {{ formatPercent(machine.metrics.cpuPercent) }}
                </p>
              </div>
              <div>
                <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Memory</p>
                <p class="text-base font-bold text-gray-900 dark:text-zinc-100 tabular-nums">
                  {{ formatBytes(machine.metrics.memAvailable) }}
                </p>
                <p class="text-[11px] text-gray-400 dark:text-zinc-500">
                  free of {{ formatBytes(machine.metrics.memTotal) }}
                </p>
              </div>
              <div>
                <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Uptime</p>
                <p class="text-base font-bold text-gray-900 dark:text-zinc-100 tabular-nums">
                  {{ formatUptime(machine.metrics.uptimeSec) }}
                </p>
              </div>
              <!-- Windows has no load average. Nothing is shown rather than
                   three zeroes, which would read as a perfectly idle machine. -->
              <div v-if="formatLoadAvg(machine.metrics.loadAvg)">
                <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Load</p>
                <p class="text-base font-bold text-gray-900 dark:text-zinc-100 tabular-nums">
                  {{ formatLoadAvg(machine.metrics.loadAvg) }}
                </p>
              </div>
            </div>

            <div v-if="machine.metrics.disks?.length" class="space-y-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
              <!-- Per mount, never one number: a box can be 2% full and still
                   fail to check out a repository. -->
              <div v-for="disk in machine.metrics.disks" :key="disk.mount" class="space-y-1">
                <div class="flex items-baseline justify-between gap-3 text-[11px]">
                  <span class="font-mono text-gray-600 dark:text-zinc-300 truncate">{{ disk.mount }}</span>
                  <span class="text-gray-400 dark:text-zinc-500 tabular-nums shrink-0">
                    {{ formatBytes(disk.free) }} free of {{ formatBytes(disk.total) }}
                  </span>
                </div>
                <div v-if="diskUsedPercent(disk) !== null" class="h-1.5 rounded-full bg-gray-100 dark:bg-zinc-800 overflow-hidden">
                  <div
                    class="h-full rounded-full"
                    :class="diskUsedPercent(disk) > 90 ? 'bg-red-500' : 'bg-gray-800 dark:bg-zinc-300'"
                    :style="{ width: `${diskUsedPercent(disk)}%` }"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Settings: one row instead of a stack, with the danger zone kept
             visually distinct (red, its own background) so sitting beside the
             harmless setting does not make it read as just another neutral
             option.

             The name is not one of them: it is set when the daemon enrols
             (`agentrqd enroll --name`, defaulting to the hostname), and a
             name editable here as well is one that can disagree with the box
             it belongs to. -->
        <div v-if="activeTab === 'settings'" class="space-y-6">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Settings</h2>

          <div class="grid gap-4 sm:grid-cols-2">
            <div class="space-y-2">
              <p class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 ml-1">
                Availability
              </p>
              <div
                class="flex items-center justify-between gap-4 p-4 bg-gray-50 dark:bg-zinc-800/50 border border-gray-100 dark:border-zinc-800 rounded-lg"
              >
                <div class="min-w-0">
                  <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">
                    {{ machine.enabled ? 'Enabled' : 'Disabled' }}
                  </p>
                  <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
                    Disabling closes this machine's connection immediately and refuses the next one.
                  </p>
                </div>
                <button
                  :disabled="busy"
                  @click="toggleEnabled"
                  class="hidden sm:block shrink-0 px-5 py-2.5 bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[10px] font-black uppercase tracking-widest rounded-lg hover:border-gray-900 dark:hover:border-white transition-all active:scale-95 disabled:opacity-50"
                >
                  {{ machine.enabled ? 'Disable' : 'Enable' }}
                </button>
                <!-- A phone gets the switch workspace settings uses instead. -->
                <button
                  type="button"
                  role="switch"
                  :aria-checked="machine.enabled"
                  aria-label="Enabled"
                  :disabled="busy"
                  @click="toggleEnabled"
                  class="sm:hidden shrink-0 w-11 h-6 rounded-full border transition-colors relative disabled:opacity-50"
                  :class="machine.enabled ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'"
                >
                  <span
                    class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                    :class="machine.enabled ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"
                  ></span>
                </button>
              </div>
            </div>

            <div class="space-y-2">
              <p class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 ml-1">
                Daemon
              </p>
              <div
                class="flex items-center justify-between gap-4 p-4 bg-gray-50 dark:bg-zinc-800/50 border border-gray-100 dark:border-zinc-800 rounded-lg"
              >
                <div class="min-w-0">
                  <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">
                    {{ updating ? `Update agentrqd to ${machine.availableVersion}` : 'Restart agentrqd' }}
                  </p>
                  <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
                    {{ canControl ? updateText : handUpdateText }}
                  </p>
                </div>
                <button
                  v-if="canControl"
                  :disabled="busy || !machine.online"
                  @click="showRestart = true"
                  :aria-label="updating ? 'Update daemon' : 'Restart daemon'"
                  class="shrink-0 p-2.5 sm:px-5 sm:py-2.5 bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[10px] font-black uppercase tracking-widest rounded-lg hover:border-gray-900 dark:hover:border-white transition-all active:scale-95 disabled:opacity-50"
                >
                  <span class="hidden sm:inline">{{ updating ? 'Update daemon' : 'Restart daemon' }}</span>
                  <svg class="sm:hidden w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="REFRESH_ICON" />
                  </svg>
                </button>
              </div>
            </div>

            <div class="space-y-2">
              <p class="block text-[10px] font-black uppercase tracking-widest text-red-400 dark:text-red-500 ml-1">
                Danger Zone
              </p>
              <div
                class="flex items-center justify-between gap-4 p-4 bg-red-50/40 dark:bg-red-500/5 border border-red-100 dark:border-red-900/30 rounded-lg"
              >
                <div class="min-w-0">
                  <p class="text-sm font-bold text-red-600 dark:text-red-500">Delete this machine</p>
                  <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-0.5">{{ deleteText }}</p>
                </div>
                <button
                  :disabled="busy"
                  @click="showDelete = true"
                  class="shrink-0 px-5 py-2.5 bg-white dark:bg-zinc-900 text-red-600 dark:text-red-400 border border-red-200 dark:border-red-900/50 text-[10px] font-black uppercase tracking-widest rounded-lg hover:bg-red-50 dark:hover:bg-red-500/10 transition-all active:scale-95 disabled:opacity-50"
                >
                  Delete
                </button>
              </div>
            </div>
          </div>
        </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <DeleteModal
      :show="showDelete"
      title="Delete machine"
      :message="deleteText"
      @close="showDelete = false"
      @confirm="confirmDelete"
    />

    <!-- A bare "Update?" is not consent: the person pressing it is usually not
         the person whose agent is mid-task. One confirm for the one command. -->
    <DeleteModal
      :show="showRestart"
      :title="updating ? `Update agentrqd to ${machine?.availableVersion}` : 'Restart agentrqd'"
      :message="updateText"
      :confirm-label="updating ? 'Update' : 'Restart'"
      @close="showRestart = false"
      @confirm="confirmRestart"
    />

  </div>
</template>
