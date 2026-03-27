<template>
  <div id="app" class="flex flex-col md:flex-row h-[100dvh] bg-white">

    <!-- Mobile Header -->
    <div v-if="!isLoginPage && !$route.meta.fullPage" class="md:hidden flex items-center justify-between px-4 py-3 bg-black border-b-2 border-black shrink-0 z-30">
      <div class="flex items-center gap-2.5 text-white font-black uppercase tracking-widest text-sm">
        <svg viewBox="0 0 32 32" class="w-7 h-7" fill="none" xmlns="http://www.w3.org/2000/svg">
          <rect width="32" height="32" rx="4" fill="#18181b"/>
          <g transform="translate(4, 4)">
            <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M12 7l-3.5 8" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M12 7l3.5 8" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M9.5 12h5" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
          </g>
        </svg>
        AgentRQ
      </div>
      <button @click="isMobileMenuOpen = true" class="p-1.5 text-white border-2 border-white hover:bg-white hover:text-black transition-colors">
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>
    </div>

    <!-- Overlay for mobile menu -->
    <div v-if="isMobileMenuOpen" @click="isMobileMenuOpen = false" class="md:hidden fixed inset-0 bg-black/60 z-40"></div>

    <!-- Sidebar -->
    <nav v-if="!isLoginPage"
         :class="[
           isMobileMenuOpen ? 'flex' : 'hidden', 'md:flex fixed inset-y-0 left-0 z-50 transform bg-black border-r-2 border-black md:relative md:translate-x-0',
           'text-white shrink-0 flex-col h-full transition-all duration-300 ease-in-out',
           isCollapsed && !isMobileMenuOpen ? 'w-16' : 'w-64',
           isMobileMenuOpen ? 'w-64 shadow-[8px_0px_0px_0px_rgba(0,0,0,1)]' : ''
         ]">
      <div :class="[isCollapsed ? 'px-2 py-4' : 'p-4']" class="flex flex-col min-h-0 grow">
        <!-- Sidebar Header -->
        <div :class="[
          'relative border-b-2 border-white/10 pb-3 flex transition-all duration-300',
          isCollapsed ? 'flex-col items-center gap-2' : 'flex-row items-center gap-1'
        ]">
          <div :class="[
            'flex items-center p-1 transition-all duration-300',
            isCollapsed ? 'justify-center w-full' : 'grow min-w-0'
          ]">
            <div class="flex items-center gap-2.5 min-w-0">
              <svg viewBox="0 0 32 32" class="w-8 h-8 shrink-0" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="4" fill="#18181b"/>
                <g transform="translate(4, 4)">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M12 7l-3.5 8" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M12 7l3.5 8" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                  <path d="M9.5 12h5" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </g>
              </svg>
              <span v-if="!isCollapsed || isMobileMenuOpen" class="text-sm font-black text-white truncate uppercase tracking-widest">AgentRQ</span>
            </div>
          </div>

          <!-- Collapse Toggle -->
          <button @click="isCollapsed = !isCollapsed"
                  class="hidden md:inline-flex items-center justify-center text-white/40 hover:text-[#00FF88] size-8 transition-all duration-200 shrink-0 border border-white/10 hover:border-[#00FF88]"
                  :title="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'">
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"
                 :class="['transition-transform duration-300', isCollapsed ? 'rotate-180' : '']">
              <path d="m15 18-6-6 6-6" />
            </svg>
          </button>
        </div>

        <div class="space-y-1 mt-4 overflow-y-auto custom-scrollbar flex-1 min-h-0 px-2">
          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-1 mb-3">
            <span class="text-[9px] font-black text-white/30 uppercase tracking-[0.3em]">Navigation</span>
          </div>
          <router-link to="/"
              @mouseenter="showTooltip($event, 'Workspaces')" @mouseleave="hideTooltip"
              class="flex items-center gap-3 px-3 py-2.5 text-xs font-black uppercase tracking-widest transition-all border-2 border-transparent"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-0' : '',
                $route.path === '/' || $route.path.startsWith('/workspaces') ? 'bg-white text-black border-white' : 'text-white/60 hover:text-white hover:border-white/20 hover:bg-white/5'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Workspaces</span>
          </router-link>

          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-1 mt-6 mb-3">
            <span class="text-[9px] font-black text-white/30 uppercase tracking-[0.3em]">Tasks</span>
          </div>

          <router-link to="/tasks/pending"
              @mouseenter="showTooltip($event, 'Pending on Me')" @mouseleave="hideTooltip"
              class="flex items-center gap-3 px-3 py-2 text-[10px] font-black uppercase tracking-widest transition-all border-2 border-transparent"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-0' : '',
                $route.path === '/tasks/pending' ? 'bg-[#00FF88] text-black border-[#00FF88]' : 'text-white/60 hover:text-white hover:border-white/20 hover:bg-white/5'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Pending on Me</span>
          </router-link>

          <router-link to="/tasks/notstarted"
              @mouseenter="showTooltip($event, 'Not Started')" @mouseleave="hideTooltip"
              class="flex items-center gap-3 px-3 py-2 text-[10px] font-black uppercase tracking-widest transition-all border-2 border-transparent"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-0' : '',
                $route.path === '/tasks/notstarted' ? 'bg-[#00FF88] text-black border-[#00FF88]' : 'text-white/60 hover:text-white hover:border-white/20 hover:bg-white/5'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Not Started</span>
          </router-link>

          <router-link to="/tasks/ongoing"
              @mouseenter="showTooltip($event, 'Ongoing')" @mouseleave="hideTooltip"
              class="flex items-center gap-3 px-3 py-2 text-[10px] font-black uppercase tracking-widest transition-all border-2 border-transparent"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-0' : '',
                $route.path === '/tasks/ongoing' ? 'bg-[#00FF88] text-black border-[#00FF88]' : 'text-white/60 hover:text-white hover:border-white/20 hover:bg-white/5'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Ongoing</span>
          </router-link>

          <router-link to="/tasks/completed"
              @mouseenter="showTooltip($event, 'Completed')" @mouseleave="hideTooltip"
              class="flex items-center gap-3 px-3 py-2 text-[10px] font-black uppercase tracking-widest transition-all border-2 border-transparent"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-0' : '',
                $route.path === '/tasks/completed' ? 'bg-[#00FF88] text-black border-[#00FF88]' : 'text-white/60 hover:text-white hover:border-white/20 hover:bg-white/5'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Completed</span>
          </router-link>
        </div>

        <!-- Sidebar Footer -->
        <div class="relative pt-3 border-t-2 border-white/10 mt-auto overflow-visible">
          <!-- User Menu Popover -->
          <div v-if="isUserMenuOpen"
               :class="[
                 'absolute bg-black border-2 border-white overflow-hidden shadow-[4px_4px_0px_0px_rgba(255,255,255,0.2)] z-50',
                 isCollapsed ? 'left-full bottom-0 ml-2 w-64 origin-bottom-left' : 'bottom-full left-0 w-full mb-3 origin-bottom'
               ]">
            <div class="px-4 py-3 border-b-2 border-white/10 bg-white/5">
              <p class="text-[9px] font-black text-white/40 uppercase tracking-widest mb-1">Signed in as</p>
              <p class="text-xs font-bold text-white truncate" :title="user?.email">{{ user?.email || 'Loading...' }}</p>
            </div>
            <div class="p-2">
              <button @click="logout" class="w-full flex items-center gap-2.5 px-3 py-2 text-xs font-black text-white/70 hover:text-black hover:bg-[#00FF88] uppercase tracking-widest transition-colors border border-transparent hover:border-[#00FF88]">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                </svg>
                Log out
              </button>
            </div>
          </div>

          <!-- User Profile Pill -->
          <button @click="isUserMenuOpen = !isUserMenuOpen"
                  class="flex items-center gap-3 w-full p-2 border-2 border-transparent hover:border-white/20 hover:bg-white/5 transition-all duration-200 group outline-none"
                  :class="(isCollapsed && !isMobileMenuOpen) ? 'justify-center' : ''">
            <div class="relative shrink-0">
              <div class="w-8 h-8 bg-[#00FF88] border-2 border-[#00FF88] flex items-center justify-center text-black font-black text-xs overflow-hidden">
                <img v-if="user?.picture" :src="user.picture" class="w-full h-full object-cover" alt="Profile" />
                <span v-else>{{ user?.name?.charAt(0).toUpperCase() || user?.email?.charAt(0).toUpperCase() || '?' }}</span>
              </div>
            </div>
            <div v-if="!isCollapsed || isMobileMenuOpen" class="flex flex-col items-start overflow-hidden text-left min-w-0 flex-1">
              <span class="text-xs font-black text-white/80 truncate w-full group-hover:text-white transition-colors uppercase tracking-wide">
                {{ user?.name || user?.email || 'User' }}
              </span>
            </div>
            <svg v-if="!isCollapsed || isMobileMenuOpen" class="w-3 h-3 text-white/30 group-hover:text-white/60 transition-colors" :class="isUserMenuOpen ? '' : 'rotate-180'" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>
        </div>

      </div>
    </nav>

    <!-- Login View (Full Screen Centered) -->
    <main v-if="isLoginPage" class="grow h-full bg-white flex flex-col overflow-hidden">
      <router-view class="grow flex flex-col" />
    </main>

    <!-- App Content View -->
    <main v-else class="grow h-full p-3 md:p-4 min-h-0 flex flex-col bg-gray-100">
      <div class="h-full overflow-y-auto scroll-smooth bg-white border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] transition-all duration-300">
        <div class="px-4 md:px-8 py-6 md:py-8 h-full flex flex-col min-h-0">
          <router-view class="grow flex flex-col min-h-0" />
        </div>
      </div>
    </main>

    <!-- Collapsed Tooltip -->
    <div v-if="tooltip.visible"
      class="fixed z-[100] px-2.5 py-1.5 text-xs font-black text-black bg-[#00FF88] border-2 border-black shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] pointer-events-none transform -translate-y-1/2 whitespace-nowrap uppercase tracking-widest"
      :style="tooltip.style">
      {{ tooltip.text }}
      <div class="absolute left-0 top-1/2 -translate-x-full -translate-y-1/2 border-y-[5px] border-y-transparent border-r-[5px] border-r-black ml-[-1px]"></div>
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

const currentWorkspaceId = computed(() => route.params.id || route.params.workspaceId)

// Setup Global Event Bus for current workspace
const { connect, disconnect, events } = useEventBus(currentWorkspaceId)

watch(currentWorkspaceId, (newId) => {
  if (newId) {
    disconnect()
    connect()
  } else {
    disconnect()
  }
}, { immediate: true })

// Watch for noteworthy events
watch(events, (newEvents) => {
  if (newEvents.length === 0) return
  const event = newEvents[newEvents.length - 1]
  
  if (event.type === 'task.created' && event.payload.created_by === 'agent') {
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
      const status = task.status.toUpperCase()
      notifyInfo(`Task "${task.title}" is now ${status}`)
    }
  }
}, { deep: true })

const currentWorkspaceName = computed(() => {
  if (!currentWorkspaceId.value) return ''
  const p = workspaces.value.find(x => x.id == currentWorkspaceId.value)
  return p ? p.name : ''
})

const currentWorkspaceIcon = computed(() => {
  if (!currentWorkspaceId.value) return ''
  const p = workspaces.value.find(x => x.id == currentWorkspaceId.value)
  return p ? p.icon : ''
})

// Tooltip State
const tooltip = ref({
  visible: false,
  text: '',
  style: { top: '0px', left: '0px' }
})

const showTooltip = (event, text) => {
  if (!isCollapsed.value || window.innerWidth < 1024) return;
  const rect = event.currentTarget.getBoundingClientRect();
  tooltip.value = {
    visible: true,
    text: text,
    style: {
      top: `${rect.top + (rect.height / 2)}px`,
      left: `${rect.right + 12}px`
    }
  }
}

const hideTooltip = () => {
  tooltip.value.visible = false;
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

onMounted(() => {
  loadUser()
  loadWorkspaces()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

watch(() => route.path, () => {
  isWorkspaceDropdownOpen.value = false
  isMobileMenuOpen.value = false
  hideTooltip()
})

watch(isLoginPage, (val) => {
  if (!val) {
    loadUser()
    loadWorkspaces()
  }
})
</script>

<style>
.custom-scrollbar::-webkit-scrollbar {
  width: 4px;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background-color: transparent;
  border-radius: 20px;
}
.custom-scrollbar:hover::-webkit-scrollbar-thumb {
  background-color: #e5e7eb;
}
</style>
