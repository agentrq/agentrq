<template>
  <div class="toast-container">
    <TransitionGroup name="toast">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="toast"
        :class="toast.type"
      >
        <div class="toast-content">
          <div v-if="toast.title" class="toast-title">{{ toast.title }}</div>
          <div class="toast-message">{{ toast.message }}</div>
        </div>
        <button @click="removeToast(toast.id)" class="toast-close">
          &times;
        </button>
        <div class="toast-progress"></div>
      </div>
    </TransitionGroup>
  </div>
</template>

<script setup>
import { useToasts } from '../composables/useToasts';

const { toasts, removeToast } = useToasts();
</script>

<style scoped>
.toast-container {
  position: fixed;
  bottom: 24px;
  right: 24px;
  z-index: 9999;
  display: flex;
  flex-direction: column-reverse;
  gap: 12px;
  pointer-events: none;
}

@media (max-width: 640px) {
  .toast-container {
    bottom: 12px;
    left: 12px;
    right: 12px;
    align-items: stretch;
  }
}

.toast {
  pointer-events: auto;
  position: relative;
  min-width: 400px;
  max-width: 600px;
  padding: 16px;
  background: #fff;
  border: 2px solid #111;
  box-shadow: 4px 4px 0 0 #111;
  border-radius: 4px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  z-index: 1;
}

@media (max-width: 640px) {
  .toast {
    min-width: 0;
    width: 100%;
    max-width: none;
    box-shadow: 3px 3px 0 0 #111;
  }
}

.dark .toast {
  background: #18181b;
  border-color: #e4e4e7;
  box-shadow: 4px 4px 0 0 #e4e4e7;
}

@media (max-width: 640px) and (prefers-color-scheme: dark) {
  .dark .toast {
    box-shadow: 3px 3px 0 0 #e4e4e7;
  }
}

/* Stacking: older toasts look smaller and faded */
.toast:nth-child(2) { transform: translateY(-8px) scale(0.98); opacity: 0.9; z-index: 2; }
.toast:nth-child(3) { transform: translateY(-16px) scale(0.96); opacity: 0.8; z-index: 3; }
.toast:nth-child(4) { transform: translateY(-24px) scale(0.94); opacity: 0.7; z-index: 4; }
.toast:nth-child(5) { transform: translateY(-32px) scale(0.92); opacity: 0.6; z-index: 5; }

.toast:first-child {
  transform: translateY(0) scale(1);
  opacity: 1;
  z-index: 10;
  box-shadow: 6px 6px 0 0 var(--shadow-color, #111);
  border-color: var(--border-color, #111);
}

.dark .toast:first-child {
  box-shadow: 6px 6px 0 0 var(--shadow-color, #e4e4e7);
  border-color: var(--border-color, #e4e4e7);
}

/* Type-specific styles */
.toast.success {
  --border-color: #166534;
  --shadow-color: rgba(22, 101, 52, 0.4);
  background: #f0fdf4;
  border-color: #166534;
  color: #14532d;
}

.toast.error {
  --border-color: #991b1b;
  --shadow-color: rgba(153, 27, 27, 0.4);
  background: #fef2f2;
  border-color: #991b1b;
  color: #7f1d1d;
}

.toast.info {
  --border-color: #111;
  --shadow-color: rgba(0, 0, 0, 0.2);
  background: #fff;
  border-color: #111;
  color: #111;
}

.dark .toast.success {
  background: #064e3b;
  border-color: #10b981;
  color: #ecfdf5;
  --border-color: #10b981;
  --shadow-color: rgba(16, 185, 129, 0.25);
}

.dark .toast.error {
  background: #7f1d1d;
  border-color: #f87171;
  color: #fef2f2;
  --border-color: #f87171;
  --shadow-color: rgba(248, 113, 113, 0.25);
}

.dark .toast.info {
  background: #18181b;
  border-color: #e4e4e7;
  color: #f4f4f5;
  --border-color: #e4e4e7;
  --shadow-color: rgba(228, 228, 231, 0.2);
}

.toast-content {
  flex: 1;
}

.toast-title {
  font-weight: 900;
  color: inherit;
  font-size: 11px;
  margin-bottom: 4px;
  text-transform: none;
  letter-spacing: normal;
}

.toast-message {
  font-size: 13px;
  font-weight: 500;
  color: inherit;
  line-height: 1.4;
  opacity: 0.9;
}

.toast-close {
  background: none;
  border: none;
  color: inherit;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  padding: 4px;
  margin: -4px;
  opacity: 0.6;
  transition: all 0.2s;
  font-weight: bold;
}

.toast-close:hover {
  opacity: 1;
  transform: scale(1.1);
}

.toast-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 3px;
  background: currentColor;
  width: 100%;
  animation: progress 5s linear forwards;
  opacity: 0.3;
  border-radius: 0 0 4px 4px;
}

@keyframes progress {
  from { width: 100%; }
  to { width: 0%; }
}

.toast-enter-active {
  transition: all 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
}
.toast-leave-active {
  transition: all 0.3s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateY(20px) scale(0.8);
}

.toast-leave-to {
  opacity: 0;
  transform: translateX(100%) rotate(5deg);
}

.toast-move {
  transition: transform 0.4s ease;
}
</style>
