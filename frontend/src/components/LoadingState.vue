<script setup>
// Reusable loading state: a spinner + optional label, centered by default.
// Pass extra classes (e.g. "py-20") via the class attribute for spacing.
defineProps({
  label: {
    type: String,
    default: 'Loading...'
  }
})
</script>

<template>
  <div class="flex flex-col items-center justify-center gap-3">
    <div class="w-10 h-10 rounded-full border-4 border-gray-200 dark:border-zinc-700 border-t-gray-900 dark:border-t-white animate-spin"></div>
    <p v-if="label" class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500">{{ label }}</p>
  </div>
</template>
