<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  The sidebar's workspaces: each one, then its forks indented under it, and
  the row menu that forks a workspace or merges a fork back. Its own component
  so the shell stays about the shell, and so this can be mounted by itself.
-->
<script setup>
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ContextMenu from './ContextMenu.vue'
import ForkIcon from './ForkIcon.vue'
import ForkNameModal from './ForkNameModal.vue'
import MergeForkModal from './MergeForkModal.vue'
import { parentName, useForkActions, workspaceTree } from '../composables/useWorkspaceForks'
import { activityOf, activityTooltip, needsYouCount } from '../composables/useWorkspaceActivity'
import { useWorkspaceContextMenu, workspaceMenuItems } from '../composables/useWorkspaceContextMenu'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useTooltipStore } from '../stores/tooltipStore'
import { useToasts } from '../composables/useToasts'
import { useFormat } from '../composables/useFormat'

const props = defineProps({
  workspaces: { type: Array, default: () => [] },
  showTooltip: { type: Function, default: () => {} },
  hideTooltip: { type: Function, default: () => {} },
})

const route = useRoute()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const { toKebabCase } = useFormat()
const workspaces = computed(() => props.workspaces)

// Forks nested under their parent, and which parents have them folded away.
// The fold is a per-browser convenience, remembered like the sidebar's width.
const workspaceGroups = computed(() => workspaceTree(workspaces.value))
const COLLAPSED_FORKS_KEY = 'agentrq:sidebarCollapsedForks'
const collapsedGroups = ref(new Set((() => {
  try { return JSON.parse(localStorage.getItem(COLLAPSED_FORKS_KEY) ?? '[]') } catch { return [] }
})()))
const isGroupCollapsed = (id) => collapsedGroups.value.has(String(id))
// A parent's folded forks, whose state its row sums up.
const foldedForks = (ws, group) => (ws === group.workspace && isGroupCollapsed(ws.id) ? group.forks : [])

// The shell shows a row's name only while the sidebar is folded. What the
// agent is doing shows whenever there is something to say.
const tooltipStore = useTooltipStore()
function onRowEnter(event, ws, group) {
  const text = activityTooltip(ws, foldedForks(ws, group))
  if (text.includes('\n')) tooltipStore.show(event, text)
  else props.showTooltip(event, ws.name)
}
function onRowLeave() {
  tooltipStore.hide()
  props.hideTooltip()
}
function toggleGroup(id) {
  const next = new Set(collapsedGroups.value)
  next.has(String(id)) ? next.delete(String(id)) : next.add(String(id))
  collapsedGroups.value = next
  try { localStorage.setItem(COLLAPSED_FORKS_KEY, JSON.stringify([...next])) } catch { /* not remembered */ }
}

// The row menu. Opening it re-reads the list, so a fork's Merge is enabled by
// the task that finished a moment ago rather than by the last page load.
const workspaceMenu = useWorkspaceContextMenu({
  workspaces: () => workspaces.value,
  onOpen: () => workspaceStore.fetchWorkspaces(),
})
const forkActions = useForkActions({ router, store: workspaceStore, toasts: useToasts() })

function openWorkspaceMenu(event, ws) {
  props.hideTooltip()
  workspaceMenu.open(event, ws)
}
function openWorkspaceMenuAt(el, ws) {
  props.hideTooltip()
  workspaceMenu.openAt(el, ws)
}
function onWorkspaceRowKeydown(event, ws) {
  if ((event.shiftKey && event.key === 'F10') || event.key === 'ContextMenu') {
    event.preventDefault()
    openWorkspaceMenuAt(event.currentTarget, ws)
  }
}
function onWorkspaceMenuSelect(key) {
  const chosen = workspaceMenu.select(key)
  if (chosen?.key === 'fork') forkActions.startFork(chosen.workspace)
  if (chosen?.key === 'merge') forkActions.startMerge(chosen.workspace)
}
</script>

<template>
  <div class="space-y-0.5">
    <!-- Each workspace, then its forks indented under it with the fork
         mark. A parent with forks folds them away like a group. The
         row's menu (right-click, its ⋯ button, or Shift+F10) forks a
         workspace, or merges a fork back. -->
    <template v-for="group in workspaceGroups" :key="group.workspace.id">
      <div v-for="ws in [group.workspace, ...(isGroupCollapsed(group.workspace.id) ? [] : group.forks)]" :key="ws.id"
           class="relative group/row" :data-test="ws.forkOfId ? 'sidebar-fork' : 'sidebar-workspace'">
        <router-link :to="`/workspaces/${ws.id}`"
            @mouseenter="onRowEnter($event, ws, group)" @mouseleave="onRowLeave"
            @contextmenu.prevent.stop="openWorkspaceMenu($event, ws)"
            @keydown="onWorkspaceRowKeydown($event, ws)"
            class="flex items-center gap-2.5 py-1.5 pr-7 text-xs transition-all duration-150 rounded-md group"
            :class="[
              ws.forkOfId ? 'pl-6' : 'pl-2',
              route.path.startsWith(`/workspaces/${ws.id}`) ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white font-semibold' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
            ]">
          <!-- The dot pulses while the agent works on a task; the row's
               tooltip says which. -->
          <span class="relative flex items-center justify-center w-1.5 h-1.5 shrink-0">
            <span class="w-1.5 h-1.5 rounded-full"
                  :class="ws.agentConnected ? 'bg-green-500 dark:bg-green-400 shadow-[0_0_6px_rgba(34,197,94,0.4)]' : 'bg-gray-300 dark:bg-zinc-600'"></span>
            <span v-if="activityOf(ws).working" class="absolute inset-0 rounded-full bg-green-500 animate-ping opacity-75" data-test="sidebar-working"></span>
          </span>
          <ForkIcon v-if="ws.forkOfId" class="w-3 h-3 shrink-0 -ml-1 opacity-70" />
          <span class="truncate flex-1">{{ toKebabCase(ws.name) }}</span>
          <!-- Tasks waiting on the person: blocked, theirs to do, or a
               question pending. A folded parent counts its forks' too. -->
          <span v-if="needsYouCount(ws, foldedForks(ws, group)) > 0"
                class="shrink-0 min-w-4 px-1 rounded-sm text-center text-[9px] font-black leading-4 bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-400"
                data-test="sidebar-needs-you">{{ needsYouCount(ws, foldedForks(ws, group)) }}</span>
          <button v-if="!ws.forkOfId && group.forks.length > 0" type="button"
                  @click.prevent.stop="toggleGroup(ws.id)"
                  :aria-expanded="!isGroupCollapsed(ws.id)"
                  :aria-label="isGroupCollapsed(ws.id) ? `Show ${group.forks.length} forks` : 'Hide forks'"
                  :title="isGroupCollapsed(ws.id) ? `Show ${group.forks.length} forks` : 'Hide forks'"
                  class="shrink-0 -my-1 p-0.5 rounded text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50">
            <svg class="w-3 h-3 transition-transform" :class="isGroupCollapsed(ws.id) ? '-rotate-90' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
          </button>
        </router-link>
        <!-- The menu's button, for touch and for the keyboard. Always
             there on a touch screen; on a pointer, on hover or focus. -->
        <button v-if="workspaceMenuItems(ws, workspaces).length > 0" type="button"
                @click.stop="openWorkspaceMenuAt($event.currentTarget, ws)"
                :aria-label="`${ws.name} actions`" title="Actions"
                class="absolute right-1 top-1/2 -translate-y-1/2 p-0.5 rounded text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-300/60 dark:hover:bg-zinc-600 opacity-100 md:opacity-0 md:group-hover/row:opacity-100 focus:opacity-100">
          <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><circle cx="5" cy="12" r="1.75" /><circle cx="12" cy="12" r="1.75" /><circle cx="19" cy="12" r="1.75" /></svg>
        </button>
      </div>
    </template>
    <!-- A sidebar workspace's menu, and what its items open. -->
    <ContextMenu :show="workspaceMenu.menu.value.show" :x="workspaceMenu.menu.value.x" :y="workspaceMenu.menu.value.y"
                 :items="workspaceMenu.items.value" :autofocus="workspaceMenu.menu.value.keyboard"
                 @close="workspaceMenu.close()" @select="onWorkspaceMenuSelect" />
    <!-- To the body: the sidebar is transformed, and a fixed overlay inside
         it would be laid out in the sidebar instead of the window. -->
    <Teleport to="body">
    <ForkNameModal :show="!!forkActions.state.forking" :parent-name="forkActions.state.forking?.name ?? ''"
                   v-model="forkActions.state.forkName" :busy="forkActions.state.busy"
                   @close="forkActions.cancelFork()" @confirm="forkActions.confirmFork($event)" />
    <MergeForkModal :fork="forkActions.state.merging" :parent-name="parentName(forkActions.state.merging, workspaces)"
                    :busy="forkActions.state.busy"
                    @close="forkActions.cancelMerge()" @confirm="forkActions.confirmMerge($event)" />
    </Teleport>
  </div>
</template>
