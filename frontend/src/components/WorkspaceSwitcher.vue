<script setup>
/**
 * The workspace switcher, opened with `W`.
 *
 * Deliberately the same box as the task finder — same overlay, same row
 * geometry, same ↑↓/↵/Esc keys — because it answers the same shape of question
 * and a second idiom for "search a list and jump to one" would be a second
 * thing to learn. The matching lives in `useWorkspaceSwitcher`.
 *
 * What it does *not* share is the loading: the workspace list is already in the
 * store (the sidebar renders from it), so this box has nothing to fetch and no
 * loading state to show.
 */
import { computed, nextTick, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import {
  isCurrentWorkspace,
  matchWorkspaces,
  workspaceRoute,
} from '../composables/useWorkspaceSwitcher';
import { useWorkspaceStore } from '../stores/workspaceStore';

const props = defineProps({
  show: Boolean,
  /** The workspace on screen, so its row can be marked rather than hidden. */
  currentWorkspaceId: { type: String, default: '' },
});
const emit = defineEmits(['close']);

const router = useRouter();
const workspaceStore = useWorkspaceStore();

const query = ref('');
const highlighted = ref(0);
const inputRef = ref(null);

const workspaces = computed(() => workspaceStore.workspaces);

/**
 * Ten rather than the finder's eight: these rows are one line instead of two,
 * so ten still fits the panel without scrolling, and a switcher that shows the
 * whole list for most accounts never needs a query at all.
 */
const results = computed(() => matchWorkspaces(workspaces.value, query.value, 10));

const hasWorkspaces = computed(() => workspaces.value.length > 0);

watch(
  () => props.show,
  async (open) => {
    if (!open) return;
    query.value = '';
    highlighted.value = 0;
    await nextTick();
    inputRef.value?.focus();
  }
);

// A shorter list can leave the highlight past the end, which would make Enter
// do nothing on a visible list.
watch(results, (rows) => {
  if (highlighted.value >= rows.length) highlighted.value = 0;
});

function move(delta) {
  const count = results.value.length;
  if (count === 0) return;
  highlighted.value = (highlighted.value + delta + count) % count;
}

function open(workspace) {
  emit('close');
  router.push(workspaceRoute(workspace));
}

function submit() {
  const hit = results.value[highlighted.value];
  if (hit) open(hit);
}

const isCurrent = (workspace) => isCurrentWorkspace(workspace, props.currentWorkspaceId);
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[150]" role="dialog" aria-modal="true" aria-label="Switch workspace">
      <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" @click="emit('close')"></div>

      <div class="relative mx-auto mt-[12vh] w-[92%] max-w-xl">
        <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl overflow-hidden">
          <!-- Query -->
          <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-100 dark:border-zinc-800">
            <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
            </svg>
            <input ref="inputRef" v-model="query" type="text" autocomplete="off" spellcheck="false"
                   placeholder="Search workspaces by name"
                   class="grow bg-transparent text-[14px] text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 focus:outline-none"
                   @keydown.down.prevent="move(1)"
                   @keydown.up.prevent="move(-1)"
                   @keydown.enter.prevent="submit"
                   @keydown.esc.prevent="emit('close')" />
            <kbd class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 border border-gray-200 dark:border-zinc-700 rounded px-1.5 py-0.5">Esc</kbd>
          </div>

          <!-- Results -->
          <ul v-if="results.length" class="max-h-80 overflow-y-auto py-1">
            <li v-for="(ws, i) in results" :key="ws.id">
              <button type="button" @click="open(ws)" @mouseenter="highlighted = i"
                      class="w-full text-left px-4 py-2.5 flex items-center gap-3 transition-colors"
                      :class="i === highlighted ? 'bg-gray-50 dark:bg-zinc-800' : 'hover:bg-gray-50/60 dark:hover:bg-zinc-800/60'">
                <!-- The same live dot as the sidebar. Which agents are up is
                     most of why you switch workspace, so it belongs on the row
                     you are choosing from. -->
                <span class="shrink-0 w-1.5 h-1.5 rounded-full"
                      :class="ws.agentConnected ? 'bg-green-500 dark:bg-green-400' : 'bg-gray-300 dark:bg-zinc-600'"
                      :title="ws.agentConnected ? 'Agent online' : 'Agent offline'"></span>
                <span class="grow min-w-0 truncate text-[13px] font-medium text-gray-900 dark:text-zinc-100">{{ ws.name }}</span>
                <span v-if="ws.archivedAt" class="shrink-0 text-[9px] font-black uppercase tracking-widest text-amber-600/70 dark:text-amber-500/70">Archived</span>
                <span v-else-if="isCurrent(ws)" class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Current</span>
              </button>
            </li>
          </ul>

          <!-- Empty states -->
          <div v-else class="px-4 py-6 text-center">
            <p v-if="!hasWorkspaces" class="text-[12px] text-gray-500 dark:text-zinc-400">
              No workspaces yet.
              <span class="block mt-1 text-gray-400 dark:text-zinc-500">Create one from the overview to switch between them.</span>
            </p>
            <p v-else class="text-[12px] text-gray-500 dark:text-zinc-400">
              No workspace matches &ldquo;{{ query }}&rdquo;.
              <span class="block mt-1 text-gray-400 dark:text-zinc-500">Names are matched anywhere, so a fragment like &ldquo;api&rdquo; finds &ldquo;payments-api&rdquo;.</span>
            </p>
          </div>

          <!-- Footer -->
          <div class="flex items-center justify-between gap-4 px-4 py-2 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-800/30">
            <span class="flex items-center gap-4 shrink-0">
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↑↓ Navigate</span>
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↵ Switch</span>
            </span>
            <span v-if="hasWorkspaces" class="text-[9px] text-right text-gray-400 dark:text-zinc-500 truncate">
              {{ workspaces.length }} workspace{{ workspaces.length === 1 ? '' : 's' }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.18s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
