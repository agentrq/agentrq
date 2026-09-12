<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
  <div class="flex flex-col h-full w-full bg-transparent min-h-0">
    <div v-if="loading" class="flex-1 flex items-center justify-center py-20">
      <LoadingState label="Loading board..." />
    </div>

    <div v-else class="flex-1 flex gap-2 md:gap-3 p-3 md:p-4 min-h-0">
      <!-- Columns are frameless: the header and the cards are enough to read one
           as a column, and four nested borders inside the page's own container
           read as a table. While a card is in flight the column still has to
           say which one would receive it, so the drop target is a tinted
           background rather than a border that only exists mid-drag. -->
      <div v-for="col in columns" :key="col.id"
           class="flex-1 min-w-0 flex flex-col min-h-0 rounded-xl transition-colors"
           :class="dragOverColId === col.id ? 'bg-gray-100/80 dark:bg-zinc-800/40' : 'bg-transparent'"
           @dragover="onColumnDragOver($event, col.id)"
           @drop="onDrop($event, col.id)">

        <!-- Column header -->
        <div class="flex items-center gap-1.5 px-2.5 md:px-3 py-2.5 shrink-0 min-w-0">
          <div class="w-1.5 h-1.5 rounded-full shrink-0" :class="col.dot"></div>
          <h3 class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-wider md:tracking-widest truncate">{{ col.title }}</h3>
          <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-500 bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 rounded-sm shrink-0 ml-auto">{{ buckets[col.id].length }}</span>
        </div>

        <!-- Cards -->
        <div class="flex-1 overflow-y-auto custom-scrollbar px-2 pb-3 space-y-2 min-h-0">
          <template v-for="t in buckets[col.id]" :key="t.id">
            <!-- Insertion indicator -->
            <div v-if="dragOverColId === col.id && dragOverBeforeId === t.id" class="h-0.5 rounded-full bg-gray-900 dark:bg-white mx-1"></div>

            <!-- Cards carry the Active feed's layout: status dot, who it is on,
                 when it arrived, then up to two lines of title. A column is
                 narrower than that feed, so the title wraps instead of being
                 truncated — a one-line card here showed little more than the
                 first few words of most task titles. -->
            <div :draggable="!isArchived"
                 @dragstart="onDragStart($event, t, col.id)"
                 @dragend="onDragEnd"
                 @dragover="onCardDragOver($event, col.id, t)"
                 @click="openTask(t)"
                 @contextmenu.prevent.stop="openContextMenu($event, t)"
                 :class="[
                   'group relative p-3 pl-4 rounded-xl bg-gray-100 dark:bg-zinc-800 transition-all',
                   !isArchived ? 'cursor-grab active:cursor-grabbing hover:bg-gray-200 dark:hover:bg-zinc-700' : 'cursor-pointer',
                   draggingId === t.id ? 'opacity-40' : ''
                 ]">
              <!-- The card's only edge: no border, so the shape comes from the
                   fill against the canvas and the one bar that carries meaning.
                   Inset and rounded, the same bar the Active feed draws down a
                   selected task — there it marks the selection, here it marks
                   the status, and it is on every card rather than one. -->
              <div class="absolute left-0 top-3 bottom-3 w-1 rounded-full" :class="taskAccentClass(t)"></div>

              <div class="flex items-center justify-between gap-2 mb-2">
                <div class="flex items-center gap-2 min-w-0">
                  <span class="text-gray-500 dark:text-zinc-400 group-hover:text-gray-900 dark:group-hover:text-white transition-colors shrink-0" :title="t.assignee === 'agent' ? 'Agent' : 'Human'">
                    <svg v-if="t.assignee === 'agent'" class="w-3.5 h-3.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
                    <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
                  </span>
                </div>
                <span class="text-[10px] text-gray-500 dark:text-zinc-400 font-medium uppercase tracking-wider tabular-nums shrink-0">
                  {{ formatTime(t.createdAt) }}
                </span>
              </div>

              <h3 class="text-[12px] md:text-[13px] leading-snug line-clamp-2 font-medium transition-colors"
                  :class="['completed','rejected'].includes(t.status) ? 'text-gray-400 dark:text-zinc-500' : 'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white'">
                {{ t.title }}
              </h3>
              
              <!-- Quick actions for Pending -->
              <div v-if="isPendingOnHuman(t) && requiresAllowDeny(t)" class="mt-3" @click.stop>
                <div class="flex flex-wrap gap-2" v-if="isAgentConnected">
                  <button @click="handleAction(t, 'allow')" class="px-2.5 py-1.5 bg-gray-900 dark:bg-white text-white dark:text-black rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-black dark:hover:bg-gray-100 transition-all shadow-sm">
                    Allow
                  </button>
                  <button @click="handleAction(t, 'deny')" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-900/10 transition-all shadow-sm">
                    Deny
                  </button>
                </div>
              </div>
            </div>
          </template>

          <!-- Trailing insertion indicator (drop at end of column) -->
          <div v-if="dragOverColId === col.id && dragOverBeforeId === null" class="h-0.5 rounded-full bg-gray-900 dark:bg-white mx-1"></div>

          <!-- Empty state -->
          <div v-if="buckets[col.id].length === 0"
               class="py-6 px-3 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-[11px] text-gray-400 dark:text-zinc-600 font-medium text-center">
            No tasks
          </div>

          <!-- Load more -->
          <button v-if="hasMore[col.id]" @click.stop="loadColumn(col.id, true)"
                  class="w-full py-1.5 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 text-[10px] font-semibold hover:border-gray-300 dark:hover:border-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800/50 transition-all">
            Load More
          </button>
        </div>
      </div>
    </div>

    <!-- Move Task Modal -->
    <MoveTaskModal
      :show="showMoveModal"
      :taskTitle="taskToMoveTitle"
      :currentWorkspaceId="workspaceId"
      @close="closeMoveModal"
      @confirm="onMoveConfirm"
    />

    <ExtensionViewPanel v-if="extensions.panel.value" :view="extensions.panel.value"
                       @action="onExtensionAction" @close="extensions.dismiss" />

    <!-- Task Context Menu -->
    <ContextMenu
      :show="contextMenu.show"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :items="contextMenuItems"
      @close="closeContextMenu"
      @select="onContextMenuSelect"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchTasks, updateTaskStatus, updateTaskOrder, moveTask, updateTaskAssignee, sendPermissionVerdict } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { menuItemsFor, parseSelection } from '../composables/useTaskContextMenu';
import { useExtensionSurfaces } from '../composables/useExtensionSurfaces';
import ExtensionViewPanel from '../components/ExtensionViewPanel.vue';
import LoadingState from '../components/LoadingState.vue';
import MoveTaskModal from '../components/MoveTaskModal.vue';
import ContextMenu from '../components/ContextMenu.vue';
import { cacheTasks, sharedCache } from '../composables/useCachedTasks';
import { readCachedTasks, shouldPaintCache } from '../composables/useCachedReads';
import { taskAccentClass } from '../composables/useTaskStatusStyle';

const route = useRoute();
const router = useRouter();
const { notifyError, notifySuccess } = useToasts();
const workspaceStore = useWorkspaceStore();

const workspaceId = computed(() => route.params.id);
const isArchived = computed(() => !!workspaceStore.workspaces.find(w => w.id == workspaceId.value)?.archivedAt);

const isAgentConnected = computed(() => !!workspaceStore.workspaces.find(w => w.id == workspaceId.value)?.agentConnected);


function requiresAllowDeny(t) {
  if (!t || typeof t !== 'object') return false;
  if (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending')) return true;
  if (t.status === 'notstarted' && t.assignee === 'human' && t.createdBy === 'agent') return true;
  return false;
}

function isPendingOnHuman(t) {
  if (!t || typeof t !== 'object') return false;
  if (t.status === 'completed' || t.status === 'rejected') return false;
  if (t.status === 'notstarted' && t.assignee === 'human') return true;
  return !!(
    t.messages &&
    t.messages.some(
      (m) => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'
    )
  );
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
      return;
    }

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
  } catch (err) {
    notifyError(`Failed to ${action} task: ` + err.message);
  }
};


// Each column buckets one or more task statuses. Dropping a card into a column
// from a different column sets its status to the column's `dropStatus`; dropping
// a card already in the column just reorders it (preserving its exact status, so
// completed/rejected cards keep their identity within the Done column).
// 'cron' tasks (scheduled templates) are intentionally excluded — their status
// is immutable and they are not part of the kanban workflow.
const columns = [
  { id: 'notstarted', title: 'Not Started', statuses: ['notstarted'], dropStatus: 'notstarted', dot: 'bg-gray-400 dark:bg-zinc-500' },
  { id: 'ongoing', title: 'Ongoing', statuses: ['ongoing'], dropStatus: 'ongoing', dot: 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)]' },
  { id: 'blocked', title: 'Blocked', statuses: ['blocked'], dropStatus: 'blocked', dot: 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]' },
  { id: 'done', title: 'Done', statuses: ['completed', 'rejected'], dropStatus: 'completed', dot: 'bg-green-500' },
];

const columnById = Object.fromEntries(columns.map(c => [c.id, c]));

const PAGE_SIZE = 10;
const loading = ref(true);
const tasks = ref([]);
const offsets = ref(Object.fromEntries(columns.map(c => [c.id, 0])));
const hasMore = ref(Object.fromEntries(columns.map(c => [c.id, false])));

function formatTime(dateStr) {
  if (!dateStr) return 'Just now';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return 'Just now';
  
  const diff = Date.now() - d.getTime();
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const months = Math.floor(days / 30);
  const years = Math.floor(days / 365);
  
  if (years > 0) return `${years} ${years === 1 ? 'year' : 'years'} ago`;
  if (months > 0) return `${months} ${months === 1 ? 'month' : 'months'} ago`;
  if (days > 0) return `${days} ${days === 1 ? 'day' : 'days'} ago`;
  if (hours > 0) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
  if (minutes > 0) return `${minutes} ${minutes === 1 ? 'minute' : 'minutes'} ago`;
  return 'Just now';
}

function getOrder(t) {
  if (t.sortOrder) return t.sortOrder;
  if (!t.createdAt) return Date.now() / 1000.0;
  return new Date(t.createdAt).getTime() / 1000.0;
}

const buckets = computed(() => {
  const out = {};
  for (const c of columns) {
    out[c.id] = tasks.value
      .filter(t => c.statuses.includes(t.status))
      .sort((a, b) => getOrder(a) - getOrder(b));
  }
  return out;
});

function upsert(t) {
  if (!t || !t.id) return;
  const idx = tasks.value.findIndex(x => String(x.id) === String(t.id));
  if (idx === -1) {
    tasks.value.push(t);
  } else {
    tasks.value[idx] = { ...tasks.value[idx], ...t };
  }
}

function removeTask(id) {
  tasks.value = tasks.value.filter(x => String(x.id) !== String(id));
}

/**
 * Paint the board from the local copy before the columns are fetched.
 *
 * Only into an empty board: the columns each replace their own tasks as they
 * arrive, so this is a first frame rather than a source the fetches merge into.
 */
async function paintFromCache() {
  const cached = await readCachedTasks(sharedCache(), workspaceId.value);
  if (shouldPaintCache(tasks.value, cached)) tasks.value = cached;
}

async function loadColumn(colId, isLoadMore = false) {
  try {
    const col = columnById[colId];
    const offset = isLoadMore ? offsets.value[colId] : 0;
    const res = await fetchTasks(workspaceId.value, { status: col.statuses.join(','), limit: PAGE_SIZE, offset });
    // Write-through, same as the other lists: what the server just returned is
    // what the local copy should hold.
    cacheTasks(sharedCache(), res.tasks || []);
    const fetched = res.tasks || [];
    if (!isLoadMore) {
      // Drop any locally-known tasks for this column that no longer exist server-side.
      tasks.value = tasks.value.filter(t => !col.statuses.includes(t.status));
    }
    fetched.forEach(upsert);
    offsets.value[colId] = offset + fetched.length;
    hasMore.value[colId] = fetched.length === PAGE_SIZE;
  } catch (err) {
    notifyError(`Failed to load ${colId} tasks: ` + err.message);
  }
}

async function loadAll() {
  loading.value = true;
  // Stale first: an empty board fills from the local copy, then each column
  // replaces its own tasks as the server answers for it.
  await paintFromCache();
  if (tasks.value.length > 0) loading.value = false;
  await Promise.all(columns.map(c => loadColumn(c.id)));
  loading.value = false;
}

// ---- Drag & drop ----
const draggingId = ref(null);
const dragFromColId = ref(null);
const dragOverColId = ref(null);
const dragOverBeforeId = ref(undefined); // task id to insert before, or null = end of column

function onDragStart(e, task, colId) {
  if (isArchived.value) return;
  draggingId.value = task.id;
  dragFromColId.value = colId;
  e.dataTransfer.effectAllowed = 'move';
  // Firefox requires data to be set for dnd to initiate.
  try { e.dataTransfer.setData('text/plain', String(task.id)); } catch (_) {}
}

function onDragEnd() {
  draggingId.value = null;
  dragFromColId.value = null;
  dragOverColId.value = null;
  dragOverBeforeId.value = undefined;
}

function onColumnDragOver(e, colId) {
  if (!draggingId.value) return;
  e.preventDefault();
  // Only initialize when entering a new column; card-level handler refines position.
  if (dragOverColId.value !== colId) {
    dragOverColId.value = colId;
    dragOverBeforeId.value = null;
  }
}

function onCardDragOver(e, colId, task) {
  if (!draggingId.value) return;
  e.preventDefault();
  const rect = e.currentTarget.getBoundingClientRect();
  const after = e.clientY > rect.top + rect.height / 2;
  dragOverColId.value = colId;
  if (!after) {
    dragOverBeforeId.value = task.id;
  } else {
    const col = buckets.value[colId];
    const idx = col.findIndex(t => String(t.id) === String(task.id));
    const next = col[idx + 1];
    dragOverBeforeId.value = next ? next.id : null;
  }
}

async function onDrop(e, colId) {
  if (!draggingId.value) return;
  e.preventDefault();
  const id = draggingId.value;
  const fromColId = dragFromColId.value;
  const beforeId = dragOverBeforeId.value;
  onDragEnd();

  const task = tasks.value.find(t => String(t.id) === String(id));
  if (!task) return;

  const col = columnById[colId];
  // Moving in from another column adopts the column's drop-status; reordering
  // within a multi-status column (Done) preserves the card's exact status.
  const newStatus = col.statuses.includes(task.status) ? task.status : col.dropStatus;

  // Neighbours in the target column, excluding the dragged task itself.
  const neighbours = buckets.value[colId].filter(t => String(t.id) !== String(id));
  let pos = beforeId == null ? neighbours.length : neighbours.findIndex(t => String(t.id) === String(beforeId));
  if (pos === -1) pos = neighbours.length;
  const prev = neighbours[pos - 1];
  const next = neighbours[pos];

  let newOrder;
  if (!prev && !next) newOrder = Date.now() / 1000.0;
  else if (!prev) newOrder = getOrder(next) - 1;
  else if (!next) newOrder = getOrder(prev) + 1;
  else newOrder = (getOrder(prev) + getOrder(next)) / 2;

  // No-op: same column, same status, dropped in its current slot.
  if (fromColId === colId && task.status === newStatus && getOrder(task) === newOrder) return;

  const snapshot = { status: task.status, sortOrder: getOrder(task) };
  upsert({ id: task.id, status: newStatus, sortOrder: newOrder }); // optimistic

  try {
    if (task.status !== newStatus) {
      await updateTaskStatus(workspaceId.value, id, newStatus);
    }
    await updateTaskOrder(workspaceId.value, id, newOrder);
  } catch (err) {
    upsert({ id: task.id, status: snapshot.status, sortOrder: snapshot.sortOrder }); // revert
    const msg = (err && err.message) || 'failed to move task';
    notifyError('Move failed: ' + msg);
  }
}

function openTask(t) {
  if (draggingId.value) return;
  router.push({ path: `/workspaces/${workspaceId.value}/tasks/${t.id}`, query: route.query });
}

// ---- Context menu / move task ----
const contextMenu = ref({ show: false, x: 0, y: 0, task: null });

// The same list TaskFeed reads. See useTaskContextMenu for why it is shared.
// Fetched when the menu opens rather than held: one bridge call on a gesture
// somebody just made, and an extension enabled a moment ago is on the next
// right-click instead of after a reload.
const extensionItems = ref([]);
const contextMenuItems = computed(() => menuItemsFor(contextMenu.value.task, extensionItems.value));

const extensions = useExtensionSurfaces();

// Said out loud. `invoke` records a refusal and draws no panel, so without this
// a menu item that failed — a grant the user narrowed, an extension that threw
// — is indistinguishable from a click that did nothing at all.
watch(
  () => extensions.error.value,
  (reason) => {
    if (reason) notifyError(reason);
  },
);
const showMoveModal = ref(false);
const taskToMoveId = ref(null);
const taskToMoveTitle = ref('');

/** Which right-click the rows on screen belong to. */
let contextMenuRequest = 0;

async function openContextMenu(event, task) {
  contextMenu.value = { show: true, x: event.clientX, y: event.clientY, task };
  // Cleared first. The rows still held were decided for the *previous* task, so
  // leaving them up means a row whose `when(task)` said no about this one is on
  // screen and clickable until the bridge answers.
  extensionItems.value = [];

  const request = (contextMenuRequest += 1);
  // The built-in items are already on screen; the extension rows arrive when
  // the main process has run each one's `when(task)`. A bridge that is slow or
  // broken costs nothing here, because `entriesFor` answers with an empty list
  // rather than throwing — right-click must keep working regardless.
  const entries = await extensions.entriesFor('task-menu', task);
  // A second right-click while this was in flight owns the menu now, and a slow
  // answer for the task before it must not land on top of the new one.
  if (request === contextMenuRequest) extensionItems.value = entries;
}

/**
 * A button inside an extension's panel, handed back to whoever drew it.
 *
 * The action is never interpreted here — the entry that produced the view is
 * asked again with the action alongside the task, which is what makes a button
 * in the vocabulary mean anything at all.
 */
function onExtensionAction(action) {
  const panel = extensions.panel.value;
  const task = contextMenu.value.task;
  if (!panel || !task) return;
  extensions.invoke({ owner: panel.owner, id: panel.id, surface: 'task-menu' }, { ...task, action });
}

function closeContextMenu() {
  contextMenu.value = { ...contextMenu.value, show: false };
}

function onContextMenuSelect(key) {
  const task = contextMenu.value.task;
  if (!task) return;

  // Namespaced on the way in, so a built-in and an extension entry can never be
  // confused however an author names theirs.
  const parsed = parseSelection(key);
  if (parsed.kind === 'extension') {
    extensions.invoke({ owner: parsed.owner, id: parsed.id, surface: 'task-menu' }, task);
    return;
  }

  if (key === 'move') {
    taskToMoveId.value = task.id;
    taskToMoveTitle.value = task.title;
    showMoveModal.value = true;
  }
}

function closeMoveModal() {
  showMoveModal.value = false;
  taskToMoveId.value = null;
  taskToMoveTitle.value = '';
}

async function onMoveConfirm(destinationWorkspaceId) {
  const taskId = taskToMoveId.value;
  if (!taskId) return;
  try {
    await moveTask(workspaceId.value, taskId, destinationWorkspaceId);
    removeTask(taskId);
    notifySuccess('Task moved');
  } catch (err) {
    notifyError('Move Error: ' + err.message);
  } finally {
    closeMoveModal();
  }
}

// ---- Live updates ----
const { connect, disconnect, events } = useEventBus(workspaceId);

watch(() => events.value.length, (newLen, oldLen) => {
  if (newLen <= oldLen) return;
  events.value.slice(oldLen).forEach(ev => {
    if (ev.type === 'task.deleted') {
      removeTask(ev.payload?.id);
      return;
    }
    if (['task.created', 'task.updated', 'status.updated', 'respond.ack'].includes(ev.type)) {
      const t = ev.payload;
      if (!t || (t.workspaceId && String(t.workspaceId) !== String(workspaceId.value))) return;
      const existing = tasks.value.find(x => String(x.id) === String(t.id));
      if (existing && existing.updatedAt && t.updatedAt &&
          new Date(existing.updatedAt).getTime() > new Date(t.updatedAt).getTime()) return;
      upsert(t);
    }
  });
});

onMounted(() => {
  loadAll();
  connect();
});

onUnmounted(() => {
  disconnect();
});

watch(workspaceId, () => {
  tasks.value = [];
  offsets.value = Object.fromEntries(columns.map(c => [c.id, 0]));
  hasMore.value = Object.fromEntries(columns.map(c => [c.id, false]));
  loadAll();
});
</script>
