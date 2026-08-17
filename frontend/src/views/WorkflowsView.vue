<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { fetchWorkflows, createWorkflow, deleteWorkflow, fetchEvents } from '../api';
import { useToasts } from '../composables/useToasts';
import DeleteModal from '../components/DeleteModal.vue';
import LoadingState from '../components/LoadingState.vue';

const router = useRouter();
const { notifyError, notifySuccess } = useToasts();

const workflows = ref([]);
const events = ref([]);
const loading = ref(true);

const showCreateForm = ref(false);
const creating = ref(false);
const formName = ref('');
const formNameError = ref('');
const formDescription = ref('');
const formStartEventId = ref('');

// Same identifier rule as events: workflows are referenced by name from the
// text format and from publishEvent, so they cannot contain spaces.
const WORKFLOW_NAME_RE = /^[a-z][a-z0-9_]*$/;

function sanitizeName(value) {
  return value.toLowerCase().replace(/\s+/g, '_').replace(/[^a-z0-9_]/g, '');
}

function onNameInput(e) {
  formName.value = sanitizeName(e.target.value);
  formNameError.value = '';
}

async function loadWorkflows() {
  loading.value = true;
  try {
    const data = await fetchWorkflows();
    workflows.value = data.workflows ?? [];
  } catch (e) {
    notifyError(e.message);
  } finally {
    loading.value = false;
  }
}

async function loadEvents() {
  try {
    const data = await fetchEvents();
    events.value = data.events ?? [];
  } catch {
    // A workflow can be created without a start event and given one later, so
    // failing to load the picker must not block the form.
    events.value = [];
  }
}

function resetForm() {
  showCreateForm.value = false;
  formName.value = '';
  formNameError.value = '';
  formDescription.value = '';
  formStartEventId.value = '';
}

async function handleCreate() {
  const name = formName.value.trim();
  if (!name) {
    formNameError.value = 'Name is required';
    return;
  }
  if (!WORKFLOW_NAME_RE.test(name)) {
    formNameError.value = 'Use lowercase letters, digits and underscores; must start with a letter';
    return;
  }
  creating.value = true;
  try {
    const data = await createWorkflow({
      name,
      description: formDescription.value.trim(),
      startEventId: formStartEventId.value,
    });
    notifySuccess('Workflow created');
    resetForm();
    // Straight into the editor: a workflow with no steps has nothing to show
    // in the list, so the next thing the user wants is always the canvas.
    router.push(`/workflows/${data.workflow.id}`);
  } catch (e) {
    notifyError(e.message);
  } finally {
    creating.value = false;
  }
}

const showDeleteModal = ref(false);
const deletingWorkflow = ref(null);
const deleting = ref(false);

function confirmDelete(wf) {
  deletingWorkflow.value = wf;
  showDeleteModal.value = true;
}

async function handleDelete() {
  if (!deletingWorkflow.value) return;
  deleting.value = true;
  try {
    await deleteWorkflow(deletingWorkflow.value.id);
    notifySuccess('Workflow deleted');
    workflows.value = workflows.value.filter(w => w.id !== deletingWorkflow.value.id);
  } catch (e) {
    notifyError(e.message);
  } finally {
    deleting.value = false;
    showDeleteModal.value = false;
    deletingWorkflow.value = null;
  }
}

function eventName(id) {
  return events.value.find(e => e.id === id)?.name ?? '';
}

onMounted(async () => {
  await Promise.all([loadWorkflows(), loadEvents()]);
});
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
      <div class="min-w-0">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Workflows</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          Chain events and workspaces into a named pipeline
        </p>
      </div>
      <button
        v-if="!showCreateForm"
        @click="showCreateForm = true"
        class="shrink-0 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95">
        New Workflow
      </button>
    </div>

    <div class="px-4 space-y-6 pb-10">
      <Transition name="slide-down">
        <div v-if="showCreateForm" class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-4">
          <div>
            <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">
              Name <span class="text-red-500">*</span>
            </label>
            <input
              :value="formName"
              @input="onNameInput"
              type="text"
              placeholder="release_pipeline"
              class="w-full px-3 py-2 text-sm font-mono border rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all"
              :class="formNameError ? 'border-red-400' : 'border-gray-200 dark:border-zinc-700'" />
            <p v-if="formNameError" class="text-[11px] text-red-500 mt-1">{{ formNameError }}</p>
            <p v-else class="text-[11px] text-gray-400 dark:text-zinc-500 mt-1">
              Lowercase letters, digits and underscores
            </p>
          </div>

          <div>
            <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">Description</label>
            <textarea
              v-model="formDescription"
              rows="2"
              placeholder="What this pipeline does"
              class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all resize-none"></textarea>
          </div>

          <div>
            <label class="block text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">Start Event</label>
            <select
              v-model="formStartEventId"
              class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all">
              <option value="">Choose later</option>
              <option v-for="ev in events" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
            </select>
            <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-1">
              The event that begins a run of this workflow
            </p>
          </div>

          <div class="flex items-center gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
            <button
              @click="handleCreate"
              :disabled="creating"
              class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
              {{ creating ? 'Creating…' : 'Create' }}
            </button>
            <button
              @click="resetForm"
              class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
              Cancel
            </button>
          </div>
        </div>
      </Transition>

      <LoadingState v-if="loading" label="Loading workflows…" class="py-8" />

      <div v-else-if="workflows.length === 0 && !showCreateForm" class="py-12 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
        <p class="text-sm font-semibold text-gray-500 dark:text-zinc-400 mb-1">No workflows yet</p>
        <p class="text-xs text-gray-400 dark:text-zinc-500">
          A workflow chains events and workspaces into one named, reviewable pipeline
        </p>
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="wf in workflows"
          :key="wf.id"
          @click="router.push(`/workflows/${wf.id}`)"
          class="group flex items-center gap-4 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all cursor-pointer">
          <div class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700">
            <svg class="w-4 h-4 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 6.878V6a2.25 2.25 0 012.25-2.25h7.5A2.25 2.25 0 0118 6v.878m-12 0c.235-.083.487-.128.75-.128h10.5c.263 0 .515.045.75.128m-12 0A2.25 2.25 0 004.5 9v.878m13.5-3A2.25 2.25 0 0119.5 9v.878m0 0a2.246 2.246 0 00-.75-.128H5.25c-.263 0-.515.045-.75.128m15 0A2.25 2.25 0 0121 12v6a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 18v-6c0-.98.626-1.813 1.5-2.122" />
            </svg>
          </div>

          <div class="min-w-0 grow">
            <p class="text-sm font-mono font-semibold text-gray-900 dark:text-zinc-100 truncate">{{ wf.name }}</p>
            <p v-if="wf.description" class="text-xs text-gray-500 dark:text-zinc-400 truncate mt-0.5">{{ wf.description }}</p>
            <p v-else-if="wf.startEventId" class="text-[11px] text-gray-400 dark:text-zinc-500 truncate mt-0.5">
              starts on <span class="font-mono">{{ eventName(wf.startEventId) || '—' }}</span>
            </p>
          </div>

          <span
            v-if="!wf.startEventId"
            class="shrink-0 text-[10px] font-bold uppercase tracking-wider text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10 px-2 py-1 rounded">
            No start event
          </span>

          <button
            @click.stop="confirmDelete(wf)"
            class="shrink-0 p-1.5 text-gray-300 dark:text-zinc-600 hover:text-red-500 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 rounded transition-all opacity-0 group-hover:opacity-100">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
            </svg>
          </button>
        </div>
      </div>
    </div>

    <DeleteModal
      :show="showDeleteModal"
      title="Delete Workflow"
      :taskTitle="deletingWorkflow?.name ?? ''"
      @close="showDeleteModal = false; deletingWorkflow = null"
      @confirm="handleDelete" />
  </div>
</template>

<style scoped>
.slide-down-enter-active,
.slide-down-leave-active { transition: all 0.2s ease; }
.slide-down-enter-from,
.slide-down-leave-to { opacity: 0; transform: translateY(-8px); }
</style>
