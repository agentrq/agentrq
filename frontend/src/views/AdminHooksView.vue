<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <AdminLayout>
    <div class="flex flex-col w-full">
      <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
        <div class="flex flex-col min-w-0 flex-1">
          <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Built-in hooks</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
            WebAssembly hooks every account can register, to turn requests from outside services into its events.
          </p>
        </div>
        <button data-test="hook-add" type="button" @click="pick('')" :disabled="busy"
          class="px-3 sm:px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 shrink-0 disabled:opacity-50">
          + Add hook
        </button>
        <input ref="fileInput" data-test="hook-file" type="file" accept=".wasm,application/wasm" class="hidden" @change="upload" />
      </div>

      <div class="px-4 flex flex-col gap-4 pb-10">
        <div v-if="error" data-test="hook-error" role="alert"
          class="p-3 rounded-sm bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-xs text-red-700 dark:text-red-300 break-words">
          {{ error }}
        </div>

        <div v-if="hooks === null" class="py-8 text-center text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">
          Loading...
        </div>

        <div v-else-if="!hooks.length" data-test="hook-empty" class="py-10 px-4 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm">
          <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">No built-in hooks</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-2">
            Add the <span class="font-mono">agentrq-hook-*.wasm</span> files attached to the server's release.
          </p>
        </div>

        <div v-for="hook in hooks || []" :key="hook.id" data-test="hook-row"
          class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 rounded-sm p-4 flex flex-col gap-3 shadow-sm">
          <div class="flex flex-wrap items-start gap-3">
            <div class="flex-1 min-w-[180px]">
              <p class="text-sm font-bold text-gray-900 dark:text-zinc-50">
                <span class="font-mono">{{ hook.name }}</span>
                <span class="ml-1 text-xs font-normal text-gray-500 dark:text-zinc-400">{{ hook.version }}</span>
                <span v-if="!hook.enabled" class="ml-2 align-middle text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Off</span>
              </p>
              <p v-if="hook.description" class="text-xs text-gray-600 dark:text-zinc-300 mt-0.5">{{ hook.description }}</p>
              <p data-test="hook-facts" class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1">
                {{ count(hook.events.length, 'event') }} · {{ count(hook.inputs.length, 'input') }} ·
                {{ size(hook.sizeBytes) }} ·
                <span class="font-mono" :title="hook.sha256">sha256 {{ short(hook.sha256) }}</span>
              </p>
            </div>
            <div class="flex items-center gap-2 shrink-0">
              <button type="button" role="switch" data-test="hook-enabled" :aria-checked="hook.enabled"
                :aria-label="`${hook.enabled ? 'Turn off' : 'Turn on'} ${hook.name}`" :disabled="busy" @click="toggle(hook)"
                class="relative w-9 h-5 rounded-full transition-colors disabled:opacity-50"
                :class="hook.enabled ? 'bg-black dark:bg-white' : 'bg-gray-200 dark:bg-zinc-700'">
                <span class="absolute top-0.5 w-4 h-4 rounded-full bg-white dark:bg-black transition-all"
                  :class="hook.enabled ? 'right-0.5' : 'left-0.5 dark:bg-zinc-400'"></span>
              </button>
              <button type="button" data-test="hook-upgrade" :disabled="busy" @click="pick(hook.id)"
                class="px-2 py-1 text-[11px] font-bold rounded-md border border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-200 hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-50">
                Upgrade
              </button>
              <button type="button" data-test="hook-delete" :disabled="busy" @click="deleting = hook"
                class="px-2 py-1 text-[11px] font-bold rounded-md border border-gray-200 dark:border-zinc-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 disabled:opacity-50">
                Delete
              </button>
            </div>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <span v-for="event in hook.events" :key="event.name" :title="event.description"
              class="px-1.5 py-0.5 rounded-sm bg-gray-100 dark:bg-zinc-800 text-[10px] font-mono text-gray-700 dark:text-zinc-300">{{ event.name }}</span>
          </div>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400">
            Inputs:
            <template v-for="(input, i) in hook.inputs" :key="input.name">
              <span class="font-mono text-gray-700 dark:text-zinc-300">{{ input.name }}</span><span v-if="input.secret || input.required"> ({{ [input.secret && 'secret', input.required && 'required'].filter(Boolean).join(', ') }})</span><span v-if="i < hook.inputs.length - 1">, </span>
            </template>
          </p>
        </div>

        <p class="text-[11px] text-gray-400 dark:text-zinc-500">
          An upgrade keeps the hook's id, so every registration follows it. One that would break a registration is refused.
          A hook turned off stops accepting requests; deleting one is refused while anything registers it.
        </p>
      </div>
    </div>
    <DeleteModal :show="!!deleting" title="Delete hook"
      :message="deleting ? `Delete the hook ${deleting.name}? Accounts can no longer register it. This is refused while anything registers it.` : ''"
      @close="deleting = null" @confirm="remove" />
  </AdminLayout>
</template>

<script setup>
/**
 * The operator's built-in hooks: add one from its .wasm file, upgrade it,
 * turn it off for every account, or delete it.
 */
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
import DeleteModal from '../components/DeleteModal.vue'
import { useToasts } from '../composables/useToasts'
import { deleteAdminHook, fetchAdminHooks, setAdminHookEnabled, uploadAdminHook } from '../api'

const router = useRouter()
const hooks = ref(null)
const error = ref('')
const deleting = ref(null)
const { addToast } = useToasts()
const busy = ref(false)
const fileInput = ref(null)
// The hook the next picked file upgrades; '' adds a new one.
let upgrading = ''

const signedOut = () => router.replace('/admin/login')

/** Runs one call, saying what went wrong; false once the session has ended. */
async function act(fn) {
  busy.value = true
  error.value = ''
  try {
    const res = await fn()
    if (res === null || res === false) {
      signedOut()
      return false
    }
    return res
  } catch (err) {
    error.value = err.message
    return false
  } finally {
    busy.value = false
  }
}

async function load() {
  const list = await act(fetchAdminHooks)
  if (list) hooks.value = list
  else if (hooks.value === null && error.value) hooks.value = []
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
  const hook = await act(() => uploadAdminHook(file, id))
  if (!hook) return
  await load()
  addToast(id ? `Upgraded ${hook.name} to ${hook.version}.` : `Added ${hook.name} ${hook.version}.`, 'success')
}

async function toggle(hook) {
  const updated = await act(() => setAdminHookEnabled(hook.id, !hook.enabled))
  if (updated) hooks.value = hooks.value.map((h) => (h.id === updated.id ? updated : h))
}

async function remove() {
  const hook = deleting.value
  deleting.value = null
  if (await act(() => deleteAdminHook(hook.id))) {
    hooks.value = hooks.value.filter((h) => h.id !== hook.id)
    addToast(`Deleted ${hook.name}.`, 'success')
  }
}

function count(n, noun) {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

function size(bytes) {
  return bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`
}

function short(sum) {
  return `${sum.slice(0, 4)}…${sum.slice(-4)}`
}

onMounted(load)
</script>
