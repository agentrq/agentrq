<!-- Copyright 2026 Contextual, Inc. https://agentrq.com -->

<template>
  <iframe ref="frame"
          :sandbox="FRAME_SANDBOX"
          :src="DRAWER_FRAME_URL"
          :style="{ height: `${height}px` }"
          class="block w-full border-0 bg-transparent"
          referrerpolicy="no-referrer"
          title="" />
</template>

<script setup>
/**
 * An extension's drawer, running where it cannot reach anything.
 *
 * The sandbox is the whole security story and it is one attribute:
 * `allow-scripts` with **no** `allow-same-origin` gives the document an opaque
 * origin — no access to this page, no bridge, no cookies, no storage. Adding
 * `allow-same-origin` would make it same-origin with `app://` and hand a
 * stranger's code the machine through the bridge. See `useDrawerFrame.js`.
 *
 * This component is deliberately thin. The rules — what the frame document
 * contains, which messages are believed, the timeout — are in that module,
 * where they are tested; a `.vue` file is not in the coverage include list and
 * logic here is logic nobody can check.
 */
import { onBeforeUnmount, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';

import { DRAWER_FRAME_URL, DRAW_TIMEOUT_MS, FRAME_SANDBOX, readFrameMessage } from '../composables/useDrawerFrame';
import { useThemeStore } from '../stores/themeStore';

const props = defineProps({
  url: { type: String, required: true },
  source: { type: String, required: true },
});

const emit = defineEmits(['failed', 'drawn']);

const themeStore = useThemeStore();
const { isDark } = storeToRefs(themeStore);

const frame = ref(null);
const height = ref(1);

/**
 * The `src` never changes, which is the point.
 *
 * Reloading the frame would throw away the imported drawer and blink every
 * diagram on the page on a theme toggle, so the theme is posted instead.
 */

let timer = null;
let ready = false;

function stopTimer() {
  if (timer) clearTimeout(timer);
  timer = null;
}

/** Ask the frame to draw, and start the clock on it answering. */
function draw() {
  if (!ready || !frame.value?.contentWindow) return;

  stopTimer();
  timer = setTimeout(() => {
    // A drawer that never answers must not leave "Drawing…" on screen forever.
    emit('failed', 'This drawer did not answer.');
  }, DRAW_TIMEOUT_MS);

  frame.value.contentWindow.postMessage(
    {
      type: 'draw',
      url: props.url,
      source: props.source,
      theme: isDark.value ? 'dark' : 'light',
      // The frame cannot read this page's stylesheet, so the colour it should
      // draw in is handed over with the request.
      colour: isDark.value ? '#e4e4e7' : '#18181b',
    },
    '*',
  );
}

function onMessage(event) {
  const message = readFrameMessage(event, frame.value?.contentWindow);
  if (!message) return;

  if (message.type === 'ready') {
    ready = true;
    draw();
    return;
  }

  stopTimer();
  if (message.type === 'drawn') {
    height.value = message.height;
    emit('drawn');
  } else {
    emit('failed', message.reason);
  }
}

window.addEventListener('message', onMessage);

onBeforeUnmount(() => {
  stopTimer();
  window.removeEventListener('message', onMessage);
});

watch(() => [props.url, props.source, isDark.value], draw);
</script>
