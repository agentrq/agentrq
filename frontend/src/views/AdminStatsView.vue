<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <AdminLayout @sign-out="stop">
    <div class="flex flex-col w-full">
      <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
        <div class="flex flex-col min-w-0 flex-1">
          <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Stats</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
            Open MCP connections and machines online, per user. Refreshes every 10 seconds.
          </p>
        </div>
        <button data-test="admin-refresh" type="button" @click="refresh" :disabled="loading" title="Refresh" aria-label="Refresh"
          class="px-3 sm:px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0 disabled:opacity-50">
          <svg class="sm:hidden w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" />
          </svg>
          <span class="hidden sm:inline">Refresh</span>
        </button>
      </div>

      <div class="px-4 flex flex-col gap-8 pb-10">
        <p v-if="error" data-test="admin-error" class="text-xs text-red-600 dark:text-red-400">{{ error }}</p>

        <div v-if="!stats" class="py-8 text-center text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">
          Loading...
        </div>

        <template v-else>
          <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div v-for="card in cards" :key="card.label" data-test="admin-total"
              class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm transition-all duration-200">
              <div class="flex items-center gap-1.5">
                <span class="w-1.5 h-1.5 rounded-full" :class="card.dot"></span>
                <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">{{ card.label }}</span>
              </div>
              <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ card.value.toLocaleString() }}</span>
            </div>
          </div>

          <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
            <div class="flex items-center justify-between mb-6">
              <div class="flex items-center gap-2">
                <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
                <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">By User</h3>
              </div>
              <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Busiest First</div>
            </div>

            <div v-if="!stats.users.length" data-test="admin-empty" class="py-8 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm">
              <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">No activity</p>
            </div>

            <div v-else class="flex flex-col">
              <div class="flex items-center gap-4 px-2 pb-2 border-b border-gray-100 dark:border-zinc-800">
                <span class="flex-1 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">User</span>
                <span class="w-14 sm:w-20 text-right text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">MCP</span>
                <span class="w-14 sm:w-20 text-right text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Machines</span>
                <span class="hidden sm:block w-28 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Share</span>
              </div>
              <div v-for="row in stats.users" :key="row.userId" data-test="admin-row"
                class="flex items-center gap-4 px-2 py-3 border-b border-gray-100 dark:border-zinc-800/60">
                <span class="flex-1 min-w-0 flex items-center gap-2">
                  <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-zinc-800 dark:bg-[#aec477]"></span>
                  <span class="text-[11px] font-mono font-bold text-gray-800 dark:text-zinc-200 truncate">{{ row.userId }}</span>
                </span>
                <span class="w-14 sm:w-20 text-right text-[11px] font-black text-gray-900 dark:text-zinc-50 tabular-nums">{{ row.mcpConnections.toLocaleString() }}</span>
                <span class="w-14 sm:w-20 text-right text-[11px] font-black text-gray-900 dark:text-zinc-50 tabular-nums">{{ row.machines.toLocaleString() }}</span>
                <span class="hidden sm:block w-28">
                  <span class="block h-1.5 rounded-sm bg-gray-100 dark:bg-zinc-800 overflow-hidden">
                    <span class="block h-full rounded-sm bg-zinc-800 dark:bg-[#aec477]" :style="{ width: shareOf(row) }"></span>
                  </span>
                </span>
              </div>
            </div>
          </div>

          <p class="text-[11px] text-gray-400 dark:text-zinc-500">
            MCP connections are counted on the server instance that answered; machines online are counted across all of them.
            <span v-if="updatedAt" data-test="admin-updated">Updated {{ updatedAt.toLocaleTimeString() }}.</span>
          </p>
        </template>
      </div>
    </div>
  </AdminLayout>
</template>

<script setup>
/**
 * The operator's stats page: open MCP connections and machines online, per
 * user. Reached only by typing its address; nothing in the app links here.
 */
import { computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
import { useAdminStats } from '../composables/useAdminStats'

const router = useRouter()
const { stats, error, loading, updatedAt, start, stop, refresh } = useAdminStats({
  onSignedOut: () => router.replace('/admin/login'),
})

const cards = computed(() => [
  { label: 'Connections', value: stats.value.mcpConnections, dot: 'bg-zinc-800 dark:bg-[#aec477]' },
  { label: 'Machines', value: stats.value.machines, dot: 'bg-zinc-800 dark:bg-[#a8a3d9]' },
])

/** A row's share of all activity, MCP connections and machines together. */
function shareOf(row) {
  const total = stats.value.mcpConnections + stats.value.machines
  return `${Math.round(((row.mcpConnections + row.machines) / total) * 100)}%`
}

onMounted(start)
onUnmounted(stop)
</script>
