<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div class="flex flex-col gap-4">
    <template v-for="key in Object.keys(fields)" :key="key">
      <fieldset v-if="isGroup(fields[key])" data-test="config-group"
        class="border border-gray-200 dark:border-zinc-800 rounded-sm p-4 flex flex-col gap-4">
        <legend class="px-1 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">{{ label(key) }}</legend>
        <AdminConfigFields :fields="fields[key]" :path="`${path}.${key}`" />
      </fieldset>

      <div v-else-if="typeof fields[key] === 'boolean'" class="flex items-center justify-between gap-4">
        <span>
          <span class="block text-xs font-bold text-gray-800 dark:text-zinc-100">{{ label(key) }}</span>
          <span class="block text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ path }}.{{ key }}</span>
        </span>
        <button type="button" role="switch" :data-test="`config-${path}.${key}`" :aria-checked="fields[key]"
          :aria-label="label(key)" @click="fields[key] = !fields[key]"
          class="relative w-9 h-5 rounded-full transition-colors shrink-0"
          :class="fields[key] ? 'bg-black dark:bg-white' : 'bg-gray-200 dark:bg-zinc-700'">
          <span class="absolute top-0.5 w-4 h-4 rounded-full bg-white dark:bg-black transition-all"
            :class="fields[key] ? 'right-0.5' : 'left-0.5 dark:bg-zinc-400'"></span>
        </button>
      </div>

      <label v-else class="flex flex-col gap-1">
        <span class="flex flex-wrap items-baseline gap-x-2">
          <span class="text-xs font-bold text-gray-800 dark:text-zinc-100">{{ label(key) }}</span>
          <span class="text-[10px] font-mono text-gray-400 dark:text-zinc-500">{{ path }}.{{ key }}</span>
        </span>
        <span class="relative">
          <input :data-test="`config-${path}.${key}`" :type="inputType(key, fields[key])" :value="shown(fields[key])"
            @input="fields[key] = parse($event.target.value, fields[key])" autocomplete="off" spellcheck="false"
            class="w-full px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-md text-gray-900 dark:text-zinc-100 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500"
            :class="isSecret(key, fields[key]) && 'pr-9'" />
          <button v-if="isSecret(key, fields[key])" type="button" :data-test="`config-reveal-${path}.${key}`"
            :aria-label="`${isRevealed(key) ? 'Hide' : 'Show'} ${label(key).toLowerCase()}`" :aria-pressed="isRevealed(key)"
            @click.prevent="toggle(key)"
            class="absolute inset-y-0 right-0 px-2.5 flex items-center text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-200">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
              <path v-if="isRevealed(key)" stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
              <template v-else>
                <path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z" />
                <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </template>
            </svg>
          </button>
        </span>
      </label>
    </template>
  </div>
</template>

<script setup>
/**
 * The settings of one config block, as a form built from its JSON: a switch
 * for a boolean, a number field for a number, a field for anything else, and
 * a group for a nested block. A setting added to the server appears here as
 * it is, with no change to the page. Edits are made in place on `fields`.
 */
import { reactive } from 'vue'

defineOptions({ name: 'AdminConfigFields' })

const props = defineProps({
  fields: { type: Object, required: true },
  path: { type: String, required: true },
})

const SECRET = /secret|password|token|privatekey|apikey/i

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
function parse(text, was) {
  if (typeof was === 'number') return text === '' ? 0 : Number(text)
  if (Array.isArray(was)) return text.split(',').map((s) => s.trim()).filter(Boolean)
  if (was === null && text === '') return null
  return text
}
</script>
