<script setup>
/**
 * The task finder, opened with Cmd/Ctrl+K.
 *
 * The matching and the ID resolution live in `useTaskFinder` — this component
 * is the box around them: it owns the open/closed state, the highlighted row
 * and the navigation, and nothing else.
 */
import { computed, nextTick, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import { fetchGlobalTasks, getTask } from '../api';
import { looksLikeTaskId, matchTasks, resolveTaskById, taskRoute } from '../composables/useTaskFinder';
import { searchCachedTasks } from '../composables/useCachedReads';
import { sharedCache } from '../composables/useCachedTasks';
import { useWorkspaceStore } from '../stores/workspaceStore';

const props = defineProps({
  show: Boolean,
  /** Rendered in the empty state so the shortcut stays discoverable. */
  shortcutLabel: { type: String, default: '' },
});
const emit = defineEmits(['close']);

const router = useRouter();
const workspaceStore = useWorkspaceStore();

const query = ref('');
const tasks = ref([]);
const highlighted = ref(0);
const loading = ref(false);
const resolving = ref(false);
const notFound = ref(false);
const inputRef = ref(null);

/**
 * The rows on screen, and which search produced them.
 *
 * Not a computed, because the better of the two searches is asynchronous. The
 * local index answers across every task this device has saved; the fallback
 * filters whatever the recent-task fetch happened to return. `searchedLocally`
 * exists so the panel can say which one answered — the two do not reach the
 * same distance, and a box that hides the difference misleads whoever searches
 * a title, finds nothing, and concludes the task is gone.
 */
const results = ref([]);
const searchedLocally = ref(false);

async function runSearch() {
  const asked = query.value;
  const local = await searchCachedTasks(sharedCache(), asked, { limit: 8 });

  // A slower search for an older query must not overwrite a newer one's answer.
  if (asked !== query.value) return;

  searchedLocally.value = local !== null;
  results.value = local ?? matchTasks(tasks.value, asked);
}

/**
 * Offer the ID lookup only when the search has come up empty — until then the
 * query is answered by rows the user can already see, and firing one request
 * per workspace behind that would be noise.
 */
const canResolveId = computed(
  () => results.value.length === 0 && looksLikeTaskId(query.value) && !resolving.value
);

/** How many recent tasks the fallback has to work with. */
const loadedCount = computed(() => tasks.value.length);

/**
 * Whether this device can answer a text search on its own.
 *
 * Drives the resting copy, which has to promise the right thing before anyone
 * has typed: the local index reaches every task saved here, and the fallback
 * reaches only what the last fetch returned.
 */
const canSearchLocally = computed(() => Boolean(sharedCache()));

/**
 * How far a title search actually reaches, in the fewest honest words.
 *
 * "Saved here" rather than "everything": the cache holds what has been opened,
 * not a whole history, and claiming otherwise would be the same mistake this
 * line exists to prevent.
 */
const titlesReach = computed(() => {
  if (loading.value) return '…';
  return canSearchLocally.value ? 'saved here' : `${loadedCount.value} recent`;
});

watch(
  () => props.show,
  async (open) => {
    if (!open) return;
    query.value = '';
    highlighted.value = 0;
    notFound.value = false;
    results.value = [];
    searchedLocally.value = false;
    await nextTick();
    inputRef.value?.focus();
    loadRecent();
  }
);

watch(query, () => {
  highlighted.value = 0;
  notFound.value = false;
  runSearch();
});

// The recent-task fetch only matters to the fallback, but it lands after the
// panel opens, so the resting list has to be refreshed when it does.
watch(tasks, () => runSearch());

async function loadRecent() {
  loading.value = true;
  try {
    // The API caps this at 50, which is also the most this list is worth
    // holding: past that, the ID lookup is the honest answer.
    const res = await fetchGlobalTasks({ limit: 50 });
    tasks.value = res.tasks || [];
  } catch {
    // A finder that cannot list still resolves an ID, so an empty list is a
    // degraded box rather than a broken one.
    tasks.value = [];
  } finally {
    loading.value = false;
  }
}

function move(delta) {
  const count = results.value.length;
  if (count === 0) return;
  highlighted.value = (highlighted.value + delta + count) % count;
}

function open(task) {
  emit('close');
  router.push(taskRoute(task));
}

async function submit() {
  const hit = results.value[highlighted.value];
  if (hit) return open(hit);
  if (!canResolveId.value) return;

  resolving.value = true;
  notFound.value = false;
  try {
    const found = await resolveTaskById(
      query.value.trim(),
      workspaceStore.workspaces.map((w) => w.id),
      getTask
    );
    if (found) {
      emit('close');
      router.push(taskRoute(found.task, found.workspaceId));
      return;
    }
    notFound.value = true;
  } finally {
    resolving.value = false;
  }
}
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[150]" role="dialog" aria-modal="true" aria-label="Find task">
      <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" @click="emit('close')"></div>

      <div class="relative mx-auto mt-[12vh] w-[92%] max-w-xl">
        <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl overflow-hidden">
          <!-- Query -->
          <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-100 dark:border-zinc-800">
            <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 11a6 6 0 11-12 0 6 6 0 0112 0z" />
            </svg>
            <input ref="inputRef" v-model="query" type="text" autocomplete="off" spellcheck="false"
                   :placeholder="canSearchLocally ? 'Task ID, or any word in a title or description' : 'Task ID, or part of a recent title'"
                   class="grow bg-transparent text-[14px] text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 focus:outline-none"
                   @keydown.down.prevent="move(1)"
                   @keydown.up.prevent="move(-1)"
                   @keydown.enter.prevent="submit"
                   @keydown.esc.prevent="emit('close')" />
            <kbd class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 border border-gray-200 dark:border-zinc-700 rounded px-1.5 py-0.5">Esc</kbd>
          </div>

          <!-- Results -->
          <ul v-if="results.length" class="max-h-80 overflow-y-auto py-1">
            <li v-for="(task, i) in results" :key="task.id">
              <button type="button" @click="open(task)" @mouseenter="highlighted = i"
                      class="w-full text-left px-4 py-2.5 flex items-center gap-3 transition-colors"
                      :class="i === highlighted ? 'bg-gray-50 dark:bg-zinc-800' : 'hover:bg-gray-50/60 dark:hover:bg-zinc-800/60'">
                <span class="grow min-w-0">
                  <span class="block truncate text-[13px] font-medium text-gray-900 dark:text-zinc-100">{{ task.title }}</span>
                  <span class="block truncate text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ task.id }}</span>
                </span>
                <span class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ task.status }}</span>
              </button>
            </li>
          </ul>

          <!-- Empty states -->
          <div v-else class="px-4 py-6 text-center">
            <p v-if="loading" class="text-[12px] text-gray-400 dark:text-zinc-500">Loading recent tasks…</p>
            <p v-else-if="resolving" class="text-[12px] text-gray-400 dark:text-zinc-500">Looking that ID up across your workspaces…</p>
            <p v-else-if="notFound" class="text-[12px] text-gray-500 dark:text-zinc-400">
              No task with that ID in any of your workspaces.
            </p>
            <p v-else-if="canResolveId" class="text-[12px] text-gray-500 dark:text-zinc-400">
              Not in the recent tasks. Press <span class="font-semibold text-gray-900 dark:text-zinc-100">Enter</span> to look this ID up across your workspaces.
            </p>
            <p v-else-if="query" class="text-[12px] text-gray-500 dark:text-zinc-400">
              <span v-if="searchedLocally">No match in the tasks saved on this device.</span>
              <span v-else>No match in your {{ loadedCount }} most recent tasks.</span>
              <span class="block mt-1 text-gray-400 dark:text-zinc-500">
                <template v-if="searchedLocally">A task you have never opened here was never saved — paste its ID to search every workspace.</template>
                <template v-else>Title search only covers those — paste a full task ID to search every workspace.</template>
              </span>
            </p>
            <p v-else class="text-[12px] text-gray-500 dark:text-zinc-400">
              <span v-if="canSearchLocally">Search the titles and descriptions of every task saved on this device, offline.</span>
              <span v-else>Search the titles of your {{ loadedCount }} most recent tasks.</span>
              <span class="block mt-1 text-gray-400 dark:text-zinc-500">
                To reach a task that is not here, paste its ID<span v-if="shortcutLabel"> — {{ shortcutLabel }} reopens this</span>.
              </span>
            </p>
          </div>

          <!-- Footer: keys on the left, the reach of each search on the right.
               The two halves of this box search different amounts of the app,
               so the panel says so rather than letting the user assume. -->
          <div class="flex items-center justify-between gap-4 px-4 py-2 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-800/30">
            <span class="flex items-center gap-4 shrink-0">
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↑↓ Navigate</span>
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↵ Open</span>
            </span>
            <span class="text-[9px] text-right text-gray-400 dark:text-zinc-500 truncate">
              Titles: <span class="text-gray-500 dark:text-zinc-400">{{ titlesReach }}</span>
              · IDs: <span class="text-gray-500 dark:text-zinc-400">every workspace</span>
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
