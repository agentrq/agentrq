<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- An invitation no email carried: its link, to send by hand. The server
     hands it over once, so this is the only time it is shown. -->
<template>
  <Teleport to="body">
    <div v-if="link" class="fixed inset-0 z-[120] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="invite-link-title" data-test="invite-link-modal">
      <div class="absolute inset-0 bg-gray-900/60 backdrop-blur-sm" @click="$emit('close')"></div>
      <div class="relative w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-2xl p-6 sm:p-8 space-y-4">
        <h3 id="invite-link-title" class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight">Invitation link</h3>
        <p class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
          Send this to <span class="font-semibold text-gray-900 dark:text-white break-all">{{ email }}</span>. It works once, for that email, for 7 days.
        </p>
        <div class="flex items-stretch gap-2">
          <input :value="link" readonly data-test="invite-link" aria-label="Invitation link" @focus="$event.target.select()"
                 class="min-w-0 flex-1 bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-xs font-mono text-gray-800 dark:text-zinc-200 outline-none" />
          <button type="button" data-test="invite-copy" @click="copy"
                  class="shrink-0 px-4 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 rounded-sm text-[10px] font-bold uppercase tracking-widest hover:opacity-90">
            {{ copied ? 'Copied' : 'Copy' }}
          </button>
        </div>
        <div class="flex justify-end">
          <button type="button" @click="$emit('close')"
                  class="rounded-sm border border-gray-200 dark:border-zinc-700 px-6 py-2.5 bg-white dark:bg-zinc-900 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800">
            Done
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue';
import { writeClipboard } from '../composables/useMarkdownLinks';
import { useEscapeKey } from '../composables/useEscapeKey';

const props = defineProps({
  link: { type: String, default: '' },
  email: { type: String, default: '' },
});
const emit = defineEmits(['close']);

const copied = ref(false);
watch(() => props.link, () => { copied.value = false; });

// A refused copy leaves the link selected in its box, to copy by hand.
async function copy() {
  try {
    await writeClipboard(props.link, { bridge: window.agentrq?.clipboard, clipboard: navigator.clipboard });
    copied.value = true;
  } catch {
    copied.value = false;
  }
}

useEscapeKey(() => {
  if (!props.link) return false;
  emit('close');
});
</script>
