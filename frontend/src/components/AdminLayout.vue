<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- The app's own shell — sidebar, then the page on its surface — for the
       operator's pages, which have no signed-in user to build App.vue's for.
       md:flex-row because App.vue's router-view adds flex-col to this root. -->
  <div class="flex md:flex-row h-full min-h-0 bg-white md:bg-zinc-100 dark:bg-zinc-950 text-gray-900 dark:text-zinc-100">
    <!-- On a phone, the app's own floating Menu button and drawer. -->
    <button data-test="admin-menu" type="button" @click.stop="menuOpen = true"
      class="md:hidden fixed bottom-3 left-1/2 -translate-x-1/2 px-4 py-1.5 bg-black/80 dark:bg-white/80 backdrop-blur-md text-white dark:text-black text-[10px] font-semibold rounded-full shadow-lg z-[60] border border-white/10 dark:border-black/10 transition-all active:scale-95 flex items-center justify-center gap-2">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
      </svg>
      Menu
    </button>
    <div v-if="menuOpen" data-test="admin-menu-overlay" @click="menuOpen = false" class="md:hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90]"></div>

    <nav :class="[
        menuOpen ? 'flex w-[280px] shadow-2xl' : 'hidden w-64',
        'md:flex md:w-64 md:shadow-none fixed inset-y-0 left-0 z-[100] md:relative bg-white md:bg-zinc-100 dark:bg-zinc-950 shrink-0 flex-col h-full p-4',
      ]">
      <div class="flex items-center gap-2.5 p-1 pb-1">
        <div class="w-8 h-8 flex items-center justify-center shrink-0">
          <svg viewBox="0 0 24 24" class="w-6 h-6 text-gray-700 dark:text-zinc-100" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
            <path d="M12 7l-3.5 8" />
            <path d="M12 7l3.5 8" />
            <path d="M9.5 12h5" />
          </svg>
        </div>
        <span class="text-sm font-bold text-gray-800 dark:text-zinc-200">AgentRQ</span>
      </div>

      <div class="flex-1 min-h-0 overflow-y-auto px-2 mt-5 space-y-0.5">
        <div class="p-2">
          <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Admin</span>
        </div>
        <router-link v-for="item in ADMIN_PAGES" :key="item.to" :to="item.to" @click="menuOpen = false"
          class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
          :class="route.path === item.to
            ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white'
            : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'">
          <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
          </svg>
          <span>{{ item.label }}</span>
        </router-link>
      </div>

      <button data-test="admin-logout" type="button" @click="signOut"
        class="mx-2 flex items-center gap-2.5 px-2 py-1.5 text-xs rounded-md text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 transition-all duration-150">
        <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75" />
        </svg>
        <span>Sign out</span>
      </button>
    </nav>

    <main class="grow min-w-0 min-h-0 flex flex-col md:py-4 md:pr-4">
      <div class="h-full overflow-y-auto min-w-0 md:rounded-sm page-surface md:border border-gray-200 dark:border-zinc-800 no-scrollbar">
        <div class="px-2 pt-4 pb-6 md:p-4 h-full flex flex-col">
          <slot />
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { adminLogout } from '../api'

/** The operator's pages, in sidebar order. Linked from nowhere else. */
const ADMIN_PAGES = [
  { to: '/admin/stats', label: 'Stats', icon: 'M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z' },
]

const route = useRoute()
const menuOpen = ref(false)
const router = useRouter()

const emit = defineEmits(['sign-out'])

async function signOut() {
  emit('sign-out')
  await adminLogout()
  router.replace('/admin/login')
}
</script>
