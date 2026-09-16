<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
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
import { useMachineDetail, updateConsequence, deleteConsequence } from '../composables/useMachineDetail'
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
} from '../composables/useMachineFormat'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import DeleteModal from '../components/DeleteModal.vue'
import { useAgentLaunch, KINDS } from '../composables/useAgentLaunch'

const route = useRoute()
const router = useRouter()
const { notifySuccess, notifyError } = useToasts()

const machineId = String(route.params.id ?? '')
const detail = useMachineDetail({ machineId })
const { machine, sessions, liveSessions, loading, error, busy } = detail

const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

const renaming = ref(false)
const draftName = ref('')
const showDelete = ref(false)
const showUpdate = ref(false)

// Destructured because refs keep their reactivity through it, and the
// alternative — reaching through `launcher.x.value` in every binding — is
// where a missing `.value` hides.
const launcher = useAgentLaunch({ machine, sessions })
const {
  workspaces: launchWorkspaces,
  workspaceId: launchWorkspace,
  kind: launchKind,
  params: launchParams,
  blockers: launchBlockers,
  canLaunch,
  launching,
  error: launchError,
} = launcher

const liveCount = computed(() => liveSessions.value.length)
const updateText = computed(() => updateConsequence(liveCount.value))
const deleteText = computed(() => deleteConsequence(machine.value, liveCount.value))

const TONES = {
  good: 'text-emerald-600 dark:text-emerald-400',
  pending: 'text-amber-600 dark:text-amber-400',
  bad: 'text-red-600 dark:text-red-400',
  muted: 'text-gray-400 dark:text-zinc-500',
}

onMounted(() => {
  detail.load()
  launcher.load()
  onEvent(detail.handleEvent)
  connect()
})
onUnmounted(disconnect)

function startRename() {
  draftName.value = machine.value?.name ?? ''
  renaming.value = true
}

async function saveName() {
  const changed = await detail.rename(draftName.value)
  renaming.value = false
  if (changed) notifySuccess('Machine renamed')
  else if (error.value) notifyError(error.value)
}

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

async function confirmUpdate() {
  showUpdate.value = false
  if (await detail.approveUpdate()) {
    notifySuccess('The machine is updating; its sessions will come back as new terminals')
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
}

async function stopSession(id) {
  if (await detail.stop(id)) notifySuccess('Asked the machine to stop that session')
  else notifyError(error.value)
}
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
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
          {{ machine.os }}/{{ machine.arch }} · agentrqd {{ machine.version || '—' }} ·
          {{ machine.online ? 'online' : 'offline' }}
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
            <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">{{ updateText }}</p>
            <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">
              Sessions come back as new terminals: same agent, same folder, empty scrollback.
            </p>
          </div>
          <button
            :disabled="busy"
            @click="showUpdate = true"
            class="shrink-0 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50"
          >
            Update and restart
          </button>
        </div>

        <!-- What the machine has left -->
        <div class="border border-gray-100 dark:border-zinc-800 rounded-xl p-5 bg-white dark:bg-zinc-900">
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

        <!-- Run an agent here.
             Every reason a launch would be refused is worked out before
             anything is sent and shown next to the button: the backend has
             five of them, and a form that fired and reported whichever it hit
             would make you press the button to find out whether you could
             press the button. -->
        <div class="border border-gray-100 dark:border-zinc-800 rounded-xl p-5 bg-white dark:bg-zinc-900 space-y-4">
          <div>
            <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Run an agent here</h2>
            <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
              Starts it in the workspace's folder on this machine.
            </p>
          </div>

          <div class="grid gap-3 md:grid-cols-2">
            <div>
              <label
                for="launch-workspace"
                class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
                >Workspace</label
              >
              <select
                id="launch-workspace"
                v-model="launchWorkspace"
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              >
                <option value="">Choose a workspace…</option>
                <option v-for="w in launchWorkspaces" :key="w.id" :value="w.id">
                  {{ w.name }}
                </option>
              </select>
            </div>

            <div>
              <label
                for="launch-kind"
                class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
                >What to run</label
              >
              <select
                id="launch-kind"
                v-model="launchKind"
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              >
                <option v-for="k in KINDS" :key="k.id" :value="k.id">{{ k.label }}</option>
              </select>
            </div>
          </div>

          <!-- Only the gateway needs these, and it needs both. -->
          <div v-if="launchKind === 'acp-gateway'" class="grid gap-3 md:grid-cols-2">
            <div>
              <label
                for="launch-model"
                class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1"
                >Model</label
              >
              <input
                id="launch-model"
                v-model="launchParams.model"
                type="text"
                spellcheck="false"
                autocapitalize="off"
                autocorrect="off"
                class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              />
            </div>
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
                spellcheck="false"
                autocapitalize="off"
                autocorrect="off"
                class="w-full px-3 py-2 text-sm font-mono border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              />
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

        <!-- Sessions -->
        <div class="border border-gray-100 dark:border-zinc-800 rounded-xl p-5 bg-white dark:bg-zinc-900">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200 mb-4">
            Sessions
            <span class="text-gray-400 dark:text-zinc-500 font-medium">({{ liveCount }} running)</span>
          </h2>

          <p v-if="sessions.length === 0" class="text-xs text-gray-400 dark:text-zinc-500">
            No agents have run on this machine yet.
          </p>

          <div v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
            <div v-for="s in sessions" :key="s.id" class="py-3 flex items-center justify-between gap-3">
              <div class="min-w-0">
                <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ s.kind || 'agent' }}</p>
                <p class="text-[11px] mt-0.5" :class="TONES[sessionTone(s.status)]">
                  {{ s.status }}
                  <span v-if="s.exitCode !== null && s.exitCode !== undefined" class="tabular-nums"
                    >· exit {{ s.exitCode }}</span
                  >
                  <span v-if="s.restored"> · restored</span>
                </p>
                <p v-if="s.error" class="text-[11px] text-red-500 mt-0.5">{{ s.error }}</p>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <button
                  v-if="isSessionLive(s.status)"
                  @click="router.push(`/sessions/${s.id}`)"
                  class="px-3 py-1.5 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95"
                >
                  Terminal
                </button>
                <button
                  v-if="isSessionLive(s.status)"
                  :disabled="busy"
                  @click="stopSession(s.id)"
                  class="px-3 py-1.5 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 disabled:opacity-50"
                >
                  Stop
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Settings -->
        <div class="border border-gray-100 dark:border-zinc-800 rounded-xl p-5 bg-white dark:bg-zinc-900 space-y-4">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Settings</h2>

          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">Name</p>
              <input
                v-if="renaming"
                v-model="draftName"
                @keyup.enter="saveName"
                type="text"
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white"
              />
              <p v-else class="text-sm text-gray-900 dark:text-zinc-100">{{ machine.name || '—' }}</p>
            </div>
            <button
              :disabled="busy"
              @click="renaming ? saveName() : startRename()"
              class="shrink-0 px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 disabled:opacity-50"
            >
              {{ renaming ? 'Save' : 'Rename' }}
            </button>
          </div>

          <div class="flex items-center justify-between gap-3 pt-4 border-t border-gray-100 dark:border-zinc-800">
            <div class="min-w-0">
              <p class="text-sm text-gray-900 dark:text-zinc-100">
                {{ machine.enabled ? 'Enabled' : 'Disabled' }}
              </p>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">
                Disabling closes this machine's connection immediately and refuses the next one.
              </p>
            </div>
            <button
              :disabled="busy"
              @click="toggleEnabled"
              class="shrink-0 px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 disabled:opacity-50"
            >
              {{ machine.enabled ? 'Disable' : 'Enable' }}
            </button>
          </div>

          <div class="flex items-center justify-between gap-3 pt-4 border-t border-gray-100 dark:border-zinc-800">
            <div class="min-w-0">
              <p class="text-sm text-gray-900 dark:text-zinc-100">Delete this machine</p>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">{{ deleteText }}</p>
            </div>
            <button
              :disabled="busy"
              @click="showDelete = true"
              class="shrink-0 px-4 py-2 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-red-50 dark:hover:bg-red-500/10 transition-all active:scale-95 disabled:opacity-50"
            >
              Delete
            </button>
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
         the person whose agent is mid-task. -->
    <DeleteModal
      :show="showUpdate"
      title="Update and restart"
      :message="updateText"
      @close="showUpdate = false"
      @confirm="confirmUpdate"
    />

  </div>
</template>
