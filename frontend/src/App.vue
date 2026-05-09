<template>
  <div id="app" class="flex flex-col md:flex-row h-[100dvh] bg-zinc-100 dark:bg-zinc-950 font-inter overflow-hidden">
    
    <!-- Global Mobile Menu Toggle (Bottom Floating Action) -->
    <button v-if="!isLoginPage" 
            @click.stop="isMobileMenuOpen = true"
            class="md:hidden fixed bottom-3 left-1/2 -translate-x-1/2 px-4 py-1.5 bg-black/80 dark:bg-white/80 backdrop-blur-md text-white dark:text-black text-[10px] font-semibold rounded-full shadow-lg z-[60] border border-white/10 dark:border-black/10 transition-all active:scale-95 flex items-center justify-center gap-2">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
      </svg>
      Menu
    </button>

    <!-- Overlay for mobile menu -->
    <div v-if="isMobileMenuOpen" @click="isMobileMenuOpen = false" class="md:hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90]"></div>

    <!-- Sidebar -->
    <nav v-if="!isLoginPage"
         :class="[
           isMobileMenuOpen ? 'flex' : 'hidden', 'md:flex fixed inset-y-0 left-0 z-[100] transform bg-zinc-100 dark:bg-zinc-950 md:relative md:translate-x-0',
           'text-gray-900 dark:text-zinc-100 shrink-0 flex-col h-full transition-all duration-300 ease-in-out',
           isCollapsed && !isMobileMenuOpen ? 'w-16' : 'w-64',
           isMobileMenuOpen ? 'w-[280px] shadow-2xl' : ''
         ]">
      <div :class="[isCollapsed ? 'px-2 py-4' : 'p-4']" class="flex flex-col min-h-0 grow">
        <!-- Sidebar Header -->
        <div :class="[
          'relative border-b border-transparent pb-3 flex transition-all duration-300',
          isCollapsed ? 'flex-col items-center gap-2' : 'flex-row items-center gap-1'
        ]">
          <div :class="[
            'flex items-center p-1 transition-all duration-300',
            isCollapsed ? 'justify-center w-full' : 'grow min-w-0'
          ]">
            <div class="flex items-center gap-2.5 min-w-0">
              <div class="w-8 h-8 flex items-center justify-center shrink-0">
                <svg viewBox="0 0 24 24" class="w-6 h-6 text-gray-700 dark:text-zinc-100" fill="none" xmlns="http://www.w3.org/2000/svg">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M12 7l-3.5 8" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M12 7l3.5 8" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M9.5 12h5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
              </div>
              <span v-if="!isCollapsed || isMobileMenuOpen" class="text-sm font-bold truncate text-gray-800 dark:text-zinc-200">AgentRQ</span>
            </div>
          </div>

          <!-- Collapse Toggle -->
          <button @click="isCollapsed = !isCollapsed"
                  class="hidden md:inline-flex items-center justify-center text-gray-500 hover:text-gray-900 dark:hover:text-white size-8 transition-all duration-200 shrink-0 rounded-sm hover:bg-gray-100 dark:hover:bg-zinc-800"
                  :title="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'">
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                 :class="['transition-transform duration-300', isCollapsed ? 'rotate-180' : '']">
              <path d="m15 18-6-6 6-6" />
            </svg>
          </button>
        </div>

        <div class="space-y-0.5 mt-4 overflow-y-auto custom-scrollbar flex-1 min-h-0 px-2">
          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-2 mb-2">
            <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Navigation</span>
          </div>
          <router-link to="/"
              @mouseenter="showTooltip($event, 'Overview')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Overview</span>
          </router-link>

          <template v-if="workspaces.length > 0 && (!isCollapsed || isMobileMenuOpen)">
            <div class="px-2 mt-5 mb-2 pt-4 border-t border-gray-200/50 dark:border-zinc-600/50">
              <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Workspaces</span>
            </div>

            <router-link v-for="ws in workspaces" :key="ws.id" :to="`/workspaces/${ws.id}`"
                @mouseenter="showTooltip($event, ws.name)" @mouseleave="hideTooltip"
                class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md group"
                :class="[
                  $route.path.startsWith(`/workspaces/${ws.id}`) ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white font-semibold' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
                ]">
              <div class="w-1.5 h-1.5 rounded-full shrink-0"
                   :class="ws.agentConnected ? 'bg-green-500 dark:bg-green-400 shadow-[0_0_6px_rgba(34,197,94,0.4)]' : 'bg-gray-300 dark:bg-zinc-600'"
                   :title="ws.agentConnected ? 'Agent Online' : 'Agent Offline'"></div>
              <span class="truncate flex-1">{{ ws.name }}</span>
            </router-link>
          </template>

          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-2 mt-5 mb-2 pt-4 border-t border-gray-200/50 dark:border-zinc-600/50">
            <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Tasks</span>
          </div>

          <router-link to="/tasks/scheduled"
              @mouseenter="showTooltip($event, 'Scheduled')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/scheduled' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Scheduled</span>
          </router-link>

          <router-link to="/tasks/pending"
              @mouseenter="showTooltip($event, 'Pending on Me')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/pending' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Pending on Me</span>
          </router-link>

          <router-link to="/tasks/notstarted"
              @mouseenter="showTooltip($event, 'Not Started')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/notstarted' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Not Started</span>
          </router-link>

          <router-link to="/tasks/ongoing"
              @mouseenter="showTooltip($event, 'Ongoing')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/ongoing' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Ongoing</span>
          </router-link>

          <router-link to="/tasks/completed"
              @mouseenter="showTooltip($event, 'Completed')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-sm transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/completed' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-600 dark:text-zinc-300 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Completed</span>
          </router-link>
        </div>

        <!-- Sidebar Footer -->
        <div class="mt-auto p-4">

          <!-- User Profile -->
          <div class="relative pt-3 border-t border-gray-300/50 dark:border-zinc-600/50 mt-2 overflow-visible">
            <!-- User Menu Popover -->
            <div v-if="isUserMenuOpen"
                 :class="[
                   'absolute bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm shadow-2xl p-2 z-[110] animate-in fade-in slide-in-from-bottom-2 duration-200',
                   (isCollapsed && !isMobileMenuOpen) ? 'left-full bottom-0 ml-2 min-w-[200px] origin-bottom-left' : 'bottom-full left-0 right-0 mb-2 origin-bottom'
                 ]">
              <div class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500">Account</p>
                <p class="text-xs font-bold text-gray-700 dark:text-zinc-200 truncate mt-0.5" :title="user?.email">{{ user?.email || 'Loading...' }}</p>
              </div>

              <!-- Theme Selection inside Menu -->
              <div class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500 mb-2">Theme Preference</p>
                <div class="flex items-center gap-1 bg-gray-50 dark:bg-zinc-800/50 p-1 rounded-sm border border-gray-100 dark:border-zinc-800">
                  <button @click="themeStore.setTheme('light')" 
                          :class="['flex-1 flex justify-center py-1.5 rounded-sm transition-all', themeStore.theme === 'light' ? 'bg-white dark:bg-zinc-700 shadow-sm border border-gray-200 dark:border-zinc-600 text-black dark:text-white' : 'text-gray-400 hover:text-gray-600 dark:hover:text-zinc-300']"
                          title="Light Mode">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" /></svg>
                  </button>
                  <button @click="themeStore.setTheme('dark')" 
                          :class="['flex-1 flex justify-center py-1.5 rounded-sm transition-all', themeStore.theme === 'dark' ? 'bg-white dark:bg-zinc-700 shadow-sm border border-gray-200 dark:border-zinc-600 text-black dark:text-white' : 'text-gray-400 hover:text-gray-600 dark:hover:text-zinc-300']"
                          title="Dark Mode">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" /></svg>
                  </button>
                  <button @click="themeStore.setTheme('system')" 
                          :class="['flex-1 flex justify-center py-1.5 rounded-sm transition-all', themeStore.theme === 'system' ? 'bg-white dark:bg-zinc-700 shadow-sm border border-gray-200 dark:border-zinc-600 text-black dark:text-white' : 'text-gray-400 hover:text-gray-600 dark:hover:text-zinc-300']"
                          title="System Default">
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" /></svg>
                  </button>
                </div>
              </div>
              <button @click="logout" class="w-full flex items-center gap-2.5 px-3 py-2 rounded-sm text-xs font-bold text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-500/10 transition-colors">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                </svg>
                Logout
              </button>
            </div>

            <!-- User Profile Button -->
            <button @click="isUserMenuOpen = !isUserMenuOpen"
                    class="flex items-center gap-3 w-full px-2 py-1.5 rounded-sm hover:bg-gray-100 dark:hover:bg-zinc-800 transition-all duration-200 group outline-none focus-visible:ring-2 focus-visible:ring-gray-200 dark:focus-visible:ring-zinc-700"
                    :class="(isCollapsed && !isMobileMenuOpen) ? 'justify-center mx-0' : ''">
              <div class="relative shrink-0">
                <div class="w-9 h-9 rounded-full bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 shadow-sm flex items-center justify-center text-gray-700 dark:text-zinc-200 font-bold text-sm overflow-hidden">
                  <img v-if="user?.picture" :src="user.picture" class="w-full h-full object-cover" alt="Profile" />
                  <span v-else class="">{{ user?.name?.charAt(0) || user?.email?.charAt(0) || '?' }}</span>
                </div>
              </div>
              <div v-if="!isCollapsed || isMobileMenuOpen" class="flex flex-col items-start overflow-hidden text-left min-w-0 flex-1">
                <span class="text-sm font-semibold text-gray-700 dark:text-zinc-200 truncate w-full group-hover:text-gray-900 dark:group-hover:text-zinc-50 transition-colors">
                  {{ user?.name || user?.email || 'User' }}
                </span>
              </div>
            </button>
          </div>
        </div>

      </div>
    </nav>

    <!-- Login View -->
    <main v-if="isLoginPage" class="grow h-full bg-white dark:bg-zinc-950 flex flex-col overflow-hidden">
      <router-view class="grow flex flex-col" />
    </main>

    <!-- App Content View -->
    <main v-else class="grow min-w-0 p-0 md:p-4 h-full min-h-0 flex flex-col relative bg-zinc-100 dark:bg-zinc-950">
      <div class="h-full overflow-y-auto min-w-0 md:rounded-sm scroll-smooth bg-white dark:bg-zinc-900 md:border border-gray-200 dark:border-zinc-800 no-scrollbar">
        <div class="px-4 py-6 md:px-8 md:py-8 h-full flex flex-col">
          <router-view class="grow flex flex-col min-h-0 min-w-0" />
        </div>
      </div>
    </main>

    <!-- Global Tooltip -->
    <div v-if="tooltipStore.visible"
      class="fixed z-[100] px-3 py-1.5 text-xs font-semibold text-black dark:text-white bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm shadow-lg pointer-events-none whitespace-nowrap"
      :style="tooltipStore.style">
      {{ tooltipStore.text }}
    </div>

    <!-- Global Toasts -->
    <Toast />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchUser, fetchWorkspaces } from './api'
import { useToasts } from './composables/useToasts'
import { useEventBus } from './useEventBus'
import { useThemeStore } from './stores/themeStore'
import { useTooltipStore } from './stores/tooltipStore'
import Toast from './components/Toast.vue'

const route = useRoute()
const { notifySuccess, notifyInfo, notifyError } = useToasts()
const isLoginPage = computed(() => route.path === '/login')
const user = ref(null)
const workspaces = ref([])
const isUserMenuOpen = ref(false)
const isWorkspaceDropdownOpen = ref(false)
const isCollapsed = ref(true);
const isMobileMenuOpen = ref(false);
const workspaceDropdownRef = ref(null)
const themeStore = useThemeStore()
const tooltipStore = useTooltipStore()

const currentWorkspaceId = computed(() => route.params.id || route.params.workspaceId)

// Setup Global Event Bus (Global stream receives events for all workspaces)
const { connect, disconnect, events } = useEventBus()

// Watch for noteworthy events
watch(events, (newEvents) => {
  if (newEvents.length === 0) return
  const event = newEvents[newEvents.length - 1]
  
  // Handle agent connection status updates globally
  if (event.type === 'agent.connected') {
    const { connected, workspaceId } = event.payload
    const ws = workspaces.value.find(w => w.id === workspaceId)
    if (ws) {
      ws.agentConnected = connected
    }
  }

  // Handle workspace metadata updates
  if (event.type === 'workspace.updated') {
    const updatedWs = event.payload
    const idx = workspaces.value.findIndex(w => w.id === updatedWs.id)
    if (idx !== -1) {
      workspaces.value[idx] = { ...workspaces.value[idx], ...updatedWs }
    }
  }
  
  if (event.type === 'task.created' && event.payload.createdBy === 'agent') {
    notifySuccess(`Agent started a new task: ${event.payload.title}`)
  } else if (event.type === 'task.updated') {
    const task = event.payload
    const lastMsg = task.messages?.[task.messages.length - 1]
    
    // Check for permission requests
    if (lastMsg?.metadata?.type === 'permission_request' && lastMsg.metadata.status !== 'allow' && lastMsg.metadata.status !== 'deny') {
      notifyError(`Permission required: ${lastMsg.metadata.tool_name}`, 'Action Needed')
    } 
    // Check for agent-initiated status updates
    else if (lastMsg?.sender === 'agent' && lastMsg.text?.includes('Status updated to:')) {
      const status = task.status;
      notifyInfo(`Task "${task.title}" is now ${status}`)
    }
  }
}, { deep: true })

onMounted(() => {
  themeStore.init()
  loadUser()
  loadWorkspaces()
  connect() // Connect to global event stream
  document.addEventListener('click', handleClickOutside)
})

const showTooltip = (event, text) => {
  if (!isCollapsed.value || window.innerWidth < 1024) return;
  tooltipStore.show(event, text);
}

const hideTooltip = () => {
  tooltipStore.hide();
}

async function logout() {
  await fetch('/api/v1/auth/logout', { method: 'POST' })
  window.location.href = '/login'
}

const loadWorkspaces = async () => {
  if (isLoginPage.value) return;
  try {
    const res = await fetchWorkspaces()
    workspaces.value = res.workspaces || []
  } catch (err) {
    console.error('Failed to fetch workspaces:', err)
  }
}

const loadUser = async () => {
  if (isLoginPage.value) return;
  try {
    user.value = await fetchUser()
  } catch (err) {
    console.error('Failed to fetch user:', err)
  }
}

const handleClickOutside = (e) => {
  if (workspaceDropdownRef.value && !workspaceDropdownRef.value.contains(e.target)) {
    isWorkspaceDropdownOpen.value = false
  }
}


onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

watch(() => route.fullPath, (fullPath) => {
  isWorkspaceDropdownOpen.value = false
  isMobileMenuOpen.value = false
  hideTooltip()
  
  const path = route.path;
  if (path === '/') document.title = 'Workspaces | AgentRQ';
  else if (path === '/login') document.title = 'Login | AgentRQ';
  else if (path.startsWith('/tasks/')) {
    const filter = route.params.filter || '';
    const title = filter ? filter.charAt(0).toUpperCase() + filter.slice(1) : 'All';
    document.title = `${title} Tasks | AgentRQ`;
  }
})

watch(isLoginPage, (val) => {
  if (!val) {
    loadUser()
    loadWorkspaces()
  }
})
</script>
