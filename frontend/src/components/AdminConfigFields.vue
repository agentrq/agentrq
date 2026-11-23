<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <!-- A container, so a row stacks wherever it is narrow: on a phone, or in a group's card. -->
  <div class="@container divide-y divide-gray-100 dark:divide-zinc-800">
    <!-- The URLs this block's app is registered with, at the provider. -->
    <div v-if="urls.length" data-test="config-urls" class="flex flex-col gap-3 py-5 first:pt-0 last:pb-0">
      <div v-for="u in urls" :key="u.label" data-test="config-callback-url"
        class="rounded-md border border-gray-200 dark:border-white/10 bg-white dark:bg-white/5 px-3 py-2.5">
        <p class="text-xs/5 font-semibold text-gray-900 dark:text-white">{{ u.label }}</p>
        <p class="text-xs/5 text-gray-500 dark:text-zinc-400">{{ u.hint }}</p>
        <div class="mt-1.5 flex items-center gap-2">
          <code class="min-w-0 flex-1 break-all font-mono text-xs/5 text-gray-900 dark:text-zinc-100">{{ u.url }}</code>
          <button type="button" data-test="config-callback-copy" :disabled="u.url.includes('<id>')" @click="copy(u)"
            class="shrink-0 rounded-lg border border-gray-200 dark:border-white/10 px-3 py-1.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-white hover:bg-gray-50 dark:hover:bg-white/10 disabled:opacity-50">
            Copy
          </button>
        </div>
      </div>
    </div>
    <template v-for="key in orderedKeys" :key="key">
      <fieldset v-if="isGroup(fields[key])" data-test="config-group" class="py-6 first:pt-0 last:pb-0">
        <legend class="float-left w-full">
          <span class="text-sm/6 font-semibold text-gray-900 dark:text-white">{{ label(key) }}</span>
          <span class="ml-2 text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ path }}.{{ key }}</span>
        </legend>
        <p v-if="help(key)" data-test="config-help" class="clear-both text-xs/5 text-gray-500 dark:text-zinc-400">{{ help(key) }}</p>
        <div class="clear-both mt-4 rounded-lg border border-gray-200 dark:border-zinc-800 bg-gray-50/50 dark:bg-white/[0.02] px-4 py-5 sm:px-6">
          <AdminConfigFields :fields="fields[key]" :original="original[key]" :path="`${path}.${key}`" />
        </div>
      </fieldset>

      <fieldset v-else-if="isList(key, fields[key])" data-test="config-list" class="py-6 first:pt-0 last:pb-0">
        <legend class="float-left w-full">
          <span class="text-sm/6 font-semibold text-gray-900 dark:text-white">{{ label(key) }}</span>
          <span class="ml-2 text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ path }}.{{ key }}</span>
        </legend>
        <p v-if="help(key)" data-test="config-help" class="clear-both text-xs/5 text-gray-500 dark:text-zinc-400">{{ help(key) }}</p>
        <div class="clear-both mt-4 flex flex-col gap-4">
          <div v-for="(item, i) in fields[key]" :key="i" data-test="config-list-item"
            class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-gray-50/50 dark:bg-white/[0.02] px-4 py-5 sm:px-6">
            <div class="mb-5 flex items-center justify-between gap-4">
              <span class="min-w-0 truncate text-sm/6 font-semibold text-gray-900 dark:text-white">{{ item.name || `New ${list(key).noun}` }}</span>
              <button type="button" data-test="config-list-remove" @click="fields[key].splice(i, 1)"
                class="shrink-0 rounded-lg border border-gray-200 dark:border-white/10 bg-white dark:bg-white/5 px-3 py-1.5 text-[11px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10">
                Remove
              </button>
            </div>
            <AdminConfigFields :fields="item" :original="storedItem(key, item)" :path="`${path}.${key}.${i}`" />
          </div>
          <button type="button" data-test="config-list-add" @click="fields[key].push(list(key).item())"
            class="self-start rounded-lg border border-dashed border-gray-300 dark:border-white/15 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-white hover:bg-gray-50 dark:hover:bg-white/5">
            Add {{ list(key).noun }}
          </button>
        </div>
      </fieldset>

      <div v-else :class="ROW">
        <div class="min-w-0">
          <label :for="inputId(key)" class="block text-sm/6 font-semibold text-gray-900 dark:text-white">{{ label(key) }}</label>
          <p class="text-[10px] font-mono text-gray-400 dark:text-zinc-500 break-all">{{ path }}.{{ key }}</p>
          <p v-if="help(key)" :id="helpId(key)" data-test="config-help" class="mt-1 text-xs/5 text-gray-500 dark:text-zinc-400">{{ help(key) }}</p>
        </div>

        <div v-if="typeof fields[key] === 'boolean'" class="@xl:pt-0.5">
          <button :id="inputId(key)" type="button" role="switch" :data-test="`config-${path}.${key}`" :aria-checked="fields[key]"
            :aria-describedby="help(key) ? helpId(key) : undefined" @click="fields[key] = !fields[key]"
            class="relative inline-flex w-11 shrink-0 rounded-full p-0.5 transition-colors duration-200 ease-in-out focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-gray-900 dark:focus-visible:outline-white"
            :class="fields[key] ? 'bg-gray-900 dark:bg-white' : 'bg-gray-200 dark:bg-white/10'">
            <span aria-hidden="true" class="size-5 rounded-full bg-white shadow-xs ring-1 ring-gray-900/5 transition-transform duration-200 ease-in-out"
              :class="fields[key] ? 'translate-x-5 dark:bg-zinc-900' : 'translate-x-0 dark:bg-zinc-400'"></span>
          </button>
        </div>

        <div v-else class="flex min-w-0 gap-2 self-start">
          <div class="relative min-w-0 flex-1">
            <input :id="inputId(key)" :aria-describedby="help(key) ? helpId(key) : undefined" :data-test="`config-${path}.${key}`" :type="inputType(key, fields[key])" :value="fields[key] === SECRET_MASK ? '' : shown(fields[key])"
              :placeholder="fields[key] === SECRET_MASK ? 'Set. Type a new value to replace it' : ''"
              @input="fields[key] = parse($event.target.value, fields[key], original[key])" autocomplete="new-password" spellcheck="false"
              class="block w-full rounded-md bg-white dark:bg-white/5 px-3 py-1.5 font-mono text-base/6 sm:text-sm/6 text-gray-900 dark:text-white outline-1 -outline-offset-1 outline-gray-300 dark:outline-white/10 placeholder:font-sans placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:outline-2 focus:-outline-offset-2 focus:outline-gray-900 dark:focus:outline-white"
              :class="isSecret(key, fields[key]) && 'pr-10'" />
            <button v-if="isSecret(key, fields[key])" type="button" :data-test="`config-reveal-${path}.${key}`"
              :aria-label="`${isRevealed(key) ? 'Hide' : 'Show'} ${label(key).toLowerCase()}`" :aria-pressed="isRevealed(key)"
              @click.prevent="toggle(key)"
              class="absolute inset-y-0 right-0 px-3 flex items-center text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-200">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                <path v-if="isRevealed(key)" stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
                <template v-else>
                  <path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z" />
                  <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                </template>
              </svg>
            </button>
          </div>
          <button v-if="canGenerate(key, fields[key])" type="button" :data-test="`config-generate-${path}.${key}`"
            :aria-label="`Generate ${label(key).toLowerCase()}`" @click="generate(key)"
            class="shrink-0 rounded-lg border border-gray-200 dark:border-white/10 bg-white dark:bg-white/5 px-3 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-white hover:bg-gray-50 dark:hover:bg-white/10">
            Generate
          </button>
          <template v-if="isIcon(key, fields[key])">
            <img v-if="fields[key]" :src="fields[key]" alt="" data-test="config-icon-preview" width="20" height="20"
              class="size-9 shrink-0 rounded-md border border-gray-200 dark:border-white/10 bg-white dark:bg-white/5 object-contain p-2" />
            <label :data-test="`config-upload-${path}.${key}`"
              class="shrink-0 cursor-pointer rounded-lg border border-gray-200 dark:border-white/10 bg-white dark:bg-white/5 px-3 flex items-center text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-white hover:bg-gray-50 dark:hover:bg-white/10 has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-gray-900 dark:has-[:focus-visible]:outline-white">
              {{ uploading.has(key) ? 'Uploading…' : 'Upload' }}
              <input type="file" class="sr-only" accept="image/png,image/jpeg,image/gif,image/webp" :disabled="uploading.has(key)"
                :aria-label="`Upload ${label(key).toLowerCase()}`" @change="upload(key, $event)" />
            </label>
          </template>
        </div>
        <p v-if="uploadErrors[key]" role="alert" data-test="config-upload-error" class="text-xs/5 text-red-600 dark:text-red-400 @xl:col-start-2">{{ uploadErrors[key] }}</p>
      </div>
    </template>
  </div>
</template>

<script setup>
/**
 * The settings of one config block, as a form built from its JSON: a switch
 * for a boolean, a number field for a number, a field for anything else, and
 * a group for a nested block, each with its line of help from CONFIG_HELP
 * under its name. A setting added to the server appears here as
 * it is, with no change to the page. Edits are made in place on `fields`.
 */
import { computed, inject, reactive } from 'vue'
import { CONFIG_HELP } from '../utils/adminConfigHelp'
import { uploadAdminLoginIcon } from '../api'
import { useToasts } from '../composables/useToasts'

defineOptions({ name: 'AdminConfigFields' })

const props = defineProps({
  fields: { type: Object, required: true },
  // The block as stored, the same shape as fields: it tells a secret that is
  // set from one being typed.
  original: { type: Object, required: true },
  path: { type: String, required: true },
})

const SECRET = /secret|password|token|privatekey|apikey/i
// What the server sends for a secret that is set, never the secret itself;
// sent back, it keeps the secret.
const SECRET_MASK = '••••••••'


// The secret fields shown in the clear, by path, so a tab's are its own.
const revealed = reactive(new Set())
const isRevealed = (key) => revealed.has(`${props.path}.${key}`)

function toggle(key) {
  const field = `${props.path}.${key}`
  if (revealed.has(field)) revealed.delete(field)
  else revealed.add(field)
}

// The secrets the server only checks against itself, so any random value
// does, by setting path, and how many random bytes each takes; a provider's
// secret is issued by the provider and cannot be made up here.
const GENERATED = {
  'auth.apiJwtSecret': 48,
  'auth.mcpJwtSecret': 48,
  'prometheus.token': 24, // 32 characters, the only length the server takes
}

/** A secret the server only needs to be random, offered a Generate button. */
const canGenerate = (key, value) => isSecret(key, value) && settingPath(key) in GENERATED

/** Fills the field with random bytes, base64url, shown so it can be copied. */
function generate(key) {
  const bytes = crypto.getRandomValues(new Uint8Array(GENERATED[settingPath(key)]))
  props.fields[key] = btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  revealed.add(`${props.path}.${key}`)
}

/** A text setting whose name says it is a secret, never a link: hidden until revealed. */
const isSecret = (key, value) => typeof value !== 'number' && SECRET.test(key) && !key.endsWith('Url')

/**
 * A setting's path with no list positions, as CONFIG_HELP and LISTS name it:
 * auth.oauth2Providers.0.clientId is auth.oauth2Providers.clientId.
 */
const settingPath = (key) => `${props.path}.${key}`.replace(/\.\d+(?=\.|$)/g, '')

/** The setting's one line of help, if it has one. */
const help = (key) => CONFIG_HELP[settingPath(key)]

/**
 * Settings shown right after another, rather than where the server's JSON,
 * sorted by name, puts them: the setting, and the one it follows. A block's
 * enabled switch always comes first.
 */
const AFTER = { 'auth.mcpJwtSecret': 'apiJwtSecret' }

const orderedKeys = computed(() => {
  const keys = Object.keys(props.fields).sort((a, b) => (b === 'enabled') - (a === 'enabled'))
  for (const key of [...keys]) {
    const anchor = AFTER[settingPath(key)]
    if (!anchor || !keys.includes(anchor)) continue
    keys.splice(keys.indexOf(key), 1)
    keys.splice(keys.indexOf(anchor) + 1, 0, key)
  }
  return keys
})

/**
 * The lists of blocks the page edits, such as sign-in providers: what one is
 * called, and a new one's settings. A list not named here is edited comma
 * separated, like any other.
 */
// The server's app.baseUrl, as stored, which a callback URL starts with.
const baseUrl = inject('configBaseUrl', { value: '' })

const LISTS = {
  'auth.oauth2Providers': {
    noun: 'provider',
    item: () => ({
      id: '', name: '', clientId: '', clientSecret: '', authUrl: '', tokenUrl: '', userInfoUrl: '',
      scopes: ['openid', 'email', 'profile'], iconUrl: '',
    }),
  },
}

/** A path on this server, as the provider reaches it. */
const at = (path) => `${(baseUrl.value || window.location.origin).replace(/\/+$/, '')}${path}`

/**
 * The URLs a block's app must be registered with at its provider, shown at
 * the top of the block, by setting path: what each is called there, and where.
 */
const URLS = {
  'auth.google': () => [
    { label: 'Callback URL', hint: 'Add this as an authorized redirect URI of the OAuth client in the Google Cloud console.', url: at('/api/v1/auth/google/callback') },
  ],
  'auth.github': () => [
    { label: 'Callback URL', hint: 'Set this as the authorization callback URL of the GitHub OAuth app.', url: at('/api/v1/auth/github/callback') },
  ],
  'auth.oauth2Providers': (item) => [
    { label: 'Callback URL', hint: 'Register this as the redirect (callback) URL of the OAuth app at the provider.', url: at(`/api/v1/auth/oauth2/${item.id || '<id>'}/callback`) },
  ],
  slack: () => [
    { label: 'Redirect URL', hint: 'Add this under OAuth & Permissions, Redirect URLs, in the Slack app.', url: at('/slack/oauth/callback') },
    { label: 'Events request URL', hint: 'The Request URL under Event Subscriptions.', url: at('/slack/events') },
    { label: 'Interactivity request URL', hint: 'The Request URL under Interactivity & Shortcuts.', url: at('/slack/interactions') },
    { label: 'Slash command request URL', hint: 'The Request URL of each slash command.', url: at('/slack/commands') },
  ],
}

const urls = computed(() => URLS[props.path.replace(/\.\d+(?=\.|$)/g, '')]?.(props.fields) ?? [])

const list = (key) => LISTS[settingPath(key)]
const isList = (key, value) => Array.isArray(value) && !!list(key)

/** The stored block with item's id, which tells its set secrets apart. */
function storedItem(key, item) {
  const stored = props.original?.[key]
  return (Array.isArray(stored) && stored.find((o) => o && item.id && o.id === item.id)) || {}
}

const { addToast } = useToasts()

async function copy({ label, url }) {
  try {
    await navigator.clipboard.writeText(url)
    addToast(`Copied the ${label.charAt(0).toLowerCase()}${label.slice(1)}.`, 'success')
  } catch {
    addToast('Could not copy. Select the URL and copy it.', 'error')
  }
}

// An icon a sign-in button shows: the server resizes an upload and keeps it.
const isIcon = (key, value) => key === 'iconUrl' && typeof value === 'string'
const uploading = reactive(new Set())
const uploadErrors = reactive({})

async function upload(key, event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  uploading.add(key)
  uploadErrors[key] = ''
  try {
    const url = await uploadAdminLoginIcon(file)
    if (url === null) uploadErrors[key] = 'The admin session has ended. Sign in again to upload.'
    else props.fields[key] = url
  } catch (err) {
    uploadErrors[key] = err.message
  } finally {
    uploading.delete(key)
  }
}
const inputId = (key) => `config-input-${props.path}.${key}`
const helpId = (key) => `config-help-${props.path}.${key}`

// A setting's row: its name and help beside its field where there is room,
// above it where there is not.
const ROW = 'grid gap-x-8 gap-y-3 py-5 first:pt-0 last:pb-0 @xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]'

const isGroup = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)

/** "maxRequestsPerSecond" as "Max requests per second". */
function label(key) {
  const words = key.replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

function inputType(key, value) {
  if (typeof value === 'number') return 'number'
  return isSecret(key, value) && !isRevealed(key) ? 'password' : 'text'
}

/** A list is edited comma separated; unset as empty. */
function shown(value) {
  if (Array.isArray(value)) return value.join(', ')
  return value ?? ''
}

/** Reads a field back as the type it had. */
function parse(text, was, stored) {
  // Emptied, a secret that is set stays as it is.
  if (text === '' && stored === SECRET_MASK) return SECRET_MASK
  if (typeof was === 'number') return text === '' ? 0 : Number(text)
  if (Array.isArray(was)) return text.split(',').map((s) => s.trim()).filter(Boolean)
  if (was === null && text === '') return null
  return text
}
</script>
