<template>
  <div id="app" class="flex flex-col md:flex-row h-[100dvh] bg-white font-inter">
    
    <!-- Mobile Header Mock -->
    <div v-if="!isLoginPage" class="md:hidden flex items-center justify-between p-4 bg-white border-b border-gray-200 shrink-0 shadow-sm z-30">
      <div class="flex items-center gap-2.5 font-bold text-gray-900">
        <div class="w-8 h-8 rounded-lg border border-gray-200 bg-white flex items-center justify-center shrink-0 text-black shadow-sm">
          <svg viewBox="0 0 24 24" class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
            <path d="M12 7l-3.5 8" />
            <path d="M12 7l3.5 8" />
            <path d="M9.5 12h5" />
          </svg>
        </div>
        AgentRQ
      </div>
      <button @click="isMobileMenuOpen = true" class="p-2 -mr-2 text-gray-600 hover:bg-gray-100 rounded-lg outline-none">
        <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>
    </div>

    <!-- Overlay for mobile menu -->
    <div v-if="isMobileMenuOpen" @click="isMobileMenuOpen = false" class="md:hidden fixed inset-0 bg-black/40 z-40 backdrop-blur-sm"></div>

    <!-- Sidebar mock matching hasmcp-app (now collapsible) -->
    <nav v-if="!isLoginPage" 
         :class="[
           isMobileMenuOpen ? 'flex' : 'hidden', 'md:flex fixed inset-y-0 left-0 z-50 transform bg-zinc-50 md:relative md:translate-x-0',
           'text-gray-800 shrink-0 flex-col h-full transition-all duration-300 ease-in-out',
           isCollapsed && !isMobileMenuOpen ? 'w-16' : 'w-64',
           isMobileMenuOpen ? 'w-64 shadow-2xl' : ''
         ]">
      <div :class="[isCollapsed ? 'px-2 py-4' : 'p-4']" class="flex flex-col min-h-0 grow">
        <!-- Sidebar Header -->
        <div :class="[
          'relative border-b border-gray-300/50 pb-3 flex transition-all duration-300',
          isCollapsed ? 'flex-col items-center gap-2' : 'flex-row items-center gap-1'
        ]">
          <div :class="[
            'flex items-center p-1.5 rounded-lg transition-all duration-300',
            isCollapsed ? 'justify-center w-full' : 'grow min-w-0'
          ]">
            <div class="flex items-center gap-2.5 min-w-0">
              <div class="w-8 h-8 rounded-lg border border-gray-200 bg-white flex items-center justify-center shrink-0 text-black shadow-sm overflow-hidden">
                <svg viewBox="0 0 24 24" class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  <path d="M12 7l-3.5 8" />
                  <path d="M12 7l3.5 8" />
                  <path d="M9.5 12h5" />
                </svg>
              </div>
              <span v-if="!isCollapsed" class="text-sm font-bold text-gray-900 truncate tracking-tight">AgentRQ</span>
            </div>
          </div>
          
          <!-- Collapse Toggle -->
          <button @click="isCollapsed = !isCollapsed" 
                  class="hidden md:inline-flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-lg size-8 transition-all duration-200 shrink-0"
                  :title="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'">
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                 :class="['w-4 h-4 transition-transform duration-300', isCollapsed ? 'rotate-180' : '']">
              <rect width="18" height="18" x="3" y="3" rx="2" />
              <path d="M9 3v18" />
              <path d="m14 9-3 3 3 3" />
            </svg>
          </button>
        </div>

        <div class="space-y-6 mt-4 overflow-y-auto custom-scrollbar flex-1 min-h-0">
          <div class="px-2">
            <div class="flex items-center justify-between px-1 mb-2">
              <h3 v-if="!isCollapsed" class="text-[11px] font-bold text-gray-400 uppercase tracking-widest">Account</h3>

            </div>

            <div class="space-y-0.5 tracking-wide">
              <router-link to="/" 
                  @mouseenter="showTooltip($event, 'Workspaces')" @mouseleave="hideTooltip"
                  class="flex items-center gap-2.5 px-2 py-2 rounded-lg text-sm transition-all duration-150"
                  :class="[
                    isCollapsed ? 'justify-center mx-1' : '',
                    $route.path === '/' ? 'bg-gray-200 text-black' : 'text-gray-600 hover:bg-gray-200 hover:text-black'
                  ]">
                <svg class="w-4.5 h-4.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
                </svg>
                <span v-if="!isCollapsed || isMobileMenuOpen">Workspaces</span>
              </router-link>
            </div>
          </div>
        </div>

        <!-- Sidebar Footer -->
        <div class="relative pt-3 border-t border-gray-200/50 mt-auto overflow-visible">
          <!-- User Menu Popover -->
          <div v-if="isUserMenuOpen" 
               :class="[
                 'absolute bg-white rounded-xl shadow-lg border border-gray-200 overflow-hidden ring-1 ring-black/5 transform transition-all duration-200 z-50',
                 isCollapsed ? 'left-full bottom-0 ml-2 w-64 origin-bottom-left' : 'bottom-full left-0 w-full mb-3 origin-bottom'
               ]">
            <div class="px-4 py-3 border-b border-gray-100 bg-gray-50/50">
              <p class="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-0.5">Signed in as</p>
              <p class="text-sm font-medium text-gray-900 truncate" :title="user?.email">{{ user?.email || 'Loading...' }}</p>
            </div>
            <div class="p-1">
              <button @click="logout" class="w-full flex items-center gap-2.5 px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg transition-colors">
                <svg class="w-4.5 h-4.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                </svg>
                Log out
              </button>
            </div>
          </div>

          <!-- User Profile Pill -->
          <button @click="isUserMenuOpen = !isUserMenuOpen" 
                  class="flex items-center gap-3 w-full p-2 -mx-2 rounded-xl hover:bg-gray-100/80 transition-all duration-200 group outline-none"
                  :class="isCollapsed ? 'justify-center mx-0' : ''">
            <div class="relative shrink-0">
              <div class="w-9 h-9 rounded-full bg-white border border-gray-200 shadow-sm flex items-center justify-center text-gray-700 font-bold text-sm overflow-hidden bg-gradient-to-br from-gray-50 to-gray-100">
                <img v-if="user?.picture" :src="user.picture" class="w-full h-full object-cover" alt="Profile" />
                <span v-else>{{ user?.name?.charAt(0).toUpperCase() || user?.email?.charAt(0).toUpperCase() || '?' }}</span>
              </div>
            </div>
            <div v-if="!isCollapsed" class="flex flex-col items-start overflow-hidden text-left min-w-0 flex-1">
              <span class="text-sm font-semibold text-gray-700 truncate w-full group-hover:text-gray-900 transition-colors">
                {{ user?.name || user?.email || 'User' }}
              </span>
            </div>
            <svg v-if="!isCollapsed" class="w-4 h-4 text-gray-400 group-hover:text-gray-600 transition-colors" :class="isUserMenuOpen ? '' : 'rotate-180'" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
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
    <main v-else class="grow h-full p-4 md:py-4 md:pr-4 md:pl-0 min-h-0 flex flex-col bg-zinc-50">
      <div class="h-full overflow-y-auto rounded-xl scroll-smooth bg-white border border-gray-200 shadow-sm transition-all duration-300">
        <div :class="[
               'px-4 md:px-10 py-6 md:pt-8 md:pb-8 h-full flex flex-col min-h-0 mx-auto transition-all duration-300',
               isCollapsed ? 'max-w-full' : 'max-w-full'
             ]">
          <router-view class="grow flex flex-col min-h-0" />
        </div>
      </div>
    </main>
    <!-- Collapsed Tooltip -->
    <div v-if="tooltip.visible"
      class="fixed z-[100] px-2.5 py-1.5 text-xs font-medium text-white bg-gray-900 rounded-md shadow-lg pointer-events-none transform -translate-y-1/2 whitespace-nowrap"
      :style="tooltip.style">
      {{ tooltip.text }}
      <div class="absolute left-0 top-1/2 -translate-x-full -translate-y-1/2 border-y-[5px] border-y-transparent border-r-[5px] border-r-gray-900 ml-[-1px]"></div>
    </div>

    <!-- Global Toasts -->
    <Toast />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchUser, fetchWorkspaces } from './api'
import Toast from './components/Toast.vue'

const route = useRoute()
const isLoginPage = computed(() => route.path === '/login')
const user = ref(null)
const workspaces = ref([])
const isUserMenuOpen = ref(false)
const isWorkspaceDropdownOpen = ref(false)
const isCollapsed = ref(true);
const isMobileMenuOpen = ref(false);
const workspaceDropdownRef = ref(null)

const currentWorkspaceId = computed(() => route.params.id || route.params.workspaceId)
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
  if (!isCollapsed.value) return;
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
