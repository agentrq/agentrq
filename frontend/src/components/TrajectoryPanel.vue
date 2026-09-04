<template>
  <div class="flex-1 min-h-0 flex flex-col overflow-hidden bg-white dark:bg-zinc-900">

      <!-- Minimap -->
      <div v-if="items.length > 0" class="px-4 py-2 border-b border-gray-100 dark:border-zinc-800 shrink-0">
        <div class="flex gap-2 overflow-x-auto custom-scrollbar pb-1">
          <div class="flex flex-col gap-0.5 shrink-0 pr-2 mr-1 border-r border-gray-100 dark:border-zinc-800">
            <span v-for="lane in LANES" :key="lane.key" class="text-[7px] font-black uppercase tracking-wider text-gray-400 dark:text-zinc-500 h-2 leading-2 flex items-center">{{ lane.label }}</span>
          </div>
          <div class="flex flex-col gap-0.5 shrink-0">
            <div v-for="lane in LANES" :key="lane.key" class="flex gap-[3px] h-2">
              <button v-for="(it, idx) in items" :key="it.id"
                      type="button"
                      @click="select(it.id)"
                      class="w-1.5 h-2 rounded-[1px] shrink-0 transition-transform"
                      :class="[
                        it.lane === lane.key ? laneDotClass(it) : 'bg-gray-100 dark:bg-zinc-800/60',
                        selectedId === it.id ? 'ring-1 ring-offset-1 ring-gray-900 dark:ring-white ring-offset-white dark:ring-offset-zinc-900 scale-125' : ''
                      ]"
                      :title="it.label"
              ></button>
            </div>
          </div>
        </div>
      </div>

      <!-- Categories — the trace answers one question at a time: what was said,
           what the agent thought, what it ran. -->
      <div v-if="items.length > 0" class="px-4 py-2 border-b border-gray-100 dark:border-zinc-800 shrink-0 flex items-center gap-1.5 overflow-x-auto custom-scrollbar">
        <button type="button" @click="laneFilter = null" :class="laneChipClass(null)"
                class="shrink-0 text-[9px] font-black uppercase tracking-wider px-2 py-1 rounded-lg border transition-colors">
          All <span class="font-bold opacity-60">{{ items.length }}</span>
        </button>
        <button v-for="lane in LANES" :key="lane.key" type="button"
                @click="toggleLane(lane.key)"
                :disabled="laneCounts[lane.key] === 0"
                :class="laneChipClass(lane.key)"
                class="shrink-0 text-[9px] font-black uppercase tracking-wider px-2 py-1 rounded-lg border transition-colors disabled:opacity-40 disabled:cursor-not-allowed">
          {{ lane.label }} <span class="font-bold opacity-60">{{ laneCounts[lane.key] }}</span>
        </button>
      </div>

      <!-- Search -->
      <div class="px-4 py-2 border-b border-gray-100 dark:border-zinc-800 shrink-0">
        <div class="relative">
          <svg class="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7"/><path stroke-linecap="round" d="M21 21l-4.35-4.35"/></svg>
          <input v-model="query" type="text" placeholder="Search tool calls and messages..."
                 class="w-full pl-8 pr-3 py-1.5 text-[11px] font-medium bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg text-gray-800 dark:text-zinc-200 placeholder:text-gray-400 dark:placeholder:text-zinc-500 outline-none focus:border-gray-400 dark:focus:border-zinc-500 transition-colors" />
        </div>
      </div>

      <!-- Body -->
      <div class="flex flex-1 min-h-0">
        <!-- List -->
        <div class="w-2/5 border-r border-gray-100 dark:border-zinc-800 overflow-y-auto custom-scrollbar">
          <div v-if="filteredItems.length === 0" class="py-10 text-center text-[11px] text-gray-400 dark:text-zinc-500 font-medium px-4">
            {{ emptyListMessage }}
          </div>
          <button v-for="it in filteredItems" :key="it.id"
                  type="button"
                  @click="select(it.id)"
                  class="w-full text-left flex items-center gap-2 px-3 py-2 border-b border-gray-50 dark:border-zinc-800/60 transition-colors"
                  :class="selectedId === it.id ? 'bg-gray-100 dark:bg-zinc-800' : 'hover:bg-gray-50 dark:hover:bg-zinc-800/50'">
            <span class="text-[7px] font-black uppercase tracking-wider px-1 py-0.5 rounded shrink-0" :class="laneBadgeClass(it)">{{ it.laneLabel }}</span>
            <span class="text-[11px] text-gray-700 dark:text-zinc-300 truncate flex-1 min-w-0 font-mono">{{ it.preview }}</span>
            <span class="text-[9px] text-gray-400 dark:text-zinc-500 shrink-0">{{ formatDateTime(it.createdAt) }}</span>
          </button>
        </div>

        <!-- Detail panel -->
        <div class="w-3/5 overflow-y-auto custom-scrollbar p-4">
          <div v-if="!selectedItem" class="h-full flex items-center justify-center text-[11px] text-gray-400 dark:text-zinc-500 font-medium">
            Select an entry to view details.
          </div>
          <div v-else class="flex flex-col gap-3">
            <div class="flex items-center gap-2">
              <span class="text-[7px] font-black uppercase tracking-wider px-1.5 py-0.5 rounded shrink-0" :class="laneBadgeClass(selectedItem)">{{ selectedItem.laneLabel }}</span>
              <h3 class="text-[13px] font-bold text-gray-800 dark:text-zinc-200 truncate">{{ selectedItem.label }}</h3>
              <span v-if="selectedItem.lane === 'tool'" class="text-[8px] font-black uppercase tracking-wider px-1.5 py-0.5 rounded shrink-0" :class="toolCallStatusStyle(selectedItem.raw.status)">{{ toolCallStatusLabel(selectedItem.raw.status) }}</span>
            </div>

            <!-- Tabs -->
            <div class="flex items-center gap-1 border-b border-gray-100 dark:border-zinc-800">
              <button v-for="tab in detailTabs" :key="tab.key" type="button" @click="activeTab = tab.key"
                      class="px-2.5 py-1.5 text-[9px] font-black uppercase tracking-wider transition-colors border-b-2 -mb-px"
                      :class="activeTab === tab.key ? 'border-gray-900 dark:border-white text-gray-900 dark:text-white' : 'border-transparent text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'">
                {{ tab.label }}
              </button>
            </div>

            <!-- Summary -->
            <div v-if="activeTab === 'summary'" class="flex flex-col gap-2">
              <div v-if="selectedItem.lane === 'tool'" class="grid grid-cols-[80px_1fr] gap-y-1.5 text-[11px]">
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">Tool</span>
                <span class="text-gray-800 dark:text-zinc-200 font-mono break-all">{{ selectedItem.raw.toolName }}</span>
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">Status</span>
                <span class="text-gray-800 dark:text-zinc-200">{{ toolCallStatusLabel(selectedItem.raw.status) }}</span>
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">Created</span>
                <span class="text-gray-800 dark:text-zinc-200">{{ formatAbsolute(selectedItem.createdAt) }}</span>
              </div>
              <div v-else class="grid grid-cols-[80px_1fr] gap-y-1.5 text-[11px]">
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">{{ selectedItem.lane === 'thought' ? 'Kind' : 'Sender' }}</span>
                <span class="text-gray-800 dark:text-zinc-200">{{ selectedItem.label }}</span>
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">Created</span>
                <span class="text-gray-800 dark:text-zinc-200">{{ formatAbsolute(selectedItem.createdAt) }}</span>
              </div>
              <p v-if="selectedItem.raw.description" class="text-[11px] text-gray-600 dark:text-zinc-400 italic border-l-2 border-gray-200 dark:border-zinc-700 pl-2 mt-1">"{{ selectedItem.raw.description }}"</p>
            </div>

            <!-- Payload (tool only) -->
            <div v-else-if="activeTab === 'payload'">
              <pre class="text-[10px] font-mono bg-zinc-950 text-zinc-300 p-3 rounded-lg overflow-x-auto whitespace-pre-wrap break-all custom-scrollbar">{{ formattedPayload }}</pre>
            </div>

            <!-- Content (messages only) -->
            <div v-else-if="activeTab === 'content'" class="bg-gray-50 dark:bg-zinc-800/50 rounded-lg p-3">
              <div class="flex items-center justify-end gap-1 mb-2">
                <button type="button" @click="toggleContentRaw(selectedItem.id)"
                        :class="!rawContent.has(selectedItem.id) ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                        class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">MD</button>
                <button type="button" @click="copyContentText(selectedItem.id, selectedItem.raw.text)"
                        class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded" title="Copy raw text">
                  <svg v-if="!copiedContent.has(selectedItem.id)" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                  <svg v-else class="w-2.5 h-2.5 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                </button>
              </div>
              <div v-if="!rawContent.has(selectedItem.id)" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(selectedItem.raw.text)"></div>
              <div v-else class="text-[13px] font-medium text-gray-800 dark:text-zinc-200 leading-relaxed whitespace-pre-wrap break-all">{{ selectedItem.raw.text || '(empty message)' }}</div>
            </div>
          </div>
        </div>
      </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import { renderMarkdown } from '../utils/markdown';
import {
  TRAJECTORY_LANES,
  buildTrajectory,
  defaultDetailTab,
  filterTrajectory,
  trajectoryLaneCounts,
} from '../composables/useTrajectory';

const props = defineProps({
  messages: { type: Array, default: () => [] },
  toolCalls: { type: Array, default: () => [] },
});

const LANES = TRAJECTORY_LANES;

const items = computed(() => buildTrajectory(props.messages, props.toolCalls));

const laneCounts = computed(() => trajectoryLaneCounts(items.value));

// Which category the reader has narrowed to, or null for all of them. Reading
// a long run means asking one question at a time — what did it think, what did
// it run — and the lanes are what separate those.
const laneFilter = ref(null);
function toggleLane(key) {
  laneFilter.value = laneFilter.value === key ? null : key;
}

function laneChipClass(key) {
  return laneFilter.value === key
    ? 'bg-gray-900 dark:bg-white text-white dark:text-black border-transparent'
    : 'bg-white dark:bg-zinc-900 text-gray-500 dark:text-zinc-400 border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600';
}

const query = ref('');
const filteredItems = computed(() =>
  filterTrajectory(items.value, { lane: laneFilter.value, query: query.value })
);

const emptyListMessage = computed(() => {
  if (items.value.length === 0) return 'No activity recorded yet.';
  if (query.value.trim()) return 'No matches for this search.';
  return 'Nothing in this category.';
});

const selectedId = ref(null);
const selectedItem = computed(() => items.value.find(it => it.id === selectedId.value) || null);

function select(id) {
  selectedId.value = id;
}

watch(items, (list) => {
  if (!selectedId.value && list.length > 0) {
    selectedId.value = list[list.length - 1].id;
  }
}, { immediate: true });

const activeTab = ref('summary');
const detailTabs = computed(() => {
  if (!selectedItem.value) return [];
  return selectedItem.value.lane === 'tool'
    ? [{ key: 'summary', label: 'Summary' }, { key: 'payload', label: 'Payload' }]
    : [{ key: 'summary', label: 'Summary' }, { key: 'content', label: 'Content' }];
});

watch(selectedItem, (item) => {
  activeTab.value = defaultDetailTab(item);
});

const formattedPayload = computed(() => {
  const raw = selectedItem.value?.raw?.inputPreview;
  if (!raw) return '(no payload recorded)';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
});

const rawContent = ref(new Set());
function toggleContentRaw(id) {
  const s = new Set(rawContent.value);
  s.has(id) ? s.delete(id) : s.add(id);
  rawContent.value = s;
}
const copiedContent = ref(new Set());
async function copyContentText(id, text) {
  await navigator.clipboard.writeText(text || '');
  const s = new Set(copiedContent.value);
  s.add(id);
  copiedContent.value = s;
  setTimeout(() => {
    const s2 = new Set(copiedContent.value);
    s2.delete(id);
    copiedContent.value = s2;
  }, 1500);
}

function laneDotClass(it) {
  if (it.lane === 'tool') {
    switch (it.raw.status) {
      case 'denied':
      case 'decline':
      case 'cancel':
        return 'bg-red-500';
      case 'pending': return 'bg-amber-400';
      case 'allowed':
      case 'auto_allowed':
      case 'accept':
      default:
        return 'bg-emerald-500';
    }
  }
  if (it.lane === 'agent') return 'bg-violet-500';
  return it.lane === 'thought' ? 'bg-teal-500' : 'bg-sky-400';
}

function laneBadgeClass(it) {
  if (it.lane === 'tool') return 'text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10';
  if (it.lane === 'agent') return 'text-violet-700 dark:text-violet-400 bg-violet-50 dark:bg-violet-500/10';
  if (it.lane === 'thought') return 'text-teal-700 dark:text-teal-400 bg-teal-50 dark:bg-teal-500/10';
  return 'text-sky-700 dark:text-sky-400 bg-sky-50 dark:bg-sky-500/10';
}

function toolCallStatusStyle(status) {
  switch (status) {
    case 'allowed':
    case 'auto_allowed':
    case 'accept':
      return 'text-gray-600 dark:text-zinc-300 bg-gray-100 dark:bg-zinc-800';
    case 'denied':
    case 'decline':
    case 'cancel':
      return 'text-red-700 dark:text-red-500 bg-red-50 dark:bg-red-500/10';
    case 'pending':
      return 'text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10';
    default:
      return 'text-gray-500 dark:text-zinc-400 bg-gray-100 dark:bg-zinc-800';
  }
}

function toolCallStatusLabel(status) {
  switch (status) {
    case 'allowed': return 'Allowed';
    case 'auto_allowed': return 'Auto-allowed';
    case 'denied': return 'Denied';
    case 'pending': return 'Pending';
    case 'accept': return 'Answered';
    case 'decline': return 'Declined';
    case 'cancel': return 'Cancelled';
    default: return status;
  }
}

function formatDateTime(dateStr) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();

  if (diffMs >= 0 && diffMs < 24 * 60 * 60 * 1000) {
    const diffMin = Math.floor(diffMs / (60 * 1000));
    if (diffMin < 1) return 'JUST NOW';
    if (diffMin < 60) return `${diffMin}M AGO`;
    const diffHours = Math.floor(diffMin / 60);
    return `${diffHours}H AGO`;
  } else if (diffMs < 0 && diffMs > -60000) {
    return 'JUST NOW';
  }

  return date.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatAbsolute(dateStr) {
  if (!dateStr) return '';
  return new Date(dateStr).toLocaleString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit'
  });
}
</script>
