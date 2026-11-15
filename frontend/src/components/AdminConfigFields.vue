<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- A container, so a row stacks wherever it is narrow: on a phone, or in a group's card. -->
  <div class="@container divide-y divide-gray-100 dark:divide-zinc-800">
    <template v-for="key in Object.keys(fields)" :key="key">
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

        <div v-else class="relative min-w-0 self-start">
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
import { reactive } from 'vue'
import { CONFIG_HELP } from '../utils/adminConfigHelp'

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

/** A text setting whose name says it is a secret: hidden until revealed. */
const isSecret = (key, value) => typeof value !== 'number' && SECRET.test(key)

/** The setting's one line of help, if it has one. */
const help = (key) => CONFIG_HELP[`${props.path}.${key}`]
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
