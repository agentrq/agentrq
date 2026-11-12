<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * One registration: its URL, its inputs and routes, and the log of the
 * requests to it. The token is in the URL only right after it is made: the
 * server keeps a hash of it.
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  deleteHookRegistration, fetchAvailableHooks, fetchEvents, fetchHookDeliveries, getHookRegistration,
  replayHookDelivery, rotateHookToken, updateHookRegistration,
} from '../api'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import DeleteModal from '../components/DeleteModal.vue'
import HookRegistrationForm from '../components/HookRegistrationForm.vue'
import LoadingState from '../components/LoadingState.vue'
import { statusStyle, takeTokenUrl } from '../utils/hooks'

const PAGE = 50

const route = useRoute()
const router = useRouter()
const { notifyError, notifySuccess } = useToasts()
const id = route.params.id

const reg = ref(null)
const hooks = ref([])
const events = ref([])
const deliveries = ref([])
const more = ref(false)
const filter = ref('all')
const tokenUrl = ref(takeTokenUrl(id))
const loading = ref(true)
const busy = ref(false)
const editing = ref(false)
const confirmRotate = ref(false)
const confirmDelete = ref(false)

async function loadDeliveries() {
  const page = await fetchHookDeliveries(id, { limit: PAGE })
  deliveries.value = page.deliveries ?? []
  more.value = deliveries.value.length === PAGE
}

async function load({ quiet = false } = {}) {
  if (!quiet) loading.value = true
  try {
    const [r, available, list] = await Promise.all([getHookRegistration(id), fetchAvailableHooks(), fetchEvents()])
    reg.value = r
    hooks.value = available.hooks ?? []
    events.value = list.events ?? []
    await loadDeliveries()
  } catch (e) {
    notifyError(e.message)
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  busy.value = true
  try {
    const page = await fetchHookDeliveries(id, { before: deliveries.value.at(-1).id, limit: PAGE })
    const next = page.deliveries ?? []
    deliveries.value = [...deliveries.value, ...next]
    more.value = next.length === PAGE
  } catch (e) {
    notifyError(e.message)
  } finally {
    busy.value = false
  }
}

/** Runs one change, showing what went wrong; its answer, or null. */
async function act(fn) {
  busy.value = true
  try {
    return await fn()
  } catch (e) {
    notifyError(e.message)
    return null
  } finally {
    busy.value = false
  }
}

async function toggle() {
  const updated = await act(() => updateHookRegistration(id, { enabled: !reg.value.enabled }))
  if (updated) reg.value = updated
}

async function save(fields) {
  const updated = await act(() => updateHookRegistration(id, fields))
  if (!updated) return
  reg.value = updated
  editing.value = false
  notifySuccess('Saved.')
}

async function rotate() {
  confirmRotate.value = false
  const updated = await act(() => rotateHookToken(id))
  if (!updated) return
  reg.value = updated
  tokenUrl.value = updated.urlWithToken
  notifySuccess('New token made. The old URL no longer works.')
}

async function remove() {
  confirmDelete.value = false
  if (await act(() => deleteHookRegistration(id))) {
    notifySuccess(`Deleted ${reg.value.name}.`)
    router.push('/events/hooks')
  }
}

async function replay(d) {
  if (!(await act(() => replayHookDelivery(id, d.id)))) return
  notifySuccess('Queued again.')
  await act(loadDeliveries)
}

async function copy() {
  try {
    await navigator.clipboard.writeText(tokenUrl.value || reg.value.url)
    notifySuccess('Copied.')
  } catch {
    notifyError('Could not copy; select the URL instead.')
  }
}

const hook = computed(() => hooks.value.find((h) => h.id === reg.value?.hookId) || null)
const eventName = (eventId) => events.value.find((e) => e.id === eventId)?.name || eventId
const routes = computed(() => Object.entries(reg.value.routes ?? {}).map(([from, to]) => ({ from, to: eventName(to) })))
const inputRows = computed(() => (hook.value?.inputs ?? []).map((input) => {
  if (input.secret) return { name: input.label || input.name, value: reg.value.secretsSet?.includes(input.name) ? '•••••••• set' : 'not set' }
  const v = reg.value.inputs?.[input.name]
  return { name: input.label || input.name, value: v === undefined || v === '' ? '—' : Array.isArray(v) ? v.join(', ') : String(v) }
}))
const counts = computed(() => ({
  failed: deliveries.value.filter((d) => d.status === 'failed').length,
  rejected: deliveries.value.filter((d) => d.status === 'rejected').length,
}))
const shown = computed(() => filter.value === 'all' ? deliveries.value : deliveries.value.filter((d) => d.status === filter.value))

function when(iso) {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

onMounted(load)
onWebMCPChange(() => load({ quiet: true }))

const card = 'bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-xl p-4'
const heading = 'text-[11px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest'
const small = 'px-2 py-1 text-[11px] font-bold rounded-md border border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-200 hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-50'
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <LoadingState v-if="loading" class="py-8" label="Loading the registration…" />
    <div v-else-if="!reg" data-test="reg-missing" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-zinc-400">
      This registration could not be loaded. <RouterLink to="/events/hooks" class="underline">Back to hooks</RouterLink>
    </div>
    <template v-else>
      <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-row items-start justify-between gap-4">
        <div class="min-w-0 flex-1">
          <p class="text-[11px] text-gray-400 dark:text-zinc-500">
            <RouterLink to="/events" class="hover:underline">Events</RouterLink> /
            <RouterLink to="/events/hooks" class="hover:underline">Hooks</RouterLink>
          </p>
          <h1 data-test="reg-title" class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">{{ reg.name }}</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
            <span class="font-mono">{{ reg.hookName }}</span><template v-if="hook">{{ ` ${hook.version} · ${hook.builtIn ? 'built-in' : 'yours'}` }}</template>
            · id <span class="font-mono">{{ reg.id }}</span>
            <span v-if="!reg.hookAvailable" class="text-red-600 dark:text-red-400"> · its hook is gone or turned off</span>
          </p>
        </div>
        <button type="button" role="switch" data-test="reg-enabled" :aria-checked="reg.enabled" :disabled="busy" @click="toggle"
          class="flex items-center gap-2 shrink-0 text-[11px] font-bold text-gray-600 dark:text-zinc-300 disabled:opacity-50">
          {{ reg.enabled ? 'Enabled' : 'Off' }}
          <span class="relative w-9 h-5 rounded-full transition-colors" :class="reg.enabled ? 'bg-black dark:bg-white' : 'bg-gray-200 dark:bg-zinc-700'">
            <span class="absolute top-0.5 w-4 h-4 rounded-full bg-white dark:bg-black transition-all" :class="reg.enabled ? 'right-0.5' : 'left-0.5 dark:bg-zinc-400'"></span>
          </span>
        </button>
      </div>

      <div class="px-4 pb-10 space-y-4 max-w-4xl">
        <section :class="card" class="space-y-2">
          <p :class="heading">Payload URL</p>
          <div class="flex flex-col sm:flex-row gap-2">
            <code data-test="reg-url" class="flex-1 min-w-0 px-3 py-2 rounded-lg bg-gray-50 dark:bg-zinc-800 text-xs text-gray-800 dark:text-zinc-100 break-all select-all">{{ tokenUrl || reg.url }}</code>
            <div class="flex gap-2 shrink-0">
              <button data-test="reg-copy" type="button" @click="copy"
                class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80">Copy</button>
              <button data-test="reg-rotate" type="button" :disabled="busy" @click="confirmRotate = true"
                class="px-3 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 disabled:opacity-50">Rotate token</button>
            </div>
          </div>
          <p v-if="tokenUrl" data-test="reg-token-once" class="text-[11px] text-amber-700 dark:text-amber-300">
            Copy it now: the token is shown only this once. Give the provider this URL and the secret you set.
          </p>
          <p v-else class="text-[11px] text-gray-500 dark:text-zinc-400">
            The token was shown once, when it was made. Lost it? Rotate the token for a new URL.
          </p>
        </section>

        <HookRegistrationForm v-if="editing" :hooks="hooks" :events="events" :registration="reg" :busy="busy"
          @submit="save" @cancel="editing = false" />
        <section v-else :class="card" class="grid gap-4 sm:grid-cols-2">
          <div class="space-y-1.5" data-test="reg-inputs">
            <div class="flex items-center justify-between">
              <p :class="heading">Inputs</p>
              <button v-if="hook" data-test="reg-edit" type="button" :class="small" @click="editing = true">Edit</button>
            </div>
            <p v-for="row in inputRows" :key="row.name" class="text-xs flex gap-2">
              <span class="text-gray-500 dark:text-zinc-400 min-w-[7rem]">{{ row.name }}</span>
              <span class="font-mono text-gray-800 dark:text-zinc-200 break-all">{{ row.value }}</span>
            </p>
          </div>
          <div class="space-y-1.5" data-test="reg-routes">
            <p :class="heading">Routes</p>
            <p v-if="!routes.length" class="text-xs text-gray-500 dark:text-zinc-400">Nothing routed: requests are logged but fire no event.</p>
            <p v-for="r in routes" :key="r.from" class="text-xs font-mono text-gray-800 dark:text-zinc-200">
              {{ r.from }} <span class="text-gray-400">→</span> {{ r.to }}
              <span v-if="reg.allowAllCommandsEvents?.includes(r.from)" class="font-sans text-amber-600 dark:text-amber-300" title="An event with a trigger that runs every command">⚠</span>
            </p>
          </div>
        </section>

        <section :class="card" class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p :class="heading">Deliveries</p>
            <div class="flex gap-1" role="group" aria-label="Show">
              <button v-for="f in [['all', 'All'], ['failed', `Failed ${counts.failed}`], ['rejected', `Rejected ${counts.rejected}`]]" :key="f[0]"
                type="button" :data-test="`reg-filter-${f[0]}`" :aria-pressed="filter === f[0]" @click="filter = f[0]"
                class="px-2 py-1 text-[11px] font-bold rounded-md"
                :class="filter === f[0] ? 'bg-black text-white dark:bg-white dark:text-black' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'">{{ f[1] }}</button>
            </div>
          </div>
          <p v-if="!shown.length" data-test="reg-no-deliveries" class="text-xs text-gray-500 dark:text-zinc-400 py-4 text-center">
            {{ deliveries.length ? 'None of these.' : 'No requests yet. Give the provider the URL above.' }}
          </p>
          <div v-for="d in shown" :key="d.id" data-test="reg-delivery"
            class="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 border-t border-gray-100 dark:border-zinc-800 first:border-t-0">
            <span class="px-1.5 py-0.5 rounded text-[10px] font-black uppercase tracking-widest" :class="statusStyle(d.status)">{{ d.status }}</span>
            <span class="order-last basis-full sm:order-none sm:basis-0 sm:flex-1 min-w-0 text-xs text-gray-700 dark:text-zinc-300 break-words">
              <span v-if="d.eventNames?.length" class="font-mono">{{ d.eventNames.join(', ') }}</span>
              <span v-if="d.error" class="text-gray-500 dark:text-zinc-400"><template v-if="d.eventNames?.length"> · </template>{{ d.error }}</span>
              <span v-if="d.attempts > 1" class="text-gray-400 dark:text-zinc-500"> · {{ d.attempts }} attempts</span>
            </span>
            <span class="flex-1 sm:flex-none text-[11px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ when(d.receivedAt) }}<template v-if="d.durationMs"> · {{ d.durationMs }} ms</template></span>
            <button v-if="d.replayable" data-test="reg-replay" type="button" :disabled="busy" :class="small" @click="replay(d)">Replay</button>
          </div>
          <div class="flex items-center justify-between gap-3 pt-1">
            <p class="text-[11px] text-gray-400 dark:text-zinc-500">Your server keeps deliveries for a while, then deletes them. Only failed ones can be replayed.</p>
            <button v-if="more" data-test="reg-more" type="button" :disabled="busy" :class="small" class="shrink-0" @click="loadMore">Load more</button>
          </div>
        </section>

        <button data-test="reg-delete" type="button" :disabled="busy" @click="confirmDelete = true"
          class="px-3 py-2 text-[11px] font-black uppercase tracking-widest rounded-lg border border-gray-200 dark:border-zinc-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10 disabled:opacity-50">
          Delete registration
        </button>
      </div>

      <DeleteModal :show="confirmRotate" title="Rotate the hook token?" confirm-label="Rotate"
        message="The current URL stops working at once. Until you give the provider the new URL, its requests get 404 and are not saved."
        @close="confirmRotate = false" @confirm="rotate" />
      <DeleteModal :show="confirmDelete" title="Delete registration"
        :message="`Delete ${reg.name}? Its URL stops working and its delivery log is deleted.`"
        @close="confirmDelete = false" @confirm="remove" />
    </template>
  </div>
</template>
