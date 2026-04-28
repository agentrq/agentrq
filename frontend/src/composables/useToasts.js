import { ref } from 'vue';

const toasts = ref([]);

export function useToasts() {
  const addToast = (message, type = 'info', title = null, duration = 4000) => {
    const id = Date.now() + Math.random();
    const toast = { id, message, type, title };
    
    toasts.value.push(toast);

    if (duration > 0) {
      setTimeout(() => {
        removeToast(id);
      }, duration);
    }
    return id;
  };

  const removeToast = (id) => {
    toasts.value = toasts.value.filter(t => t.id !== id);
  };

  const notifyError = (message, title = 'Error') => addToast(message, 'error', title);
  const notifySuccess = (message, title = 'Success') => addToast(message, 'success', title);
  const notifyInfo = (message, title = 'Notice') => addToast(message, 'info', title);

  return {
    toasts,
    addToast,
    removeToast,
    notifyError,
    notifySuccess,
    notifyInfo
  };
}
