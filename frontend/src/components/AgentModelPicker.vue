<template>
  <!-- Nothing at all unless a choice exists and would do something. An agent
       that reports no models, or one too old to act on being told to switch,
       leaves the surrounding layout exactly as it was. -->
  <div v-if="picker.available.value" class="relative" v-click-outside="() => (open = false)">
    <button type="button"
            @click.stop="open = !open"
            :disabled="picker.sending.value"
            :class="compact
              ? 'text-[9px] font-medium text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100 max-w-[140px]'
              : 'h-7 px-2 gap-1 rounded-md text-[10px] font-semibold bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
            class="flex items-center transition-all disabled:opacity-40 truncate">
      <span class="truncate">{{ picker.selectedName.value || 'Model' }}</span>
      <!-- Pending is said, not implied. The agent has been asked and has not
           answered, and a picker that simply showed the new value would be
           claiming something nothing has confirmed. -->
      <span v-if="picker.isPending.value" class="ml-1 shrink-0 opacity-60">…</span>
    </button>

    <div v-if="open"
         class="fixed sm:absolute left-4 right-4 sm:left-auto sm:right-0 bottom-4 sm:bottom-full sm:mb-2 w-auto sm:w-[240px] max-h-[320px] overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-2"
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
 * All the logic lives in useAgentModelPicker, which is tested. What is left
 * here is the markup and the two things a component has to own: the popover,
 * and the clock that gives up on a switch nothing confirmed.
 */
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { useAgentModelPicker } from '../composables/useAgentModelPicker';
import { useToasts } from '../composables/useToasts';
import { setAgentModel } from '../api';

const props = defineProps({
  /** The workspace whose agent is being asked. Read live, never copied. */
  workspace: { type: Object, default: null },
  /** Narrow styling, for the one-line agent summary on a workspace card. */
  compact: { type: Boolean, default: false },
});

const open = ref(false);
const { addToast } = useToasts();

const picker = useAgentModelPicker({
  workspace: () => props.workspace,
  selectModel: (modelId) => setAgentModel(props.workspace?.id, modelId),
});

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
});
onBeforeUnmount(() => clearInterval(tick));
</script>
