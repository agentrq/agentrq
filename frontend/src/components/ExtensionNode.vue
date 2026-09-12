<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
  <!-- text -->
  <p v-if="node.type === 'text'" class="text-[12px] leading-relaxed" :class="toneClass">{{ node.value }}</p>

  <!-- heading -->
  <h3 v-else-if="node.type === 'heading'"
      class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
    {{ node.value }}
  </h3>

  <!-- rows -->
  <dl v-else-if="node.type === 'rows'" class="flex flex-col divide-y divide-gray-100 dark:divide-zinc-800">
    <ExtensionNode v-for="(item, i) in node.items" :key="i" :node="item" @action="$emit('action', $event)" />
  </dl>

  <!-- row: a label and a value, so numbers line up down the column -->
  <div v-else-if="node.type === 'row'" class="flex items-baseline justify-between gap-4 py-1.5">
    <dt class="text-[11px] text-gray-500 dark:text-zinc-400 min-w-0 truncate">{{ node.label }}</dt>
    <dd class="text-[12px] text-gray-900 dark:text-white font-medium text-right tabular-nums min-w-0 truncate">
      <a v-if="node.href" :href="node.href" target="_blank" rel="noopener noreferrer" class="hover:underline">
        {{ node.value }}
      </a>
      <template v-else>{{ node.value }}</template>
    </dd>
  </div>

  <!-- badge -->
  <span v-else-if="node.type === 'badge'"
        class="self-start px-2 py-0.5 rounded-sm text-[10px] font-bold uppercase tracking-wider" :class="badgeClass">
    {{ node.value }}
  </span>

  <!-- button: the action is passed back verbatim and never interpreted here -->
  <button v-else-if="node.type === 'button'" type="button" @click="$emit('action', node.action)"
          class="self-start px-3 py-1.5 rounded-lg text-[11px] font-black uppercase tracking-widest transition-all hover:opacity-80"
          :class="node.tone === 'critical'
            ? 'bg-red-600 text-white'
            : 'bg-black dark:bg-white text-white dark:text-black'">
    {{ node.label }}
  </button>

  <!-- link: already reduced to http(s) or nothing by normaliseView -->
  <a v-else-if="node.type === 'link' && node.href" :href="node.href" target="_blank" rel="noopener noreferrer"
     class="self-start text-[11px] font-bold text-gray-700 dark:text-zinc-300 underline underline-offset-2 hover:text-black dark:hover:text-white transition-colors">
    {{ node.label }}
  </a>
  <span v-else-if="node.type === 'link'" class="text-[11px] text-gray-400 dark:text-zinc-500">{{ node.label }}</span>

  <!-- empty: a state, not a failure, and worth saying in words -->
  <p v-else-if="node.type === 'empty'"
     class="text-[11px] text-gray-500 dark:text-zinc-400 border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-4 text-center">
    {{ node.value }}
  </p>

  <!-- diagram: the source is drawn by AgentRQ, never markup from an extension -->
  <DiagramBlock v-else-if="node.type === 'diagram'"
                :format="node.format" :source="node.source" :label="node.label" />

  <!-- group -->
  <section v-else-if="node.type === 'group'" class="flex flex-col gap-1.5">
    <h3 v-if="node.label" class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
      {{ node.label }}
    </h3>
    <ExtensionNode v-for="(child, i) in node.children" :key="i" :node="child" @action="$emit('action', $event)" />
  </section>
</template>

<script setup>
/**
 * One node of an extension's view.
 *
 * Recursive, because `group` and `rows` hold children — and bounded before it
 * gets here: `normaliseView` refuses anything deeper than five levels or larger
 * than two hundred nodes, so this cannot be handed a spec that renders forever.
 *
 * Every branch is `v-if` on a type from a closed vocabulary. There is no
 * fallback branch on purpose: an unknown type never reaches this component,
 * because the validator rejects the whole view rather than skipping the node —
 * a page quietly missing what its author wrote is worse for them than being
 * told, and worse for the reader than seeing nothing.
 */
import { computed } from 'vue';

import DiagramBlock from './DiagramBlock.vue';

const props = defineProps({ node: { type: Object, required: true } });
defineEmits(['action']);

/** Extension tones map to this application's palette, never to a colour they chose. */
const TONE_TEXT = {
  default: 'text-gray-900 dark:text-white',
  muted: 'text-gray-500 dark:text-zinc-400',
  positive: 'text-emerald-700 dark:text-emerald-400',
  warning: 'text-amber-700 dark:text-amber-400',
  critical: 'text-red-700 dark:text-red-400',
};

const TONE_BADGE = {
  default: 'bg-gray-100 dark:bg-zinc-800 text-gray-700 dark:text-zinc-300',
  muted: 'bg-gray-50 dark:bg-zinc-900 text-gray-500 dark:text-zinc-400',
  positive: 'bg-emerald-50 dark:bg-emerald-950/40 text-emerald-700 dark:text-emerald-400',
  warning: 'bg-amber-50 dark:bg-amber-950/40 text-amber-700 dark:text-amber-400',
  critical: 'bg-red-50 dark:bg-red-950/40 text-red-700 dark:text-red-400',
};

const toneClass = computed(() => TONE_TEXT[props.node.tone] ?? TONE_TEXT.default);
const badgeClass = computed(() => TONE_BADGE[props.node.tone] ?? TONE_BADGE.default);
</script>
