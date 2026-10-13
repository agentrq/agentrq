<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * A chat with the supervisor opened from the Pano: a card of its own left of
 * the page, styled as the page's own card, and spaced and resized from it as
 * the desktop side panel is. Alone, and full width, on a small screen, where
 * App.vue hides the page.
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { usePanoChatWidth } from '../composables/usePano'
import ResizeHandle from './ResizeHandle.vue'

const asideRef = ref(null)
const chatWidth = usePanoChatWidth()

/** The room the column and the page share; the sidebar is not theirs. */
function measure() {
  const aside = asideRef.value
  return aside.offsetWidth + (aside.nextElementSibling?.offsetWidth ?? 0) || globalThis.innerWidth
}

const room = ref(Infinity)
const width = computed(() => chatWidth.drawn(room.value))

function onWindowResize() {
  room.value = measure()
}

// The sidebar before it collapsing changes the room without the window resizing.
let observer = null

onMounted(() => {
  room.value = measure()
  if (globalThis.ResizeObserver && asideRef.value?.previousElementSibling) {
    observer = new ResizeObserver(onWindowResize)
    observer.observe(asideRef.value.previousElementSibling)
  }
  globalThis.addEventListener('resize', onWindowResize)
})
onUnmounted(() => {
  observer?.disconnect()
  globalThis.removeEventListener('resize', onWindowResize)
})
</script>

<template>
  <aside ref="asideRef" data-pano-chat :style="{ '--pano-chat-width': `${width}px` }"
    class="relative w-full lg:w-[var(--pano-chat-width)] lg:shrink-0 p-0 md:py-4 md:pr-4 h-full min-h-0 flex flex-col bg-white md:bg-zinc-100 dark:bg-zinc-950">
    <!-- The drag handle, in the gap between the chat and the page. -->
    <ResizeHandle data-pano-chat-handle aria-label="Resize the chat" class="hidden lg:flex right-0"
      :width="width" edge="right" @resize="(w) => chatWidth.set(w, room)" @reset="chatWidth.reset()" />

    <div class="h-full overflow-y-auto min-w-0 md:rounded-sm scroll-smooth page-surface md:border border-gray-200 dark:border-zinc-800 no-scrollbar">
      <div class="px-2 pt-4 pb-6 md:p-4 h-full flex flex-col">
        <router-view name="chat" class="grow flex flex-col min-h-0 min-w-0" />
      </div>
    </div>
  </aside>
</template>
