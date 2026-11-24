<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
import { ref, computed, nextTick, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getEvent, updateEvent, deleteEvent, fetchEvents, fetchEventTriggers, createEventTrigger, updateEventTrigger, deleteEventTrigger, fetchEventTasks } from '../api'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useTooltipStore } from '../stores/tooltipStore'
import { triggerWorkspaceTooltip, workspaceLabel } from '../composables/useWorkflowLabels'
import { taskDotClass } from '../composables/useTaskStatusStyle'
import { STATUS_LABELS } from '../composables/useStreamToasts'
import { usesCommandKey } from '../composables/useKeyboardShortcuts'
import { ACTIONS, CANCEL_BUTTON, FORM_CARD, PRIMARY_BUTTON, fieldClass } from '../utils/formStyles'
import { formKeyAction, submitHint } from '../utils/forms'
import DeleteModal from '../components/DeleteModal.vue'
import EventTriggerForm from '../components/EventTriggerForm.vue'
import FormField from '../components/FormField.vue'
import LoadingState from '../components/LoadingState.vue'
import LoadMoreButton from '../components/LoadMoreButton.vue'

const route = useRoute()
const router = useRouter()
const { notifyError, notifySuccess } = useToasts()
const workspaceStore = useWorkspaceStore()
const tooltipStore = useTooltipStore()
const saveKeys = submitHint(usesCommandKey())

const eventId = route.params.id

// ── event ──────────────────────────────────────────────────────────────────────

const event = ref(null)
const loadingEvent = ref(false)

async function loadEvent({ quiet = false } = {}) {
  if (!quiet) loadingEvent.value = true
  try {
    const data = await getEvent(eventId)
    event.value = data.event
  } catch (e) {
    notifyError(e.message)
  } finally {
    loadingEvent.value = false
  }
}

// payload guidelines, edited in place
const editingGuidelines = ref(false)
const guidelinesInput = ref('')
const savingGuidelines = ref(false)
const guidelinesEl = ref(null)

async function startEditGuidelines() {
  guidelinesInput.value = event.value?.payloadGuidelines ?? ''
  editingGuidelines.value = true
  await nextTick()
  guidelinesEl.value?.focus()
}

async function saveGuidelines() {
  if (savingGuidelines.value) return
  savingGuidelines.value = true
  try {
    const data = await updateEvent(eventId, guidelinesInput.value.trim())
    event.value = data.event
    editingGuidelines.value = false
    notifySuccess('Payload guidelines saved')
  } catch (e) {
    notifyError(e.message)
  } finally {
    savingGuidelines.value = false
  }
}

function onGuidelinesKeydown(e) {
  const action = formKeyAction(e)
  if (!action) return
  e.preventDefault()
  if (action === 'submit') saveGuidelines()
  else editingGuidelines.value = false
}

// ── triggers ───────────────────────────────────────────────────────────────────

const triggers = ref([])
const loadingTriggers = ref(false)

async function loadTriggers({ quiet = false } = {}) {
  if (!quiet) loadingTriggers.value = true
  try {
    const data = await fetchEventTriggers(eventId)
    triggers.value = data.eventTriggers ?? []
  } catch (e) {
    notifyError(e.message)
  } finally {
    loadingTriggers.value = false
  }
}

// The form opens at the top of the list for a new trigger, and in place of
// the trigger being edited.
const showNewTrigger = ref(false)
const editingTriggerId = ref(null)
const submittingTrigger = ref(false)

// The events a trigger's task may fire once completed: every other event.
const allEvents = ref([])
async function loadAllEvents() {
  try {
    const data = await fetchEvents()
    allEvents.value = (data.events ?? []).filter(e => e.id !== eventId)
  } catch (_) {}
}
const eventsById = computed(() => Object.fromEntries(allEvents.value.map(e => [e.id, e])))

function openNewTrigger() {
  editingTriggerId.value = null
  showNewTrigger.value = true
}

function startEditTrigger(t) {
  showNewTrigger.value = false
  editingTriggerId.value = t.id
}

function closeTriggerForm() {
  showNewTrigger.value = false
  editingTriggerId.value = null
}

async function handleSubmitTrigger(payload) {
  submittingTrigger.value = true
  try {
    if (editingTriggerId.value) {
      const id = editingTriggerId.value
      const data = await updateEventTrigger(eventId, id, payload)
      triggers.value = triggers.value.map(t => (t.id === id ? data.eventTrigger : t))
      notifySuccess('Trigger saved')
    } else {
      const data = await createEventTrigger(eventId, payload)
      triggers.value.unshift(data.eventTrigger)
      notifySuccess('Trigger created')
    }
    closeTriggerForm()
  } catch (e) {
    notifyError(e.message)
  } finally {
    submittingTrigger.value = false
  }
}

// delete event
const showDeleteEventModal = ref(false)

async function handleDeleteEvent() {
  try {
    await deleteEvent(eventId)
    notifySuccess('Event deleted')
    router.push('/events')
  } catch (e) {
    notifyError(e.message)
  } finally {
    showDeleteEventModal.value = false
  }
}

// delete trigger
const showDeleteTriggerModal = ref(false)
const deletingTrigger = ref(null)

function confirmDeleteTrigger(t) {
  deletingTrigger.value = t
  showDeleteTriggerModal.value = true
}

async function handleDeleteTrigger() {
  try {
    await deleteEventTrigger(eventId, deletingTrigger.value.id)
    triggers.value = triggers.value.filter(t => t.id !== deletingTrigger.value.id)
    notifySuccess('Trigger deleted')
  } catch (e) {
    notifyError(e.message)
  } finally {
    showDeleteTriggerModal.value = false
    deletingTrigger.value = null
  }
}

// Keyed once rather than scanned per row: a busy event lists a trigger for
// every workspace that reacts to it.
const workspacesById = computed(() => {
  const map = {}
  for (const ws of workspaceStore.workspaces) map[ws.id] = ws
  return map
})

function workspaceName(wsId) {
  return workspaceLabel(workspacesById.value, wsId)
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

async function loadTasks({ quiet = false } = {}) {
  if (!quiet) loadingTasks.value = true
  try {
    const data = await fetchEventTasks(eventId)
    tasks.value = data.tasks ?? []
    if (!quiet) visibleTaskCount.value = TASKS_PAGE_SIZE
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

// ── init ──────────────────────────────────────────────────────────────────────

onMounted(async () => {
  if (!workspaceStore.workspaces.length) await workspaceStore.fetchWorkspaces()
  await Promise.all([loadEvent(), loadTriggers(), loadTasks(), loadAllEvents()])
})

// Quietly, so a browser agent's change does not blank the page; an open form
// keeps its own copy of what is being edited.
onWebMCPChange(() => Promise.all([
  loadEvent({ quiet: true }),
  loadTriggers({ quiet: true }),
  loadTasks({ quiet: true }),
  loadAllEvents(),
]))

const SECTION_TITLE = 'text-sm font-bold text-gray-900 dark:text-zinc-50'
const SECTION_HELP = 'text-xs text-gray-500 dark:text-zinc-400 mt-0.5'
const SECONDARY = 'inline-flex items-center gap-1.5 px-3 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 shrink-0'
// Row actions show on hover where there is a mouse, and always on a touch screen.
const ROW_ACTIONS = 'flex items-center gap-0.5 shrink-0 transition-opacity pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:group-focus-within:opacity-100'
const ICON_BUTTON = 'p-2 rounded-lg text-gray-400 dark:text-zinc-500 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/20'
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">

    <!-- Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <div v-if="loadingEvent" class="h-8 w-64 bg-gray-100 dark:bg-zinc-800 animate-pulse rounded-lg"></div>
        <h1 v-else class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate">
          <RouterLink to="/events" class="opacity-50 hover:opacity-100 transition-opacity">Events</RouterLink>
          <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium" aria-hidden="true">/</span>
          <span class="font-mono" data-test="event-name">{{ event?.name }}</span>
        </h1>
      </div>
      <button v-if="!loadingEvent && event" type="button" data-test="event-delete" aria-label="Delete event" title="Delete event"
        @click="showDeleteEventModal = true"
        :class="ICON_BUTTON" class="hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 shrink-0">
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
        </svg>
      </button>
    </div>

    <div class="px-4 space-y-10 pb-10">

      <!-- Payload guidelines -->
      <section aria-labelledby="event-guidelines-title" data-test="event-guidelines">
        <div class="flex items-start justify-between gap-4 mb-3">
          <div class="min-w-0">
            <h2 id="event-guidelines-title" :class="SECTION_TITLE">Payload guidelines</h2>
            <p :class="SECTION_HELP">What an agent includes in the payload when it publishes this event.</p>
          </div>
          <button v-if="!editingGuidelines && event" type="button" data-test="event-guidelines-edit" :class="SECONDARY" @click="startEditGuidelines">
            Edit
          </button>
        </div>

        <form v-if="editingGuidelines" novalidate :class="FORM_CARD" @submit.prevent="saveGuidelines" @keydown="onGuidelinesKeydown">
          <div class="p-4 sm:p-5">
            <FormField v-slot="f" label="Guidelines" description="Agents read this before they publish. Name the fields the triggered tasks need.">
              <textarea :id="f.id" ref="guidelinesEl" v-model="guidelinesInput" data-test="event-guidelines-input" rows="4"
                :aria-describedby="f.describedBy"
                placeholder="The commit SHA, the failing job's name and a link to its log."
                :class="[fieldClass(), 'leading-relaxed resize-y custom-scrollbar']"></textarea>
            </FormField>
          </div>
          <div :class="ACTIONS">
            <span class="hidden sm:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-auto">{{ saveKeys }} to save · Esc to cancel</span>
            <button type="button" :class="CANCEL_BUTTON" @click="editingGuidelines = false">Cancel</button>
            <button type="submit" data-test="event-guidelines-save" :disabled="savingGuidelines" :class="PRIMARY_BUTTON">
              {{ savingGuidelines ? 'Saving…' : 'Save guidelines' }}
            </button>
          </div>
        </form>
        <div v-else-if="loadingEvent" class="h-16 bg-gray-50 dark:bg-zinc-900 animate-pulse rounded-xl"></div>
        <p v-else-if="event?.payloadGuidelines" data-test="event-guidelines-text"
          class="px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl text-sm text-gray-700 dark:text-zinc-300 whitespace-pre-wrap break-words">{{ event.payloadGuidelines }}</p>
        <div v-else-if="event" data-test="event-guidelines-empty"
          class="px-4 py-3 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-xs text-gray-500 dark:text-zinc-400">
          None yet, so agents choose what to send.
          <button type="button" class="font-bold text-gray-800 dark:text-zinc-200 underline underline-offset-2 hover:opacity-80" @click="startEditGuidelines">Add guidelines</button>
        </div>
      </section>

      <!-- Triggers -->
      <section aria-labelledby="event-triggers-title">
        <div class="flex items-start justify-between gap-4 mb-3">
          <div class="min-w-0">
            <h2 id="event-triggers-title" :class="SECTION_TITLE">Triggers</h2>
            <p :class="SECTION_HELP">Each one creates a task in a workspace when this event fires.</p>
          </div>
          <button v-if="!showNewTrigger" type="button" data-test="trigger-add" @click="openNewTrigger"
            class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0">
            + Add<span class="hidden sm:inline"> trigger</span>
          </button>
        </div>

        <EventTriggerForm v-if="showNewTrigger" class="mb-3" :workspaces="workspaceStore.workspaces" :events="allEvents"
          :busy="submittingTrigger" @submit="handleSubmitTrigger" @cancel="closeTriggerForm" />

        <div v-if="loadingTriggers" class="py-4">
          <LoadingState label="Loading triggers…" />
        </div>
        <div v-else-if="triggers.length === 0 && !showNewTrigger" data-test="triggers-empty"
          class="py-10 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-600 dark:text-zinc-300">No triggers yet</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">Firing this event does nothing until a trigger turns it into a task.</p>
          <button type="button" data-test="triggers-empty-add" @click="openNewTrigger"
            class="mt-4 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95">
            + Add trigger
          </button>
        </div>
        <ul v-else class="space-y-2" aria-label="Triggers">
          <li v-for="t in triggers" :key="t.id" data-test="trigger-row">
            <EventTriggerForm v-if="editingTriggerId === t.id" :trigger="t" :workspaces="workspaceStore.workspaces" :events="allEvents"
              :busy="submittingTrigger" @submit="handleSubmitTrigger" @cancel="closeTriggerForm" />
            <div v-else
              class="group flex items-start gap-3 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-colors">
              <div class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700 mt-0.5" aria-hidden="true">
                <svg class="w-3.5 h-3.5 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
                </svg>
              </div>
              <div class="flex-1 min-w-0">
                <div class="flex flex-col sm:flex-row sm:items-center gap-x-2 gap-y-0.5 min-w-0">
                  <span
                    @mouseenter="tooltipStore.show($event, triggerWorkspaceTooltip(workspaceName(t.workspaceId)), 'top')"
                    @mouseleave="tooltipStore.hide()"
                    class="sm:max-w-[12rem] truncate text-[10px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-wider">{{ workspaceName(t.workspaceId) }}</span>
                  <svg class="hidden sm:block w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
                  <span class="text-sm font-bold text-gray-800 dark:text-zinc-200 truncate">{{ t.title }}</span>
                </div>
                <p v-if="t.body" class="text-xs text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ t.body }}</p>
                <div class="flex items-center gap-1.5 mt-2 flex-wrap">
                  <span data-test="trigger-assignee" class="inline-flex items-center h-5 px-1.5 rounded bg-gray-100 dark:bg-zinc-800 text-[10px] font-bold text-gray-600 dark:text-zinc-300 leading-none">
                    {{ t.assignee === 'human' ? 'Human' : 'Agent' }}
                  </span>
                  <span v-if="t.assignee !== 'human' && t.allowAllCommands" data-test="trigger-yolo-badge"
                    class="inline-flex items-center h-5 px-1.5 rounded bg-amber-50 dark:bg-amber-500/10 text-[10px] font-bold text-amber-700 dark:text-amber-300 leading-none">YOLO</span>
                  <span v-if="t.emitEventId" data-test="trigger-emits"
                    class="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-sky-50 dark:bg-sky-500/10 text-[10px] font-bold text-sky-700 dark:text-sky-300 leading-none">
                    then fires <span class="font-mono leading-none">{{ eventsById[t.emitEventId]?.name ?? '(deleted event)' }}</span>
                  </span>
                  <code v-if="t.cronSchedule" class="inline-flex items-center h-5 px-1.5 rounded text-[10px] font-mono text-violet-600 dark:text-violet-400 bg-violet-50 dark:bg-violet-900/20 leading-none">{{ t.cronSchedule }}</code>
                </div>
              </div>
              <div :class="ROW_ACTIONS">
                <button type="button" data-test="trigger-edit" :aria-label="`Edit trigger ${t.title}`" title="Edit trigger" @click="startEditTrigger(t)"
                  :class="ICON_BUTTON" class="hover:text-gray-700 dark:hover:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                  </svg>
                </button>
                <button type="button" data-test="trigger-delete" :aria-label="`Delete trigger ${t.title}`" title="Delete trigger" @click="confirmDeleteTrigger(t)"
                  :class="ICON_BUTTON" class="hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>
            </div>
          </li>
        </ul>
      </section>

      <!-- Tasks From This Event -->
      <section aria-labelledby="event-tasks-title">
        <div class="mb-3">
          <h2 id="event-tasks-title" :class="SECTION_TITLE">Tasks created from this event</h2>
          <p :class="SECTION_HELP">Newest first, one for each trigger every time the event fired.</p>
        </div>

        <div v-if="loadingTasks" class="py-4">
          <LoadingState label="Loading tasks…" />
        </div>
        <div v-else-if="tasks.length === 0"
          class="py-8 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-600 dark:text-zinc-300">No tasks yet</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-1">They appear here once the event fires.</p>
        </div>
        <div v-else class="space-y-2">
          <RouterLink
            v-for="t in visibleTasks"
            :key="t.id"
            :to="`/workspaces/${t.workspaceId}/tasks/${t.id}`"
            data-test="event-task"
            class="flex items-center gap-3 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-colors">
            <span class="w-2 h-2 rounded-full shrink-0" :class="taskDotClass(t)" aria-hidden="true"></span>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-semibold text-gray-800 dark:text-zinc-200 truncate">{{ t.title }}</p>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5 truncate">
                {{ STATUS_LABELS[t.status] ?? t.status }} · {{ workspaceName(t.workspaceId) }} · <span class="tabular-nums">{{ formatDate(t.createdAt) }}</span>
              </p>
            </div>
            <svg class="w-4 h-4 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
          </RouterLink>
          <LoadMoreButton v-if="hasMoreTasks" @click="loadMoreTasks" />
        </div>
      </section>
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
