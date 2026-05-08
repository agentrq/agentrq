<template>
  <div class="flex-1 min-w-0 flex flex-col h-full bg-white dark:bg-zinc-900 overflow-hidden px-4">

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
