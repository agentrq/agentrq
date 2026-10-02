<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div data-site-permissions class="absolute inset-0 flex flex-col bg-white dark:bg-zinc-900">
    <div class="shrink-0 flex items-center justify-between gap-3 px-4 py-3 border-b border-gray-100 dark:border-zinc-800">
      <div class="min-w-0">
        <h2 class="text-sm font-black text-gray-900 dark:text-white">Site permissions</h2>
        <p class="text-[11px] text-gray-500 dark:text-zinc-400">What sites in the panel may use. Kept on this computer only.</p>
      </div>
      <button type="button" data-site-permissions-close @click="$emit('close')"
              class="shrink-0 px-3 py-1.5 rounded-md border border-gray-200 dark:border-zinc-800 text-[11px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors">
        Done
      </button>
    </div>

    <div class="flex-1 min-h-0 overflow-y-auto custom-scrollbar px-4 py-3">
      <p v-if="!sites.length" data-site-permissions-empty class="py-10 text-center text-[11px] text-gray-500 dark:text-zinc-400">
        No site has asked for anything yet.
      </p>

      <section v-for="site in sites" :key="site.origin" data-site class="py-3 border-b border-gray-100 dark:border-zinc-800 last:border-b-0">
        <div class="flex items-center justify-between gap-3 mb-1.5">
          <p class="min-w-0 truncate text-xs font-semibold text-gray-800 dark:text-zinc-100" :title="site.origin">{{ siteLabel(site.origin) }}</p>
          <button type="button" data-site-remove-all @click="$emit('remove', site.origin)"
                  class="shrink-0 text-[11px] font-semibold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white transition-colors">
            Remove all
          </button>
        </div>
        <div v-for="entry in site.permissions" :key="entry.permission" data-site-permission
             class="flex items-center justify-between gap-3 py-1">
          <span class="min-w-0 truncate text-[11px] text-gray-600 dark:text-zinc-300">{{ permissionName(entry.permission) }}</span>
          <div class="shrink-0 flex items-center gap-2">
            <span :class="['px-1.5 py-0.5 rounded text-[10px] font-semibold', entry.decision === 'allow' ? 'bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-400' : 'bg-gray-100 text-gray-600 dark:bg-zinc-800 dark:text-zinc-400']">
              {{ entry.decision === 'allow' ? 'Allowed' : 'Blocked' }}
            </span>
            <button type="button" data-site-remove @click="$emit('remove', site.origin, entry.permission)"
                    class="text-[11px] font-semibold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white transition-colors">
              Remove
            </button>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
/**
 * The side panel's Site permissions list: every Allow and Block a person has
 * given a site in the panel, and the way to take one back. A removed decision
 * means the site asks again next time.
 */
import { permissionName, siteLabel } from '../composables/useSitePermissions'

defineProps({
  /** `[{ origin, permissions: [{ permission, decision }] }]`, from the shell. */
  sites: { type: Array, default: () => [] },
})
defineEmits(['remove', 'close'])
</script>
