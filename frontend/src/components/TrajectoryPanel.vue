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
            {{ items.length === 0 ? 'No activity recorded yet.' : 'No matches for this search.' }}
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
                <span class="text-gray-400 dark:text-zinc-500 font-semibold">Sender</span>
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
            <div v-else-if="activeTab === 'content'" class="text-[13px] font-medium text-gray-800 dark:text-zinc-200 leading-relaxed whitespace-pre-wrap break-all bg-gray-50 dark:bg-zinc-800/50 rounded-lg p-3">
              {{ selectedItem.raw.text || '(empty message)' }}
            </div>
          </div>
        </div>
      </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue';

const props = defineProps({
  messages: { type: Array, default: () => [] },
  toolCalls: { type: Array, default: () => [] },
});

const LANES = [
  { key: 'input', label: 'INPUT' },
  { key: 'agent', label: 'AGENT' },
  { key: 'tool', label: 'TOOLS' },
];

function truncate(text, len) {
  if (!text) return '';
  const t = String(text).trim().replace(/\s+/g, ' ');
  return t.length > len ? t.slice(0, len) + '…' : t;
}

function permissionStatus(status) {
  switch (status) {
    case 'allow': return 'allowed';
    case 'allow_always': return 'auto_allowed';
    case 'deny': return 'denied';
    default: return status || 'pending';
  }
}

const items = computed(() => {
  const fromMessages = (props.messages || []).map(m => {
    const isPermissionRequest = m.metadata?.type === 'permission_request';
    if (isPermissionRequest) {
      const toolName = m.metadata?.toolName || 'Permission Request';
      return {
        id: `m-${m.id}`,
        lane: 'tool',
        laneLabel: 'TOOL',
        createdAt: m.createdAt,
        label: toolName,
        preview: truncate(`${toolName} ${m.metadata?.inputPreview || m.metadata?.description || ''}`, 140),
        raw: {
          ...m,
          toolName,
          description: m.metadata?.description,
          inputPreview: m.metadata?.inputPreview,
          status: permissionStatus(m.metadata?.status),
        },
      };
    }
    return {
      id: `m-${m.id}`,
      lane: m.sender === 'agent' ? 'agent' : 'input',
      laneLabel: m.sender === 'agent' ? 'AGENT' : (m.sender === 'slack' ? 'SLACK' : 'INPUT'),
      createdAt: m.createdAt,
      label: m.sender === 'agent' ? 'Agent' : (m.sender === 'slack' ? 'Slack' : 'You'),
      preview: truncate(m.text, 140) || '(no text)',
      raw: m,
    };
  });
  const fromToolCalls = (props.toolCalls || []).map(tc => ({
    id: `t-${tc.id}`,
    lane: 'tool',
    laneLabel: 'TOOL',
    createdAt: tc.createdAt,
    label: tc.toolName,
    preview: truncate(`${tc.toolName} ${tc.inputPreview || tc.description || ''}`, 140),
    raw: tc,
  }));
  return [...fromMessages, ...fromToolCalls].sort((a, b) => new Date(a.createdAt) - new Date(b.createdAt));
});

const query = ref('');
const filteredItems = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return items.value;
  return items.value.filter(it =>
    it.label?.toLowerCase().includes(q) ||
    it.preview?.toLowerCase().includes(q) ||
    it.raw?.description?.toLowerCase().includes(q)
  );
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

watch(selectedItem, () => {
  activeTab.value = 'summary';
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

function laneDotClass(it) {
  if (it.lane === 'tool') {
    switch (it.raw.status) {
      case 'denied': return 'bg-red-500';
      case 'pending': return 'bg-amber-400';
      case 'allowed':
      case 'auto_allowed':
      default:
        return 'bg-emerald-500';
    }
  }
  return it.lane === 'agent' ? 'bg-violet-500' : 'bg-sky-400';
}

function laneBadgeClass(it) {
  if (it.lane === 'tool') return 'text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10';
  return it.lane === 'agent'
    ? 'text-violet-700 dark:text-violet-400 bg-violet-50 dark:bg-violet-500/10'
    : 'text-sky-700 dark:text-sky-400 bg-sky-50 dark:bg-sky-500/10';
}

function toolCallStatusStyle(status) {
  switch (status) {
    case 'allowed':
    case 'auto_allowed':
      return 'text-gray-600 dark:text-zinc-300 bg-gray-100 dark:bg-zinc-800';
    case 'denied':
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
