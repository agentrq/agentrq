<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The ⋮ menu at the end of the macOS title bar: what an installed web app's
     window menu offers, where it applies to this shell. -->
<template>
  <div ref="menuRef" data-tauri-drag-region="false" class="app-no-drag relative shrink-0">
    <button type="button" @click="toggle" :aria-expanded="open" aria-haspopup="menu" title="More"
            class="w-7 h-7 rounded-full flex items-center justify-center text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-white transition-colors outline-none focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600">
      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <circle cx="12" cy="5" r="1.75" /><circle cx="12" cy="12" r="1.75" /><circle cx="12" cy="19" r="1.75" />
      </svg>
    </button>

    <div v-if="open" role="menu" data-window-menu
         class="absolute right-0 top-full mt-2 w-72 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-md shadow-2xl py-1.5 text-xs text-gray-700 dark:text-zinc-200">
      <div class="flex items-center gap-3 px-3 py-2">
        <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" /></svg>
        <span class="font-semibold">AgentRQ<template v-if="version"> v{{ version }}</template></span>
        <span v-if="host" class="ml-auto min-w-0 truncate text-gray-400 dark:text-zinc-500" :title="serverUrl">{{ host }}</span>
      </div>

      <template v-if="host">
        <div class="my-1.5 border-t border-gray-100 dark:border-zinc-800"></div>
        <button type="button" role="menuitem" @click="run(menu.copyUrl)" :class="item">
          <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244" /></svg>
          Copy URL
        </button>
        <button type="button" role="menuitem" @click="run(menu.openInBrowser)" :class="item">
          <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" /></svg>
          Open in browser
        </button>
      </template>

      <div class="my-1.5 border-t border-gray-100 dark:border-zinc-800"></div>
      <div class="flex items-center gap-3 px-3 py-1">
        <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607zM10.5 7.5v6m3-3h-6" /></svg>
        <span>Zoom</span>
        <div class="ml-auto flex items-center gap-1">
          <button type="button" @click="menu.zoomOut" title="Zoom out" :class="round">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" d="M5 12h14" /></svg>
          </button>
          <button type="button" @click="menu.resetZoom" title="Reset zoom" data-zoom
                  class="w-12 py-1 rounded-sm text-center tabular-nums hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">{{ menu.zoom.value }}%</button>
          <button type="button" @click="menu.zoomIn" title="Zoom in" :class="round">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" d="M12 5v14M5 12h14" /></svg>
          </button>
          <span class="mx-1 h-5 border-l border-gray-200 dark:border-zinc-700"></span>
          <button type="button" @click="menu.toggleFullScreen" :title="menu.fullScreen.value ? 'Exit full screen' : 'Full screen'" :class="round">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" /></svg>
          </button>
        </div>
      </div>

      <div class="my-1.5 border-t border-gray-100 dark:border-zinc-800"></div>
      <button type="button" role="menuitem" @click="run(menu.print)" :class="item">
        <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6.72 13.829c-.24.03-.48.062-.72.096m.72-.096a42.415 42.415 0 0110.56 0m-10.56 0L6.34 18m10.94-4.171c.24.03.48.062.72.096m-.72-.096L17.66 18m0 0l.229 2.523a1.125 1.125 0 01-1.12 1.227H7.231c-.662 0-1.18-.568-1.12-1.227L6.34 18m11.318 0h1.091A2.25 2.25 0 0021 15.75V9.456c0-1.081-.768-2.015-1.837-2.175a48.055 48.055 0 00-1.913-.247M6.34 18H5.25A2.25 2.25 0 013 15.75V9.456c0-1.081.768-2.015 1.837-2.175a48.041 48.041 0 011.913-.247m10.5 0a48.536 48.536 0 00-10.5 0m10.5 0V3.375c0-.621-.504-1.125-1.125-1.125h-8.25c-.621 0-1.125.504-1.125 1.125v3.659M18 10.5h.008v.008H18V10.5zm-3 0h.008v.008H15V10.5z" /></svg>
        Print…
      </button>
      <button type="button" role="menuitem" @click="run(menu.checkForUpdates)" :class="item">
        <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
        Check for updates…
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { serverHost, useWindowMenu } from '../composables/useWindowMenu'
import { useToasts } from '../composables/useToasts'

const props = defineProps({
  serverUrl: { type: String, default: '' },
  /** The router's full path, for the page's web address. */
  path: { type: String, default: '/' },
  version: { type: String, default: '' },
  copyText: { type: Function, required: true },
})

const item = 'w-full flex items-center gap-3 px-3 py-2 text-left hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors'
const round = 'w-7 h-7 rounded-full flex items-center justify-center hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'

const { notifySuccess, notifyError } = useToasts()
const menu = useWindowMenu({
  serverUrl: () => props.serverUrl,
  path: () => props.path,
  view: window.agentrq?.view,
  updates: window.agentrq?.updates,
  copyText: (text) => props.copyText(text),
  // The shell sends every window.open to the user's browser.
  openUrl: (url) => window.open(url, '_blank', 'noopener'),
  print: () => window.print(),
  notify: ({ tone, message, title }) => (tone === 'error' ? notifyError : notifySuccess)(message, title),
})

const host = computed(() => (props.serverUrl ? serverHost(props.serverUrl) : ''))

const open = ref(false)
const menuRef = ref(null)

function toggle() {
  open.value = !open.value
  if (open.value) menu.refresh()
}

/** An item closes the menu before it acts, so a print dialog does not show it. */
async function run(action) {
  open.value = false
  await nextTick()
  action()
}

function onDocumentClick(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) open.value = false
}
function onKeydown(e) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocumentClick)
  window.removeEventListener('keydown', onKeydown)
})
</script>
