<script setup>
import { onMounted, onUnmounted } from 'vue';

const props = defineProps({
  show: Boolean,
  x: { type: Number, default: 0 },
  y: { type: Number, default: 0 },
  items: { type: Array, default: () => [] } // [{ key, label }]
});

const emit = defineEmits(['close', 'select']);

function onSelect(item) {
  emit('select', item.key);
  emit('close');
}

function onClickOutside() {
  emit('close');
}

onMounted(() => {
  window.addEventListener('click', onClickOutside);
  window.addEventListener('contextmenu', onClickOutside);
  window.addEventListener('scroll', onClickOutside, true);
});

onUnmounted(() => {
  window.removeEventListener('click', onClickOutside);
  window.removeEventListener('contextmenu', onClickOutside);
  window.removeEventListener('scroll', onClickOutside, true);
});
</script>

<template>
  <Teleport to="body">
    <div v-if="show"
         class="fixed z-[150] min-w-[160px] py-1 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg shadow-2xl"
         :style="{ top: y + 'px', left: x + 'px' }"
         @click.stop
         @contextmenu.prevent.stop>
      <button v-for="item in items" :key="item.key" @click="onSelect(item)"
              class="w-full text-left px-3 py-2 text-[11px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 hover:text-black dark:hover:text-white transition-colors">
        {{ item.label }}
      </button>
    </div>
  </Teleport>
</template>
