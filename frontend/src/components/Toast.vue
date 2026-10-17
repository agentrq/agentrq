<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div class="toast-container">
    <TransitionGroup name="toast">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="toast"
        :class="[toast.type, { card: toast.kind, clickable: hasLink(toast) }]"
        @click="openLink(toast)"
      >
        <!-- A stream toast is a task row, as on the homepage: the workspace
             above, then status icon, title and pill. -->
        <div v-if="toast.kind" class="toast-eyebrow">{{ toast.eyebrow || 'AgentRQ' }}</div>
        <div class="toast-row">
          <span class="toast-dot" :class="dotFor(toast).tone" aria-hidden="true">{{ dotFor(toast).glyph }}</span>
          <div class="toast-content">
            <div v-if="toast.title || pillsFor(toast).length" class="toast-line">
              <div v-if="toast.title" class="toast-title" :title="toast.title">{{ toast.title }}</div>
              <div v-if="pillsFor(toast).length" class="toast-pills">
                <template v-for="(pill, i) in pillsFor(toast)" :key="i">
                  <span v-if="i > 0" class="toast-arrow" aria-hidden="true">→</span>
                  <span class="toast-pill" :class="[pill.tone, { was: pill.was }]">{{ pill.label }}</span>
                </template>
              </div>
            </div>
            <!-- Two lines at most, four for an error; the whole message is on hover. -->
            <div class="toast-message" :class="{ solo: !toast.title }" :title="toast.message">{{ toast.message }}</div>
            <div v-if="hasLink(toast)" class="toast-link">{{ toast.link.label || 'View task' }}</div>
          </div>
        </div>
        <button @click.stop="removeToast(toast.id)" class="toast-close" aria-label="Dismiss">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
        </button>
        <div
          v-if="!toast.persistent"
          class="toast-progress"
          :style="{ animationDuration: `${toast.duration}ms` }"
        ></div>
      </div>
    </TransitionGroup>
  </div>
</template>

<script setup>
import { useRouter } from 'vue-router';
import { useToasts } from '../composables/useToasts';
import { STATUS_LABELS } from '../composables/useStreamToasts';

const { toasts, removeToast } = useToasts();
const router = useRouter();

// Falsy ids mean the toast has nothing to link to — the same sentinel
// useStreamToasts.toastFor uses for "no task".
function hasLink(toast) {
  return Boolean(toast.link?.path || (toast.link?.taskId && toast.link?.workspaceId));
}

function openLink(toast) {
  if (!hasLink(toast)) return;
  removeToast(toast.id);
  if (toast.link.path) return router.push(toast.link.path);
  router.push(`/workspaces/${toast.link.workspaceId}/tasks/${toast.link.taskId}`);
}

// The round icon: the task's status for a status change, the kind of news for
// the rest of the stream, and the toast's type for everything else.
const STATUS_DOTS = {
  blocked: { tone: 'alert', glyph: '!' },
  completed: { tone: 'done', glyph: '✓' },
  ongoing: { tone: 'working', glyph: '' },
  rejected: { tone: 'muted', glyph: '×' },
  notstarted: { tone: 'idle', glyph: '' },
  cron: { tone: 'new', glyph: '↻' },
};
const KIND_DOTS = {
  created: { tone: 'new', glyph: '→' },
  scheduled: { tone: 'new', glyph: '↻' },
  permission: { tone: 'alert', glyph: '!' },
  failed: { tone: 'alert', glyph: '!' },
  reply: { tone: 'reply', glyph: '↩' },
  connected: { tone: 'live', glyph: '' },
  disconnected: { tone: 'offline', glyph: '' },
};
const TYPE_DOTS = {
  error: { tone: 'alert', glyph: '!' },
  success: { tone: 'done', glyph: '✓' },
  info: { tone: 'info', glyph: 'i' },
  warning: { tone: 'warn', glyph: '!' },
};

function dotFor(toast) {
  if (toast.kind === 'status') return STATUS_DOTS[toast.to] ?? TYPE_DOTS.info;
  return KIND_DOTS[toast.kind] ?? TYPE_DOTS[toast.type] ?? TYPE_DOTS.info;
}

const KIND_PILLS = {
  created: { tone: 'new', label: 'New' },
  scheduled: { tone: 'new', label: 'Scheduled' },
  permission: { tone: 'blocked', label: 'Approval' },
  failed: { tone: 'blocked', label: 'Failed' },
  reply: { tone: 'neutral', label: 'Reply' },
  connected: { tone: 'new', label: 'Live' },
  disconnected: { tone: 'neutral', label: 'Offline' },
  notice: { tone: 'neutral', label: 'Notice' },
};

const statusPill = (status) => ({ tone: status, label: STATUS_LABELS[status] ?? status });

// A status change shows both ends, the one it left faded; the rest one pill.
function pillsFor(toast) {
  if (toast.kind === 'status') {
    const to = statusPill(toast.to);
    return toast.from ? [{ ...statusPill(toast.from), was: true }, to] : [to];
  }
  const pill = KIND_PILLS[toast.kind];
  return pill ? [pill] : [];
}
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

/* The homepage's card: warm hairline border, and a flat offset shadow. */
.toast {
  pointer-events: auto;
  position: relative;
  overflow: hidden;
  width: 400px;
  padding: 12px 14px 14px;
  background: #ffffff;
  color: #131314;
  border: 1px solid #e8e6dc;
  border-radius: 8px;
  box-shadow: 4px 4px 0 rgba(232, 230, 220, 0.7), 0 10px 28px -12px rgba(19, 19, 20, 0.18);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (max-width: 640px) {
  .toast {
    width: 100%;
  }
}

.dark .toast {
  background: #1f1f1e;
  color: #f4f4f5;
  border-color: #3a3a36;
  box-shadow: 4px 4px 0 rgba(0, 0, 0, 0.35), 0 10px 28px -12px rgba(0, 0, 0, 0.7);
}

.toast.clickable {
  cursor: pointer;
}

.toast.clickable:hover {
  border-color: #c9c6b8;
}

.dark .toast.clickable:hover {
  border-color: #5a5a54;
}

.toast-eyebrow {
  margin: 0 26px 8px 0;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 10px;
  font-weight: 700;
  line-height: 14px;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #3d5a42;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dark .toast-eyebrow {
  color: #9cc3a2;
}

.toast-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

/* A plain toast has no eyebrow, so its first line runs up to the close button. */
.toast:not(.card) .toast-content {
  padding-right: 22px;
}

.toast-content {
  flex: 1;
  min-width: 0;
}

.toast-line {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-height: 20px;
}

/* Two lines before it gives way, so the pills beside it cost no words. */
.toast-title {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  font-weight: 700;
  line-height: 20px;
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  overflow: hidden;
}

/* The status icon: a small ring, its glyph and colour saying which state. */
.toast-dot {
  flex: none;
  width: 18px;
  height: 18px;
  margin-top: 1px;
  border-radius: 9999px;
  border: 1.5px solid #737971;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
  color: #737971;
}

.toast-dot.alert { border-color: #fca5a5; color: #dc2626; }
.toast-dot.warn { border-color: #fcd34d; color: #d97706; }
.toast-dot.done { border-color: #86efac; color: #16a34a; }
.toast-dot.new { border-color: #3d5a42; background: #e8f2ea; color: #3d5a42; animation: pulse 2s ease-in-out infinite; }
.toast-dot.reply { border-color: #c2c8c0; color: #3d5a42; }
.toast-dot.muted { border-color: #c2c8c0; color: #737971; }
.toast-dot.idle { border: 1.5px dashed #c2c8c0; }
.toast-dot.info { border-color: #c2c8c0; color: #737971; font-family: Georgia, serif; font-style: italic; }
.toast-dot.offline { border-color: #c2c8c0; }

/* In progress, and live: a filled centre, pulsing while there is life in it. */
.toast-dot.working::after,
.toast-dot.live::after,
.toast-dot.offline::after {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 9999px;
  background: #ca8a04;
}
.toast-dot.working::after { animation: pulse 1.6s ease-in-out infinite; }
.toast-dot.live { border-color: #3d5a42; }
.toast-dot.live::after { background: #3d5a42; animation: pulse 1.6s ease-in-out infinite; }
.toast-dot.offline::after { background: #c2c8c0; }

.dark .toast-dot { border-color: #71717a; color: #a1a1aa; }
.dark .toast-dot.alert { border-color: #7f1d1d; color: #f87171; }
.dark .toast-dot.warn { border-color: #78350f; color: #fbbf24; }
.dark .toast-dot.done { border-color: #166534; color: #4ade80; }
.dark .toast-dot.new { border-color: #6b9a72; background: rgba(61, 90, 66, 0.35); color: #9cc3a2; }
.dark .toast-dot.reply { border-color: #52524e; color: #9cc3a2; }
.dark .toast-dot.muted,
.dark .toast-dot.idle,
.dark .toast-dot.info,
.dark .toast-dot.offline { border-color: #52524e; color: #a1a1aa; }
.dark .toast-dot.live { border-color: #6b9a72; }
.dark .toast-dot.live::after { background: #9cc3a2; }
.dark .toast-dot.offline::after { background: #52524e; }

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}

.toast-pills {
  flex: none;
  margin-top: 1px;
  display: flex;
  align-items: center;
  gap: 5px;
}

/* The homepage's status pill, colour for colour. */
.toast-pill {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 9.5px;
  font-weight: 700;
  line-height: 14px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  white-space: nowrap;
  padding: 2px 7px;
  border-radius: 2px;
  background: #f6f4ec;
  color: #737971;
}

.toast-pill.ongoing { background: #fefce8; color: #a16207; }
.toast-pill.blocked { background: #fef2f2; color: #b91c1c; }
.toast-pill.completed { background: #f0fdf4; color: #15803d; }
.toast-pill.new { background: #e8f2ea; color: #3d5a42; }
.toast-pill.neutral { color: #424842; }

/* The status it left: there to read, not to compete with where it went. */
.toast-pill.was { opacity: 0.55; }

.dark .toast-pill { background: rgba(255, 255, 255, 0.06); color: #a1a1aa; }
.dark .toast-pill.ongoing { background: rgba(234, 179, 8, 0.14); color: #facc15; }
.dark .toast-pill.blocked { background: rgba(239, 68, 68, 0.15); color: #f87171; }
.dark .toast-pill.completed { background: rgba(34, 197, 94, 0.14); color: #4ade80; }
.dark .toast-pill.new { background: rgba(61, 90, 66, 0.35); color: #9cc3a2; }
.dark .toast-pill.neutral { color: #d4d4d8; }

.toast-arrow {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 11px;
  color: #737971;
}

.toast-message {
  margin-top: 2px;
  font-size: 13px;
  line-height: 19px;
  color: #424842;
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  overflow: hidden;
}

.dark .toast-message {
  color: #a1a1aa;
}

.toast.error .toast-message {
  -webkit-line-clamp: 4;
  line-clamp: 4;
}

/* Without a title the message is the headline. */
.toast-message.solo {
  margin-top: 0;
  font-size: 14px;
  line-height: 20px;
  color: inherit;
}

.toast-link {
  margin-top: 8px;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #3d5a42;
}

.toast-link::after {
  content: ' →';
}

.dark .toast-link {
  color: #9cc3a2;
}

.toast.clickable:hover .toast-link {
  text-decoration: underline;
  text-underline-offset: 3px;
}

.toast-close {
  position: absolute;
  top: 10px;
  right: 10px;
  background: none;
  border: none;
  color: #737971;
  cursor: pointer;
  padding: 2px;
  border-radius: 4px;
  transition: color 0.15s, background-color 0.15s;
}

.toast-close svg {
  display: block;
  width: 16px;
  height: 16px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linecap: round;
}

.toast-close:hover {
  color: #131314;
  background: #f6f4ec;
}

.dark .toast-close {
  color: #a1a1aa;
}

.dark .toast-close:hover {
  color: #fafafa;
  background: #3a3a36;
}

/* The auto-close countdown, as long as the toast's own duration. */
.toast-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 2px;
  width: 100%;
  background: #3d5a42;
  opacity: 0.35;
  animation: progress linear forwards;
}

.dark .toast-progress {
  background: #9cc3a2;
}

@keyframes progress {
  from { width: 100%; }
  to { width: 0%; }
}

.toast-enter-active {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}
.toast-leave-active {
  transition: all 0.2s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateY(12px);
}

.toast-leave-to {
  opacity: 0;
  transform: translateX(24px);
}

.toast-move {
  transition: transform 0.3s ease;
}

@media (prefers-reduced-motion: reduce) {
  .toast-dot,
  .toast-dot::after {
    animation: none !important;
  }
}
</style>
