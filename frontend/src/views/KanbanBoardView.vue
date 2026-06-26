<template>
  <div class="flex flex-col h-full w-full bg-transparent min-h-0">
    <div v-if="loading" class="flex-1 flex items-center justify-center py-20 text-sm font-bold text-gray-500 dark:text-zinc-500 animate-pulse">
      Loading board...
    </div>

    <div v-else class="flex-1 flex gap-2 md:gap-3 p-3 md:p-4 min-h-0">
      <div v-for="col in columns" :key="col.id"
           class="flex-1 min-w-0 flex flex-col min-h-0 rounded-xl border bg-gray-50/50 dark:bg-zinc-900/40 transition-colors"
           :class="dragOverColId === col.id ? 'border-gray-900/40 dark:border-white/40' : 'border-gray-100 dark:border-zinc-800'"
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

            <div :draggable="!isArchived"
                 @dragstart="onDragStart($event, t, col.id)"
                 @dragend="onDragEnd"
                 @dragover="onCardDragOver($event, col.id, t)"
                 @click="openTask(t)"
                 :class="[
                   'group relative flex items-center gap-2 px-2.5 py-2 rounded-lg border bg-white dark:bg-zinc-900 shadow-sm transition-all',
                   !isArchived ? 'cursor-grab active:cursor-grabbing hover:border-gray-200 dark:hover:border-zinc-700' : 'cursor-pointer',
                   draggingId === t.id ? 'opacity-40' : 'border-gray-100 dark:border-zinc-800'
                 ]">
              <!-- Assignee icon — same bot/person icons used in the chat view -->
              <svg v-if="t.assignee === 'agent'" title="Agent" class="w-3.5 h-3.5 shrink-0 text-gray-400 dark:text-zinc-500" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
              <svg v-else title="Human" class="w-3.5 h-3.5 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>

              <span class="flex-1 min-w-0 text-[12px] md:text-[13px] leading-snug truncate font-medium"
                    :class="['completed','rejected'].includes(t.status) ? 'text-gray-400 dark:text-zinc-500' : 'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white'">
                {{ t.title }}
              </span>
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
                  class="w-full py-1.5 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 text-[10px] font-semibold hover:border-gray-300 dark:hover:border-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-50 dark:hover:bg-zinc-800/50 transition-all">
            Load More
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchTasks, updateTaskStatus, updateTaskOrder } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';
import { useWorkspaceStore } from '../stores/workspaceStore';

const route = useRoute();
const router = useRouter();
const { notifyError } = useToasts();
const workspaceStore = useWorkspaceStore();

const workspaceId = computed(() => route.params.id);
const isArchived = computed(() => !!workspaceStore.workspaces.find(w => w.id == workspaceId.value)?.archivedAt);

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

async function loadColumn(colId, isLoadMore = false) {
  try {
    const col = columnById[colId];
    const offset = isLoadMore ? offsets.value[colId] : 0;
    const res = await fetchTasks(workspaceId.value, { status: col.statuses.join(','), limit: PAGE_SIZE, offset });
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
