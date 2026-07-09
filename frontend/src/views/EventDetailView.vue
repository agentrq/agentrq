<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getEvent, updateEvent, deleteEvent, fetchEvents, fetchEventTriggers, createEventTrigger, deleteEventTrigger, fetchEventTasks } from '../api'
import { useToasts } from '../composables/useToasts'
import { useCron } from '../composables/useCron'
import { useWorkspaceStore } from '../stores/workspaceStore'
import DeleteModal from '../components/DeleteModal.vue'

const route = useRoute()
const router = useRouter()
const { notifyError, notifySuccess } = useToasts()
const { getNextRunLabel, daysOptions } = useCron()
const workspaceStore = useWorkspaceStore()

const eventId = route.params.id

// ── event ──────────────────────────────────────────────────────────────────────

const event = ref(null)
const loadingEvent = ref(false)

async function loadEvent() {
  loadingEvent.value = true
  try {
    const data = await getEvent(eventId)
    event.value = data.event
  } catch (e) {
    notifyError(e.message)
  } finally {
    loadingEvent.value = false
  }
}

// edit payload guidelines
const editingGuidelines = ref(false)
const guidelinesInput = ref('')
const savingGuidelines = ref(false)

function startEditGuidelines() {
  guidelinesInput.value = event.value?.payloadGuidelines ?? ''
  editingGuidelines.value = true
}

async function saveGuidelines() {
  savingGuidelines.value = true
  try {
    const data = await updateEvent(eventId, guidelinesInput.value.trim())
    event.value = data.event
    editingGuidelines.value = false
    notifySuccess('Payload guidelines updated')
  } catch (e) {
    notifyError(e.message)
  } finally {
    savingGuidelines.value = false
  }
}

// ── triggers ───────────────────────────────────────────────────────────────────

const triggers = ref([])
const loadingTriggers = ref(false)

async function loadTriggers() {
  loadingTriggers.value = true
  try {
    const data = await fetchEventTriggers(eventId)
    triggers.value = data.eventTriggers ?? []
  } catch (e) {
    notifyError(e.message)
  } finally {
    loadingTriggers.value = false
  }
}

// create trigger form
const showTriggerForm = ref(false)
const creatingTrigger = ref(false)

const triggerForm = ref({
  workspaceId: '',
  title: '',
  body: '',
  assignee: 'agent',
  allowAllCommands: false,
  cronSchedule: '',
  emitEventId: '',
})

// events list for the "emit on completion" selector
const allEvents = ref([])
async function loadAllEvents() {
  try {
    const data = await fetchEvents()
    // exclude the current event from the list
    allEvents.value = (data.events ?? []).filter(e => e.id !== eventId)
  } catch (_) {}
}

// trigger cron UI
const triggerScheduleType = ref('none')
const triggerOneTimeDate = ref('')
const triggerRepeatPreset = ref('daily')
const triggerRepeatTime = ref('09:00')
const triggerSelectedDays = ref([1, 2, 3, 4, 5])

const triggerNextRunPreview = computed(() => {
  if (triggerScheduleType.value === 'none' || !triggerForm.value.cronSchedule) return ''
  return getNextRunLabel(triggerForm.value.cronSchedule)
})

watch([triggerScheduleType, triggerOneTimeDate, triggerRepeatPreset, triggerRepeatTime, triggerSelectedDays], () => {
  if (triggerScheduleType.value === 'none') { triggerForm.value.cronSchedule = ''; return }
  if (triggerScheduleType.value === 'onetime') {
    if (!triggerOneTimeDate.value) { triggerForm.value.cronSchedule = ''; return }
    const d = new Date(triggerOneTimeDate.value)
    triggerForm.value.cronSchedule = `${d.getUTCMinutes()} ${d.getUTCHours()} ${d.getUTCDate()} ${d.getUTCMonth() + 1} *`
    return
  }
  const [lh, lm] = triggerRepeatTime.value.split(':').map(Number)
  const d = new Date(); d.setHours(lh, lm, 0, 0)
  const minutes = d.getUTCMinutes()
  const hours = d.getUTCHours()
  const p = triggerRepeatPreset.value
  if (p === 'hourly') triggerForm.value.cronSchedule = '0 * * * *'
  else if (p === '2hour') triggerForm.value.cronSchedule = '0 */2 * * *'
  else if (p === '12hour') {
    const h2 = new Date(d); h2.setHours(h2.getHours() + 12)
    const hrs = [d.getUTCHours(), h2.getUTCHours()].sort((a, b) => a - b).join(',')
    triggerForm.value.cronSchedule = `${minutes} ${hrs} * * *`
  } else if (p === 'daily') triggerForm.value.cronSchedule = `${minutes} ${hours} * * *`
  else if (p === 'weekly') { const utcDay = d.getUTCDay(); triggerForm.value.cronSchedule = `${minutes} ${hours} * * ${utcDay}` }
  else if (p === 'monthly') { const dd = new Date(); dd.setHours(lh, lm, 0, 0); dd.setDate(1); triggerForm.value.cronSchedule = `${minutes} ${hours} ${dd.getUTCDate()} * *` }
  else if (p === 'custom') {
    const utcDays = new Set()
    triggerSelectedDays.value.forEach(day => {
      const dd = new Date(); dd.setHours(lh, lm, 0, 0)
      dd.setDate(dd.getDate() + (day - dd.getDay()))
      utcDays.add(dd.getUTCDay())
    })
    triggerForm.value.cronSchedule = `${minutes} ${hours} * * ${[...utcDays].sort().join(',')}`
  }
}, { deep: true })

function toggleTriggerDay(day) {
  const idx = triggerSelectedDays.value.indexOf(day)
  if (idx === -1) triggerSelectedDays.value.push(day)
  else if (triggerSelectedDays.value.length > 1) triggerSelectedDays.value.splice(idx, 1)
}

function insertTemplate(token) {
  const body = triggerForm.value.body
  triggerForm.value.body = body ? body + ' ' + token : token
}

function resetTriggerForm() {
  triggerForm.value = { workspaceId: '', title: '', body: '', assignee: 'agent', allowAllCommands: false, cronSchedule: '', emitEventId: '' }
  triggerScheduleType.value = 'none'
  triggerOneTimeDate.value = ''
  triggerRepeatPreset.value = 'daily'
  triggerRepeatTime.value = '09:00'
  triggerSelectedDays.value = [1, 2, 3, 4, 5]
  showTriggerForm.value = false
}

async function handleCreateTrigger() {
  if (!triggerForm.value.workspaceId || !triggerForm.value.title) return
  creatingTrigger.value = true
  try {
    const data = await createEventTrigger(eventId, {
      workspaceId: triggerForm.value.workspaceId,
      title: triggerForm.value.title,
      body: triggerForm.value.body,
      assignee: triggerForm.value.assignee,
      cronSchedule: triggerForm.value.cronSchedule,
      allowAllCommands: triggerForm.value.allowAllCommands,
      emitEventId: triggerForm.value.emitEventId,
    })
    triggers.value.unshift(data.eventTrigger)
    notifySuccess('Trigger created')
    resetTriggerForm()
  } catch (e) {
    notifyError(e.message)
  } finally {
    creatingTrigger.value = false
  }
}

// delete event
const showDeleteEventModal = ref(false)
const deletingEvent = ref(false)

async function handleDeleteEvent() {
  deletingEvent.value = true
  try {
    await deleteEvent(eventId)
    notifySuccess('Event deleted')
    router.push('/events')
  } catch (e) {
    notifyError(e.message)
  } finally {
    deletingEvent.value = false
    showDeleteEventModal.value = false
  }
}

// delete trigger
const showDeleteTriggerModal = ref(false)
const deletingTrigger = ref(null)
const deletingTriggerInProgress = ref(false)

function confirmDeleteTrigger(t) {
  deletingTrigger.value = t
  showDeleteTriggerModal.value = true
}

async function handleDeleteTrigger() {
  if (!deletingTrigger.value) return
  deletingTriggerInProgress.value = true
  try {
    await deleteEventTrigger(eventId, deletingTrigger.value.id)
    triggers.value = triggers.value.filter(t => t.id !== deletingTrigger.value.id)
    notifySuccess('Trigger deleted')
  } catch (e) {
    notifyError(e.message)
  } finally {
    deletingTriggerInProgress.value = false
    showDeleteTriggerModal.value = false
    deletingTrigger.value = null
  }
}

function workspaceName(wsId) {
  return workspaceStore.workspaces.find(w => w.id === wsId)?.name ?? wsId
}

// ── tasks ─────────────────────────────────────────────────────────────────────

const TASKS_PAGE_SIZE = 10
const tasks = ref([])
const loadingTasks = ref(false)
const visibleTaskCount = ref(TASKS_PAGE_SIZE)
const visibleTasks = computed(() => tasks.value.slice(0, visibleTaskCount.value))
const hasMoreTasks = computed(() => visibleTaskCount.value < tasks.value.length)

function loadMoreTasks() {
  visibleTaskCount.value += TASKS_PAGE_SIZE
}

async function loadTasks() {
  loadingTasks.value = true
  try {
    const data = await fetchEventTasks(eventId)
    tasks.value = data.tasks ?? []
    visibleTaskCount.value = TASKS_PAGE_SIZE
  } catch (e) {
    notifyError(e.message)
  } finally {
    loadingTasks.value = false
  }
}

function formatDate(iso) {
  if (!iso) return ''
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function statusColor(s) {
  const map = {
    notstarted: 'text-gray-400 dark:text-zinc-500',
    ongoing: 'text-sky-500 dark:text-sky-400',
    completed: 'text-emerald-500 dark:text-emerald-400',
    rejected: 'text-red-500 dark:text-red-400',
    blocked: 'text-amber-500 dark:text-amber-400',
    cron: 'text-violet-500 dark:text-violet-400',
  }
  return map[s] ?? 'text-gray-400'
}

// ── init ──────────────────────────────────────────────────────────────────────

onMounted(async () => {
  if (!workspaceStore.workspaces.length) await workspaceStore.fetchWorkspaces()
  await Promise.all([loadEvent(), loadTriggers(), loadTasks(), loadAllEvents()])
})
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">

    <!-- Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <div v-if="loadingEvent" class="h-8 w-64 bg-gray-100 dark:bg-zinc-800 animate-pulse rounded-lg"></div>
        <template v-else>
          <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate">
            <span class="opacity-50 cursor-pointer hover:opacity-100 transition-opacity" @click="router.push('/events')">Events</span>
            <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium">/</span>
            <span class="font-mono">{{ event?.name }}</span>
          </h1>
        </template>
      </div>
      <div v-if="!loadingEvent && !editingGuidelines" class="flex items-center gap-1 shrink-0">
        <button @click="startEditGuidelines"
                class="p-2 text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-lg transition-all active:scale-95">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
          </svg>
        </button>
        <button @click="showDeleteEventModal = true"
                class="p-2 text-gray-400 dark:text-zinc-500 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 rounded-lg transition-all active:scale-95">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
          </svg>
        </button>
      </div>
    </div>

    <div class="px-4 space-y-8 pb-10">

      <!-- Payload Guidelines Section -->
      <Transition name="slide-down">
        <div v-if="editingGuidelines" class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-4">
          <textarea
            v-model="guidelinesInput"
            rows="3"
            placeholder="Describe what agents should include in the payload when publishing this event…"
            class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 resize-none focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all"
            @keydown.escape="editingGuidelines = false"
          ></textarea>
          <div class="flex items-center gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
            <button @click="saveGuidelines" :disabled="savingGuidelines"
                    class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
              {{ savingGuidelines ? 'Saving…' : 'Save' }}
            </button>
            <button @click="editingGuidelines = false"
                    class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
              Cancel
            </button>
          </div>
        </div>
      </Transition>

      <!-- Triggers Section -->
      <div>
        <div class="flex items-center justify-between mb-3">
          <div>
            <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Triggers</h2>
            <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-0.5">Workspaces that receive a task when this event fires.</p>
          </div>
          <button
            @click="showTriggerForm = !showTriggerForm"
            class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0">
            + Add Trigger
          </button>
        </div>

        <!-- Create Trigger Form -->
        <Transition name="slide-down">
          <div v-if="showTriggerForm" class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-4 mb-4">
            <h3 class="text-xs font-bold text-gray-800 dark:text-zinc-200">New Trigger</h3>

            <!-- Workspace Picker -->
            <div>
              <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">
                Target Workspace <span class="text-red-500">*</span>
              </label>
              <select
                v-model="triggerForm.workspaceId"
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all">
                <option value="">Select workspace…</option>
                <option v-for="ws in workspaceStore.workspaces" :key="ws.id" :value="ws.id">{{ ws.name }}</option>
              </select>
            </div>

            <!-- Task Title -->
            <div>
              <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">
                Task Title <span class="text-red-500">*</span>
              </label>
              <input
                v-model="triggerForm.title"
                type="text"
                placeholder="e.g. Review the latest deploy"
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all"
              />
            </div>

            <!-- Task Body -->
            <div>
              <div class="flex items-center justify-between mb-1">
                <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Task Instructions</label>
                <div class="flex items-center gap-1.5">
                  <span class="text-[10px] text-gray-400 dark:text-zinc-500">Insert:</span>
                  <button
                    type="button"
                    @click="insertTemplate('{{EVENT_PAYLOAD}}')"
                    class="px-2 py-0.5 text-[10px] font-mono font-bold bg-violet-50 dark:bg-violet-900/20 text-violet-600 dark:text-violet-400 border border-violet-200 dark:border-violet-800 rounded hover:bg-violet-100 dark:hover:bg-violet-900/40 transition-colors">
                    {{EVENT_PAYLOAD}}
                  </button>
                  <button
                    type="button"
                    @click="insertTemplate('{{EVENT_FAQ}}')"
                    class="px-2 py-0.5 text-[10px] font-mono font-bold bg-sky-50 dark:bg-sky-900/20 text-sky-600 dark:text-sky-400 border border-sky-200 dark:border-sky-800 rounded hover:bg-sky-100 dark:hover:bg-sky-900/40 transition-colors">
                    {{EVENT_FAQ}}
                  </button>
                </div>
              </div>
              <textarea
                v-model="triggerForm.body"
                rows="3"
                placeholder="Describe what the agent should do. Use {{EVENT_PAYLOAD}} and {{EVENT_FAQ}} as template variables."
                class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 resize-none focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all"
              ></textarea>
            </div>

            <!-- Assignee + YOLO -->
            <div class="flex flex-wrap gap-6">
              <div>
                <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1.5">Responsibility</label>
                <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-lg w-fit">
                  <button type="button" @click="triggerForm.assignee = 'agent'"
                    :class="triggerForm.assignee === 'agent' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'"
                    class="px-5 py-1.5 rounded-md text-[10px] font-semibold transition-all">Agent</button>
                  <button type="button" @click="triggerForm.assignee = 'human'"
                    :class="triggerForm.assignee === 'human' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'"
                    class="px-5 py-1.5 rounded-md text-[10px] font-semibold transition-all">Human</button>
                </div>
              </div>
              <div v-if="triggerForm.assignee === 'agent'">
                <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1.5">YOLO (Auto-Allow)</label>
                <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-lg w-fit">
                  <button type="button" @click="triggerForm.allowAllCommands = true"
                    :class="triggerForm.allowAllCommands ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'"
                    class="px-5 py-1.5 rounded-md text-[10px] font-semibold uppercase transition-all">ON</button>
                  <button type="button" @click="triggerForm.allowAllCommands = false"
                    :class="!triggerForm.allowAllCommands ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'"
                    class="px-5 py-1.5 rounded-md text-[10px] font-semibold uppercase transition-all">OFF</button>
                </div>
              </div>
            </div>

            <!-- Emit Event on Completion -->
            <div v-if="allEvents.length > 0" class="pt-2 border-t border-gray-100 dark:border-zinc-800">
              <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">Emit Event on Completion</label>
              <select v-model="triggerForm.emitEventId"
                class="w-full bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg px-3 py-2 text-sm text-gray-900 dark:text-zinc-50 outline-none focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all">
                <option value="">None</option>
                <option v-for="ev in allEvents" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
              </select>
            </div>

            <!-- Form Actions -->
            <div class="flex items-center gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
              <button
                @click="handleCreateTrigger"
                :disabled="creatingTrigger || !triggerForm.workspaceId || !triggerForm.title"
                class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
                {{ creatingTrigger ? 'Creating…' : 'Create Trigger' }}
              </button>
              <button
                @click="resetTriggerForm"
                class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
                Cancel
              </button>
            </div>
          </div>
        </Transition>

        <!-- Triggers List -->
        <div v-if="loadingTriggers" class="text-sm text-gray-500 dark:text-zinc-400 py-4 text-center">Loading triggers…</div>
        <div v-else-if="triggers.length === 0 && !showTriggerForm"
          class="py-8 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-500 dark:text-zinc-400">No triggers yet</p>
          <p class="text-xs text-gray-400 dark:text-zinc-500 mt-1">Add a trigger to auto-create tasks in a workspace when this event fires.</p>
        </div>
        <div v-else class="space-y-2">
          <div v-for="t in triggers" :key="t.id"
            class="group flex items-start gap-3 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all">
            <div class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700 mt-0.5">
              <svg class="w-3.5 h-3.5 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
              </svg>
            </div>
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-wider">{{ workspaceName(t.workspaceId) }}</span>
                <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
                <span class="text-xs font-bold text-gray-800 dark:text-zinc-200 truncate">{{ t.title }}</span>
              </div>
              <p v-if="t.body" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ t.body }}</p>
              <div class="flex items-center gap-3 mt-1 flex-wrap">
                <span class="text-[10px] text-gray-400 dark:text-zinc-500">{{ t.assignee }}</span>
                <code v-if="t.cronSchedule" class="text-[10px] font-mono text-violet-600 dark:text-violet-400 bg-violet-50 dark:bg-violet-900/20 px-1.5 py-0.5 rounded">{{ t.cronSchedule }}</code>
              </div>
            </div>
            <button
              @click="confirmDeleteTrigger(t)"
              class="p-1.5 text-gray-300 dark:text-zinc-600 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 rounded transition-all opacity-0 group-hover:opacity-100 shrink-0">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          </div>
        </div>
      </div>

      <!-- Tasks From This Event -->
      <div>
        <div class="mb-3">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Tasks created from this event</h2>
          <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-0.5">Tasks automatically spawned each time this event fired.</p>
        </div>

        <div v-if="loadingTasks" class="text-sm text-gray-500 dark:text-zinc-400 py-4 text-center">Loading tasks…</div>
        <div v-else-if="tasks.length === 0"
          class="py-8 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-500 dark:text-zinc-400">No tasks yet</p>
          <p class="text-xs text-gray-400 dark:text-zinc-500 mt-1">Tasks will appear here after the event fires and triggers are executed.</p>
        </div>
        <div v-else class="space-y-2">
          <router-link
            v-for="t in visibleTasks"
            :key="t.id"
            :to="`/workspaces/${t.workspaceId}/tasks/${t.id}`"
            class="flex items-center gap-3 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-semibold text-gray-800 dark:text-zinc-200 truncate">{{ t.title }}</p>
              <div class="flex items-center gap-3 mt-0.5 flex-wrap">
                <span :class="['text-[11px] font-semibold', statusColor(t.status)]">{{ t.status }}</span>
                <span class="text-[11px] text-gray-400 dark:text-zinc-500">{{ t.assignee }}</span>
                <span class="text-[11px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatDate(t.createdAt) }}</span>
              </div>
            </div>
            <svg class="w-4 h-4 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
          </router-link>
          <button
            v-if="hasMoreTasks"
            @click="loadMoreTasks"
            class="w-full py-2 text-[11px] font-bold uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all">
            Load more ({{ tasks.length - visibleTaskCount }} remaining)
          </button>
        </div>
      </div>
    </div>

    <!-- Delete Event Modal -->
    <DeleteModal
      :show="showDeleteEventModal"
      title="Delete Event"
      :taskTitle="event?.name ?? ''"
      @close="showDeleteEventModal = false"
      @confirm="handleDeleteEvent"
    />

    <!-- Delete Trigger Modal -->
    <DeleteModal
      :show="showDeleteTriggerModal"
      title="Delete Trigger"
      :taskTitle="deletingTrigger?.title ?? ''"
      @close="showDeleteTriggerModal = false; deletingTrigger = null"
      @confirm="handleDeleteTrigger"
    />
  </div>
</template>

<style scoped>
.slide-down-enter-active { transition: all 0.2s ease; }
.slide-down-leave-active { transition: all 0.15s ease; }
.slide-down-enter-from { opacity: 0; transform: translateY(-8px); }
.slide-down-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
