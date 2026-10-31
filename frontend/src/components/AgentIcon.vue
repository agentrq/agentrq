<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- An SVG is drawn inline so a one-colour icon takes the text's colour. -->
  <span v-if="icon?.svg" data-test="agent-icon" aria-hidden="true"
        class="inline-flex w-3.5 h-3.5 shrink-0 [&>svg]:w-full [&>svg]:h-full" v-html="icon.svg"></span>
  <img v-else-if="icon" data-test="agent-icon" :src="icon.src" alt="" class="w-3.5 h-3.5 shrink-0 rounded-sm" />
</template>

<script setup>
import { computed } from 'vue';
import { agentIconFor } from '../composables/useAgentIcon';

const props = defineProps({ name: { type: String, default: '' } });
const icon = computed(() => agentIconFor(props.name));
</script>
