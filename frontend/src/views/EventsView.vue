<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
import { ref, nextTick, onMounted } from 'vue'
import { fetchEvents, createEvent, deleteEvent } from '../api'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import { usesCommandKey } from '../composables/useKeyboardShortcuts'
import { ACTIONS, CANCEL_BUTTON, FORM_CARD, PRIMARY_BUTTON, fieldClass } from '../utils/formStyles'
import { focusFirstInvalid, formKeyAction, submitHint } from '../utils/forms'
import DeleteModal from '../components/DeleteModal.vue'
import FormField from '../components/FormField.vue'
import LoadingState from '../components/LoadingState.vue'
import EventsTabs from '../components/EventsTabs.vue'

const { notifyError, notifySuccess } = useToasts()
const saveKeys = submitHint(usesCommandKey())

// ── state ─────────────────────────────────────────────────────────────────────

const events = ref([])
const loading = ref(true)

// create form
const showCreateForm = ref(false)
const creating = ref(false)
const formName = ref('')
const formNameError = ref('')
const formGuidelines = ref('')
const formEl = ref(null)

// delete modal
const showDeleteModal = ref(false)
const deletingEvent = ref(null)
const deleting = ref(false)

// ── helpers ───────────────────────────────────────────────────────────────────

const EVENT_NAME_RE = /^[a-z][a-z0-9_]*$/

function formatDate(iso) {
  if (!iso) return ''
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function sanitizeName(raw) {
  return raw
    .toLowerCase()
    .replace(/ /g, '_')
    .replace(/[^a-z0-9_]/g, '')
}

function validateName(name) {
  if (!name) return 'Give the event a name.'
  if (!EVENT_NAME_RE.test(name)) return 'Start with a letter; then only letters, digits and underscores.'
  if (name.length > 129) return 'Use at most 129 characters.'
  return ''
}

// ── data ops ──────────────────────────────────────────────────────────────────

async function loadEvents({ quiet = false } = {}) {
  if (!quiet) loading.value = true
  try {
    const data = await fetchEvents()
    events.value = data.events ?? []
  } catch (e) {
    notifyError(e.message)
  } finally {
    loading.value = false
  }
}

function resetForm() {
  formName.value = ''
  formNameError.value = ''
  formGuidelines.value = ''
  showCreateForm.value = false
}

async function openCreateForm() {
  showCreateForm.value = true
  await nextTick()
  formEl.value?.querySelector('input').focus()
}

function toggleCreateForm() {
  if (showCreateForm.value) resetForm()
  else openCreateForm()
}

function onFormKeydown(e) {
  const action = formKeyAction(e)
  if (!action) return
  e.preventDefault()
  if (action === 'submit') handleCreate()
  else resetForm()
}

async function handleCreate() {
  if (creating.value) return
  formNameError.value = validateName(formName.value)
  if (formNameError.value) {
    await nextTick()
    focusFirstInvalid(formEl.value)
    return
  }

  creating.value = true
  try {
    await createEvent(formName.value.trim(), formGuidelines.value.trim())
    notifySuccess('Event created')
    resetForm()
    await loadEvents()
  } catch (e) {
    notifyError(e.message)
  } finally {
    creating.value = false
  }
}

function confirmDelete(ev) {
  deletingEvent.value = ev
  showDeleteModal.value = true
}

async function handleDelete() {
  deleting.value = true
  try {
    await deleteEvent(deletingEvent.value.id)
    notifySuccess('Event deleted')
    events.value = events.value.filter(e => e.id !== deletingEvent.value.id)
  } catch (e) {
    notifyError(e.message)
  } finally {
    deleting.value = false
    showDeleteModal.value = false
    deletingEvent.value = null
  }
}

onMounted(loadEvents)
onWebMCPChange(() => loadEvents({ quiet: true }))
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <!-- Header -->
    <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Events</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Named signals that trigger tasks in subscriber workspaces.</p>
      </div>
      <button
        type="button"
        data-test="event-new"
        :aria-expanded="showCreateForm"
        aria-controls="event-create-form"
        @click="toggleCreateForm"
        class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0">
        + New<span class="hidden sm:inline"> Event</span>
      </button>
    </div>

    <EventsTabs active="events" />

    <div class="px-4 space-y-6 pb-10">

      <!-- Create Form -->
      <Transition name="slide-down">
        <form v-if="showCreateForm" id="event-create-form" ref="formEl" novalidate data-test="event-form"
          aria-labelledby="event-create-title" :class="FORM_CARD"
          @submit.prevent="handleCreate" @keydown="onFormKeydown">
          <div class="p-4 sm:p-5 space-y-6">
            <div>
              <h2 id="event-create-title" class="text-sm font-bold text-gray-900 dark:text-zinc-50">New event</h2>
              <p class="text-[11px] font-medium text-gray-500 dark:text-zinc-400 mt-0.5">Agents and hooks fire it by name; its triggers turn it into tasks.</p>
            </div>

            <div class="grid gap-6 lg:grid-cols-2">
            <FormField v-slot="f" label="Name" required :error="formNameError"
              description="Lowercase letters, digits and underscores. Spaces turn into underscores as you type.">
              <input
                :id="f.id"
                :value="formName"
                @input="e => { formName = sanitizeName(e.target.value); formNameError = '' }"
                data-test="event-name-input"
                type="text"
                required
                maxlength="129"
                spellcheck="false" autocapitalize="off" autocorrect="off" autocomplete="off"
                placeholder="deploy_done"
                :aria-describedby="f.describedBy"
                :aria-invalid="f.invalid"
                :class="[fieldClass(f.invalid), 'font-mono']"
              />
            </FormField>

            <FormField v-slot="f" label="Payload guidelines" optional
              description="Agents read this before they publish the event, so the payload carries what its tasks need.">
              <textarea
                :id="f.id"
                v-model="formGuidelines"
                data-test="event-guidelines-new"
                rows="3"
                placeholder="The commit SHA, the failing job's name and a link to its log."
                :aria-describedby="f.describedBy"
                :class="[fieldClass(), 'leading-relaxed resize-y custom-scrollbar']"
              ></textarea>
            </FormField>
            </div>
          </div>

          <div :class="ACTIONS">
            <span class="hidden sm:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-auto">{{ saveKeys }} to create · Esc to cancel</span>
            <button type="button" data-test="event-cancel" :class="CANCEL_BUTTON" @click="resetForm">Cancel</button>
            <button type="submit" data-test="event-submit" :disabled="creating" :class="PRIMARY_BUTTON">
              {{ creating ? 'Creating…' : 'Create event' }}
            </button>
          </div>
        </form>
      </Transition>

      <!-- Events List -->
      <div>
        <div v-if="loading" class="py-8">
          <LoadingState label="Loading events…" />
        </div>

        <div v-else-if="events.length === 0 && !showCreateForm" data-test="events-empty"
             class="py-12 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-600 dark:text-zinc-300 mb-1">No events yet</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400">Create an event to let agents and outside services signal your workspaces.</p>
          <button type="button" data-test="events-empty-new" @click="openCreateForm"
            class="mt-4 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95">
            + New Event
          </button>
        </div>

        <ul v-else class="space-y-2" aria-label="Events">
          <li
            v-for="ev in events"
            :key="ev.id"
            data-test="event-row"
            class="group relative flex items-center gap-4 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-colors has-[a:focus-visible]:ring-2 has-[a:focus-visible]:ring-gray-900/20 dark:has-[a:focus-visible]:ring-white/20">

            <!-- Icon -->
            <div class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700" aria-hidden="true">
              <svg class="w-4 h-4 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9.348 14.651a3.75 3.75 0 010-5.303m5.304-.002a3.75 3.75 0 010 5.304m-7.425 2.122a6.75 6.75 0 010-9.546m9.546.001a6.75 6.75 0 010 9.545m-11.667 2.121a9.75 9.75 0 010-13.788m13.788.001a9.75 9.75 0 010 13.787M12 12h.008v.008H12V12z" />
              </svg>
            </div>

            <!-- Info; the link covers the whole row -->
            <div class="flex-1 min-w-0">
              <RouterLink :to="`/events/${ev.id}`" data-test="event-link"
                class="block text-sm font-bold text-gray-800 dark:text-zinc-200 truncate font-mono outline-none after:absolute after:inset-0 after:rounded-xl">{{ ev.name }}</RouterLink>
              <p v-if="ev.payloadGuidelines" class="text-xs text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ ev.payloadGuidelines }}</p>
            </div>

            <!-- Date + actions -->
            <div class="flex items-center gap-2 shrink-0">
              <span class="hidden sm:inline text-[11px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatDate(ev.createdAt) }}</span>
              <button
                type="button"
                data-test="event-row-delete"
                :aria-label="`Delete event ${ev.name}`"
                title="Delete event"
                @click="confirmDelete(ev)"
                class="relative z-10 p-2 text-gray-400 dark:text-zinc-500 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 rounded-lg transition-opacity outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/20 pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:focus-visible:opacity-100">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            </div>
          </li>
        </ul>
      </div>
    </div>

    <!-- Delete Modal -->
    <DeleteModal
      :show="showDeleteModal"
      title="Delete Event"
      :taskTitle="deletingEvent?.name ?? ''"
      @close="showDeleteModal = false; deletingEvent = null"
      @confirm="handleDelete"
    />
  </div>
</template>

<style scoped>
.slide-down-enter-active { transition: all 0.2s ease; }
.slide-down-leave-active { transition: all 0.15s ease; }
.slide-down-enter-from { opacity: 0; transform: translateY(-8px); }
.slide-down-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
