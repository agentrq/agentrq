<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { fetchEvents, createEvent, deleteEvent } from '../api'
import { useToasts } from '../composables/useToasts'
import DeleteModal from '../components/DeleteModal.vue'

const router = useRouter()
const { notifyError, notifySuccess } = useToasts()

// ── state ─────────────────────────────────────────────────────────────────────

const events = ref([])
const loading = ref(true)

// create form
const showCreateForm = ref(false)
const creating = ref(false)
const formName = ref('')
const formNameError = ref('')
const formGuidelines = ref('')

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
  if (!name) return 'Name is required'
  if (!EVENT_NAME_RE.test(name)) return 'Must start with a-z, then only a-z, 0-9, _'
  if (name.length > 129) return 'Max 129 characters'
  return ''
}

// ── data ops ──────────────────────────────────────────────────────────────────

async function loadEvents() {
  loading.value = true
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

async function handleCreate() {
  formNameError.value = validateName(formName.value)
  if (formNameError.value) return

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
  if (!deletingEvent.value) return
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
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <!-- Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Events</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Named signals that trigger tasks in subscriber workspaces.</p>
      </div>
      <button
        @click="showCreateForm = !showCreateForm"
        class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0">
        + New Event
      </button>
    </div>

    <div class="px-4 space-y-6 pb-10">

      <!-- Create Form -->
      <Transition name="slide-down">
        <div v-if="showCreateForm" class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-4">
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">New Event</h2>

          <!-- Name -->
          <div>
            <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">
              Event Name <span class="text-red-500">*</span>
            </label>
            <input
              :value="formName"
              @input="e => { formName = sanitizeName(e.target.value); formNameError = '' }"
              type="text"
              placeholder="deploy_done"
              class="w-full px-3 py-2 text-sm border rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all font-mono"
              :class="formNameError ? 'border-red-400' : 'border-gray-200 dark:border-zinc-700'"
            />
            <p v-if="formNameError" class="text-[11px] text-red-500 mt-1">{{ formNameError }}</p>
            <p v-else class="text-[11px] text-gray-400 dark:text-zinc-500 mt-1">Spaces become <code class="font-mono">_</code> · only lowercase letters, digits, underscores</p>
          </div>

          <!-- Payload Guidelines -->
          <div>
            <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">
              Payload Guidelines
            </label>
            <textarea
              v-model="formGuidelines"
              rows="2"
              placeholder="Describe what the agent should include in the payload when publishing this event…"
              class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 resize-none focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all"
            ></textarea>
          </div>

          <!-- Actions -->
          <div class="flex items-center gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
            <button
              @click="handleCreate"
              :disabled="creating"
              class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
              {{ creating ? 'Creating…' : 'Create Event' }}
            </button>
            <button
              @click="resetForm"
              class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
              Cancel
            </button>
          </div>
        </div>
      </Transition>

      <!-- Events List -->
      <div>
        <div v-if="loading" class="text-sm text-gray-500 dark:text-zinc-400 py-8 text-center">Loading events…</div>

        <div v-else-if="events.length === 0 && !showCreateForm"
             class="py-12 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-500 dark:text-zinc-400 mb-1">No events yet</p>
          <p class="text-xs text-gray-400 dark:text-zinc-500">Create an event to let agents signal other workspaces.</p>
        </div>

        <div v-else class="space-y-2">
          <div
            v-for="ev in events"
            :key="ev.id"
            class="group flex items-center gap-4 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all cursor-pointer"
            @click="router.push(`/events/${ev.id}`)">

            <!-- Icon -->
            <div class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700">
              <svg class="w-4 h-4 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9.348 14.651a3.75 3.75 0 010-5.303m5.304-.002a3.75 3.75 0 010 5.304m-7.425 2.122a6.75 6.75 0 010-9.546m9.546.001a6.75 6.75 0 010 9.545m-11.667 2.121a9.75 9.75 0 010-13.788m13.788.001a9.75 9.75 0 010 13.787M12 12h.008v.008H12V12z" />
              </svg>
            </div>

            <!-- Info -->
            <div class="flex-1 min-w-0">
              <p class="text-sm font-bold text-gray-800 dark:text-zinc-200 truncate font-mono">{{ ev.name }}</p>
              <p v-if="ev.payloadGuidelines" class="text-xs text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ ev.payloadGuidelines }}</p>
            </div>

            <!-- Date + actions -->
            <div class="flex items-center gap-3 shrink-0">
              <span class="text-[11px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatDate(ev.createdAt) }}</span>
              <button
                @click.stop="confirmDelete(ev)"
                class="p-1.5 text-gray-300 dark:text-zinc-600 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 rounded transition-all opacity-0 group-hover:opacity-100">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            </div>
          </div>
        </div>
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
