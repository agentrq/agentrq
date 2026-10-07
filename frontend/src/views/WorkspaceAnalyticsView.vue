<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div class="flex-1 min-w-0 flex flex-col h-full page-surface overflow-hidden px-4">

    <!-- Content -->
    <div class="flex-1 overflow-y-auto custom-scrollbar">
      <WorkspaceStats :workspaceId="workspaceId" />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import WorkspaceStats from '../components/WorkspaceStats.vue';

const route = useRoute();
const workspaceId = computed(() => route.params.id);
</script>
