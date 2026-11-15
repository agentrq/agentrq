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
          <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Config</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
            The server's settings, kept in its database. A change takes effect when the server restarts.
            The database, the node id, the root access token and the encryption key stay in env vars.
          </p>
        </div>
      </div>

      <div class="px-4 flex flex-col gap-4 pb-10">
        <div v-if="error" data-test="config-error" role="alert"
          class="p-3 rounded-sm bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-xs text-red-700 dark:text-red-300 break-words">
          {{ error }}
        </div>

        <div v-if="blocks === null" class="py-8 text-center text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">
          Loading...
        </div>

        <div v-else-if="blocks.length" class="flex flex-col md:flex-row gap-4 md:gap-8 min-w-0">
          <!-- On a phone the tabs are a select, so nothing scrolls sideways. -->
          <select data-test="config-tab-select" :value="active" @change="active = $event.target.value" aria-label="Config section"
            class="md:hidden w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-2 text-xs font-semibold text-gray-900 dark:text-zinc-100 outline-none focus:border-gray-900 dark:focus:border-white">
            <optgroup v-for="g in groups" :key="g.name" :label="g.name">
              <option v-for="b in g.blocks" :key="b.key" :value="b.key">{{ title(b.key) }}{{ dirty(b.key) ? ' •' : '' }}</option>
            </optgroup>
          </select>

          <!-- The workspace settings' sidebar, in small categories. -->
          <nav role="tablist" aria-orientation="vertical" class="hidden md:flex flex-col gap-1 w-48 shrink-0 self-start sticky top-0">
            <template v-for="(g, i) in groups" :key="g.name">
              <p data-test="config-group-name" class="px-4 pb-1 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500" :class="i ? 'pt-4' : ''">{{ g.name }}</p>
              <button v-for="b in g.blocks" :key="b.key" type="button" role="tab" :data-test="`config-tab-${b.key}`"
                :aria-selected="active === b.key" @click="active = b.key"
                class="flex items-center justify-between gap-2 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left"
                :class="active === b.key
                  ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5'
                  : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'">
                <span class="truncate">{{ title(b.key) }}</span>
                <span v-if="dirty(b.key)" data-test="config-dirty" class="w-1.5 h-1.5 rounded-full bg-amber-500 shrink-0" aria-label="unsaved"></span>
              </button>
            </template>
          </nav>

          <section v-if="current" role="tabpanel" data-test="config-panel"
            class="flex-1 min-w-0 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm p-4 sm:p-8 flex flex-col gap-5 shadow-sm">
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0">
                <h2 class="text-sm font-black text-gray-900 dark:text-zinc-50">{{ title(current.key) }}</h2>
                <p class="text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ current.key }}</p>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <button type="button" data-test="config-reset" :disabled="busy || !dirty(current.key)" @click="reset(current.key)"
                  class="px-3 py-2 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-200 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-50">
                  Reset
                </button>
                <button type="button" data-test="config-save" :disabled="busy || !dirty(current.key)" @click="save(current.key)"
                  class="px-3 sm:px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50">
                  Save
                </button>
              </div>
            </div>
            <AdminConfigFields :fields="drafts[current.key]" :path="current.key" />
          </section>
        </div>
      </div>
    </div>
  </AdminLayout>
</template>

<script setup>
/**
 * The operator's config: one vertical tab per block of the server's config,
 * each a form built from the block as the database holds it.
 */
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
import AdminConfigFields from '../components/AdminConfigFields.vue'
import { useToasts } from '../composables/useToasts'
import { fetchAdminConfigurations, updateAdminConfiguration } from '../api'

const router = useRouter()
const { addToast } = useToasts()
const blocks = ref(null)
const drafts = reactive({})
const active = ref('')
const error = ref('')
const busy = ref(false)

/** The names a block is known by, where its key does not read as one. */
const TITLES = {
  acp: 'ACP', ddos: 'DDoS', mcp: 'MCP', pubsub: 'Pub/Sub', ratelimit: 'Rate limit',
  s3: 'S3', smtp: 'SMTP', ssl: 'SSL', webPush: 'Web push',
}

function title(key) {
  if (TITLES[key]) return TITLES[key]
  const words = key.replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/**
 * The sidebar's categories, in order. A block no category names, such as one
 * a later release adds, is listed under Other, so it is never left out.
 */
const CATEGORIES = [
  ['General', ['app', 'network', 'ssl']],
  ['Security', ['auth', 'ddos', 'ratelimit']],
  ['Messaging', ['smtp', 'slack', 'webPush']],
  ['Storage', ['storage', 's3', 'artifacts']],
  ['Features', ['skills', 'memory', 'pano', 'hooks', 'teams']],
  ['Protocols', ['mcp', 'acp', 'pubsub']],
]

const groups = computed(() => {
  const byKey = new Map(blocks.value.map((b) => [b.key, b]))
  const named = new Set(CATEGORIES.flatMap(([, keys]) => keys))
  const out = CATEGORIES.map(([name, keys]) => ({ name, blocks: keys.filter((k) => byKey.has(k)).map((k) => byKey.get(k)) }))
  out.push({ name: 'Other', blocks: blocks.value.filter((b) => !named.has(b.key)) })
  return out.filter((g) => g.blocks.length)
})

const current = computed(() => blocks.value?.find((b) => b.key === active.value))
const stored = (key) => blocks.value.find((b) => b.key === key).value
const dirty = (key) => JSON.stringify(drafts[key]) !== JSON.stringify(stored(key))
const copy = (v) => JSON.parse(JSON.stringify(v))

function reset(key) {
  drafts[key] = copy(stored(key))
}

/** Runs one call, saying what went wrong; false once the session has ended. */
async function act(fn) {
  busy.value = true
  error.value = ''
  try {
    const res = await fn()
    if (res === null) {
      router.replace('/admin/login')
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
  const list = await act(fetchAdminConfigurations)
  if (!list) {
    if (error.value) blocks.value = []
    return
  }
  for (const b of list) drafts[b.key] = copy(b.value)
  blocks.value = list
  active.value = groups.value[0]?.blocks[0].key ?? ''
}

async function save(key) {
  const saved = await act(() => updateAdminConfiguration(key, drafts[key]))
  if (!saved) return
  blocks.value = blocks.value.map((b) => (b.key === key ? saved : b))
  drafts[key] = copy(saved.value)
  addToast(`Saved ${title(key)}. It takes effect when the server restarts.`, 'success')
}

onMounted(load)
</script>
