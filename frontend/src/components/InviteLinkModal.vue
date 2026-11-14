<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A new invitation's link, to share by hand: the only way in when no email
     carried it. The server hands it over once, so this is the only time it
     is shown. -->
<template>
  <Teleport to="body">
    <div v-if="link" class="fixed inset-0 z-[120] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="invite-link-title" data-test="invite-link-modal">
      <div class="absolute inset-0 bg-gray-900/60 backdrop-blur-sm" @click="$emit('close')"></div>
      <div class="relative w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-2xl p-6 sm:p-8 space-y-4">
        <h3 id="invite-link-title" class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight">Invitation link</h3>
        <p class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium" data-test="invite-link-text">
          <template v-if="emailSent">We emailed <span class="font-semibold text-gray-900 dark:text-white break-all">{{ email }}</span>. You can also share this link.</template>
          <template v-else>No email was sent. Share this link with <span class="font-semibold text-gray-900 dark:text-white break-all">{{ email }}</span>.</template>
          It works once, for that email, for 7 days.
        </p>
        <div class="relative">
          <input :value="link" readonly data-test="invite-link" aria-label="Invitation link" @focus="$event.target.select()"
                 class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-700 rounded-sm pl-3 pr-10 py-2.5 text-xs font-mono text-gray-800 dark:text-zinc-200 outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
          <button type="button" data-test="invite-copy" @click="copy" :title="copied ? 'Copied' : 'Copy link'" :aria-label="copied ? 'Copied' : 'Copy link'" :data-copied="copied"
                  class="absolute inset-y-0 right-0 flex items-center px-3 text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-white transition-colors">
            <svg v-if="!copied" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
            <svg v-else class="w-4 h-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
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
  emailSent: { type: Boolean, default: false },
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
