<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- One workspace on the workspaces page: who is live and on what, its task
     counts, and the first few tasks of its Active list. A click on a task
     opens the task; anywhere else opens the workspace. -->
<template>
  <div data-test="workspace-card"
       class="group relative bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-4 cursor-pointer transition-all duration-300 hover:border-gray-400 dark:hover:border-zinc-600 shadow-sm hover:shadow-md"
       :class="dimmed ? 'opacity-60 hover:opacity-100' : ''"
       @click="$emit('open', workspace)">

    <div class="flex items-start justify-between gap-3">
      <div class="flex items-center gap-3 min-w-0">
        <!-- Minimal Live Indicator -->
        <div class="relative flex items-center justify-center shrink-0">
          <div class="w-2 h-2 rounded-full z-10"
               :class="workspace.agentConnected ? 'bg-green-500' : 'bg-gray-300 dark:bg-zinc-700'"></div>
          <div v-if="workspace.agentConnected" class="absolute w-2 h-2 rounded-full bg-green-500 animate-ping opacity-75"></div>
        </div>

        <div class="min-w-0">
          <h3 class="font-black text-sm text-gray-800 dark:text-zinc-200 truncate group-hover:text-black dark:group-hover:text-white transition-colors leading-none flex items-center gap-1.5 min-w-0">
            <!-- The sidebar's fork mark, so a fork reads as one here too. -->
            <ForkIcon v-if="workspace.forkOfId" class="w-3 h-3 shrink-0 text-gray-500 dark:text-zinc-400" :title="`Fork of ${workspace.forkOf?.name ?? 'another workspace'}`" data-test="card-fork" />
            <span class="truncate">{{ toKebabCase(workspace.name) }}</span>
          </h3>
          <!-- The dot already says an agent is live, so when the workspace
               can say *what* is live the line spends its width on that
               instead of repeating the dot. Falls back to the plain state
               whenever nothing is known. -->
          <div class="flex items-baseline gap-1.5 mt-1 h-3.5 min-w-0" :title="agentSummary(workspace)">
            <template v-if="workspace.agentConnected && agentDetails(workspace)">
              <span v-if="agentDetails(workspace).client"
                    class="text-[9px] font-black uppercase tracking-wider text-green-600 dark:text-green-500 truncate">
                {{ agentDetails(workspace).client }}
              </span>
              <!-- Or a choice with nothing chosen yet: an agent may report
                   models without naming a current one. -->
              <template v-if="agentDetails(workspace).model || canChooseModel(workspace)">
                <span v-if="agentDetails(workspace).client" class="text-[9px] font-black text-gray-300 dark:text-zinc-700 shrink-0">·</span>
                <!-- The card navigates on click, so the picker stops its own
                     clicks from reaching it: choosing a model must not also
                     leave the page. -->
                <AgentModelPicker v-if="canChooseModel(workspace)" :workspace="workspace" compact @click.stop />
                <span v-else class="text-[9px] font-medium text-gray-500 dark:text-zinc-400 truncate">{{ agentDetails(workspace).model }}</span>
              </template>
              <template v-if="canChooseEffort(workspace)">
                <span class="text-[9px] font-black text-gray-300 dark:text-zinc-700 shrink-0">·</span>
                <AgentEffortPicker :workspace="workspace" compact @click.stop />
              </template>
            </template>
            <span v-else class="text-[8px] font-black uppercase tracking-widest transition-colors"
                  :class="workspace.agentConnected ? 'text-green-600 dark:text-green-500' : 'text-gray-400 dark:text-zinc-500'">
              {{ workspace.agentConnected ? 'Agent Live' : 'Agent Offline' }}
            </span>
          </div>
        </div>
      </div>

      <!-- On a phone only the numbers: the dot's colour says what each is.
           One line always, so a busy workspace's card is no taller than the
           rest; the name beside it truncates instead. -->
      <div class="flex items-center gap-1.5 shrink-0">
        <span v-for="c in chips" :key="c.key" data-test="count-chip" :title="`${c.count} ${c.label}`"
              class="inline-flex items-center gap-1 h-5 px-1.5 leading-none rounded-sm bg-gray-50 dark:bg-zinc-800/60 text-[9px] font-black uppercase tracking-wider"
              :class="c.text">
          <span class="w-1.5 h-1.5 rounded-full" :class="c.dot"></span>{{ c.count }}<span v-if="!narrow" class="ml-0.5">{{ c.short }}</span>
        </span>
      </div>
    </div>

    <!-- A fixed height, so every card in a column is the same height whether
         it shows four tasks, two, or none. -->
    <div class="mt-3 pt-3 border-t border-gray-100 dark:border-zinc-800" :class="narrow ? '' : 'h-[142px]'">
      <div v-if="shown.length === 0"
           class="h-full min-h-[60px] flex items-center justify-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm text-[11px] font-medium text-gray-500 dark:text-zinc-500">
        No active tasks
      </div>
      <div v-else class="flex flex-col">
        <div v-for="t in shown" :key="t.id" data-test="preview-task"
             class="group/t flex items-center gap-2.5 h-[30px] px-2 -mx-2 rounded-sm hover:bg-gray-50 dark:hover:bg-zinc-800/60"
             @click.stop="$emit('open-task', t)">
          <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="taskDotClass(t)"></span>
          <span class="flex-1 min-w-0 truncate text-[12.5px] font-medium"
                :class="isFinished(t) ? 'text-gray-500 dark:text-zinc-500' : 'text-gray-700 dark:text-zinc-200 group-hover/t:text-black dark:group-hover/t:text-white'">{{ t.title }}</span>
          <span v-if="!narrow" class="shrink-0 text-[9px] font-black uppercase tracking-wider text-gray-400 dark:text-zinc-500 w-[76px] text-right">{{ previewStatusLabel(t.status) }}</span>
          <span class="shrink-0 text-[10px] font-medium tabular-nums text-gray-500 dark:text-zinc-400 w-[72px] text-right truncate">{{ when(t) }}</span>
        </div>
        <span v-if="more > 0" data-test="more-tasks"
              class="mt-1 self-start text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white">
          +{{ more }} more →
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import ForkIcon from './ForkIcon.vue';
import AgentModelPicker from './AgentModelPicker.vue';
import AgentEffortPicker from './AgentEffortPicker.vue';
import { useFormat } from '../composables/useFormat';
import { useCron } from '../composables/useCron';
import { timeAgo } from '../composables/usePano';
import { agentDetails, agentSummary } from '../composables/useAgentSummary';
import { canChooseModel } from '../composables/useAgentModelPicker';
import { canChooseEffort } from '../composables/useAgentEffortPicker';
import { taskDotClass } from '../composables/useTaskStatusStyle';
import { OVERVIEW_TASKS, OVERVIEW_TASKS_NARROW, countChips, isFinished, moreTasks, previewStatusLabel } from '../composables/useWorkspaceOverview';

const props = defineProps({
  workspace: { type: Object, required: true },
  // The workspace's entry in the overview: its counts and first few tasks.
  overview: { type: Object, default: null },
  dimmed: { type: Boolean, default: false },
  narrow: { type: Boolean, default: false },
});
defineEmits(['open', 'open-task']);

const { toKebabCase } = useFormat();
const { getNextRunLabel } = useCron();

const counts = computed(() => props.overview?.counts ?? {});
const shown = computed(() => (props.overview?.tasks ?? []).slice(0, props.narrow ? OVERVIEW_TASKS_NARROW : OVERVIEW_TASKS));
const chips = computed(() => countChips(counts.value));
const more = computed(() => moreTasks(counts.value, shown.value));

// A scheduled task is about when it runs next; anything else, when it last moved.
function when(t) {
  return t.status === 'cron' ? getNextRunLabel(t.cronSchedule) : timeAgo(t.updatedAt);
}
</script>
