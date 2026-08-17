<script setup>
import { ref, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  getWorkflow, updateWorkflow, fetchWorkflowSteps, createWorkflowStep,
  deleteWorkflowStep, fetchWorkflowTasks, fetchWorkflowText, replaceWorkflowFromText,
  fetchEvents,
} from '../api';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useToasts } from '../composables/useToasts';
import DeleteModal from '../components/DeleteModal.vue';
import LoadingState from '../components/LoadingState.vue';

const route = useRoute();
const router = useRouter();
const workspaceStore = useWorkspaceStore();
const { notifyError, notifySuccess } = useToasts();

const workflowId = route.params.id;

const workflow = ref(null);
const steps = ref([]);
const events = ref([]);
const loading = ref(true);

const mode = ref('graph');

// ── Layout geometry ───────────────────────────────────────────────────────────
// Nodes are auto-placed in alternating columns (event, workspaces, event, …) so
// the graph is readable without anyone arranging it. A node the user has
// dragged gets an entry in `positions` and stops being auto-placed; clearing
// that entry returns it to the automatic spot.

const COLUMN_WIDTH = 260;
const ROW_HEIGHT = 96;
const NODE_WIDTH = 200;
const NODE_HEIGHT = 56;
const CANVAS_PADDING = 32;

const positions = ref({});

function nodeKey(node) {
  return node.kind === 'event' ? `event:${node.eventId}` : `step:${node.stepId}`;
}

// ── Graph model ───────────────────────────────────────────────────────────────
// Derived entirely from steps, so the canvas can never disagree with what will
// actually run.

const eventsById = computed(() => {
  const map = {};
  for (const ev of events.value) map[ev.id] = ev;
  return map;
});

const workspacesById = computed(() => {
  const map = {};
  for (const ws of workspaceStore.workspaces) map[ws.id] = ws;
  return map;
});

function eventName(id) {
  return eventsById.value[id]?.name ?? '(deleted event)';
}

function workspaceName(id) {
  return workspacesById.value[id]?.name ?? '(deleted workspace)';
}

// stepsByEvent groups the fan-out for each event.
const stepsByEvent = computed(() => {
  const map = {};
  for (const s of steps.value) {
    (map[s.eventId] ||= []).push(s);
  }
  return map;
});

/**
 * Walks outward from the start event assigning each node a column.
 *
 * An event reachable by several paths is placed once, at its deepest column, so
 * a diamond re-join renders as one node with two incoming edges rather than as
 * duplicates. The visited set also stops the walk on a cyclic graph — cycles
 * are rejected on save, but the canvas must not hang on data that predates that.
 */
const graph = computed(() => {
  const startId = workflow.value?.startEventId;
  if (!startId) return { nodes: [], edges: [], width: 0, height: 0 };

  const eventColumn = {};
  const order = [];

  const queue = [{ eventId: startId, column: 0 }];
  const guard = new Set();
  while (queue.length) {
    const { eventId, column } = queue.shift();
    const seenKey = `${eventId}@${column}`;
    if (guard.has(seenKey)) continue;
    guard.add(seenKey);

    if (eventColumn[eventId] === undefined) order.push(eventId);
    eventColumn[eventId] = Math.max(eventColumn[eventId] ?? 0, column);

    for (const step of stepsByEvent.value[eventId] ?? []) {
      if (step.emitEventId) queue.push({ eventId: step.emitEventId, column: column + 2 });
    }
  }

  const nodes = [];
  const edges = [];
  const rowCursor = {};

  function place(column) {
    const row = rowCursor[column] ?? 0;
    rowCursor[column] = row + 1;
    return row;
  }

  // Events first so their rows are assigned before the steps that hang off them.
  const eventNodes = {};
  for (const eventId of order) {
    const column = eventColumn[eventId];
    const node = {
      kind: 'event',
      eventId,
      label: eventName(eventId),
      column,
      row: place(column),
      isStart: eventId === startId,
    };
    eventNodes[eventId] = node;
    nodes.push(node);
  }

  for (const step of steps.value) {
    const source = eventNodes[step.eventId];
    if (!source) continue; // orphaned step: its source event is unreachable
    const column = source.column + 1;
    const node = {
      kind: 'step',
      stepId: step.id,
      step,
      label: workspaceName(step.workspaceId),
      column,
      row: place(column),
    };
    nodes.push(node);
    edges.push({ from: source, to: node });
    if (step.emitEventId && eventNodes[step.emitEventId]) {
      edges.push({ from: node, to: eventNodes[step.emitEventId] });
    }
  }

  for (const node of nodes) {
    const override = positions.value[nodeKey(node)];
    node.x = override?.x ?? CANVAS_PADDING + node.column * COLUMN_WIDTH;
    node.y = override?.y ?? CANVAS_PADDING + node.row * ROW_HEIGHT;
  }

  const width = Math.max(...nodes.map(n => n.x + NODE_WIDTH), 0) + CANVAS_PADDING;
  const height = Math.max(...nodes.map(n => n.y + NODE_HEIGHT), 0) + CANVAS_PADDING;

  return { nodes, edges, width, height };
});

// Orphans are steps whose source event no longer sits on any path from the
// start event — usually because the start event changed. They would silently
// never fire, so they get called out rather than hidden.
const orphanSteps = computed(() => {
  const drawn = new Set(graph.value.nodes.filter(n => n.kind === 'step').map(n => n.stepId));
  return steps.value.filter(s => !drawn.has(s.id));
});

// Dead ends: an event that nothing consumes. Legal, but usually a mistake, so
// it is surfaced as a warning rather than an error.
const deadEndEvents = computed(() => {
  const consumed = new Set(steps.value.map(s => s.eventId));
  const emitted = steps.value.filter(s => s.emitEventId).map(s => s.emitEventId);
  return [...new Set(emitted.filter(id => !consumed.has(id)))];
});

// ── Cycle prevention ──────────────────────────────────────────────────────────

/** Whether `fromEventId -> emitEventId` would close a loop in the current graph. */
function wouldCreateCycle(fromEventId, emitEventId) {
  if (!emitEventId) return false;
  if (emitEventId === fromEventId) return true;
  const adjacency = {};
  for (const s of steps.value) {
    if (s.emitEventId) (adjacency[s.eventId] ||= []).push(s.emitEventId);
  }
  const seen = new Set();
  const stack = [emitEventId];
  while (stack.length) {
    const current = stack.pop();
    if (current === fromEventId) return true;
    if (seen.has(current)) continue;
    seen.add(current);
    stack.push(...(adjacency[current] ?? []));
  }
  return false;
}

/** Events a step may legally emit: everything that would not close a loop. */
function legalEmitEvents(step) {
  return events.value.filter(ev => !wouldCreateCycle(step.eventId, ev.id));
}

// ── Drag and drop ─────────────────────────────────────────────────────────────
// Palette items carry their kind, and a node only accepts the kind the
// alternation allows — a workspace onto an event, an event onto a workspace.
// Illegal targets never highlight, so a mistake is not expressible.

const dragItem = ref(null);
const dropTarget = ref(null);

function startPaletteDrag(kind, id, e) {
  dragItem.value = { kind, id };
  try { e.dataTransfer.setData('text/plain', `${kind}:${id}`); } catch { /* Firefox */ }
  e.dataTransfer.effectAllowed = 'copy';
}

function canDropOn(node) {
  if (!dragItem.value) return false;
  if (node.kind === 'event') return dragItem.value.kind === 'workspace';
  if (node.kind === 'step') {
    if (dragItem.value.kind !== 'event') return false;
    if (node.step.emitEventId) return false; // a step emits at most one event
    return !wouldCreateCycle(node.step.eventId, dragItem.value.id);
  }
  return false;
}

function onNodeDragOver(node, e) {
  if (!canDropOn(node)) return;
  e.preventDefault();
  e.dataTransfer.dropEffect = 'copy';
  dropTarget.value = nodeKey(node);
}

function onNodeDragLeave(node) {
  if (dropTarget.value === nodeKey(node)) dropTarget.value = null;
}

async function onNodeDrop(node, e) {
  e.preventDefault();
  const item = dragItem.value;
  dropTarget.value = null;
  dragItem.value = null;
  if (!item || !canDropOn(node)) return;

  if (node.kind === 'event' && item.kind === 'workspace') {
    await addStep(node.eventId, item.id);
  } else if (node.kind === 'step' && item.kind === 'event') {
    await setStepEmit(node.step, item.id);
  }
}

// ── Node repositioning ────────────────────────────────────────────────────────

const draggingNode = ref(null);

function startNodeDrag(node, e) {
  // Left button only, and never from the palette-drop path.
  if (e.button !== 0) return;
  draggingNode.value = {
    key: nodeKey(node),
    offsetX: e.clientX - node.x,
    offsetY: e.clientY - node.y,
  };
  window.addEventListener('pointermove', onNodeDragMove);
  window.addEventListener('pointerup', endNodeDrag, { once: true });
}

function onNodeDragMove(e) {
  const drag = draggingNode.value;
  if (!drag) return;
  positions.value = {
    ...positions.value,
    [drag.key]: {
      x: Math.max(0, Math.round(e.clientX - drag.offsetX)),
      y: Math.max(0, Math.round(e.clientY - drag.offsetY)),
    },
  };
}

async function endNodeDrag() {
  window.removeEventListener('pointermove', onNodeDragMove);
  if (!draggingNode.value) return;
  draggingNode.value = null;
  await persistLayout();
}

async function persistLayout() {
  try {
    await updateWorkflow(workflowId, { layout: JSON.stringify(positions.value) });
  } catch {
    // Layout is presentational: a failed save costs the arrangement, not the
    // graph, so it is not worth interrupting the user with a toast.
  }
}

async function resetLayout() {
  positions.value = {};
  await persistLayout();
  notifySuccess('Layout reset');
}

// ── Mutations ─────────────────────────────────────────────────────────────────

async function addStep(eventId, workspaceId) {
  try {
    const data = await createWorkflowStep(workflowId, {
      eventId,
      workspaceId,
      title: `${workflow.value.name}: ${eventName(eventId)}`,
      body: '{{EVENT_PAYLOAD}}',
    });
    steps.value = [...steps.value, data.workflowStep];
  } catch (e) {
    notifyError(e.message);
  }
}

// A step's emitted event is part of the row, and there is no PATCH for a step,
// so changing it is a delete plus a create. Ordering matters: create first
// would momentarily double the fan-out for that event.
async function setStepEmit(step, emitEventId) {
  try {
    await deleteWorkflowStep(workflowId, step.id);
    const data = await createWorkflowStep(workflowId, {
      eventId: step.eventId,
      workspaceId: step.workspaceId,
      emitEventId,
      title: step.title,
      body: step.body,
      assignee: step.assignee,
      allowAllCommands: step.allowAllCommands,
    });
    steps.value = [...steps.value.filter(s => s.id !== step.id), data.workflowStep];
  } catch (e) {
    notifyError(e.message);
    await loadSteps();
  }
}

async function removeStep(step) {
  try {
    await deleteWorkflowStep(workflowId, step.id);
    steps.value = steps.value.filter(s => s.id !== step.id);
  } catch (e) {
    notifyError(e.message);
  }
}

async function clearStepEmit(step) {
  await setStepEmit(step, '');
}

async function setStartEvent(eventId) {
  try {
    const data = await updateWorkflow(workflowId, { startEventId: eventId });
    workflow.value = data.workflow;
  } catch (e) {
    notifyError(e.message);
  }
}

// ── Text mode ─────────────────────────────────────────────────────────────────

const text = ref('');
const textDirty = ref(false);
const textError = ref(null);
const savingText = ref(false);

async function loadText() {
  try {
    const data = await fetchWorkflowText(workflowId);
    text.value = data.text ?? '';
    textDirty.value = false;
    textError.value = null;
  } catch (e) {
    notifyError(e.message);
  }
}

async function switchMode(next) {
  if (next === mode.value) return;
  if (next === 'text') await loadText();
  if (next === 'graph' && textDirty.value) {
    notifyError('Save or discard your text changes first');
    return;
  }
  mode.value = next;
}

async function saveText() {
  savingText.value = true;
  textError.value = null;
  try {
    await replaceWorkflowFromText(workflowId, text.value);
    notifySuccess('Workflow saved');
    textDirty.value = false;
    await Promise.all([loadWorkflow(), loadSteps()]);
  } catch (e) {
    textError.value = { message: e.message, line: e.line ?? null };
  } finally {
    savingText.value = false;
  }
}

async function discardText() {
  await loadText();
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

const TASKS_PAGE_SIZE = 10;
const tasks = ref([]);
const loadingTasks = ref(false);
const visibleTaskCount = ref(TASKS_PAGE_SIZE);
const visibleTasks = computed(() => tasks.value.slice(0, visibleTaskCount.value));
const hasMoreTasks = computed(() => visibleTaskCount.value < tasks.value.length);

function loadMoreTasks() {
  visibleTaskCount.value += TASKS_PAGE_SIZE;
}

async function loadTasks() {
  loadingTasks.value = true;
  try {
    const data = await fetchWorkflowTasks(workflowId);
    tasks.value = data.tasks ?? [];
    visibleTaskCount.value = TASKS_PAGE_SIZE;
  } catch (e) {
    notifyError(e.message);
  } finally {
    loadingTasks.value = false;
  }
}

function statusColor(status) {
  return {
    notstarted: 'text-gray-400 dark:text-zinc-500',
    ongoing: 'text-sky-500 dark:text-sky-400',
    completed: 'text-emerald-500 dark:text-emerald-400',
    rejected: 'text-red-500 dark:text-red-400',
    blocked: 'text-amber-500 dark:text-amber-400',
    cron: 'text-violet-500 dark:text-violet-400',
  }[status] ?? 'text-gray-400 dark:text-zinc-500';
}

// ── Loading ───────────────────────────────────────────────────────────────────

async function loadWorkflow() {
  const data = await getWorkflow(workflowId);
  workflow.value = data.workflow;
  if (data.workflow.layout) {
    // Layout is opaque to the backend, so a malformed blob is possible; an
    // unreadable arrangement should fall back to auto-layout, not break the view.
    try {
      positions.value = typeof data.workflow.layout === 'string'
        ? JSON.parse(data.workflow.layout)
        : data.workflow.layout;
    } catch {
      positions.value = {};
    }
  }
}

async function loadSteps() {
  const data = await fetchWorkflowSteps(workflowId);
  steps.value = data.workflowSteps ?? [];
}

const showDeleteStepModal = ref(false);
const deletingStep = ref(null);

function confirmDeleteStep(step) {
  deletingStep.value = step;
  showDeleteStepModal.value = true;
}

async function handleDeleteStep() {
  if (deletingStep.value) await removeStep(deletingStep.value);
  showDeleteStepModal.value = false;
  deletingStep.value = null;
}

onMounted(async () => {
  loading.value = true;
  try {
    if (!workspaceStore.workspaces.length) await workspaceStore.fetchWorkspaces();
    const [, , eventsRes] = await Promise.all([
      loadWorkflow(),
      loadSteps(),
      fetchEvents().catch(() => ({ events: [] })),
    ]);
    events.value = eventsRes.events ?? [];
    await loadTasks();
  } catch (e) {
    notifyError(e.message);
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <!-- Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4">
      <div class="min-w-0">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate">
          <span class="opacity-50 cursor-pointer hover:opacity-100 transition-opacity" @click="router.push('/workflows')">Workflows</span>
          <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium">/</span>
          <span class="font-mono">{{ workflow?.name }}</span>
        </h1>
        <p v-if="workflow?.description" class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">{{ workflow.description }}</p>
      </div>

      <div class="flex items-center gap-2 shrink-0">
        <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-lg w-fit">
          <button
            @click="switchMode('graph')"
            class="px-5 py-1.5 rounded-md text-[10px] font-semibold transition-all"
            :class="mode === 'graph' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'">
            Graph
          </button>
          <button
            @click="switchMode('text')"
            class="px-5 py-1.5 rounded-md text-[10px] font-semibold transition-all"
            :class="mode === 'text' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'">
            Text
          </button>
        </div>
      </div>
    </div>

    <LoadingState v-if="loading" label="Loading workflow…" class="py-20" />

    <div v-else class="px-4 space-y-8 pb-10">
      <!-- No start event yet -->
      <div v-if="!workflow?.startEventId" class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5 space-y-3">
        <div>
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Pick a start event</h2>
          <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-0.5">
            A run begins when this event fires. Everything else hangs off it.
          </p>
        </div>
        <select
          @change="setStartEvent($event.target.value)"
          class="w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all">
          <option value="">Choose an event…</option>
          <option v-for="ev in events" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
        </select>
      </div>

      <!-- ── Graph mode ── -->
      <template v-else-if="mode === 'graph'">
        <div class="flex gap-4 items-start">
          <!-- Palette -->
          <div class="shrink-0 w-48 space-y-4">
            <div>
              <h3 class="text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-2">Workspaces</h3>
              <p class="text-[10px] text-gray-400 dark:text-zinc-500 mb-2 leading-snug">Drag onto an event to subscribe it</p>
              <div class="space-y-1 max-h-52 overflow-y-auto custom-scrollbar pr-1">
                <div
                  v-for="ws in workspaceStore.workspaces"
                  :key="ws.id"
                  draggable="true"
                  @dragstart="startPaletteDrag('workspace', ws.id, $event)"
                  @dragend="dragItem = null; dropTarget = null"
                  class="px-2.5 py-1.5 text-[11px] font-semibold text-gray-700 dark:text-zinc-300 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-lg cursor-grab active:cursor-grabbing hover:border-gray-300 dark:hover:border-zinc-600 transition-all truncate">
                  {{ ws.name }}
                </div>
              </div>
            </div>

            <div>
              <h3 class="text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-2">Events</h3>
              <p class="text-[10px] text-gray-400 dark:text-zinc-500 mb-2 leading-snug">Drag onto a workspace to emit it on completion</p>
              <div class="space-y-1 max-h-52 overflow-y-auto custom-scrollbar pr-1">
                <div
                  v-for="ev in events"
                  :key="ev.id"
                  draggable="true"
                  @dragstart="startPaletteDrag('event', ev.id, $event)"
                  @dragend="dragItem = null; dropTarget = null"
                  class="px-2.5 py-1.5 text-[11px] font-mono font-semibold text-violet-700 dark:text-violet-400 bg-violet-50 dark:bg-violet-900/20 border border-violet-100 dark:border-violet-900/40 rounded-lg cursor-grab active:cursor-grabbing hover:border-violet-300 dark:hover:border-violet-700 transition-all truncate">
                  {{ ev.name }}
                </div>
              </div>
            </div>

            <button
              @click="resetLayout"
              class="w-full px-3 py-1.5 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[10px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95">
              Reset Layout
            </button>
          </div>

          <!-- Canvas -->
          <div class="grow min-w-0 overflow-auto custom-scrollbar border border-gray-200 dark:border-zinc-800 rounded-xl bg-gray-50/50 dark:bg-zinc-950/50">
            <div class="relative" :style="{ width: graph.width + 'px', height: Math.max(graph.height, 240) + 'px' }">
              <!-- Edges. currentColor + a text class is how the rest of the app
                   gets theme-aware SVG. -->
              <svg class="absolute inset-0 pointer-events-none text-gray-300 dark:text-zinc-700" :width="graph.width" :height="Math.max(graph.height, 240)">
                <defs>
                  <marker id="wf-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse">
                    <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
                  </marker>
                </defs>
                <path
                  v-for="(edge, i) in graph.edges"
                  :key="i"
                  :d="`M ${edge.from.x + 200} ${edge.from.y + 28} C ${edge.from.x + 230} ${edge.from.y + 28}, ${edge.to.x - 30} ${edge.to.y + 28}, ${edge.to.x} ${edge.to.y + 28}`"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  marker-end="url(#wf-arrow)" />
              </svg>

              <!-- Nodes -->
              <div
                v-for="node in graph.nodes"
                :key="nodeKey(node)"
                class="absolute select-none rounded-xl border transition-shadow"
                :style="{ left: node.x + 'px', top: node.y + 'px', width: '200px' }"
                :class="[
                  node.kind === 'event'
                    ? 'bg-violet-50 dark:bg-violet-900/20 border-violet-200 dark:border-violet-900/50'
                    : 'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-800',
                  dropTarget === nodeKey(node) ? 'ring-2 ring-black dark:ring-white shadow-lg' : '',
                  dragItem && !canDropOn(node) ? 'opacity-40' : '',
                ]"
                @dragover="onNodeDragOver(node, $event)"
                @dragleave="onNodeDragLeave(node)"
                @drop="onNodeDrop(node, $event)">
                <div class="flex items-center gap-2 px-3 py-2 cursor-grab active:cursor-grabbing" @pointerdown="startNodeDrag(node, $event)">
                  <span
                    class="shrink-0 text-[9px] font-black uppercase tracking-wider px-1.5 py-0.5 rounded"
                    :class="node.kind === 'event'
                      ? 'text-violet-700 dark:text-violet-300 bg-violet-100 dark:bg-violet-900/40'
                      : 'text-gray-600 dark:text-zinc-300 bg-gray-100 dark:bg-zinc-800'">
                    {{ node.kind === 'event' ? 'event' : 'agent' }}
                  </span>
                  <span class="min-w-0 grow truncate text-[11px] font-mono font-semibold text-gray-900 dark:text-zinc-100">{{ node.label }}</span>
                  <span v-if="node.isStart" class="shrink-0 text-[9px] font-bold uppercase text-emerald-600 dark:text-emerald-400">start</span>
                  <button
                    v-if="node.kind === 'step'"
                    @click.stop="confirmDeleteStep(node.step)"
                    class="shrink-0 p-0.5 text-gray-300 dark:text-zinc-600 hover:text-red-500 dark:hover:text-red-400 rounded transition-all">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
                <div v-if="node.kind === 'step' && node.step.emitEventId" class="px-3 pb-1.5 -mt-1">
                  <button
                    @click.stop="clearStepEmit(node.step)"
                    class="text-[9px] text-gray-400 dark:text-zinc-500 hover:text-red-500 dark:hover:text-red-400 transition-colors">
                    emits {{ eventName(node.step.emitEventId) }} · clear
                  </button>
                </div>
              </div>

              <div v-if="graph.nodes.length <= 1" class="absolute inset-0 flex items-center justify-center pointer-events-none">
                <p class="text-xs text-gray-400 dark:text-zinc-500">Drag a workspace onto the start event to begin</p>
              </div>
            </div>
          </div>
        </div>

        <!-- Warnings -->
        <div v-if="orphanSteps.length || deadEndEvents.length" class="space-y-2">
          <div v-if="orphanSteps.length" class="px-4 py-3 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-xl">
            <p class="text-[11px] font-bold text-amber-700 dark:text-amber-400 uppercase tracking-widest mb-1">Unreachable steps</p>
            <p class="text-xs text-amber-700/80 dark:text-amber-400/80">
              {{ orphanSteps.length }} step(s) subscribe to an event no run can reach from the start event, so they will never fire.
            </p>
            <div class="mt-2 space-y-1">
              <div v-for="s in orphanSteps" :key="s.id" class="flex items-center gap-2 text-[11px] font-mono text-amber-800 dark:text-amber-300">
                <span>{{ eventName(s.eventId) }} → {{ workspaceName(s.workspaceId) }}</span>
                <button @click="removeStep(s)" class="underline hover:no-underline">remove</button>
              </div>
            </div>
          </div>

          <div v-if="deadEndEvents.length" class="px-4 py-3 bg-gray-50 dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl">
            <p class="text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-1">Dead ends</p>
            <p class="text-xs text-gray-500 dark:text-zinc-400">
              Nothing in this workflow consumes
              <span class="font-mono">{{ deadEndEvents.map(eventName).join(', ') }}</span>.
              That is fine if the run should stop there.
            </p>
          </div>
        </div>
      </template>

      <!-- ── Text mode ── -->
      <template v-else>
        <div class="space-y-3">
          <div class="flex items-start justify-between gap-4">
            <p class="text-[11px] text-gray-400 dark:text-zinc-500 leading-snug">
              Two spaces per level. Every item is <span class="font-mono">- agent:name</span> or <span class="font-mono">- event:name</span>,
              alternating: an event is consumed by agents, and each agent may emit one event.
            </p>
            <div class="flex items-center gap-2 shrink-0">
              <button
                @click="discardText"
                :disabled="!textDirty || savingText"
                class="px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 disabled:opacity-50">
                Discard
              </button>
              <button
                @click="saveText"
                :disabled="savingText"
                class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
                {{ savingText ? 'Saving…' : 'Save' }}
              </button>
            </div>
          </div>

          <textarea
            v-model="text"
            @input="textDirty = true; textError = null"
            spellcheck="false"
            rows="18"
            class="w-full px-3 py-2 text-[12px] font-mono leading-relaxed border rounded-lg bg-white dark:bg-zinc-900 text-gray-900 dark:text-zinc-100 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all resize-y"
            :class="textError ? 'border-red-400' : 'border-gray-200 dark:border-zinc-700'"></textarea>

          <div v-if="textError" class="px-3 py-2 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 rounded-lg">
            <p class="text-[11px] text-red-600 dark:text-red-400">
              <span v-if="textError.line" class="font-bold">Line {{ textError.line }}: </span>{{ textError.message }}
            </p>
          </div>
        </div>
      </template>

      <!-- Resulting tasks -->
      <div class="space-y-2">
        <div>
          <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Tasks from this workflow</h2>
          <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-0.5">Created by runs of this pipeline</p>
        </div>

        <LoadingState v-if="loadingTasks" label="Loading tasks…" class="py-4" />

        <div v-else-if="tasks.length === 0" class="py-8 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-xs text-gray-400 dark:text-zinc-500">No runs yet</p>
        </div>

        <div v-else class="space-y-2">
          <router-link
            v-for="t in visibleTasks"
            :key="t.id"
            :to="`/workspaces/${t.workspaceId}/tasks/${t.id}`"
            class="flex items-center gap-3 px-4 py-2.5 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-all">
            <span class="shrink-0 text-[10px] font-bold uppercase tracking-wider" :class="statusColor(t.status)">{{ t.status }}</span>
            <span class="min-w-0 grow truncate text-xs text-gray-800 dark:text-zinc-200">{{ t.title }}</span>
            <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500 font-mono">{{ workspaceName(t.workspaceId) }}</span>
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

    <DeleteModal
      :show="showDeleteStepModal"
      title="Remove Step"
      :taskTitle="deletingStep ? workspaceName(deletingStep.workspaceId) : ''"
      @close="showDeleteStepModal = false; deletingStep = null"
      @confirm="handleDeleteStep" />
  </div>
</template>
