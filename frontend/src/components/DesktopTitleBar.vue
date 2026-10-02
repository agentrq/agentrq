<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The macOS desktop window's title bar. The shell hides the native one, so
     this strip is the window's drag handle and the space the traffic lights sit
     in; it also carries what an installed web app's title bar does — the page's
     title, the profile the window belongs to, and a window menu. Everything clickable in it
     needs `app-no-drag`, or the drag region swallows the click. -->
<template>
  <div class="app-drag fixed top-0 inset-x-0 h-10 z-[210] flex items-center gap-3 pl-20 pr-3 select-none">
    <p class="min-w-0 flex-1 truncate text-xs font-semibold text-gray-600 dark:text-zinc-300" data-window-title :title="title">{{ title }}</p>

    <div v-if="user" ref="menuRef" class="app-no-drag relative shrink-0">
      <button type="button" @click="open = !open"
              :aria-expanded="open" aria-haspopup="true"
              :title="accountName"
              class="w-7 h-7 rounded-full bg-white dark:bg-zinc-900 flex items-center justify-center text-[11px] font-bold text-gray-700 dark:text-zinc-200 overflow-hidden hover:ring-2 hover:ring-gray-300 dark:hover:ring-zinc-700 transition-shadow outline-none focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600">
        <img v-if="user.picture" :src="user.picture" class="w-full h-full object-cover" alt="Profile" />
        <span v-else>{{ initial }}</span>
      </button>

      <div v-if="open" data-profile-card
           class="absolute right-0 top-full mt-2 w-72 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-md shadow-2xl p-2">
        <div class="flex flex-col items-center text-center gap-1 px-3 py-4 rounded-sm bg-gray-50 dark:bg-zinc-800/60">
          <div class="w-14 h-14 mb-1 rounded-full bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 flex items-center justify-center text-lg font-bold text-gray-700 dark:text-zinc-200 overflow-hidden">
            <img v-if="user.picture" :src="user.picture" class="w-full h-full object-cover" alt="" />
            <span v-else>{{ initial }}</span>
          </div>
          <p class="w-full text-sm font-bold text-gray-800 dark:text-zinc-100 truncate">
            {{ accountName }}<template v-if="activeProfile?.label"> · {{ activeProfile.label }}</template>
          </p>
          <p v-if="user.name && user.email" class="w-full text-xs font-medium text-gray-500 dark:text-zinc-400 truncate" :title="user.email">{{ user.email }}</p>
          <p v-if="activeProfile?.serverUrl" class="w-full text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate" :title="activeProfile.serverUrl">{{ activeProfile.serverUrl }}</p>
          <p v-if="duplicate" class="w-full text-[10px] font-bold text-amber-600 dark:text-amber-500 truncate">{{ duplicate }}</p>
        </div>

        <ProfileList v-if="profiles.length" class="px-1 pt-3 pb-1"
                     :profiles="profiles" :disabled="disabled"
                     @switch="choose('switch', $event)" @remove="choose('remove', $event)" @add="choose('add')" />
      </div>
    </div>

    <!-- The side panel, which the menu also opens with Cmd+\. -->
    <button type="button" data-side-panel-toggle @click="emit('toggle-side-panel')" :aria-pressed="sidePanelOpen"
            title="Side panel (⌘\)" aria-label="Side panel"
            class="app-no-drag shrink-0 w-7 h-7 rounded-full flex items-center justify-center text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-white transition-colors outline-none focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600">
      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 5h16a1 1 0 011 1v12a1 1 0 01-1 1H4a1 1 0 01-1-1V6a1 1 0 011-1zm11 0v14" />
      </svg>
    </button>
    <WindowMenu :server-url="activeProfile?.serverUrl || ''" :path="path" :version="version" :copy-text="copyText" />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import ProfileList from './ProfileList.vue'
import WindowMenu from './WindowMenu.vue'
import { duplicateNotice } from '../composables/useProfileDisplay'

const props = defineProps({
  title: { type: String, default: '' },
  /** The signed-in account; the profile button waits for it. */
  user: { type: Object, default: null },
  /** Every desktop profile, the active one included. */
  profiles: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
  /** For the window menu: the page's route, the app version and the clipboard. */
  path: { type: String, default: '/' },
  version: { type: String, default: '' },
  copyText: { type: Function, default: async () => {} },
  /** Whether the side panel is showing, for its button's pressed state. */
  sidePanelOpen: { type: Boolean, default: false },
})
const emit = defineEmits(['switch', 'remove', 'add', 'toggle-side-panel'])

const open = ref(false)
const menuRef = ref(null)

const activeProfile = computed(() => props.profiles.find((p) => p.active) ?? null)
const accountName = computed(() => props.user?.name || props.user?.email || 'Account')
const initial = computed(() => accountName.value.charAt(0).toUpperCase())
const duplicate = computed(() => (activeProfile.value ? duplicateNotice(activeProfile.value, props.profiles) : ''))

/** A choice made in the card closes it; removing asks for confirmation elsewhere. */
function choose(event, id) {
  open.value = false
  if (id === undefined) emit(event)
  else emit(event, id)
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
