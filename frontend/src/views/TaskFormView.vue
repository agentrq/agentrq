<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- Laid out by its own width, not the window's: beside the desktop side
       panel the form gets a phone's width, and should look like it does on a
       phone. `@min-[40rem]:` and `@min-[48rem]:` are `sm:` and `md:` measured
       against this element. -->
  <div class="@container h-full page-surface flex flex-col w-full max-w-full overflow-x-hidden relative">
    <!-- Breadcrumb Header -->
    <header class="py-2 border-b border-gray-100 dark:border-zinc-800 shrink-0 flex items-center justify-between gap-4 page-surface sticky top-0 z-30 px-4">
      <div class="flex items-center gap-2 text-xs font-semibold min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 transition-colors shrink-0">
          {{ workspace?.name || 'Workspace' }}
        </router-link>
        <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-900 dark:text-zinc-50 truncate flex-1 min-w-0 text-sm">{{ isEditMode ? 'Edit Task' : 'New Task' }}</span>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button @click="() => goBack()" class="p-2 text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm transition-all">
          <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto pt-8 @min-[48rem]:pt-16 pb-24 px-1 @min-[40rem]:px-2 @min-[48rem]:px-4 scroll-smooth custom-scrollbar flex items-start justify-center"
          @dragover.prevent="isDragging = true"
          @dragleave.prevent="isDragging = false"
          @drop.prevent="handleDrop">
      
      <div class="w-full max-w-3xl space-y-4">
        
        <h1 class="text-xl @min-[48rem]:text-3xl font-black text-gray-800 dark:text-zinc-200 tracking-tight text-center mb-8">
          {{ isEditMode ? 'Edit Task' : 'What do you want to achieve?' }}
        </h1>

        <!-- Drag & Drop Overlay inside the main container -->
        <div v-if="isDragging" class="absolute inset-4 z-50 border-4 border-dashed border-gray-300 dark:border-zinc-700 bg-white/95 dark:bg-zinc-900/95 rounded-2xl flex flex-col items-center justify-center shadow-2xl animate-in zoom-in-95 pointer-events-none">
           <svg class="w-16 h-16 text-gray-400 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" /></svg>
           <p class="text-xl font-black text-gray-700 dark:text-zinc-300">Drop files to attach</p>
        </div>

        <form id="taskForm" @submit.prevent="isEditMode ? submitEditProtocol() : submitHumanTask()" 
              class="flex flex-col bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-xl focus-within:border-gray-900 dark:focus-within:border-white focus-within:ring-0 transition-all shadow-xl relative overflow-visible z-10">
          
          <!-- Auto-generated Title Input (Top edge) -->
          <div class="px-4 pt-3 pb-1 border-b border-gray-100 dark:border-zinc-800/50 flex items-center gap-2 group relative">
            <svg class="w-3.5 h-3.5 text-gray-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
            
            <input v-model="titleRef" 
                   @input="markOverridden"
                   :placeholder="titlePlaceholder"
                   class="w-full bg-transparent outline-none border-none text-[13px] font-bold text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 pl-1" />

            <!-- AI Sparkles Button -->
            <button v-if="isAutoTitleSupported && bodyRef && bodyRef.trim().length >= 5" type="button" @click="generateTitle(); tooltipStore.hide()"
                    :disabled="isGenerating"
                    @mouseenter="tooltipStore.show($event, isModelLoading ? `Loading Model... ${modelProgress}%` : 'Generate title from description using local AI', 'top')"
                    @mouseleave="tooltipStore.hide()"
                    class="p-1 hover:bg-gray-100 dark:hover:bg-zinc-850 rounded text-gray-450 dark:text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400 transition-all shrink-0">
               <svg v-if="isGenerating" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path></svg>
               <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9.813 15.904L9 21l-.813-5.096L3 15l5.188-.813L9 9l.813 5.187L15 15l-5.187.813zM19.071 4.929l-.571 3.571-3.571.571 3.571.571.571 3.571.571-3.571 3.571-.571-3.571-.571-.571-3.571z" /></svg>
            </button>
          </div>

          <!-- Description Textarea -->
          <textarea v-model="bodyRef"
                    ref="bodyInput"
                    @keydown="onDescriptionKeydown"
                    placeholder="Provide detailed context or instructions..." 
                    class="w-full px-4 pt-3 pb-2 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none border-none focus:outline-none focus:ring-0 resize-none min-h-[160px] custom-scrollbar"
                    required></textarea>

          <!-- Attachments Preview -->
          <TaskAttachmentChips :attachments="opts.attachments.value" @remove="(i) => opts.attachments.value.splice(i, 1)"
                               class="px-4 pb-2 border-t border-gray-50 dark:border-zinc-800/50 pt-2" />

          <!-- Bottom Toolbar -->
          <div class="flex items-center justify-between px-3 pb-2 pt-2 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-900/50 rounded-b-xl flex-wrap gap-2 relative">
             <div class="flex items-center gap-1 @min-[40rem]:gap-2 flex-wrap">
                <TaskOptionsTools :opts="opts" v-model:text="bodyRef" :workspace="liveWorkspace" :workspace-id="workspaceId"
                  :clear-context-offered="clearContextOffered" :is-edit-mode="isEditMode" :input-el="bodyInput" />
             </div>

             <!-- The shortcut, said out loud. Hidden on narrow screens, where
                  there is no keyboard to press it with and no room to say so. -->
             <span class="hidden @min-[40rem]:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-2 shrink-0">
               {{ isEditMode ? 'Cmd ⌘ + Enter to save' : 'Cmd ⌘ + Enter to create' }}
             </span>

             <!-- Submit Button -->
             <button type="submit"
                     :disabled="!canSubmit"
                     class="h-8 w-8 @min-[40rem]:h-9 @min-[40rem]:w-9 rounded-full bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 disabled:opacity-30 transition-all flex items-center justify-center shrink-0 shadow-md border border-transparent">
                <svg v-if="sending" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M5 10l7-7m0 0l7 7m-7-7v18" /></svg>
             </button>
          </div>
        </form>
        
        <div class="flex justify-center mt-6">
          <button type="button" @click="() => goBack()" class="px-6 py-2 rounded-full bg-transparent text-gray-500 hover:text-gray-800 dark:text-zinc-500 dark:hover:text-zinc-300 text-[10px] font-bold uppercase tracking-widest transition-colors">Cancel</button>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, createTask, updateScheduledTask, getTask, fetchEvents, fetchWorkflows } from '../api';
import { useToasts } from '../composables/useToasts';
import { useTaskOptions } from '../composables/useTaskOptions';
import { useAutoTitle } from '../composables/useAutoTitle';
import { nameUntitledTask } from '../composables/useUntitledTaskTitles';
import { useTooltipStore } from '../stores/tooltipStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useClearContext } from '../composables/useClearContext';
import { useEscapeKey } from '../composables/useEscapeKey';
import TaskOptionsTools from '../components/TaskOptionsTools.vue';
import TaskAttachmentChips from '../components/TaskAttachmentChips.vue';

const route = useRoute();
const router = useRouter();
const { notifyError, notifySuccess } = useToasts();
const tooltipStore = useTooltipStore();

const workspaceId = route.params.id;
// Whether this workspace has a terminal to type /clear into. Asked once, on
// mount; a workspace with no running Claude Code session simply never shows
// the icon.
const {
  offered: clearContextOffered,
  load: loadClearContext,
} = useClearContext({ workspaceId });
const taskId = route.params.taskId;
const isEditMode = computed(() => !!taskId);

const workspace = ref(null);
const sending = ref(false);

// The model picker reads the *store's* copy of this workspace, not the one
// fetched above. The agent's confirming report arrives on the event stream and
// lands in the store, so a picker bound to this view's own snapshot would show
// a switch as pending forever — the answer would never reach it.
const workspaceStore = useWorkspaceStore();
// Falls back to this view's own copy only until the store has one. That copy
// cannot receive the confirming report, so it is a starting value rather than a
// source: enough to draw the picker on a cold load, replaced the moment the
// store knows about this workspace.
const liveWorkspace = computed(() => workspaceStore.getWorkspace(workspaceId) || workspace.value);

const newTask = ref({ title: '', body: '' });
const opts = useTaskOptions();

// We use computed refs to bridge to the composables
const titleRef = computed({
  get: () => newTask.value.title,
  set: (v) => { newTask.value.title = v; },
});
const bodyRef = computed({
  get: () => newTask.value.body,
  set: (v) => { newTask.value.body = v; },
});

/**
 * Cmd/Ctrl-Enter submits the form from the description.
 *
 * Guarded exactly as the button is, so the shortcut and the button agree: a
 * shortcut that quietly does nothing on an incomplete form is worse than one
 * that is simply not offered, and worse still if it disagrees with what the
 * button would have done.
 */
function onDescriptionKeydown(event) {
  if (event.key !== 'Enter' || !(event.metaKey || event.ctrlKey)) return;
  event.preventDefault();
  if (!canSubmit.value) return;
  if (isEditMode.value) submitEditProtocol();
  else submitHumanTask();
}

const bodyInput = ref(null);

// Auto-Title
const {
  isSupported: isAutoTitleSupported,
  isGenerating,
  isModelLoading,
  modelProgress,
  isOverridden,
  markOverridden,
  generateTitle
} = useAutoTitle(bodyRef, titleRef, workspaceId);

// A new task may leave its title out: the server calls it Untitled and this
// browser names it afterwards (see useUntitledTaskTitles). Editing a scheduled
// task still needs one, since that saves the title as typed.
const canSubmit = computed(() =>
  !sending.value && !!newTask.value.body && (!isEditMode.value || !!newTask.value.title)
);

const titlePlaceholder = computed(() => {
  if (isModelLoading.value) return `Loading AI Model... ${modelProgress.value}%`;
  if (isGenerating.value) return 'Generating title...';
  if (isEditMode.value) return isAutoTitleSupported ? 'Task Title (Click sparkle to auto-generate)' : 'Task Title';
  // Short enough to fit a phone; the sparkle button says what it does on hover.
  return 'Task Title (optional)';
});

const isDragging = ref(false);

function handleDrop(e) {
  isDragging.value = false;
  const files = e.dataTransfer.files;
  if (files && files.length > 0) {
    opts.processFiles(files);
  }
}

onMounted(async () => {
  bodyInput.value?.focus();
  try {
    // Both pickers degrade to empty rather than blocking the form: a task can
    // always be created without an on-completion trigger.
    const [wsRes, eventsRes, workflowsRes] = await Promise.all([
      getWorkspace(workspaceId),
      fetchEvents().catch(() => ({ events: [] })),
      fetchWorkflows().catch(() => ({ workflows: [] })),
      // Alongside the rest rather than after: it decides one icon, and the
      // form must not wait on it. It swallows its own failures.
      loadClearContext(),
    ]);
    workspace.value = wsRes.workspace;
    opts.events.value = eventsRes.events ?? [];
    opts.workflows.value = workflowsRes.workflows ?? [];

    if (isEditMode.value) {
      const taskRes = await getTask(workspaceId, taskId);
      const t = taskRes.task;
      newTask.value = { title: t.title, body: t.body };
      opts.assignee.value = t.assignee;
      opts.allowAllCommands.value = t.allowAllCommands || false;
      opts.clearContext.value = t.clearContext || false;
      markOverridden(); // Assume edited tasks shouldn't auto-title

      if (t.cronSchedule) {
        opts.parseCronToUI(t.cronSchedule);
      }
      document.title = `Edit Task | ${workspace.value?.name || 'Workspace'} | AgentRQ`;
    } else {
      opts.applyWorkspaceDefaults(wsRes.workspace);
      document.title = `New Task | ${workspace.value?.name || 'Workspace'} | AgentRQ`;
    }
  } catch (err) {
    notifyError("Access Error: " + err.message);
    router.push(`/workspaces/${workspaceId}`);
  }
});

async function submitHumanTask() {
  sending.value = true;
  try {
    const status = opts.scheduleType.value !== 'none' ? 'cron' : 'notstarted';
    const title = newTask.value.title.trim();
    const res = await createTask(
      workspaceId, title, newTask.value.body,
      opts.assignee.value, opts.attachments.value,
      status, opts.cronSchedule.value, opts.allowAllCommands.value,
      opts.selectedEventId.value, opts.selectedWorkflowId.value,
      // Only ever true when the icon was there to be pressed: the option is
      // hidden when nothing could act on it.
      clearContextOffered.value && opts.clearContext.value
    );
    if (!title) nameUntitledTask(workspaceId, res?.task?.id, newTask.value.body);
    notifySuccess('Task Created successfully');
    goBack(status === 'cron');
  } catch(err) {
    notifyError("Dispatch Error: " + err.message);
  } finally { sending.value = false; }
}

async function submitEditProtocol() {
  sending.value = true;
  try {
    await updateScheduledTask(
      workspaceId, taskId, newTask.value.title, newTask.value.body,
      opts.assignee.value, opts.cronSchedule.value,
      opts.allowAllCommands.value,
      // Sent on edit as well as create, so the icon is not a control that
      // looks like it saved and did not. Still only offered where there is a
      // terminal to act on it.
      clearContextOffered.value && opts.clearContext.value
    );
    notifySuccess('Task Updated');
    goBack(opts.cronSchedule.value !== '');
  } catch(err) {
    notifyError("Update Error: " + err.message);
  } finally { sending.value = false; }
}

useEscapeKey(() => goBack());

function goBack(isScheduled = false) {
  if (isScheduled) {
    router.push({ path: `/workspaces/${workspaceId}`, query: { scheduled: 'true' } });
  } else {
    router.push(`/workspaces/${workspaceId}`);
  }
}
</script>
