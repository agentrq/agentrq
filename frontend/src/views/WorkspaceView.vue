<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div ref="pageRef" class="h-full flex flex-col w-full">
    <!-- Page Header -->
    <div class="py-2 mb-6 flex flex-col md:flex-row md:items-start justify-between gap-4 px-4">
      <div class="flex-1">
        <div class="flex items-center justify-between md:justify-start gap-4">
          <div class="flex items-center gap-3">
            <h1 class="text-xl md:text-2xl font-black text-gray-800 dark:text-zinc-200 leading-tight">Workspaces</h1>
          </div>
        </div>
      </div>

      <!-- Tabs -->
      <div class="flex items-center gap-1.5 bg-gray-100 dark:bg-zinc-900/90 p-1 border border-gray-200 dark:border-zinc-800 rounded-sm max-w-max shadow-sm shrink-0">
        <button
          v-for="tab in TABS"
          :key="tab.id"
          type="button"
          @click="activeTab = tab.id"
          class="px-3 py-1.5 text-[10px] font-black uppercase tracking-widest rounded-sm transition-all"
          :class="activeTab === tab.id
            ? 'bg-white dark:bg-zinc-800 text-black dark:text-zinc-50 shadow-sm border border-gray-200 dark:border-zinc-700'
            : 'text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-50'"
        >
          {{ tab.label }}
        </button>
      </div>
    </div>

    <!-- Performance: the same analytics as a single workspace, summed across
         every workspace the user owns. -->
    <div v-if="activeTab === 'performance'" class="flex-1 overflow-y-auto px-4 pb-10 custom-scrollbar">
      <AccountStats />
    </div>

    <div v-if="error && activeTab === 'workspaces'" class="bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-5 py-3 rounded-sm text-[10px] font-black shadow-sm">
      {{ error }}
    </div>

    <!-- Workspace list -->
    <div v-if="activeTab === 'workspaces'" class="flex-1 overflow-y-auto px-4 pb-10 custom-scrollbar">
      <div v-if="loadingWorkspaces" class="py-8">
        <LoadingState label="Loading Workspaces..." />
      </div>

      <div v-else class="space-y-12">
        <!-- Active Workspaces -->
        <section v-if="activeWorkspaces.length > 0">
          <div class="mb-6 flex flex-col md:flex-row md:items-center justify-between gap-4">
            <div class="flex items-center gap-3">
              <span class="text-[10px] font-black text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Active workspaces</span>
              <div class="h-px bg-gray-200 dark:bg-zinc-800 w-8"></div>
            </div>
            
            <div class="flex items-center gap-2 flex-1 md:max-w-2xl">
              <!-- Inline Search -->
              <div class="relative flex-1">
                <svg class="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
                <input v-model="searchQuery"
                       type="text"
                       placeholder="Search workspaces..."
                       class="w-full pl-9 pr-4 py-2 bg-gray-50 dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm text-[11px] outline-none font-bold text-gray-800 dark:text-zinc-200 placeholder:text-gray-500 focus:border-gray-900 dark:focus:border-white transition-all" />
              </div>
              
              <!-- Archived Toggle -->
              <!-- Icon-only on a phone; the label is in the tooltip. -->
              <button @click="showArchived = !showArchived"
                      :title="showArchived ? 'Hide archived' : 'Archived'" :aria-label="showArchived ? 'Hide archived workspaces' : 'Show archived workspaces'"
                      class="px-3 py-2 rounded-sm border transition-all flex items-center gap-2 shrink-0"
                      :class="showArchived ? 'bg-gray-100 dark:bg-zinc-800 border-gray-300 dark:border-zinc-700 text-black dark:text-white' : 'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 hover:border-gray-400'">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
                <span class="text-[9px] font-black uppercase tracking-wider hidden sm:inline">{{ showArchived ? 'Hide' : 'Archived' }}</span>
              </button>

              <!-- Inline New Button -->
              <button @click="newWorkspace" title="New workspace" aria-label="New workspace" class="bg-black dark:bg-white text-white dark:text-black px-3 sm:px-4 py-2 rounded-sm text-[10px] font-black hover:opacity-80 transition-all flex items-center gap-2 shrink-0 shadow-sm border border-transparent">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
                <span class="hidden sm:inline">New</span>
              </button>
            </div>
          </div>
          <!-- Two columns where there is room, each workspace with its forks
               into whichever is shorter (splitIntoColumns). Measured on the
               list rather than the window, so beside the desktop side panel
               it is one column like a phone's. -->
          <div class="grid gap-4 items-start" :class="singleColumn ? 'grid-cols-1' : 'grid-cols-2'">
            <div v-for="(column, ci) in columns" :key="ci" class="space-y-4 min-w-0" data-test="workspace-column">
              <div v-for="g in column" :key="g.workspace.id" data-test="workspace-group">
                <WorkspaceOverviewCard :workspace="g.workspace" :overview="overview.get(g.workspace.id)"
                                       :dimmed="g.dimmed" :narrow="narrow"
                                       @open="goToWorkspace(g.workspace.id)" @open-task="openTask" />
                <!-- A fork hangs under its parent on a rail, as the fork
                     animation draws it: each elbow reaches up to the card
                     above, and every fork but the last carries it on. -->
                <div v-if="g.forks.length" class="pt-3 space-y-3" :class="narrow ? 'ml-2.5 pl-3.5' : 'ml-[18px] pl-[18px]'">
                  <div v-for="(f, fi) in g.forks" :key="f.id" class="relative" data-test="fork-card">
                    <span class="absolute -top-3 h-9 border-l-2 border-b-2 rounded-bl-sm border-gray-300 dark:border-zinc-700"
                          :class="narrow ? '-left-3.5 w-3.5' : '-left-[18px] w-[18px]'"></span>
                    <span v-if="fi < g.forks.length - 1" class="absolute top-6 -bottom-3 border-l-2 border-gray-300 dark:border-zinc-700"
                          :class="narrow ? '-left-3.5' : '-left-[18px]'"></span>
                    <WorkspaceOverviewCard :workspace="f" :overview="overview.get(f.id)" :narrow="narrow"
                                           @open="goToWorkspace(f.id)" @open-task="openTask" />
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- Empty State for Search -->
        <div v-if="cardGroups.length === 0 && searchQuery && !loadingWorkspaces" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm bg-gray-50 dark:bg-zinc-900/50">
          <svg class="mx-auto h-12 w-12 text-gray-300 dark:text-zinc-600 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">No matches for "{{ searchQuery }}"</p>
          <button @click="searchQuery = ''" class="mt-4 text-xs font-semibold text-gray-600 dark:text-zinc-300 border border-gray-300 dark:border-zinc-600 rounded-sm px-5 py-2 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors shadow-sm">Clear Search</button>
        </div>

        <!-- No Workspaces State -->
        <div v-if="activeWorkspaces.length === 0 && !loadingWorkspaces && !searchQuery" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 rounded-sm">
          <div class="w-16 h-16 bg-gray-50 dark:bg-zinc-800 rounded-sm mx-auto flex items-center justify-center mb-5 border border-gray-100 dark:border-zinc-700">
            <svg class="h-8 w-8 text-gray-300 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"></path></svg>
          </div>
          <p class="text-sm font-bold text-gray-700 dark:text-zinc-100">No active workspaces</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-2 font-bold">Build your first AgentRQ pipeline today.</p>
          <button @click="newWorkspace" class="mt-6 px-6 py-2.5 bg-black dark:bg-white text-white dark:text-zinc-900 rounded-sm border border-transparent text-xs font-semibold transition-all shadow-sm inline-flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
            New Workspace
          </button>
        </div>

        <!-- Archived Workspaces -->
        <section v-if="showArchived" class="border-t border-gray-200 dark:border-zinc-800 pt-8 mt-8">
          <div class="flex items-center gap-4 mb-8">
            <span class="text-[10px] font-black text-gray-500 dark:text-zinc-500">Archived Workspaces</span>
            <div class="flex-1 h-px bg-gray-200 dark:bg-zinc-800"></div>
          </div>

          <div v-if="filteredArchivedWorkspaces.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div v-for="p in filteredArchivedWorkspaces"
                 :key="p.id"
                 class="group relative border border-gray-200 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-900/50 rounded-sm p-4 opacity-60 hover:opacity-100 hover:border-gray-300 dark:hover:border-zinc-700 transition-all cursor-pointer shadow-sm hover:shadow-md"
                 @click="goToWorkspace(p.id)">
              
              <div class="flex items-center justify-between gap-3">
                <div class="flex items-center gap-3 min-w-0">
                  <div class="w-1.5 h-1.5 rounded-full bg-gray-300 dark:bg-zinc-600 shrink-0"></div>
                  <div class="min-w-0">
                    <div class="font-black text-sm text-gray-500 dark:text-zinc-400 group-hover:text-gray-900 dark:group-hover:text-zinc-100 transition-colors truncate leading-none">{{ toKebabCase(p.name) }}</div>
                    <div class="text-[8px] font-black text-amber-600/70 truncate mt-1 uppercase tracking-widest">Archived {{ new Date(p.archivedAt).toLocaleDateString() }}</div>
                  </div>
                </div>

                <div class="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button @click.stop="toggleArchive(p)" class="text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-white dark:hover:bg-zinc-800 transition-all p-1.5 rounded-sm border border-transparent shadow-sm hover:border-gray-200 dark:hover:border-zinc-700" title="Restore Workspace">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M3 10h10a5 5 0 015 5v2M3 10l5-5M3 10l5 5"/></svg>
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div v-else class="py-12 text-center text-[10px] font-black text-gray-500 dark:text-zinc-600 border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm bg-gray-50 dark:bg-zinc-900/50">
            No archived workspaces found
          </div>
        </section>
      </div>
    </div>

    <!-- Bottom Stats -->
    <div v-if="activeTab === 'workspaces' && !loadingWorkspaces && workspaces.length > 0" class="px-4 pb-4 flex flex-wrap items-center gap-6 border-t border-gray-100 dark:border-zinc-800 pt-3">
      <div class="flex items-center gap-1.5 cursor-help" 
           @mouseenter="tooltipStore.show($event, 'Online Agents', 'top')"
           @mouseleave="tooltipStore.hide()">
        <svg class="w-4 h-4" :class="activeWorkspaces.filter(w => w.agentConnected).length > 0 ? 'text-green-600 dark:text-green-500' : 'text-gray-400 dark:text-zinc-500'" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 8V4H8"></path>
          <rect width="16" height="12" x="4" y="8" rx="2"></rect>
          <path d="M2 14h2"></path>
          <path d="M20 14h2"></path>
          <path d="M15 13v2"></path>
          <path d="M9 13v2"></path>
        </svg>
        <span class="text-sm font-black" :class="activeWorkspaces.filter(w => w.agentConnected).length > 0 ? 'text-green-600 dark:text-green-500' : 'text-gray-400 dark:text-zinc-500'">{{ activeWorkspaces.filter(w => w.agentConnected).length }}</span>
      </div>
      <div class="flex items-center gap-1.5 cursor-help" 
           @mouseenter="tooltipStore.show($event, 'Pending Tasks', 'top')"
           @mouseleave="tooltipStore.hide()">
        <svg class="w-4 h-4 text-amber-500 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span class="text-sm font-black text-amber-500 dark:text-amber-400">{{ globalStats.pendingTasks }}</span>
      </div>
      <div class="flex items-center gap-1.5 cursor-help" 
           @mouseenter="tooltipStore.show($event, 'Scheduled Tasks', 'top')"
           @mouseleave="tooltipStore.hide()">
        <svg class="w-4 h-4 text-sky-500 dark:text-sky-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span class="text-sm font-black text-sky-500 dark:text-sky-400">{{ globalStats.scheduledTasks }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { useRouter } from 'vue-router';
import { unarchiveWorkspace, fetchGlobalTaskStats, fetchTaskOverview, recordTelemetry, TELEMETRY_UI_OVERVIEW_TASK_OPEN } from '../api';
import { useToasts } from '../composables/useToasts';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useFormat } from '../composables/useFormat';
import { useTooltipStore } from '../stores/tooltipStore';
import { useEventBus } from '../useEventBus';
import { useNarrowLayout } from '../composables/useNarrowLayout';
import { coalesce, filterWorkspaceGroups, groupWorkspaces, indexOverview, splitIntoColumns, workspaceMatches } from '../composables/useWorkspaceOverview';
import WorkspaceOverviewCard from '../components/WorkspaceOverviewCard.vue';
import LoadingState from '../components/LoadingState.vue';
import AccountStats from '../components/AccountStats.vue';

/**
 * The overview screen's tabs. "Workspaces" is the list this page has always
 * been; "Performance" is the account-wide analytics.
 *
 * The tab is local state rather than a route: it is a view of the same
 * overview page, and giving it a URL would mean a second route table entry for
 * something the desktop build would have to mirror.
 */
const TABS = Object.freeze([
  { id: 'workspaces', label: 'Workspaces' },
  { id: 'performance', label: 'Performance' },
]);
const activeTab = ref('workspaces');

const { toKebabCase } = useFormat();
const tooltipStore = useTooltipStore();

const router = useRouter();
const { notifySuccess, notifyError } = useToasts();

const workspaceStore = useWorkspaceStore();
const workspaces = computed(() => workspaceStore.workspaces);
const showArchived = ref(false);
const searchQuery = ref('');
const loadingWorkspaces = ref(true);
const error = ref(null);

const globalStats = ref({
  totalTasks: 0,
  pendingTasks: 0,
  completedTasks: 0,
  scheduledTasks: 0
});

const activeWorkspaces = computed(() => {
  return workspaces.value.filter(p => !p.archivedAt);
});

const archivedWorkspaces = computed(() => {
  return workspaces.value.filter(p => !!p.archivedAt);
});

// A card per workspace, its forks under it; a search keeps a parent, dimmed,
// for the forks that match.
const cardGroups = computed(() => filterWorkspaceGroups(groupWorkspaces(activeWorkspaces.value), searchQuery.value));

const filteredArchivedWorkspaces = computed(() => {
  if (!searchQuery.value) return archivedWorkspaces.value;
  return archivedWorkspaces.value.filter(p => workspaceMatches(p, searchQuery.value));
});

// Laid out by the page's own width: two columns where two cards fit, one
// beside the desktop side panel or on a phone, and a phone's card shows less.
// Measured on the page rather than the list, which is not there until the
// workspaces load, after the measuring has started.
const pageRef = ref(null);
const singleColumn = useNarrowLayout(pageRef, { below: 900 });
const narrow = useNarrowLayout(pageRef);
const columns = computed(() => splitIntoColumns(cardGroups.value, singleColumn.value ? 1 : 2));

// Every workspace's counts and first few tasks, by workspace id.
const overview = ref(new Map());
const loadOverview = coalesce(async () => {
  overview.value = indexOverview(await fetchTaskOverview());
});

async function loadWorkspaces() {
  try {
    await workspaceStore.fetchWorkspaces();
    // Also load some global stats
    const stats = await fetchGlobalTaskStats();
    globalStats.value = {
      totalTasks: 0,
      completedTasks: 0,
      pendingTasks: stats.pendingTasks || 0,
      scheduledTasks: stats.scheduledTasks || 0
    };
  } catch (err) {
    error.value = err.message;
  } finally {
    loadingWorkspaces.value = false;
  }
}

// Creating one is its own page, as creating a task is.
function newWorkspace() {
  router.push('/workspaces/new');
}

async function toggleArchive(p) {
  try {
    if (p.archivedAt) {
      await unarchiveWorkspace(p.id);
      notifySuccess('Workspace protocol restored');
    }
    await loadWorkspaces();
  } catch (err) {
    notifyError(err.message, 'Operation Failed');
  }
}

function goToWorkspace(id) {
  router.push(`/workspaces/${id}`);
}

function openTask(task) {
  recordTelemetry(TELEMETRY_UI_OVERVIEW_TASK_OPEN, task.workspaceId);
  router.push(`/workspaces/${task.workspaceId}/tasks/${task.id}`);
}

// The workspaces themselves need no stream here: the shell already writes
// agent connection state and workspace metadata into the store this view
// renders. The tasks on the cards are not in any store, so this view follows
// the user's stream for them, as the task list does, and fetches the overview
// again on any task change; a burst of changes costs two fetches (coalesce).
const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false });
onEvent((event) => {
  if (event.type === 'task.created' || event.type === 'task.updated' || event.type === 'task.deleted') {
    loadOverview();
  }
});

onMounted(async () => {
  connect();
  loadOverview();
  await loadWorkspaces();
});

onUnmounted(() => disconnect());
</script>
