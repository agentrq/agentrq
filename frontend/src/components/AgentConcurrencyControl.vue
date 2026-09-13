<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
  <!-- Nothing at all unless a gateway is attached and has reported a limit. An
       agent with no queue of its own — Claude Code connected directly — leaves
       the surrounding layout exactly as it was. -->
  <div v-if="control.visible.value" class="relative flex items-center">
    <!-- A button where the gateway will act on being told a new limit, and a
         plain chip everywhere else. Older gateways report their limit perfectly
         well and ignore the request, so a control drawn on the strength of the
         number alone would do nothing at all on most deployments. -->
    <component :is="control.editable.value ? 'button' : 'div'"
               ref="trigger"
               :type="control.editable.value ? 'button' : undefined"
               @click.stop="control.editable.value ? toggle() : undefined"
               :disabled="control.editable.value ? control.sending.value : undefined"
               @mouseenter="tooltipStore.show($event, tooltip, 'bottom')"
               @mouseleave="tooltipStore.hide()"
               :class="control.editable.value
                 ? 'hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 cursor-pointer'
                 : 'cursor-help'"
               class="h-8 px-2 gap-1.5 text-gray-500 dark:text-zinc-400 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center disabled:opacity-40">
      <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
      </svg>
      <span class="text-[10px] font-bold tabular-nums">
        {{ control.active.value }}<span class="opacity-40">/</span>{{ control.value.value }}
      </span>
      <!-- Queued is only mentioned when there is a queue. A permanent "0"
           beside the limit is noise on every workspace that is keeping up. -->
      <span v-if="control.queued.value > 0" class="text-[9px] font-semibold text-amber-600 dark:text-amber-500 tabular-nums">
        +{{ control.queued.value }}
      </span>
      <!-- Pending is said, not implied. The gateway has been asked and has not
           answered, and a control that simply showed the new value would be
           claiming something nothing has confirmed. -->
      <span v-if="control.isPending.value" class="text-[10px] shrink-0 opacity-60">…</span>
    </component>

    <!-- Rendered into <body>, not beside the button, and positioned fixed.
         The header sits above scrolling content and inside containers of its
         own, and an absolutely positioned panel is clipped by any ancestor that
         scrolls. The geometry is the model picker's, imported rather than
         copied: it is the same problem with the same answer, and it is already
         tested. -->
    <Teleport to="body">
      <div v-if="open"
           ref="menu"
           :style="{ top: `${position.top}px`, left: `${position.left}px`, maxHeight: `${position.maxHeight}px` }"
           class="fixed w-[232px] overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-3"
           @click.stop>
        <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mb-2">Concurrency</h3>

        <p class="text-[10px] font-medium text-gray-500 dark:text-zinc-400 mb-3 tabular-nums">{{ control.summary.value }}</p>

        <div class="flex items-center gap-1.5">
          <button type="button"
                  @click="step(-1)"
                  :disabled="control.sending.value || control.value.value <= control.range.value.min"
                  class="h-7 w-7 shrink-0 rounded-md border border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800 disabled:opacity-30 transition-colors flex items-center justify-center text-sm font-bold"
                  aria-label="Run one fewer at a time">−</button>
          <!-- Committed on change and on Enter rather than on every keystroke:
               typing "12" would otherwise ask the gateway for 1 on the way
               past, and each of those is a real reconfiguration of a live
               queue. -->
          <input :value="control.value.value"
                 @change="commit($event.target.value)"
                 @keyup.enter="commit($event.target.value)"
                 type="number"
                 inputmode="numeric"
                 :min="control.range.value.min"
                 :max="control.range.value.max > 0 ? control.range.value.max : undefined"
                 :disabled="control.sending.value"
                 class="h-7 w-full min-w-0 text-center bg-gray-50 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-md text-[11px] font-bold text-gray-900 dark:text-zinc-100 tabular-nums outline-none disabled:opacity-40" />
          <button type="button"
                  @click="step(1)"
                  :disabled="control.sending.value || (control.range.value.max > 0 && control.value.value >= control.range.value.max)"
                  class="h-7 w-7 shrink-0 rounded-md border border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800 disabled:opacity-30 transition-colors flex items-center justify-center text-sm font-bold"
                  aria-label="Run one more at a time">+</button>
        </div>

        <p class="text-[9px] font-medium text-gray-400 dark:text-zinc-500 mt-2 leading-snug">{{ rangeHint }}</p>

        <!-- Said out loud, because it looks like a fault and is not. Lowering
             the limit never interrupts a running task; the queue simply stops
             handing out new ones until the active count falls back under it. -->
        <p v-if="control.overLimit.value" class="text-[9px] font-medium text-amber-600 dark:text-amber-500 mt-2 leading-snug">
          {{ control.active.value }} already running will finish first. Nothing new starts until the count drops below {{ control.value.value }}.
        </p>

        <!-- The one thing a person cannot discover from the control itself.
             The limit lives in the gateway's memory: it survives a reconnect
             and is lost on a restart, which falls back to the gateway's
             --max-concurrency flag. -->
        <p class="text-[9px] font-medium text-gray-400 dark:text-zinc-500 mt-2 leading-snug">
          Resets to the gateway's own setting when it restarts.
        </p>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
/**
 * How many tasks the workspace's gateway runs at once.
 *
 * The logic lives in useAgentConcurrency, which is tested, as does the panel's
 * geometry. What is left here is the markup and the things only a mounted
 * component can own: the panel's open state, where it currently sits, and the
 * clock that gives up on a change nothing confirmed.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { MAX_POPOVER_HEIGHT, popoverPosition } from '../composables/useAgentModelPicker';
import { useAgentConcurrency } from '../composables/useAgentConcurrency';
import { useToasts } from '../composables/useToasts';
import { useTooltipStore } from '../stores/tooltipStore';
import { setAgentConcurrency } from '../api';

const props = defineProps({
  /** The workspace whose gateway is being asked. Read live, never copied. */
  workspace: { type: Object, default: null },
});

const open = ref(false);
const trigger = ref(null);
const menu = ref(null);
const position = ref({ top: 0, left: 0, maxHeight: MAX_POPOVER_HEIGHT });
const { addToast } = useToasts();
const tooltipStore = useTooltipStore();

const control = useAgentConcurrency({
  workspace: () => props.workspace,
  setConcurrency: (limit) => setAgentConcurrency(props.workspace?.id, limit),
});

/** What the chip says on hover, which is the only place a read-only one can explain itself. */
const tooltip = computed(() => {
  const state = `${control.summary.value}, limit ${control.value.value}`;
  return control.editable.value
    ? `${state} — click to change`
    : `${state} — this gateway cannot be told to change it`;
});

/** The range, in the words the gateway reported, or an open-ended one. */
const rangeHint = computed(() => {
  const { min, max } = control.range.value;
  return max > 0 ? `Between ${min} and ${max} at a time` : `At least ${min} at a time`;
});

/**
 * Measure, then place. Done after the panel exists rather than from an assumed
 * height, so it never opens upward into a place it does not fit.
 */
async function place() {
  const anchor = trigger.value?.$el?.getBoundingClientRect?.() ?? trigger.value?.getBoundingClientRect?.();
  if (!anchor) return;
  await nextTick();
  const box = menu.value?.getBoundingClientRect();
  position.value = popoverPosition(
    anchor,
    { width: box?.width || 232, height: box?.height || 0 },
    { width: window.innerWidth, height: window.innerHeight },
  );
}

async function toggle() {
  open.value = !open.value;
  if (open.value) await place();
}

/** One step from what is displayed, which is the pending value while there is one. */
function step(delta) {
  control.choose(control.value.value + delta);
}

function commit(raw) {
  control.choose(raw);
}

/**
 * Closed on scroll, not followed. A fixed panel does not move with the
 * container it was opened from, so it would otherwise hang in place while the
 * page slid away beneath it.
 */
function closeOnScroll() {
  open.value = false;
}

/**
 * The panel is no longer a descendant of this component, so containment has to
 * be checked against both halves. Relying on `v-click-outside` here would close
 * it on the first click inside.
 */
function onDocumentClick(event) {
  if (!open.value) return;
  const el = trigger.value?.$el ?? trigger.value;
  if (el?.contains?.(event.target)) return;
  if (menu.value?.contains(event.target)) return;
  open.value = false;
}

// A clamp, or a gateway that never came back, is feedback worth surfacing: the
// human asked for something and something else happened. The control has
// already reverted to what is true by the time this shows.
watch(control.error, (message) => {
  if (message) addToast(message, 'error');
});

// The pending state is bounded by a clock rather than by the request, because
// what is being waited for is the gateway's own report and that arrives — or
// does not — after the request has resolved.
let tick;
onMounted(() => {
  tick = setInterval(() => control.expirePending(), 1000);
  document.addEventListener('click', onDocumentClick);
  // Capturing, so a scroll inside a container of the page's own is seen too —
  // it does not bubble to the window.
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
