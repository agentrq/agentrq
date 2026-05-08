import { ref, onMounted, onUnmounted } from 'vue';

export function useViewport() {
  const isMobile = ref(window.innerWidth < 768);
  const isTablet = ref(window.innerWidth >= 768 && window.innerWidth < 1024);
  const isDesktop = ref(window.innerWidth >= 1024);

  const update = () => {
    isMobile.value = window.innerWidth < 768;
    isTablet.value = window.innerWidth >= 768 && window.innerWidth < 1024;
    isDesktop.value = window.innerWidth >= 1024;
  };

  onMounted(() => window.addEventListener('resize', update));
  onUnmounted(() => window.removeEventListener('resize', update));

  return { isMobile, isTablet, isDesktop };
}
