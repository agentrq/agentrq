<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <section class="w-full border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden" data-testid="task-plan">
    <!-- The bar: always in view, so the plan is a glance away however long the conversation gets -->
    <button type="button" @click="expanded = !expanded" :aria-expanded="expanded"
            class="w-full flex items-center gap-2 px-3 py-1.5 text-left bg-gray-50 dark:bg-zinc-800/80 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors min-w-0"
            data-testid="task-plan-toggle">
      <svg class="w-3.5 h-3.5 shrink-0 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" /></svg>
      <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200 shrink-0">Plan</span>
      <span v-if="parsed.progress" class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 shrink-0" data-testid="task-plan-progress">{{ parsed.progress.done }} / {{ parsed.progress.total }}</span>
      <span v-if="summary" class="text-[11px] text-gray-600 dark:text-zinc-400 truncate min-w-0" data-testid="task-plan-summary">· {{ summary }}</span>
      <span class="flex-1"></span>
      <span v-if="commentCount" class="text-[9px] font-semibold text-gray-400 dark:text-zinc-500 shrink-0" :title="`${commentCount} comment${commentCount === 1 ? '' : 's'}`">💬 {{ commentCount }}</span>
      <svg class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500 transition-transform" :class="expanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
    </button>

    <div v-if="expanded" class="p-3 max-h-[45vh] overflow-y-auto flex flex-col gap-1.5 min-w-0" data-testid="task-plan-body">
      <template v-for="(block, i) in parsed.blocks" :key="i">
        <div v-if="block.type === 'markdown'" class="md-body text-[12px] text-gray-600 dark:text-zinc-400 min-w-0" v-html="renderMarkdown(block.text)"></div>

        <div v-else class="min-w-0" data-testid="task-plan-step">
          <div class="group flex items-start gap-2 text-[11px] min-w-0">
            <span class="mt-[3px] w-3 h-3 shrink-0 rounded-[3px] border flex items-center justify-center"
                  :class="block.done ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white'
                          : block.text === parsed.current ? 'border-gray-900 dark:border-white'
                          : 'border-gray-300 dark:border-zinc-600'">
              <svg v-if="block.done" class="w-2 h-2 text-white dark:text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="4" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
              <span v-else-if="block.text === parsed.current" class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white animate-pulse"></span>
            </span>
            <span class="min-w-0 flex-1 break-words"
                  :class="block.done ? 'line-through text-gray-400 dark:text-zinc-500'
                          : block.text === parsed.current ? 'font-semibold text-gray-900 dark:text-zinc-100'
                          : 'text-gray-600 dark:text-zinc-400'">{{ block.text }}</span>
            <button type="button" @click="startComment(block.text)" :disabled="disabled"
                    class="shrink-0 flex items-center gap-1 px-1 rounded text-[9px] font-semibold text-gray-400 dark:text-zinc-500 hover:text-gray-800 dark:hover:text-zinc-200 disabled:opacity-40 transition-colors"
                    :class="comments.byStep.get(block.text)?.length ? '' : 'opacity-60 group-hover:opacity-100'"
                    :title="`Comment on: ${block.text}`" data-testid="task-plan-comment">
              💬<span v-if="comments.byStep.get(block.text)?.length">{{ comments.byStep.get(block.text).length }}</span>
            </button>
          </div>

          <ul v-if="comments.byStep.get(block.text)?.length" class="ml-5 mt-1 flex flex-col gap-1">
            <li v-for="c in comments.byStep.get(block.text)" :key="c.id"
                class="text-[11px] text-gray-600 dark:text-zinc-400 border-l-2 border-gray-200 dark:border-zinc-700 pl-2 break-words">{{ c.comment }}</li>
          </ul>

          <form v-if="commenting === block.text" @submit.prevent="submit" class="ml-5 mt-1.5 flex flex-col gap-1.5" data-testid="task-plan-comment-form">
            <textarea ref="draftRef" v-model="draft" rows="2" @keydown.enter.exact.prevent="submit" @keydown.esc.prevent="cancel"
                      placeholder="Comment on this step — the agent gets it as a message"
                      class="w-full text-[12px] px-2 py-1.5 rounded border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-gray-800 dark:text-zinc-200 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500 resize-none"></textarea>
            <div class="flex items-center justify-end gap-2">
              <button type="button" @click="cancel" class="text-[10px] font-black uppercase tracking-widest px-2 py-1 rounded-lg border border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-300">Cancel</button>
              <button type="submit" :disabled="!draft.trim() || disabled" class="text-[10px] font-black uppercase tracking-widest px-2 py-1 rounded-lg bg-black dark:bg-white text-white dark:text-black disabled:opacity-40">Send</button>
            </div>
          </form>
        </div>
      </template>

      <div v-if="comments.earlier.length" class="mt-2 pt-2 border-t border-gray-100 dark:border-zinc-800" data-testid="task-plan-earlier">
        <span class="text-[9px] font-semibold uppercase tracking-wider text-gray-400 dark:text-zinc-500">Earlier steps</span>
        <ul class="mt-1 flex flex-col gap-1.5">
          <li v-for="c in comments.earlier" :key="c.id" class="text-[11px] min-w-0 break-words">
            <span class="text-gray-400 dark:text-zinc-500 line-through">{{ c.step }}</span>
            <span class="block border-l-2 border-gray-200 dark:border-zinc-700 pl-2 text-gray-600 dark:text-zinc-400">{{ c.comment }}</span>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

<script setup>
/**
 * The task's plan, pinned above the conversation: a one-line bar that opens
 * to the whole plan, with a comment button on every step.
 *
 * The agent owns the plan, so nothing here edits it. A comment is emitted for
 * the view to send as an ordinary message, which keeps it on the composer's
 * path: held while the agent is mid-turn, and subject to the send delay.
 */
import { computed, nextTick, ref } from 'vue';
import { renderMarkdown } from '../utils/markdown';
import { parsePlan, planComments, planSummary } from '../composables/useTaskPlan';

const props = defineProps({
  plan: { type: Object, required: true },
  messages: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(['comment']);

const expanded = ref(false);
const commenting = ref(null);
const draft = ref('');
const draftRef = ref(null);

const parsed = computed(() => parsePlan(props.plan.content));
const summary = computed(() => planSummary(props.plan.content));
const comments = computed(() => planComments(props.messages, parsed.value.steps));
const commentCount = computed(() =>
  [...comments.value.byStep.values()].reduce((n, list) => n + list.length, 0) + comments.value.earlier.length);

async function startComment(step) {
  commenting.value = step;
  draft.value = '';
  await nextTick();
  // The ref sits inside a v-for, so Vue hands it over as an array.
  [draftRef.value].flat()[0]?.focus();
}

function cancel() {
  commenting.value = null;
  draft.value = '';
}

function submit() {
  const text = draft.value.trim();
  if (!text || props.disabled) return;
  emit('comment', commenting.value, text);
  cancel();
}
</script>
