<template>
  <div id="app"
       @click="onMarkdownLinkActivate"
       @keydown="onMarkdownLinkActivate"
       :class="[
         'flex flex-col md:flex-row h-[100dvh] bg-zinc-100 dark:bg-zinc-950 font-inter overflow-hidden',
         isMacDesktop ? 'pt-10' : ''
       ]">

    <!-- The window's drag handle, and the space the traffic lights sit in.
         macOS desktop only: the shell hides the title bar there, so without
         this the window cannot be moved and the close, minimise and zoom
         buttons are drawn on top of the sidebar. -->
    <div v-if="isMacDesktop" class="app-drag fixed top-0 inset-x-0 h-10 z-[150]" aria-hidden="true"></div>

    <!-- PWA Update Banner -->
    <Transition name="slide-down">
      <div v-if="needRefresh && !isUpdating"
           :class="[
             'fixed inset-x-0 z-[200] flex items-center justify-between gap-3 px-4 py-2.5 bg-black text-white text-xs font-medium shadow-lg',
             isMacDesktop ? 'top-10' : 'top-0'
           ]">
        <div class="flex items-center gap-2">
          <svg class="w-3.5 h-3.5 shrink-0 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>A new version of AgentRQ is available.</span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <button @click="handleUpdateNow()"
                  class="px-3 py-1 bg-white text-black text-[10px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-100 active:scale-95 transition-all">
            Update now
          </button>
          <button @click="needRefresh = false" class="text-gray-400 hover:text-white transition-colors p-0.5" title="Dismiss">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </div>
    </Transition>
    
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
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
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
              <span class="truncate flex-1">{{ toKebabCase(ws.name) }}</span>
            </router-link>
          </template>

          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-2 mt-5 mb-2 pt-4 border-t border-gray-200/50 dark:border-zinc-600/50">
            <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Tasks</span>
          </div>

          <router-link to="/tasks/scheduled"
              @mouseenter="showTooltip($event, 'Scheduled')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/scheduled' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Scheduled</span>
          </router-link>

          <router-link to="/tasks/pending"
              @mouseenter="showTooltip($event, 'Pending on Me')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/pending' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Pending on Me</span>
          </router-link>

          <router-link to="/tasks/notstarted"
              @mouseenter="showTooltip($event, 'Not Started')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/notstarted' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Not Started</span>
          </router-link>

          <router-link to="/tasks/ongoing"
              @mouseenter="showTooltip($event, 'Ongoing')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/ongoing' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Ongoing</span>
          </router-link>

          <router-link to="/tasks/completed"
              @mouseenter="showTooltip($event, 'Completed')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path === '/tasks/completed' ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Completed</span>
          </router-link>

          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-2 mt-5 mb-2 pt-4 border-t border-gray-200/50 dark:border-zinc-600/50">
            <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Advanced</span>
          </div>
          <div v-else class="mt-4 pt-4 border-t border-gray-200/50 dark:border-zinc-600/50"></div>

          <router-link to="/events"
              @mouseenter="showTooltip($event, 'Events')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path.startsWith('/events') ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9.348 14.651a3.75 3.75 0 010-5.303m5.304-.002a3.75 3.75 0 010 5.304m-7.425 2.122a6.75 6.75 0 010-9.546m9.546.001a6.75 6.75 0 010 9.545m-11.667 2.121a9.75 9.75 0 010-13.788m13.788.001a9.75 9.75 0 010 13.787M12 12h.008v.008H12V12z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Events</span>
          </router-link>

          <router-link to="/workflows"
              @mouseenter="showTooltip($event, 'Workflows')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path.startsWith('/workflows') ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M7.217 10.907a2.25 2.25 0 100 2.186m0-2.186c.18.324.283.696.283 1.093s-.103.769-.283 1.093m0-2.186l9.566-5.314m-9.566 7.5l9.566 5.314m0 0a2.25 2.25 0 103.935 2.186 2.25 2.25 0 00-3.935-2.186zm0-12.814a2.25 2.25 0 103.933-2.185 2.25 2.25 0 00-3.933 2.185z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Workflows</span>
          </router-link>

          <!-- Desktop only: extensions run in the app, so the browser has
               nothing to show. Absent rather than disabled, and branched on the
               platform store rather than by sniffing for the bridge. -->
          <router-link v-if="platformStore.isDesktop" to="/extensions"
              @mouseenter="showTooltip($event, 'Extensions')" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center' : '',
                $route.path.startsWith('/extensions') ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 01-.657.643 48.39 48.39 0 01-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 01-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 00-1.003-.349c-1.036 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 01-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 00.657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 01-.349-1.003c0-1.035 1.007-1.875 2.25-1.875s2.25.84 2.25 1.875c0 .369-.128.713-.349 1.003-.215.283-.4.604-.4.959v0c0 .333.277.599.61.58a48.1 48.1 0 005.427-.63 48.05 48.05 0 00.582-4.717.532.532 0 00-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.96.401v0a.656.656 0 00.658-.663 48.422 48.422 0 00-.37-5.36c-1.886.342-3.81.542-5.766.59a.658.658 0 01-.663-.658v0z" />
            </svg>
            <span v-if="!isCollapsed || isMobileMenuOpen">Extensions</span>
          </router-link>

          <!-- What extensions contributed, under the screen that manages them.
               Indented rather than mixed in with our own items, so it stays
               visible which rows came from where. -->
          <router-link v-for="page in extensionPages" :key="page.to" :to="page.to"
              @mouseenter="showTooltip($event, `${page.label} — ${page.owner}`)" @mouseleave="hideTooltip"
              class="flex items-center gap-2.5 py-1.5 text-xs transition-all duration-150 rounded-md"
              :class="[
                (isCollapsed && !isMobileMenuOpen) ? 'justify-center px-2' : 'pl-7 pr-2',
                $route.path === page.to ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50'
              ]">
            <span class="w-1.5 h-1.5 rounded-full bg-current shrink-0 opacity-50"></span>
            <span v-if="!isCollapsed || isMobileMenuOpen" class="truncate">{{ page.label }}</span>
          </router-link>
        </div>

        <!-- Sidebar Footer -->
        <div class="mt-auto p-4">

          <!-- App Version & Docs -->
          <div v-if="!isCollapsed || isMobileMenuOpen" class="px-2 mb-3 flex items-center justify-between gap-2">
            <span class="text-[10px] text-gray-400 dark:text-zinc-600 font-mono">v{{ appVersion }}</span>
            <a href="https://agentrq.com/docs" target="_blank" rel="noopener noreferrer"
               class="flex items-center gap-1 text-[10px] text-gray-400 dark:text-zinc-600 hover:text-gray-700 dark:hover:text-zinc-300 transition-colors">
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25" />
              </svg>
              Docs
            </a>
          </div>

          <!-- Docs (collapsed) -->
          <div v-else class="mb-3 flex justify-center">
            <a href="https://agentrq.com/docs" target="_blank" rel="noopener noreferrer"
               @mouseenter="showTooltip($event, 'Docs')" @mouseleave="hideTooltip"
               class="flex items-center justify-center size-8 rounded-md text-gray-400 dark:text-zinc-600 hover:text-gray-900 dark:hover:text-white hover:bg-gray-200 dark:hover:bg-zinc-800 transition-all duration-150">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25" />
              </svg>
            </a>
          </div>

          <!-- User Profile -->
          <div ref="userMenuRef" class="relative pt-3 border-t border-gray-300/50 dark:border-zinc-600/50 mt-2 overflow-visible">
            <!-- User Menu Popover -->
            <div v-if="isUserMenuOpen"
                 :class="[
                   'absolute bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm shadow-2xl p-2 z-[110] animate-in fade-in slide-in-from-bottom-2 duration-200',
                   (isCollapsed && !isMobileMenuOpen) ? 'left-full bottom-0 ml-2 min-w-[200px] origin-bottom-left' : 'bottom-full left-0 right-0 mb-2 origin-bottom'
                 ]">
              <div class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500">{{ activeProfile ? activeProfile.label : 'Account' }}</p>
                <p class="text-xs font-bold text-gray-700 dark:text-zinc-200 truncate mt-0.5" :title="user?.email">{{ user?.name || user?.email || 'Loading...' }}</p>
                <p v-if="user?.name && user?.email" class="text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate mt-0.5" :title="user.email">{{ user.email }}</p>
                <p v-if="activeProfile?.serverUrl" class="text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate mt-0.5" :title="activeProfile.serverUrl">{{ activeProfile.serverUrl }}</p>
              </div>

              <!-- Other profiles. Desktop only: each is a separate session, which
                   a browser tab cannot give us. -->
              <div v-if="showProfiles" class="px-3 py-2 border-b border-gray-50 dark:border-zinc-800/50 mb-1">
                <p class="text-[10px] font-black text-gray-500 dark:text-zinc-500 mb-2">Other Profiles</p>
                <div v-if="otherProfiles.length" class="space-y-1 mb-1">
                  <button v-for="p in otherProfiles" :key="p.id" type="button"
                          @click="switchToProfile(p.id)" :disabled="switchingProfile"
                          class="w-full flex items-center gap-2.5 px-2 py-1.5 rounded-sm text-left hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
                    <span class="w-6 h-6 shrink-0 rounded-full bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 flex items-center justify-center text-[10px] font-black text-gray-600 dark:text-zinc-300 overflow-hidden">
                      <img v-if="p.identity?.picture" :src="p.identity.picture" class="w-full h-full object-cover" alt="" />
                      <template v-else>{{ profileInitial(p) }}</template>
                    </span>
                    <span class="min-w-0 flex-1">
                      <span class="block text-xs font-bold text-gray-700 dark:text-zinc-200 truncate">{{ profileTitle(p) }}</span>
                      <span class="block text-[10px] font-medium text-gray-400 dark:text-zinc-500 truncate" :title="profileSubtitle(p)">{{ profileSubtitle(p) }}</span>
                    </span>
                  </button>
                </div>
                <button type="button" @click="addProfile" :disabled="switchingProfile"
                        class="w-full flex items-center gap-2.5 px-2 py-1.5 rounded-sm text-xs font-bold text-gray-600 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
                  <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" /></svg>
                  Add profile
                </button>
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
    <!-- whitespace-pre, not nowrap: a tooltip may carry more than one line
         (the context gauge shows tokens and cost on separate lines), and
         neither should wrap. The interpolation sits tight against the tags so
         the preserved whitespace is only what the text itself holds. -->
    <div v-if="tooltipStore.visible"
      class="fixed z-[100] px-3 py-1.5 text-xs font-semibold text-black dark:text-white bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm shadow-lg pointer-events-none whitespace-pre"
      :style="tooltipStore.style">{{ tooltipStore.text }}</div>

    <!-- Global Toasts -->
    <Toast />

    <!-- Keyboard-driven overlays. Both are shell-level: the finder crosses
         workspaces, and the help sheet describes shortcuts a view may own. -->
    <CommandPalette :show="overlay === 'palette'" :shortcut-label="findTaskLabel" @close="closeOverlay()" />
    <WorkspaceSwitcher :show="overlay === 'switcher'" :current-workspace-id="currentWorkspaceId ?? ''" @close="closeOverlay()" />
    <ShortcutsHelp :show="overlay === 'help'" :mac="isMacKeyboard" @close="closeOverlay()" />

    <!-- What a shortcut drew. It is here rather than in a view because `x` then
         a key works wherever you are, so what it opens has to as well. -->
    <ExtensionViewPanel v-if="extensionPanel" :view="extensionPanel" @close="extensionSurfaces.dismiss" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useRegisterSW } from 'virtual:pwa-register/vue'
import { fetchUser, fetchWorkspaces, API_BASE_URL, TELEMETRY_UI_COPY_LINK, TELEMETRY_UI_SHORTCUT_USE } from './api'
// The whole module, because the WebMCP catalogue mirrors it function for
// function — naming each one here would be a second list to keep in step.
import * as api from './api'
import { useToasts } from './composables/useToasts'
import { useEventBus } from './useEventBus'
import { profileDisplay } from './composables/useProfileDisplay'
import { connectWebMCP } from './composables/useWebMCP'
import { recordUiAction } from './composables/useUiTelemetry'
import { usePlatformStore } from './stores/platformStore'
import {
  copyLinkTarget,
  copyTargetFromEvent,
  fileLinkFromEvent,
  followFileLink,
  writeClipboard,
} from './composables/useMarkdownLinks'
import { useThemeStore } from './stores/themeStore'
import { useTooltipStore } from './stores/tooltipStore'
import { useWorkspaceStore } from './stores/workspaceStore'
import { useFormat } from './composables/useFormat'
import { usePushNotifications } from './composables/usePushNotifications'
import Toast from './components/Toast.vue'
import { cacheTaskEvent, connectCache, sharedCache } from './composables/useCachedTasks'
import { forgetCachedTask, forgetEverything } from './composables/useCacheStorage'
import { SWEEP_INTERVAL_MS, sweepIfDue, whenIdle } from './composables/useCacheRetention'
import CommandPalette from './components/CommandPalette.vue'
import ShortcutsHelp from './components/ShortcutsHelp.vue'
import WorkspaceSwitcher from './components/WorkspaceSwitcher.vue'
import {
  SHORTCUTS,
  formatShortcut,
  newTaskRoute,
  useShortcuts,
  usesCommandKey,
} from './composables/useKeyboardShortcuts'
import { useExtensionPages } from './composables/useExtensionPages'
import { useExtensionShortcuts } from './composables/useExtensionShortcuts'
import ExtensionViewPanel from './components/ExtensionViewPanel.vue'

const appVersion = __APP_VERSION__
const { needRefresh, updateServiceWorker } = useRegisterSW()

const isUpdating = ref(false)

const handleUpdateNow = async () => {
  isUpdating.value = true
  needRefresh.value = false
  await updateServiceWorker(true)
}

const { toKebabCase } = useFormat()

const route = useRoute()
const router = useRouter()
const { notifySuccess, notifyInfo, notifyError } = useToasts()
const isLoginPage = computed(() => route.path === '/login')
const user = ref(null)
const isUserMenuOpen = ref(false)

const platformStore = usePlatformStore()

/** The clipboard, through the shell where there is one. */
const copyText = (text) =>
  writeClipboard(text, { bridge: window.agentrq?.clipboard, clipboard: navigator.clipboard })

/**
 * Acting on a link in rendered markdown: copying where it points, or — for a
 * `file:///…` link — following it.
 *
 * Delegated from the app root, so one wiring serves every view that injects a
 * message body as HTML. Enter and space are bound as well as click because a
 * file link is an anchor with no href — see `useMarkdownLinks` for why — and
 * something standing in for a link owes that to a person not using a mouse.
 */
async function onMarkdownLinkActivate(event) {
  const copyTarget = copyTargetFromEvent(event)
  if (copyTarget) {
    event.preventDefault()
    const copied = await copyLinkTarget(copyTarget, { copyText })
    const notify = copied.tone === 'error' ? notifyError : notifySuccess
    notify(copied.message, copied.title)
    // Only a copy that worked: a refused clipboard is a failure to count, not
    // a use of the feature.
    if (copied.tone !== 'error') recordUiAction(TELEMETRY_UI_COPY_LINK, route)
    return
  }

  const fileUrl = fileLinkFromEvent(event)
  if (!fileUrl) return
  event.preventDefault()

  const { tone, message } = await followFileLink(fileUrl, {
    isDesktop: platformStore.isDesktop,
    bridge: window.agentrq?.files,
    copyText,
  })

  // A file that simply opened says so by opening; a toast would only be noise.
  if (!message) return
  if (tone === 'error') notifyError(message, 'Could not open file')
  else notifyInfo(message)
}

// Signed-in profiles. Each is its own session in the desktop shell, so the
// browser build has nothing to show and the section stays hidden there.
const profiles = ref([])
const switchingProfile = ref(false)
const showProfiles = computed(() => platformStore.isDesktop && profiles.value.length > 0)

/** Whether this build has to draw its own window chrome. See style.css. */
const isMacDesktop = computed(() => platformStore.isMacDesktop)
const activeProfile = computed(() => profiles.value.find((p) => p.active) ?? null)
const otherProfiles = computed(() => profiles.value.filter((p) => !p.active))

const profileInitial = (p) => profileDisplay(p).initial
const profileTitle = (p) => profileDisplay(p).title
const profileSubtitle = (p) => profileDisplay(p).subtitle

async function loadProfiles() {
  if (!platformStore.isDesktop || !window.agentrq?.profiles) return
  try {
    const state = await window.agentrq.profiles.get()
    profiles.value = state?.profiles ?? []
  } catch {
    // Not being able to list profiles is not a reason to break the sidebar.
    profiles.value = []
  }
}

/**
 * Switching replaces the window from the shell side, so there is nothing to do
 * here afterwards — this renderer is about to be replaced along with it.
 */
async function switchToProfile(id) {
  if (switchingProfile.value) return
  switchingProfile.value = true
  try {
    await window.agentrq.profiles.switch(id)
  } catch (err) {
    switchingProfile.value = false
    notifyError('Could not switch profile: ' + err.message)
  }
}

/** A new profile has never signed in, so its window opens on the login screen. */
async function addProfile() {
  if (switchingProfile.value) return
  switchingProfile.value = true
  try {
    await window.agentrq.profiles.add('')
  } catch (err) {
    switchingProfile.value = false
    notifyError('Could not add a profile: ' + err.message)
  }
}
const isWorkspaceDropdownOpen = ref(false)
const isCollapsed = ref(true);
const isMobileMenuOpen = ref(false);
const workspaceDropdownRef = ref(null)
const userMenuRef = ref(null)
const themeStore = useThemeStore()
const tooltipStore = useTooltipStore()
const workspaceStore = useWorkspaceStore()
const workspaces = computed(() => workspaceStore.workspaces)

const currentWorkspaceId = computed(() => route.params.id || route.params.workspaceId)

/**
 * Enforce each workspace's retention limit, at most once a day.
 *
 * Started once the cache is open and repeated on a timer, because a desktop app
 * or a pinned tab can stay open for weeks and would otherwise sweep only at
 * launch. The daily guard lives in the database, so the interval firing more
 * often than that costs one cheap check.
 *
 * Deliberately not a background job: browsers do not reliably offer one, and
 * the only cost of sweeping while the app is open is holding data slightly
 * longer than asked.
 */
let sweepTimer = null
function startRetentionSweep(db) {
  if (!db || sweepTimer) return
  const run = () => whenIdle(() => sweepIfDue(db))
  run()
  sweepTimer = setInterval(run, SWEEP_INTERVAL_MS)
}

// --- Keyboard shortcuts -----------------------------------------------------
// The shell owns the ones that work anywhere. A view registers its own on top
// of these; see TaskDetailView for the chat/trajectory pair.

/**
 * Which full-screen overlay is up, if any: 'palette', 'switcher' or 'help'.
 *
 * One value rather than a flag each, because they are mutually exclusive and
 * every opener would otherwise have to remember to close the other two — a
 * rule that held for two overlays and would not survive a fourth.
 */
const overlay = ref(null)
const openOverlay = (name) => { overlay.value = name }
const closeOverlay = () => { overlay.value = null }

/** Command on a Mac keyboard, Control everywhere else. */
const isMacKeyboard = computed(() => usesCommandKey(platformStore.$state))
const findTaskLabel = computed(() =>
  formatShortcut(SHORTCUTS.find((s) => s.id === 'find-task'), { mac: isMacKeyboard.value })
)

useShortcuts(
  {
    'find-task': () => openOverlay('palette'),
    'new-task': () => router.push(newTaskRoute(currentWorkspaceId.value, workspaces.value)),
    'switch-workspace': () => openOverlay('switcher'),
    'show-help': () => openOverlay('help'),
  },
  { mac: () => isMacKeyboard.value, onUse: recordShortcutUse }
)

/**
 * Count a shortcut that actually ran.
 *
 * Both registrations report through the same function so the metric does not
 * depend on each place remembering to; the id is not sent, only that a
 * shortcut was used, because the telemetry route records an action and nothing
 * else about it.
 */
function recordShortcutUse() {
  recordUiAction(TELEMETRY_UI_SHORTCUT_USE, route)
}

/**
 * What extensions contribute, and the keys they claimed.
 *
 * Loaded once and refreshed when the Extensions screen says something changed —
 * a sidebar is redrawn on every navigation, and a bridge call per render would
 * be a call per keystroke in the address bar.
 */
const { pages: extensionPages, load: loadExtensionPages, surfaces: extensionSurfaces } = useExtensionPages()
const extensionPanel = extensionSurfaces.panel

const extensionKeys = ref([])

/**
 * `x` then a letter, which is the whole extension keyboard scheme.
 *
 * Registered here rather than beside our own shortcuts because it is a
 * *sequence*: `useShortcuts` dispatches single keys, and the prefix has to hold
 * state between two of them. `handle` answers whether it consumed the key, so
 * the application's own bare letters keep working when it did not.
 */
const extensionShortcuts = useExtensionShortcuts({
  entries: () => extensionKeys.value,
  onInvoke: (binding) => {
    recordShortcutUse()
    extensionSurfaces.invoke({ owner: binding.owner, id: binding.id, surface: 'shortcut' }, {})
  },
})

function onExtensionKey(event) {
  extensionShortcuts.handle(event)
}

/** Read what is installed now: the sidebar's pages, and the keys they hold. */
async function refreshExtensions() {
  if (!extensionSurfaces.available) return
  await loadExtensionPages()
  extensionKeys.value = await extensionSurfaces.entriesFor('shortcut', {})
}

// Escape closes an overlay wherever focus happens to be. The palette handles it
// on its own input too — this is for the help sheet, which has nothing focused.

const openCommandPaletteHandler = () => openOverlay('palette')

onMounted(() => {
  window.addEventListener('open-command-palette', openCommandPaletteHandler)
})

onUnmounted(() => {
  window.removeEventListener('open-command-palette', openCommandPaletteHandler)
})

const closeOverlaysOnEscape = (e) => {
  if (e.key !== 'Escape') return
  closeOverlay()
}

// Setup Global Event Bus (Global stream receives events for all workspaces)
//
// The shell owns this one subscription for the whole app: it is what keeps the
// workspace store — and with it every agent-connection indicator on screen —
// current, so a view never has to open a stream of its own just to learn that
// an agent came online.
//
// Handled per event rather than by watching the buffer. Reacting to a
// transition means seeing every event, and a watcher only ever showed the
// handler the last one in the flush.
const { connect, disconnect, isConnected, onEvent } = useEventBus(undefined, { buffer: false })

onEvent((event) => {
  // Handle agent connection status updates globally
  if (event.type === 'agent.connected') {
    const { connected, workspaceId } = event.payload
    workspaceStore.updateAgentStatus(workspaceId, connected)
  }

  // The agent's slash commands, which arrive once its session is up rather
  // than with the page. Same reasoning as the status above: reacting to the
  // event is what keeps every surface current.
  if (event.type === 'agent.commands') {
    const { commands, workspaceId } = event.payload
    workspaceStore.updateAgentCommands(workspaceId, commands)
  }

  // The agent's models, which arrive when its session comes up and again on
  // every switch. Same reasoning as the commands above: without this the model
  // named on the Overview card is whatever the last page load fetched, and a
  // switch never shows.
  if (event.type === 'agent.models') {
    const { configId, currentModel, canSet, models, workspaceId } = event.payload
    workspaceStore.updateAgentModels(workspaceId, { configId, currentModel, canSet, models })
  }

  // Handle workspace metadata updates
  if (event.type === 'workspace.updated') {
    workspaceStore.updateWorkspaceMetadata(event.payload)
  }

  // Keep the local copy current. Merged against what the cache already holds
  // rather than written straight through: a payload built without its relations
  // is indistinguishable from one whose relations are genuinely empty, and this
  // stream carries tasks nobody has open to merge against. See useCachedTasks.
  if (event.type === 'task.created' || event.type === 'task.updated') {
    cacheTaskEvent(sharedCache(), event.payload)
  }

  // A deleted task must not outlive itself on this device. Driven by the event
  // rather than by the button, so it also covers a task deleted from another
  // device or by an agent.
  if (event.type === 'task.deleted') {
    forgetCachedTask(sharedCache(), event.payload?.id)
  }

  if (event.type === 'task.created' && event.payload.createdBy === 'agent') {
    notifySuccess(`Agent started a new task: ${event.payload.title}`)
  } else if (event.type === 'reply.received') {
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
})

// Nothing replays the events missed while the stream was down, and a dropped
// `agent.connected` is invisible — the dot simply keeps showing the state from
// before the drop, forever. Re-reading the list on every reconnect is what
// bounds how long the indicator can be wrong to the length of the outage.
watch(isConnected, (now, before) => {
  if (now && before === false) workspaceStore.fetchWorkspaces()
})

// The Extensions screen is the only place an install or an uninstall happens,
// so leaving it is exactly when what they contribute can have changed.
watch(
  () => route.path.startsWith('/extensions'),
  (onScreen, wasOnScreen) => {
    if (wasOnScreen && !onScreen) refreshExtensions()
  }
)

onMounted(() => {
  themeStore.init()
  loadUser()
  loadProfiles()
  workspaceStore.fetchWorkspaces()
  connect() // Connect to global event stream
  refreshExtensions()
  document.addEventListener('click', handleClickOutside)
  window.addEventListener('keydown', closeOverlaysOnEscape)
  window.addEventListener('keydown', onExtensionKey)
})

const showTooltip = (event, text) => {
  if (!isCollapsed.value || window.innerWidth < 1024) return;
  tooltipStore.show(event, text);
}

const hideTooltip = () => {
  tooltipStore.hide();
}

/** The registration handle, so the tools can be withdrawn on sign-out. */
let webmcp = null

async function logout() {
  // Before anything else: these tools act as the signed-in user, and the page
  // is not reloaded on sign-out, so leaving them registered would leave an
  // agent holding the last person's session.
  webmcp?.unregister()
  webmcp = null
  await unsubscribePush()
  // Unconditional, and before the request: a browser several people use must
  // not leave one person's task titles readable by the next, and that has to
  // hold even if the sign-out call itself fails.
  await forgetEverything({ userId: user.value?.id })
  await fetch(`${API_BASE_URL}/auth/logout`, { method: 'POST' })
  router.push('/login')
}

const loadWorkspaces = () => workspaceStore.fetchWorkspaces()

const { unsubscribe: unsubscribePush } = usePushNotifications()

const loadUser = async () => {
  if (isLoginPage.value) return;
  try {
    user.value = await fetchUser()
    // The database is named for the signed-in user, so it cannot be opened
    // before we know who that is. Failing to open one is not an error worth
    // showing: the app reads from the network either way.
    if (user.value?.id) {
      const db = await connectCache(user.value.id)
      startRetentionSweep(db)
      offerWebMCPTools()
    }
  } catch (err) {
    console.error('Failed to fetch user:', err)
  }
}

/**
 * Hand the interface's own capabilities to an agent running in this browser.
 *
 * Only once signed in: the tools act as this user, with their cookie and their
 * permissions, so there is nothing to offer before we know who that is. The
 * browser is usually one without WebMCP at all, which is not a failure — the
 * catalogue is simply not registered and nothing else changes.
 */
async function offerWebMCPTools() {
  if (webmcp) return
  try {
    webmcp = await connectWebMCP({ api, router })
  } catch (err) {
    // An agent-facing extra must never cost the person their interface.
    console.error('Failed to register WebMCP tools:', err)
  }
}

const handleClickOutside = (e) => {
  if (workspaceDropdownRef.value && !workspaceDropdownRef.value.contains(e.target)) {
    isWorkspaceDropdownOpen.value = false
  }
  if (userMenuRef.value && !userMenuRef.value.contains(e.target)) {
    isUserMenuOpen.value = false
  }
}


onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('keydown', closeOverlaysOnEscape)
  window.removeEventListener('keydown', onExtensionKey)
  if (sweepTimer) clearInterval(sweepTimer)
})

watch(() => route.fullPath, (fullPath) => {
  isWorkspaceDropdownOpen.value = false
  isMobileMenuOpen.value = false
  isUserMenuOpen.value = false
  hideTooltip()
  
  const path = route.path;
  if (path === '/') document.title = 'Workspaces | AgentRQ';
  else if (path === '/login') document.title = 'Login | AgentRQ';
  else if (path.startsWith('/events')) document.title = 'Events | AgentRQ';
  else if (path.startsWith('/workflows')) document.title = 'Workflows | AgentRQ';
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

<style scoped>
.slide-down-enter-active,
.slide-down-leave-active {
  transition: transform 0.25s ease, opacity 0.25s ease;
}
.slide-down-enter-from,
.slide-down-leave-to {
  transform: translateY(-100%);
  opacity: 0;
}
</style>
