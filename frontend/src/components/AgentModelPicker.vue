<template>
  <!-- Nothing at all unless a choice exists and would do something. An agent
       that reports no models, or one too old to act on being told to switch,
       leaves the surrounding layout exactly as it was. -->
  <div v-if="picker.available.value" class="relative">
    <button ref="trigger"
            type="button"
            @click.stop="toggle"
            :disabled="picker.sending.value"
            :class="compact
              ? 'text-[9px] font-medium text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100 max-w-[140px]'
              : 'w-7 h-7 sm:w-auto sm:px-2 gap-1 justify-center rounded-md text-[10px] font-semibold bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
            class="flex items-center transition-all disabled:opacity-40 truncate">
      <!-- On a narrow screen this is the whole button, which is what the
           schedule, event and YOLO controls beside it already do: a row of
           fixed squares, not one of them widened by whatever the model happens
           to be called. The name returns at `sm`. -->
      <svg v-if="!compact" class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 5h10a2 2 0 012 2v10a2 2 0 01-2 2H7a2 2 0 01-2-2V7a2 2 0 012-2z" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M10 10h4v4h-4z" />
      </svg>
      <span :class="compact ? 'truncate' : 'hidden sm:inline truncate'">{{ picker.selectedName.value || 'Model' }}</span>
      <!-- Pending is said, not implied. The agent has been asked and has not
           answered, and a picker that simply showed the new value would be
           claiming something nothing has confirmed.
           Kept visible at every width: it is the one thing on this button that
           is not decoration. -->
      <span v-if="picker.isPending.value" class="ml-1 shrink-0 opacity-60">…</span>
    </button>

    <!-- Rendered into <body>, not beside the button.
         The workspace cards sit inside a scrolling container, and an absolutely
         positioned menu is clipped by any ancestor that scrolls — so a menu
         opening upward from a card lost whatever overflowed the container's
         edge, heading first. Fixed positioning in the body escapes that
         entirely, and costs a little arithmetic instead. -->
    <Teleport to="body">
      <div v-if="open"
           ref="menu"
           :style="{ top: `${position.top}px`, left: `${position.left}px`, maxHeight: `${position.maxHeight}px` }"
           class="fixed w-[240px] overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-2"
           @click.stop>
        <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 px-2 py-1.5">Model</h3>

        <div v-for="group in picker.groups.value" :key="group.name || '_'">
          <p v-if="group.name" class="text-[9px] font-bold uppercase tracking-wider text-gray-400 dark:text-zinc-600 px-2 pt-2 pb-1">
            {{ group.name }}
          </p>
          <button v-for="model in group.models" :key="model.id"
                  type="button"
                  @click="pick(model.id)"
                  :class="model.id === picker.selectedId.value
                    ? 'bg-gray-100 dark:bg-zinc-800 text-gray-900 dark:text-white'
                    : 'text-gray-600 dark:text-zinc-400 hover:bg-gray-50 dark:hover:bg-zinc-800/60'"
                  class="w-full text-left px-2 py-1.5 rounded-md text-[11px] font-semibold transition-colors flex items-center gap-2">
            <span class="truncate grow">{{ model.name || model.id }}</span>
            <span v-if="model.id === picker.selectedId.value" class="shrink-0 text-[9px]">●</span>
          </button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
/**
 * Choosing the model the workspace's agent runs.
 *
 * One component in two places — the workspace card and the task form — because
 * it is one choice about one agent, and two copies of this markup would drift
 * on the part that matters: what is shown while the agent has been asked but
 * has not answered.
 *
 * The selection logic lives in useAgentModelPicker, which is tested, as does
 * the menu's geometry. What is left here is the markup and the things only a
 * mounted component can own: the menu's open state, where it currently sits,
 * and the clock that gives up on a switch nothing confirmed.
 */
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { MAX_POPOVER_HEIGHT, popoverPosition, useAgentModelPicker } from '../composables/useAgentModelPicker';
import { useToasts } from '../composables/useToasts';
import { setAgentModel } from '../api';

const props = defineProps({
  /** The workspace whose agent is being asked. Read live, never copied. */
  workspace: { type: Object, default: null },
  /** Narrow styling, for the one-line agent summary on a workspace card. */
  compact: { type: Boolean, default: false },
});

const open = ref(false);
const trigger = ref(null);
const menu = ref(null);
const position = ref({ top: 0, left: 0, maxHeight: MAX_POPOVER_HEIGHT });
const { addToast } = useToasts();

const picker = useAgentModelPicker({
  workspace: () => props.workspace,
  selectModel: (modelId) => setAgentModel(props.workspace?.id, modelId),
});

/**
 * Measure, then place.
 *
 * Done after the menu exists rather than from an assumed height: the list is as
 * long as the agent's, and guessing it is what decides whether the menu opens
 * upward into a place it does not fit.
 */
async function place() {
  const anchor = trigger.value?.getBoundingClientRect();
  if (!anchor) return;
  await nextTick();
  const box = menu.value?.getBoundingClientRect();
  position.value = popoverPosition(
    anchor,
    { width: box?.width || 240, height: box?.height || 0 },
    { width: window.innerWidth, height: window.innerHeight },
  );
}

async function toggle() {
  open.value = !open.value;
  if (open.value) await place();
}

/**
 * Closed on scroll, not followed.
 *
 * A fixed menu does not move with the container it was opened from, so it would
 * otherwise hang in place while the card slid away beneath it. Closing is both
 * simpler and what a person expects from a menu they have scrolled away from.
 */
function closeOnScroll() {
  open.value = false;
}

/**
 * The menu is no longer a descendant of this component, so containment has to
 * be checked against both halves. Relying on `v-click-outside` here would close
 * the menu on the first click inside it.
 */
function onDocumentClick(event) {
  if (!open.value) return;
  if (trigger.value?.contains(event.target)) return;
  if (menu.value?.contains(event.target)) return;
  open.value = false;
}

// A refusal, or an agent that never came back, is mission-critical feedback:
// the human asked for something and it did not happen. The picker has already
// reverted to what is true by the time this shows.
watch(picker.error, (message) => {
  if (message) addToast(message, 'error');
});

async function pick(modelId) {
  open.value = false;
  await picker.choose(modelId);
}

// The pending state is bounded by a clock rather than by the request, because
// what is being waited for is the agent's own report and that arrives — or does
// not — long after the request resolved.
let tick;
onMounted(() => {
  tick = setInterval(() => picker.expirePending(), 1000);
  document.addEventListener('click', onDocumentClick);
  // Capturing, so a scroll inside the cards' own container is seen too — it
  // does not bubble to the window.
  window.addEventListener('scroll', closeOnScroll, true);
  window.addEventListener('resize', closeOnScroll);
});
onBeforeUnmount(() => {
  clearInterval(tick);
  document.removeEventListener('click', onDocumentClick);
  window.removeEventListener('scroll', closeOnScroll, true);
  window.removeEventListener('resize', closeOnScroll);
});
</script>
