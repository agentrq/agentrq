<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * The Events page's Hooks tab: the account's registrations, each a URL an
 * outside service calls, and, when the server allows it, the account's own
 * hooks.
 */
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { deleteHook, fetchAvailableHooks, fetchHookRegistrations, uploadHook } from '../api'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import DeleteModal from '../components/DeleteModal.vue'
import EventsTabs from '../components/EventsTabs.vue'
import LoadingState from '../components/LoadingState.vue'
import { plural } from '../utils/hooks'

const router = useRouter()
const { notifyError, notifySuccess } = useToasts()

const registrations = ref([])
const ownHooks = ref([])
const allowUserHooks = ref(false)
const loading = ref(true)
const busy = ref(false)
const deleting = ref(null)
const fileInput = ref(null)
// The hook the next picked file upgrades; '' adds a new one.
let upgrading = ''

async function load({ quiet = false } = {}) {
  if (!quiet) loading.value = true
  try {
    const [regs, available] = await Promise.all([fetchHookRegistrations(), fetchAvailableHooks()])
    registrations.value = regs.registrations ?? []
    allowUserHooks.value = !!available.allowUserHooks
    ownHooks.value = (available.hooks ?? []).filter((h) => !h.builtIn)
  } catch (e) {
    notifyError(e.message)
  } finally {
    loading.value = false
  }
}

function pick(id) {
  upgrading = id
  fileInput.value.value = ''
  fileInput.value.click()
}

async function upload(e) {
  const file = e.target.files?.[0]
  if (!file) return
  const id = upgrading
  busy.value = true
  try {
    const hook = await uploadHook(file, id)
    notifySuccess(id ? `Upgraded ${hook.name} to ${hook.version}.` : `Added ${hook.name} ${hook.version}.`)
    await load({ quiet: true })
  } catch (err) {
    notifyError(err.message)
  } finally {
    busy.value = false
  }
}

async function remove() {
  const hook = deleting.value
  deleting.value = null
  try {
    await deleteHook(hook.id)
    ownHooks.value = ownHooks.value.filter((h) => h.id !== hook.id)
    notifySuccess(`Deleted ${hook.name}.`)
  } catch (err) {
    notifyError(err.message)
  }
}

const routeCount = (reg) => Object.keys(reg.routes ?? {}).length

onMounted(load)
onWebMCPChange(() => load({ quiet: true }))
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Events</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Outside services call a hook's URL; the hook turns the request into events.</p>
      </div>
      <button data-test="reg-new" type="button" @click="router.push('/events/hooks/new')"
        class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0">
        + New<span class="hidden sm:inline"> registration</span>
      </button>
    </div>

    <EventsTabs active="hooks" />

    <div class="px-4 space-y-6 pb-10">
      <div v-if="loading" class="py-8">
        <LoadingState label="Loading hooks…" />
      </div>

      <template v-else>
        <div v-if="!registrations.length" data-test="reg-empty"
          class="py-12 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-center">
          <p class="text-sm font-semibold text-gray-600 dark:text-zinc-300 mb-1">No hooks registered</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400">Register a hook to let GitHub, your CI or any service fire your events.</p>
          <button type="button" data-test="reg-empty-new" @click="router.push('/events/hooks/new')"
            class="mt-4 px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95">
            + New registration
          </button>
        </div>

        <div v-else class="space-y-2">
          <RouterLink v-for="reg in registrations" :key="reg.id" :to="`/events/hooks/${reg.id}`" data-test="reg-row"
            class="flex items-center gap-4 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl hover:border-gray-200 dark:hover:border-zinc-700 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/20">
            <div aria-hidden="true" class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg bg-gray-50 dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700 text-[10px] font-black uppercase text-gray-500 dark:text-zinc-400">
              {{ reg.hookName.slice(0, 2) }}
            </div>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-bold text-gray-800 dark:text-zinc-200 truncate">
                {{ reg.name }}
                <span v-if="reg.allowAllCommandsEvents?.length" data-test="reg-risky"
                  title="Routes to an event with a trigger that runs every command"
                  class="ml-1 inline-flex items-center gap-1 h-5 align-middle px-1.5 rounded text-[9px] font-black uppercase tracking-widest leading-none bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300">
                  <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495ZM10 5a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 10 5Zm0 9a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" clip-rule="evenodd" /></svg>
                  Runs all commands
                </span>
              </p>
              <p class="text-xs text-gray-500 dark:text-zinc-400 truncate mt-0.5">
                <span class="font-mono">{{ reg.hookName }}</span> · {{ plural(routeCount(reg), 'route') }}
                <span v-if="!reg.hookAvailable" class="text-red-600 dark:text-red-400"> · hook unavailable</span>
              </p>
            </div>
            <span v-if="!reg.enabled" class="shrink-0 inline-flex items-center h-5 px-1.5 rounded bg-gray-100 dark:bg-zinc-800 text-[9px] font-black uppercase tracking-widest leading-none text-gray-500 dark:text-zinc-400">Off</span>
            <svg class="w-4 h-4 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
          </RouterLink>
        </div>

        <section v-if="allowUserHooks" data-test="own-hooks" class="space-y-2">
          <div class="flex items-center justify-between gap-3">
            <div>
              <h2 class="text-sm font-bold text-gray-800 dark:text-zinc-200">Your hooks</h2>
              <p class="text-[11px] text-gray-500 dark:text-zinc-400">WebAssembly modules you added. Each must check a required secret.</p>
            </div>
            <button data-test="hook-upload" type="button" :disabled="busy" @click="pick('')"
              class="px-3 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 disabled:opacity-50 shrink-0">
              {{ busy ? 'Uploading…' : 'Upload hook' }}
            </button>
            <input ref="fileInput" data-test="hook-file" type="file" accept=".wasm,application/wasm" class="hidden" @change="upload" />
          </div>
          <div v-for="h in ownHooks" :key="h.id" data-test="own-hook"
            class="flex items-center gap-3 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl">
            <div class="flex-1 min-w-0">
              <p class="text-sm font-bold font-mono text-gray-800 dark:text-zinc-200 truncate">{{ h.name }} <span class="font-sans font-normal text-xs text-gray-500 dark:text-zinc-400">{{ h.version }}</span></p>
              <p class="text-xs text-gray-500 dark:text-zinc-400 truncate">{{ plural(h.events.length, 'event') }} · {{ plural(h.inputs.length, 'input') }}</p>
            </div>
            <button data-test="own-hook-upgrade" type="button" :aria-label="`Upgrade ${h.name}`" :disabled="busy" @click="pick(h.id)"
              class="px-2 py-1 text-[11px] font-bold rounded-md border border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-200 hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-50">Upgrade</button>
            <button data-test="own-hook-delete" type="button" :aria-label="`Delete ${h.name}`" :disabled="busy" @click="deleting = h"
              class="px-2 py-1 text-[11px] font-bold rounded-md border border-gray-200 dark:border-zinc-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 disabled:opacity-50">Delete</button>
          </div>
        </section>
      </template>
    </div>

    <DeleteModal :show="!!deleting" title="Delete hook"
      :message="deleting ? `Delete the hook ${deleting.name}? This is refused while a registration uses it.` : ''"
      @close="deleting = null" @confirm="remove" />
  </div>
</template>
