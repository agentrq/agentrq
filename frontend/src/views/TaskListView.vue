<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- Laid out by its own width, not the window's (useNarrowLayout): beside
       the desktop side panel it is a phone's width, and shows what a phone
       does. `@min-[40rem]:`, `@min-[48rem]:` and `@min-[64rem]:` are `sm:`,
       `md:` and `lg:` measured against this element. -->
  <div ref="layoutRef" class="@container flex flex-col h-full w-full bg-transparent">
    <DeleteModal
      :show="!!taskToDelete"
      :taskTitle="taskToDelete?.title || ''"
      title="Delete Task"
      @close="taskToDelete = null"
      @confirm="onDeleteConfirm"
    />

    <MoveTaskModal
      :show="!!taskToMove"
      :taskTitle="taskToMove?.title || ''"
      :currentWorkspaceId="taskToMove?.workspaceId"
      @close="taskToMove = null"
      @confirm="onMoveConfirm"
    />

    <ContextMenu
      :show="contextMenu.show"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :items="contextMenuItems"
      @close="closeContextMenu"
      @select="onContextMenuSelect"
    />

    <ExtensionViewPanel v-if="extensions.panel.value" :view="extensions.panel.value" :values="extensions.values"
                       @action="onExtensionAction" @input="extensions.setValue"
                       @submit="extensions.submit" @close="extensions.dismiss" />

    <!-- Global Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4"
         :class="{'hidden @min-[40rem]:flex': selectedTaskId}">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg @min-[48rem]:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">{{ title }}</h1>
      </div>



      <!-- Filters Segment Control (Top Right) -->
      <div class="flex items-center gap-2">
        
        <!-- Mobile Search Button -->
        <button @click="openSearch" 
                class="@min-[48rem]:hidden p-2 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0" 
                title="Search Tasks">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
        </button>

        <!-- Filters Toggle (Mobile) -->
        <button @click="showMobileFilters = !showMobileFilters" 
                class="@min-[48rem]:hidden p-2 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0" 
                :class="{'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white': showMobileFilters}"
                title="Filters">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12" /></svg>
        </button>

        <div v-if="showMobileFilters || !isMobile" 
             class="p-0.5 bg-gray-100 dark:bg-zinc-800 rounded-md border border-gray-200 dark:border-zinc-700/50 shadow-inner flex overflow-x-auto no-scrollbar"
             :class="[showMobileFilters ? 'fixed top-[70px] right-4 z-50 flex shadow-xl border-gray-900 dark:border-white animate-in fade-in slide-in-from-top-2' : 'hidden @min-[48rem]:flex']">
          <button v-for="f in filters" :key="f.id"
                  @click="router.push(`/tasks/${f.id}`); isMobile && (showMobileFilters = false)"
                  @mouseenter="tooltipStore.show($event, f.label, 'bottom')"
                  @mouseleave="tooltipStore.hide()"
                  :class="[filterType === f.id ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300']"
                  class="p-2 rounded-sm transition-all duration-200 whitespace-nowrap">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" :d="f.icon" />
            </svg>
          </button>
        </div>
      </div>
    </div>


    <!-- Content Area (Split Pane) -->
    <div class="flex flex-col @min-[48rem]:flex-row flex-1 min-h-0 w-full bg-transparent">
      <!-- Tasks Sidebar (Left Pane) -->
      <div v-show="!selectedTaskId || !isMobile" class="w-full @min-[48rem]:w-96 shrink-0 h-full flex flex-col min-h-0 bg-transparent @min-[48rem]:border-r border-gray-100 dark:border-zinc-800">
        
        <!-- Task List -->
        <div v-if="loading" class="flex-1 overflow-y-auto custom-scrollbar min-h-0 px-2 pb-20 relative flex items-center justify-center">
          <LoadingState label="Loading tasks..." />
        </div>
        
        <div v-else class="space-y-6 pb-20 @min-[48rem]:pb-0 overflow-y-auto custom-scrollbar px-4">
          <div v-for="grp in displayGroups" :key="grp.title" class="mb-4 @min-[48rem]:last:mb-0">
            <div class="mb-3 flex items-center gap-3">
              <h3 class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">{{ grp.title }}</h3>
              <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-500 bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 rounded-sm">{{ grp.tasks.length }}</span>
            </div>
            
            <div v-if="grp.tasks.length === 0" class="py-4 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-[11px] text-gray-500 dark:text-zinc-500 font-medium">
              No {{ grp.title.toLowerCase() }} tasks found.
            </div>

            <div v-else class="space-y-2">
              <template v-for="task in grp.tasks" :key="task.id">
                <!-- Consistent Compact Task Item (KeywordInbox Style) -->
                <div @click="openTask(task)"
                     @contextmenu.prevent.stop="openContextMenu($event, task)"
                     :class="[ 'p-4 -mx-4 @min-[48rem]:mx-0 cursor-pointer border-b border-gray-50 dark:border-zinc-800/50 group relative rounded-xl mb-1', String(selectedTaskId) === String(task.id) ? 'bg-white dark:bg-zinc-800 border-gray-100 dark:border-zinc-800 z-10' : 'bg-transparent hover:bg-gray-50 dark:hover:bg-zinc-800/50 ' ]">
                  
                  <div v-if="String(selectedTaskId) === String(task.id)" class="absolute left-0 top-4 bottom-4 w-1 bg-black dark:bg-white rounded-full"></div>
                  
                  <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center gap-2">
                      <div class="w-2 h-2 rounded-full shrink-0" :class="taskDotClass(task)"></div>
                      <span class="text-[10px] font-bold text-gray-500 dark:text-zinc-400 bg-gray-50 dark:bg-zinc-800/50 px-1.5 py-0.5 rounded uppercase tracking-tight group-hover:bg-gray-100 dark:group-hover:bg-zinc-700 group-hover:text-black dark:group-hover:text-white transition-colors">
                        {{ getWorkspaceName(task.workspaceId) }}
                      </span>
                    </div>
                    <div class="flex items-center gap-2 relative">
                       <!-- Action Menu (Visible on mobile, hover on desktop), as in a workspace's list -->
                       <div class="opacity-100 @min-[48rem]:opacity-0 @min-[48rem]:group-hover:opacity-100 flex items-center gap-1 mr-2 transition-opacity duration-150">
                          <template v-if="filterType === 'notstarted' && !isArchived(task)">
                            <button @click.stop="reorderTask(grp.tasks, task, -1)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all" title="Move Up">
                              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" /></svg>
                            </button>
                            <button @click.stop="reorderTask(grp.tasks, task, 1)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all" title="Move Down">
                              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
                            </button>
                          </template>
                          <button v-if="canEditTask(task, isArchived(task))" @click.stop="router.push(taskEditPath(task))" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all" title="Edit Task">
                            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                          </button>
                          <button v-if="canDeleteTask(task, isArchived(task))" @click.stop="taskToDelete = task" class="text-gray-500 hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10 p-1 rounded-sm transition-all" title="Delete Task">
                            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                          </button>
                       </div>
                       <span class="text-[10px] text-gray-500 dark:text-zinc-400 font-bold uppercase tracking-wider tabular-nums shrink-0">
                         {{ task.status === 'cron' ? formatDate(task.createdAt) : formatTime(task.createdAt) }}
                       </span>
                    </div>
                  </div>

                  <h3 :class="[ 'text-[13px] leading-relaxed line-clamp-2 transition-colors', 
                                 String(selectedTaskId) === String(task.id) ? 'text-gray-800 dark:text-zinc-200 font-bold' : 
                                 task.status === 'completed' ? 'text-gray-500 dark:text-zinc-500 font-light' : 
                                 'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white font-semibold' ]">
                    {{ task.title }}
                  </h3>
                  
                  <!-- Quick actions for Pending -->
                  <div v-if="filterType === 'pending'" class="mt-3" @click.stop>
                    <div class="flex flex-wrap gap-2" v-if="isAgentConnected(task.workspaceId)">
                      <template v-if="requiresAllowDeny(task)">
                        <button @click="handleAction(task, 'allow')" class="px-2.5 py-1.5 bg-gray-900 dark:bg-white text-white dark:text-black rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-black dark:hover:bg-gray-100 transition-all shadow-sm">
                          Allow
                        </button>
                        <button @click="handleAction(task, 'deny')" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-900/10 transition-all shadow-sm">
                          Deny
                        </button>
                      </template>
                      <button @click="openTask(task)" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-200 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all shadow-sm">
                        Review
                      </button>
                    </div>
                  </div>

                  <!-- Next execution for scheduled tasks -->
                  <div v-if="task.status === 'cron' && task.cronSchedule" class="mt-2 flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-zinc-500 font-medium uppercase tracking-tight">
                    <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                    <span>{{ getNextRunLabel(task.cronSchedule) }}</span>
                  </div>
                </div>
              </template>
            </div>

            <!-- One page runs across every group, so the button closes the last one. -->
            <LoadMoreButton v-if="hasMore && tasks.length > 0 && grp === displayGroups[displayGroups.length - 1]" class="mt-2"
                            :loading="loadingMore" @click="loadMore" />
          </div>
        </div>
        </div>
      

      <!-- Task Detail Pane (Right) -->
      <div v-show="selectedTaskId || !isMobile" class="flex-1 min-w-0 flex flex-col h-full bg-transparent">
        <router-view v-if="selectedTaskId" />
        
        <!-- Empty state when no task is selected -->
        <div v-else class="flex-1 flex flex-col items-center justify-center m-4 p-8 text-center h-full bg-gray-50 dark:bg-zinc-900/50 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800">
          <div class="w-16 h-16 bg-white dark:bg-zinc-800 rounded-sm border border-gray-100 dark:border-zinc-700 flex items-center justify-center mb-4 shadow-sm">
            <svg class="w-8 h-8 text-gray-300 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" />
            </svg>
          </div>
          <p class="text-lg font-bold text-gray-800 dark:text-zinc-200 tracking-tight">Select a task</p>
          <p class="text-sm text-gray-500 dark:text-zinc-400 mt-2 max-w-[420px] leading-relaxed">Choose a task from the list to view its details, conversation history, and take actions.</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchGlobalTasks, sendPermissionVerdict, updateTaskAssignee, updateTaskStatus, updateTaskOrder, deleteTask, moveTask } from '../api';
import { useToasts } from '../composables/useToasts';
import { useTooltipStore } from '../stores/tooltipStore';
import { useCron } from '../composables/useCron';
import { buildTaskGroups, pendingOnHuman } from '../composables/useTaskGroups';
import { orderForStep } from '../composables/useKanbanOrder';
import { taskDotClass } from '../composables/useTaskStatusStyle';
import { useEventBus } from '../useEventBus';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useViewport } from '../composables/useViewport';
import { useNarrowLayout } from '../composables/useNarrowLayout';
import LoadingState from '../components/LoadingState.vue';
import LoadMoreButton from '../components/LoadMoreButton.vue';
import DeleteModal from '../components/DeleteModal.vue';
import MoveTaskModal from '../components/MoveTaskModal.vue';
import ContextMenu from '../components/ContextMenu.vue';
import ExtensionViewPanel from '../components/ExtensionViewPanel.vue';
import { canEditTask, canDeleteTask, taskEditPath, withoutTask } from '../composables/useTaskRowActions';
import { menuItemsFor, parseSelection } from '../composables/useTaskContextMenu';
import { useExtensionSurfaces } from '../composables/useExtensionSurfaces';
import { cacheTasks, sharedCache } from '../composables/useCachedTasks';
import { readAllCachedTasks, shouldPaintCache } from '../composables/useCachedReads';

const { getNextRunLabel, getNextRunDateTime, getNextRunDate } = useCron();
const route = useRoute();
const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
// A phone's layout on a phone, and beside the desktop side panel.
const { isMobile: isMobileWindow } = useViewport();
const layoutRef = ref(null);
const isNarrow = useNarrowLayout(layoutRef);
const isMobile = computed(() => isMobileWindow.value || isNarrow.value);

const tasks = ref([]);
// Read, never written: the shell keeps the store's agentConnected current from
// the event stream, so the per-row dots below follow an agent coming and going
// instead of freezing at whatever was true when this list was fetched.
const workspaceStore = useWorkspaceStore();
const loading = ref(false);
const loadingMore = ref(false);
const offset = ref(0);
const limit = 10;
const hasMore = ref(true);
const tooltipStore = useTooltipStore();
const showMobileFilters = ref(false);

function openSearch() {
  window.dispatchEvent(new CustomEvent('open-command-palette'));
}


// Setup Global Event Bus
const { connect, disconnect, events } = useEventBus();

watch(events, (newEvents) => {
  if (newEvents.length === 0) return;
  const event = newEvents[newEvents.length - 1];
  
  // Refresh list on relevant task events
  if (['task.created', 'task.updated', 'status.updated', 'task.deleted', 'reply.received', 'respond.ack'].includes(event.type)) {
     // For global list, we refresh to keep it simple
     fetchInitial();
  }
}, { deep: true });

const filterType = computed(() => route.params.filter);

const title = computed(() => {
  const map = {
    active: 'Active Tasks',
    scheduled: 'Scheduled',
    pending: 'Pending on Me',
    notstarted: 'Not Started',
    ongoing: 'Ongoing Tasks',
    completed: 'Completed Tasks'
  };
  return map[filterType.value] || 'Active';
});

const emptyStateLabel = computed(() => {
  switch (filterType.value) {
    case 'active':
      return 'No active tasks found (includes not started and ongoing).';
    case 'notstarted':
      return 'No tasks waiting to start.';
    case 'pending':
      return 'No tasks pending your attention.';
    case 'ongoing':
      return 'No tasks currently in progress.';
    case 'completed':
      return 'No completed tasks found.';
    case 'scheduled':
      return 'No scheduled tasks configured.';
    default:
      return 'No tasks match this category across your workspaces.';
  }
});

const filters = [
  { id: 'active', label: 'Active', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' },
  { id: 'notstarted', label: 'Not Started', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2' },
  { id: 'pending', label: 'Pending on Me', icon: 'M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'ongoing', label: 'Ongoing', icon: 'M13 10V3L4 14h7v7l9-11h-7z' },
  { id: 'completed', label: 'Completed', icon: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'scheduled', label: 'Scheduled', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' }
];

const displayGroups = computed(() =>
  buildTaskGroups(tasks.value, filterType.value, { nextRunAt: getNextRunDate })
);

const activeTaskCount = computed(() => tasks.value.filter(t => t.status !== 'cron').length);
const scheduledCount = computed(() => tasks.value.filter(t => t.status === 'cron').length);
const pendingInputCount = computed(() => pendingOnHuman(tasks.value).length);

const isArchived = (task) => !!workspaceStore.getWorkspace(task.workspaceId)?.archivedAt;

// The same edit, delete and right-click a workspace's own list offers, so a task
// reached from the sidebar can be handled without first opening its workspace.
const taskToDelete = ref(null);
const taskToMove = ref(null);

// After a delete or a move the row goes at once, and the selected task's pane
// with it; the event that follows refreshes the list anyway.
const dropTask = (taskId) => {
  tasks.value = withoutTask(tasks.value, taskId);
  if (String(selectedTaskId.value) === String(taskId)) router.push(`/tasks/${filterType.value}`);
};

const onDeleteConfirm = async () => {
  const task = taskToDelete.value;
  if (!task) return;
  try {
    await deleteTask(task.workspaceId, task.id);
    dropTask(task.id);
    notifySuccess('Task deleted');
  } catch (err) {
    notifyError('Delete Error: ' + err.message);
  } finally {
    taskToDelete.value = null;
  }
};

const onMoveConfirm = async (destinationWorkspaceId) => {
  const task = taskToMove.value;
  if (!task) return;
  try {
    await moveTask(task.workspaceId, task.id, destinationWorkspaceId);
    dropTask(task.id);
    notifySuccess('Task moved');
  } catch (err) {
    notifyError('Move Error: ' + err.message);
  } finally {
    taskToMove.value = null;
  }
};

const contextMenu = ref({ show: false, x: 0, y: 0, task: null });
const extensionItems = ref([]);
const contextMenuItems = computed(() => menuItemsFor(contextMenu.value.task, extensionItems.value));
const extensions = useExtensionSurfaces();

watch(() => extensions.error.value, (reason) => {
  if (reason) notifyError(reason);
});

// Mirrors TaskFeed's: see there for why the extension rows are cleared first
// and why a late answer for an earlier right-click is dropped.
let contextMenuRequest = 0;
const openContextMenu = async (event, task) => {
  if (isArchived(task)) return;
  contextMenu.value = { show: true, x: event.clientX, y: event.clientY, task };
  extensionItems.value = [];
  const request = (contextMenuRequest += 1);
  const entries = await extensions.entriesFor('task-menu', task);
  if (request === contextMenuRequest) extensionItems.value = entries;
};

const closeContextMenu = () => {
  contextMenu.value = { ...contextMenu.value, show: false };
};

const onContextMenuSelect = (key) => {
  const task = contextMenu.value.task;
  if (!task) return;
  const parsed = parseSelection(key);
  if (parsed.kind === 'extension') {
    extensions.invoke({ owner: parsed.owner, id: parsed.id, surface: 'task-menu' }, task);
    return;
  }
  if (key === 'move') taskToMove.value = task;
};

const onExtensionAction = (action) => {
  const panel = extensions.panel.value;
  const task = contextMenu.value.task;
  if (!panel || !task) return;
  extensions.invoke({ owner: panel.owner, id: panel.id, surface: 'task-menu' }, { ...task, action });
};

const getWorkspaceName = (workspaceId) => workspaceStore.getWorkspace(workspaceId)?.name || '...';

const isAgentConnected = (workspaceId) => workspaceStore.isAgentConnected(workspaceId);

const getLastMessageText = (task) => {
  if (!task.messages || task.messages.length === 0) return 'No message content available.';
  const last = task.messages[task.messages.length - 1];
  return last.text || 'No message content available.';
};


function requiresAllowDeny(t) {
  if (!t || typeof t !== 'object') return false;
  if (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending')) return true;
  if (t.status === 'notstarted' && t.assignee === 'human' && t.createdBy === 'agent') return true;
  return false;
}

const handleAction = async (task, action) => {
  try {
    if (task.status === 'notstarted' && task.assignee === 'human') {
      if (action === 'allow') {
        await updateTaskAssignee(task.workspaceId, task.id, 'agent');
        await updateTaskStatus(task.workspaceId, task.id, 'ongoing');
        notifySuccess('Task started and assigned to agent');
      } else {
        await updateTaskStatus(task.workspaceId, task.id, 'rejected');
        notifySuccess('Task rejected');
      }
      await fetchInitial();
      return;
    }

    // Find the latest message that is a permission_request and has no verdict yet
    const pendingMsg = [...(task.messages || [])].reverse().find(m => 
      m.metadata?.type === 'permission_request' && 
      m.metadata?.status !== 'allow' && 
      m.metadata?.status !== 'deny'
    );
    
    const requestId = pendingMsg?.metadata?.request_id || pendingMsg?.metadata?.requestId;
    if (!requestId) throw new Error('No pending permission request found');
    
    const behavior = action === 'allow' ? 'allow' : 'deny';
    await sendPermissionVerdict(task.workspaceId, task.id, requestId, behavior);
    notifySuccess(`Permission ${action === 'allow' ? 'allowed' : 'denied'}`);
    // Refresh the list to remove the acted task
    await fetchInitial();
  } catch (err) {
    notifyError(`Failed to ${action} task: ` + err.message);
  }
};

const reorderTask = async (groupTasks, task, direction) => {
  const newOrder = orderForStep(groupTasks, task, direction);
  if (newOrder === null) return;

  try {
    await updateTaskOrder(task.workspaceId, task.id, newOrder);
    await fetchInitial();
  } catch (err) {
    notifyError('Reorder Error: ' + err.message);
  }
};

// Every task event starts a refresh, and one piece of activity is a burst of
// them, so refreshes overlap. Only the latest may write the list: an older one
// answering late appended its first page to the newer one's, once per event.
let generation = 0;

const fetchInitial = async () => {
  const gen = (generation += 1);
  loading.value = true;
  tasks.value = [];
  offset.value = 0;
  hasMore.value = true;
  
  // Stale first, then true — and before any request, including the one for the
  // workspace list. Reading the cache needs no server round trip, so putting one
  // in front of it would defeat the point.
  const cached = await readAllCachedTasks(sharedCache(), { limit });
  if (gen !== generation) return;
  if (shouldPaintCache(tasks.value, cached)) {
    tasks.value = cached;
    loading.value = false;
  }

  try {
    // Only when the shell has not already filled it. This runs again on every
    // task event, and re-fetching a list the store keeps live from the event
    // stream would be a request per event plus a re-render everywhere it is
    // rendered.
    if (workspaceStore.workspaces.length === 0) await workspaceStore.fetchWorkspaces();

    await fetchNext();
  } catch (err) {
    console.error('Failed to fetch tasks:', err);
  } finally {
    loading.value = false;
  }
};

const fetchNext = async () => {
  const params = getBackendParams(filterType.value);
  params.limit = limit;
  params.offset = offset.value;
  
  const gen = generation;
  try {
    const res = await fetchGlobalTasks(params);
    if (gen !== generation) return;
    const newTasks = res.tasks || [];
    
    if (newTasks.length < limit) {
      hasMore.value = false;
    }
    
    // The first page replaces whatever is on screen — which may be the cached
    // rows painted a moment ago — and later pages append to it. Appending the
    // first page would show every cached task twice.
    tasks.value = params.offset === 0 ? newTasks : [...tasks.value, ...newTasks];
    offset.value += newTasks.length;
    // Write-through: the server just told us about these, so the local copy is
    // brought up to date with the same answer the view is rendering.
    cacheTasks(sharedCache(), newTasks);
  } catch (err) {
    console.error('Failed to load more tasks:', err);
  }
};

// Its own flag, not `loading`: that one swaps the whole list for a spinner, so
// a load more used to blank every row already on screen until the page landed.
const loadMore = async () => {
  if (loading.value || loadingMore.value) return;
  loadingMore.value = true;
  try {
    await fetchNext();
  } finally {
    loadingMore.value = false;
  }
};

const getBackendParams = (filter) => {
  if (filter === 'active') return { status: 'ongoing,blocked,notstarted,cron' };
  if (filter === 'scheduled') return { status: 'cron' };
  if (filter === 'pending') return { filter: 'pending_approval' };
  if (filter === 'notstarted') return { status: 'notstarted' };
  if (filter === 'ongoing') return { status: 'ongoing,blocked' };
  if (filter === 'completed') return { status: 'completed,rejected' };
  return { status: 'ongoing,blocked,notstarted' };
};

const selectedTaskId = computed(() => route.params.taskId);

const openTask = (task) => {
  if (task.status === 'cron') {
    router.push(`/tasks/${filterType.value}/${task.workspaceId}/${task.id}/instances`);
  } else {
    router.push(`/tasks/${filterType.value}/${task.workspaceId}/${task.id}`);
  }
};

const formatDate = (dateStr) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
};

const formatTime = (dateStr) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  
  const now = new Date();
  const diff = now - d;
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return 'Just Now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
};

const headerIconPath = computed(() => {
  switch (filterType.value) {
    case 'scheduled':
      return 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z';
    case 'pending':
      return 'M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z';
    case 'notstarted':
      return 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2';
    case 'ongoing':
      return 'M13 10V3L4 14h7v7l9-11h-7z';
    case 'completed':
      return 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z';
    default:
      return 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z';
  }
});

const headerIconClass = computed(() => {
  return 'text-gray-700 dark:text-zinc-400';
});

const getTaskBgStyle = (t) => {
  const isSelected = String(selectedTaskId.value) === String(t.id);
  if (isSelected) {
    if (t.status === 'ongoing') return 'bg-yellow-50 dark:bg-yellow-900/10 border-l-yellow-400 dark:border-l-yellow-500 shadow-sm';
    if (t.status === 'blocked') return 'bg-red-50 dark:bg-red-900/10 border-l-red-400 dark:border-l-red-500 shadow-sm';
    if (t.status === 'completed') return 'bg-gray-50 dark:bg-zinc-900 border-l-gray-900 dark:border-l-zinc-100 shadow-sm';
    if (t.status === 'cron') return 'bg-sky-50 dark:bg-sky-900/10 border-l-sky-400 dark:border-l-sky-500 shadow-sm';
    return 'bg-white dark:bg-zinc-800 border-l-gray-400 dark:border-l-gray-500 shadow-sm';
  }
  
  if (t.status === 'ongoing') return 'bg-yellow-50/50 dark:bg-yellow-900/5 hover:bg-yellow-50 dark:hover:bg-yellow-900/20 hover:shadow-sm';
  if (t.status === 'blocked') return 'bg-red-50/50 dark:bg-red-900/5 hover:bg-red-50 dark:hover:bg-red-900/20 hover:shadow-sm';
  if (t.status === 'completed') return 'bg-gray-50/50 dark:bg-zinc-900/5 hover:bg-gray-100 dark:hover:bg-zinc-800/80 hover:shadow-sm';
  if (t.status === 'cron') return 'bg-sky-50/50 dark:bg-sky-900/5 hover:bg-sky-50 dark:hover:bg-sky-900/20 hover:shadow-sm';
  return 'bg-white/40 dark:bg-zinc-900/30 hover:bg-white dark:hover:bg-zinc-900/80 hover:shadow-sm';
};

const getTaskLabel = (status) => {
  if (status === 'cron') return 'Scheduled';
  if (status === 'notstarted') return 'Not Started';
  return status;
};

watch(() => route.params.filter, () => {
  fetchInitial();
});

onMounted(() => {
  fetchInitial();
  connect();
});

onUnmounted(() => {
  disconnect();
});
</script>
