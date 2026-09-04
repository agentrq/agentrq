<template>
  <div class="h-full flex flex-col w-full max-w-full overflow-x-hidden relative bg-white dark:bg-zinc-900" v-if="task && workspace"
       @dragenter="onDragEnter"
       @dragover="onDragOver"
       @dragleave="onDragLeave"
       @drop="onDrop">

    <!-- Main Header Section (Matching KeywordInbox Design) -->
    <div class="px-1.5 md:px-4 pt-1 pb-1 shrink-0">
      <div class="flex flex-col gap-1">
        <!-- Title & Status Row -->
        <div class="flex items-start justify-between gap-2">
          <div class="flex items-center gap-1 flex-wrap flex-1 min-w-0">
            <button @click="router.back()" class="md:hidden h-7 w-7 -ml-1.5 text-gray-500 hover:text-black dark:hover:text-white transition-colors flex items-center justify-center" title="Go Back">
              <svg class="w-4.5 h-4.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" /></svg>
            </button>
            <!-- Removed workspace name on mobile per user request -->
            <h1 class="text-lg md:text-xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate flex-1 min-w-0">
              {{ task.title }}
            </h1>
          </div>

          <div class="flex items-center gap-1.5 shrink-0 relative z-10">
            <!-- Assignee Toggle -->
            <div class="flex p-0.5 bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700/50 rounded-lg h-7">
              <button @click.stop="updateAssignee('agent')"
                      @mouseenter="tooltipStore.show($event, 'Assign to Agent', 'bottom')"
                      @mouseleave="tooltipStore.hide()"
                      :class="task.assignee === 'agent' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                      class="px-1.5 rounded-md text-[8px] font-black uppercase tracking-tighter transition-all flex items-center justify-center">
                <span class="hidden sm:inline">Agent</span>
                <svg class="sm:hidden w-3 h-3" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
              </button>
              <button @click.stop="updateAssignee('human')"
                      @mouseenter="tooltipStore.show($event, 'Assign to Human (Stop Agent)', 'bottom')"
                      @mouseleave="tooltipStore.hide()"
                      :class="task.assignee === 'human' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                      class="px-1.5 rounded-md text-[8px] font-black uppercase tracking-tighter transition-all flex items-center justify-center">
                <span class="hidden sm:inline">Human</span>
                <svg class="sm:hidden w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
              </button>
            </div>

            <!-- Status Selector -->
            <div class="relative">
              <button @click.stop="isStatusMenuOpen = !isStatusMenuOpen"
                      @mouseenter="tooltipStore.show($event, 'Change Task Status', 'bottom')"
                      @mouseleave="tooltipStore.hide()"
                      class="px-2 md:px-4 text-[8px] font-black text-gray-700 dark:text-zinc-200 bg-gray-100 dark:bg-zinc-800 rounded-lg border border-transparent hover:border-black/10 transition-all flex items-center gap-1.5 shadow-sm uppercase tracking-tighter h-7">
                <div class="w-1.5 h-1.5 rounded-full" :class="getTaskDotStyle(task.status)"></div>
                <span class="hidden md:inline">{{ task.status }}</span>
                <svg class="w-2.5 h-2.5 transition-transform" :class="isStatusMenuOpen ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
              </button>
              <!-- Status Menu -->
              <div v-if="isStatusMenuOpen" v-click-outside="() => isStatusMenuOpen = false"
                   class="absolute right-0 top-full mt-2 w-12 md:w-40 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-2xl shadow-xl z-50 p-1 md:p-2 animate-in fade-in slide-in-from-top-2">
                <button v-for="s in ['notstarted', 'ongoing', 'completed', 'rejected']" :key="s"
                        @click="updateStatus(s); isStatusMenuOpen = false"
                        class="w-full flex items-center justify-center md:justify-start gap-3 px-3 py-3 md:py-2 text-[10px] font-bold uppercase tracking-widest text-gray-600 dark:text-zinc-100 hover:bg-gray-50 dark:hover:bg-zinc-800 rounded-xl transition-colors cursor-pointer"
                        :title="s">
                  <div class="w-2 h-2 rounded-full shrink-0" :class="getTaskDotStyle(s)"></div>
                  <span class="hidden md:inline text-gray-900 dark:text-zinc-100">{{ s }}</span>
                </button>
              </div>
            </div>

            <!-- Chat / Trajectory view toggle -->
            <div class="flex p-0.5 bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700/50 rounded-lg h-7">
              <button @click.stop="activeView = 'chat'"
                      @mouseenter="tooltipStore.show($event, `Message thread  ${viewShortcut('chat-view')}`, 'bottom')"
                      @mouseleave="tooltipStore.hide()"
                      :class="activeView === 'chat' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                      class="px-1.5 rounded-md text-[8px] font-black uppercase tracking-tighter transition-all flex items-center justify-center">
                <span class="hidden sm:inline">Chat</span>
                <svg class="sm:hidden w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8-1.06 0-2.077-.163-3.02-.463L3 21l1.51-4.532A7.965 7.965 0 013 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"/></svg>
              </button>
              <button @click.stop="activeView = 'trajectory'"
                      @mouseenter="tooltipStore.show($event, `Tool call trajectory  ${viewShortcut('trajectory-view')}`, 'bottom')"
                      @mouseleave="tooltipStore.hide()"
                      :class="activeView === 'trajectory' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                      class="px-1.5 rounded-md text-[8px] font-black uppercase tracking-tighter transition-all flex items-center gap-1 justify-center">
                <span class="hidden sm:inline">Trajectory</span>
                <svg class="sm:hidden w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M11.42 15.17L17.25 21A2.652 2.652 0 1021 17.25l-5.877-5.877M11.42 15.17l2.496-3.03c.317-.384.74-.626 1.208-.766M11.42 15.17l-4.655 5.653a2.548 2.548 0 11-3.586-3.586l6.837-5.63m5.108-.929a2.548 2.548 0 00-3.586-3.586l-6.837 5.63m5.108-.929l-4.655 5.653" /></svg>
                <span v-if="sortedToolCalls.length > 0" class="min-w-[13px] h-3 px-1 rounded-full bg-gray-900 dark:bg-white text-white dark:text-black text-[7px] flex items-center justify-center font-black">{{ sortedToolCalls.length }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- Body Content (Collapsed) -->
        <div class="mt-0.5">
          <div v-if="task.body" class="mb-1">
            <div @click="expandDescription"
                 :class="[
                   isDescriptionCollapsed 
                     ? 'truncate text-[11px] text-gray-500 dark:text-zinc-500 py-1 cursor-pointer font-medium hover:text-gray-800 dark:hover:text-zinc-100' 
                     : 'p-3 bg-gray-50/50 dark:bg-zinc-800/30 rounded-xl border border-gray-100 dark:border-zinc-800 text-[13px] text-gray-600 dark:text-zinc-300 animate-in fade-in slide-in-from-top-1 duration-200'
                 ]"
                 class="transition-all">
              <div v-if="isDescriptionCollapsed">
                {{ stripNote(task.body) }}
              </div>
              <div v-else>
                <div class="flex items-center justify-between mb-1.5 pb-1 border-b border-gray-100 dark:border-zinc-800/60">
                  <span class="text-[9px] font-semibold text-gray-400 dark:text-zinc-500 uppercase tracking-wider"></span>
                  <div class="flex items-center gap-1.5">
                    <button type="button" @click.stop="toggleTaskBodyRender"
                            :class="!isTaskBodyRaw ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                            class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">MD</button>
                    <button type="button" @click.stop="copyTaskBodyText"
                            class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded" title="Copy raw text">
                      <svg v-if="!taskBodyCopied" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                      <svg v-else class="w-2.5 h-2.5 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                    </button>
                    <button type="button" @click.stop="isDescriptionCollapsed = true"
                            class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded ml-1" title="Collapse details">
                      <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7"/></svg>
                    </button>
                  </div>
                </div>
                <div v-if="!isTaskBodyRaw" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(stripNote(task.body))"></div>
                <div v-else class="text-[13px] font-medium text-gray-800 dark:text-zinc-200 leading-relaxed whitespace-pre-wrap break-all">{{ stripNote(task.body) }}</div>
              </div>
            </div>
          </div>
          
          <!-- Attachments -->
          <div v-if="task.attachments && task.attachments.length > 0" class="mt-8 flex flex-wrap gap-3">
            <div v-for="(att, i) in task.attachments" :key="i"
                 @click="previewAttachment(att)"
                 class="flex items-center gap-3 px-4 py-2 rounded-xl border border-gray-100 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-800/50 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-all cursor-pointer group shadow-sm">
              <div class="w-6 h-6 flex items-center justify-center overflow-hidden rounded-lg">
                <img v-if="att.mimeType && att.mimeType.startsWith('image/')" :src="getAttachmentUrl(workspaceId, taskId, att.id)" class="w-full h-full object-cover" />
                <svg v-else class="w-4 h-4 text-gray-500 group-hover:text-black dark:group-hover:text-white transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>
              </div>
              <span class="text-[10px] font-bold text-gray-500 dark:text-zinc-400 group-hover:text-black dark:group-hover:text-white transition-colors uppercase tracking-widest">{{ att.filename }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Scrollable chat area -->
    <div v-if="activeView === 'chat'" ref="scrollContainer" class="flex-1 overflow-y-auto px-1 md:px-4 pt-0 pb-6 flex flex-col gap-4 scroll-smooth custom-scrollbar overflow-x-hidden relative" style="overscroll-behavior-y: contain;">

      <!-- Drag & Drop Overlay -->
      <div v-if="isDragging" class="absolute inset-0 bg-white/95 dark:bg-zinc-900/95 z-50 flex flex-col items-center justify-center border-4 border-dashed border-gray-300 dark:border-zinc-700 m-4 rounded-xl transition-all duration-200 animate-in fade-in zoom-in-95">
        <div class="flex flex-col items-center gap-3 text-center pointer-events-none">
          <div class="w-16 h-16 rounded-full bg-gray-100 dark:bg-zinc-800 flex items-center justify-center text-gray-600 dark:text-zinc-300 shadow-md">
            <svg class="w-8 h-8" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
            </svg>
          </div>
          <div>
            <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">Drop files to attach</p>
            <p class="text-[10px] text-gray-500 dark:text-zinc-500 mt-1">Files will be uploaded with your next message</p>
          </div>
        </div>
      </div>

      <!-- Messages -->
      <template v-for="m in displayMessages" :key="m.id">

        <!-- The agent's plan, indented under its avatar. The rest of the
             telemetry is not in the conversation but about it, and is read in
             the trajectory instead — see belongsInThread. -->
        <div v-if="isThreadTelemetry(m)" class="flex gap-3 animate-in fade-in slide-in-from-bottom-2 duration-300 max-w-full md:max-w-[90%] w-full">
          <div class="w-8 shrink-0"></div>
          <div class="flex flex-col items-start min-w-0 w-full">

            <!-- Plan: one card per plan, rewritten in place as the agent works -->
            <div class="w-full border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden shadow-sm"
                 :class="planIsWithdrawn(m) ? 'opacity-60' : ''">
              <div class="bg-gray-50 dark:bg-zinc-800/80 border-b border-gray-200 dark:border-zinc-700 px-3 py-2 flex items-center gap-2">
                <svg class="w-3.5 h-3.5 shrink-0 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" /></svg>
                <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200">Plan</span>
                <span v-if="planIsWithdrawn(m)" class="text-[8px] font-bold uppercase tracking-wider text-gray-400 dark:text-zinc-500">Withdrawn</span>
                <span class="flex-1"></span>
                <span v-if="planProgress(m)" class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 shrink-0">{{ planProgress(m).done }} / {{ planProgress(m).total }}</span>
              </div>
              <div class="p-3 min-w-0">
                <ul v-if="planContent(m).type === 'items' && planContent(m).entries.length" class="flex flex-col gap-1.5">
                  <li v-for="(entry, i) in planContent(m).entries" :key="i" class="flex items-start gap-2 text-[11px] min-w-0">
                    <span class="mt-[3px] w-3 h-3 shrink-0 rounded-[3px] border flex items-center justify-center"
                          :class="entry.done ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white'
                                  : entry.active ? 'border-gray-900 dark:border-white'
                                  : 'border-gray-300 dark:border-zinc-600'">
                      <svg v-if="entry.done" class="w-2 h-2 text-white dark:text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="4"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                      <span v-else-if="entry.active" class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white animate-pulse"></span>
                    </span>
                    <span class="min-w-0 break-words"
                          :class="entry.done ? 'line-through text-gray-400 dark:text-zinc-500'
                                  : entry.active ? 'font-semibold text-gray-900 dark:text-zinc-100'
                                  : 'text-gray-600 dark:text-zinc-400'">{{ entry.content }}</span>
                    <span v-if="entry.priority === 'high' && !entry.done" class="ml-auto shrink-0 text-[8px] font-bold uppercase tracking-wider text-amber-600 dark:text-amber-500">High</span>
                  </li>
                </ul>
                <a v-else-if="planContent(m).type === 'file'" :href="planContent(m).uri" target="_blank" rel="noopener noreferrer"
                   class="text-[11px] font-medium text-gray-700 dark:text-zinc-300 underline break-all">{{ planContent(m).uri }}</a>
                <div v-else class="md-body text-[12px] text-gray-600 dark:text-zinc-400"
                     v-html="renderMarkdown(planContent(m).type === 'markdown' ? planContent(m).content : telemetryText(m))"></div>
              </div>
            </div>
          </div>
        </div>

        <!-- Agent message — left aligned -->
        <div v-else-if="m.sender === 'agent'" class="group flex gap-3 animate-in fade-in slide-in-from-bottom-2 duration-300 max-w-full md:max-w-[90%]">
          <div class="w-8 h-8 rounded-full bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 flex items-center justify-center shrink-0 mt-0.5">
            <svg class="w-4 h-4 text-gray-700 dark:text-zinc-100" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
          </div>
          <div class="flex flex-col items-start min-w-0 max-w-full">
             <div class="bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm p-3.5 shadow-sm min-w-0 max-w-full">
               <div class="flex items-center justify-between mb-1.5">
                 <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400">Agent · {{ formatDateTime(m.createdAt) }}</span>
                 <div class="flex items-center gap-1 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity duration-150">
                   <button type="button" @click.stop="toggleMessageRender(m.id)"
                           :class="!rawMessages.has(m.id) ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                           class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">MD</button>
                   <button type="button" @click.stop="copyMessageText(m.id, m.text)"
                           class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded" title="Copy raw text">
                     <svg v-if="!copiedMessages.has(m.id)" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                     <svg v-else class="w-2.5 h-2.5 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                   </button>
                 </div>
               </div>
               <div v-if="!rawMessages.has(m.id)" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(m.text)"></div>
               <div v-else class="text-[13px] font-medium text-gray-800 dark:text-zinc-200 leading-relaxed whitespace-pre-wrap break-all">{{ m.text }}</div>

               <!-- Permission Request (agent message) -->
               <div v-if="m.metadata?.type === 'permission_request'" class="mt-4 border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden shadow-sm">
                 <div class="bg-gray-50 dark:bg-zinc-800/80 border-b border-gray-200 dark:border-zinc-700 px-3 py-2 flex items-center justify-between gap-3">
                   <div class="flex items-center gap-2">
                     <svg class="w-3.5 h-3.5 text-yellow-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                     <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200">Authorization Required</span>
                   </div>
                   <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-500 hidden sm:block">{{ permMeta(m).requestId }}</span>
                 </div>
                 <div class="p-3 flex flex-col gap-3 min-w-0">
                   <template v-if="m.metadata.status === 'pending'">
                     <div class="min-w-0">
                       <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-500">Action</span>
                       <div class="relative mt-0.5">
                         <pre class="text-[10px] font-mono bg-zinc-950 text-zinc-300 p-3 pr-8 rounded-sm overflow-x-auto whitespace-pre-wrap break-all custom-scrollbar">{{ permMeta(m).toolName }}</pre>
                         <button type="button" @click.stop="copyMessageText(m.id + '-action', permMeta(m).toolName)"
                                 class="absolute top-1.5 right-1.5 p-1 rounded bg-black/40 hover:bg-black/60 text-zinc-400 hover:text-white transition-colors" title="Copy">
                           <svg v-if="!copiedMessages.has(m.id + '-action')" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                           <svg v-else class="w-2.5 h-2.5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                         </button>
                       </div>
                     </div>
                     <div v-if="permMeta(m).inputPreview" class="relative">
                       <pre class="text-[10px] font-mono bg-zinc-950 text-zinc-300 p-3 pr-8 rounded-sm overflow-x-auto whitespace-pre-wrap break-all custom-scrollbar">{{ permMeta(m).inputPreview }}</pre>
                       <button type="button" @click.stop="copyMessageText(m.id + '-payload', permMeta(m).inputPreview)"
                               class="absolute top-1.5 right-1.5 p-1 rounded bg-black/40 hover:bg-black/60 text-zinc-400 hover:text-white transition-colors" title="Copy">
                         <svg v-if="!copiedMessages.has(m.id + '-payload')" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                         <svg v-else class="w-2.5 h-2.5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                       </button>
                     </div>

                     <!-- Pending verdict buttons -->
                     <div class="flex flex-wrap gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
                        <button @click="handleVerdict(permMeta(m).requestId, 'allow')"
                                :disabled="!!workspace.archivedAt"
                                class="px-3 py-1.5 rounded-sm bg-gray-900 hover:bg-black dark:bg-white dark:hover:bg-gray-100 text-white dark:text-black text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                          Allow Once
                        </button>
                        <button @click="handleVerdict(permMeta(m).requestId, 'allow_always')"
                                :disabled="!!workspace.archivedAt"
                                class="px-3 py-1.5 rounded-sm bg-white dark:bg-zinc-800 hover:bg-gray-50 dark:hover:bg-zinc-700 text-gray-700 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                          Always Allow
                        </button>
                        <button @click="handleVerdict(permMeta(m).requestId, 'deny')"
                                :disabled="!!workspace.archivedAt"
                                class="px-3 py-1.5 rounded-sm bg-red-50 hover:bg-red-100 dark:bg-red-500/10 dark:hover:bg-red-500/20 text-red-700 dark:text-red-500 text-[10px] font-semibold transition-all disabled:opacity-50 border border-red-100 dark:border-red-500/20">
                          Deny
                        </button>
                     </div>
                   </template>

                   <!-- Resolved verdict (collapsible) -->
                   <div v-else
                        @click="m._detailsExpanded = !m._detailsExpanded"
                        class="border rounded-sm cursor-pointer transition-all select-none overflow-hidden min-w-0"
                        :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-gray-200 dark:border-zinc-700 bg-gray-50 dark:bg-zinc-800/50' : 'border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/5'">
                     <div class="flex items-center gap-2.5 px-3 py-2 min-w-0">
                       <svg v-if="m.metadata.status === 'allow' || m.metadata.status === 'allow_always'" class="w-3.5 h-3.5 text-gray-700 dark:text-zinc-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                       <svg v-else class="w-3.5 h-3.5 text-red-600 dark:text-red-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                       <span class="text-[10px] font-semibold flex-1 min-w-0 truncate"
                             :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-gray-700 dark:text-zinc-100' : 'text-red-700 dark:text-red-500'">
                         {{ permMeta(m).toolName }}
                       </span>
                       <span class="text-[9px] font-semibold shrink-0"
                             :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-gray-500 dark:text-zinc-400' : 'text-red-600 dark:text-red-500'">
                         {{ m.metadata.status === 'deny' ? 'Denied' : m.metadata.status === 'cancelled' ? 'Stopped' : m.metadata.status === 'allow_always' ? 'Always' : 'Allowed' }}
                       </span>
                       <svg class="w-3 h-3 text-gray-500 shrink-0 transition-transform duration-200" :class="m._detailsExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
                     </div>
                     <div v-if="m._detailsExpanded" class="px-3 pb-3 pt-1 border-t border-dashed min-w-0"
                          :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-gray-200 dark:border-zinc-700' : 'border-red-200 dark:border-red-500/20'">
                       <div class="relative mb-2">
                         <pre class="text-[9px] font-mono bg-zinc-950 text-gray-300 p-2 pr-8 rounded overflow-x-auto whitespace-pre-wrap break-all">{{ permMeta(m).toolName }}</pre>
                         <button type="button" @click.stop="copyMessageText(m.id + '-action', permMeta(m).toolName)"
                                 class="absolute top-1.5 right-1.5 p-1 rounded bg-black/40 hover:bg-black/60 text-zinc-400 hover:text-white transition-colors" title="Copy">
                           <svg v-if="!copiedMessages.has(m.id + '-action')" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                           <svg v-else class="w-2.5 h-2.5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                         </button>
                       </div>
                       <div v-if="permMeta(m).inputPreview" class="relative mb-2">
                         <pre class="text-[9px] font-mono bg-zinc-950 text-gray-300 p-2 pr-8 rounded overflow-x-auto whitespace-pre-wrap break-all">{{ permMeta(m).inputPreview }}</pre>
                         <button type="button" @click.stop="copyMessageText(m.id + '-payload', permMeta(m).inputPreview)"
                                 class="absolute top-1.5 right-1.5 p-1 rounded bg-black/40 hover:bg-black/60 text-zinc-400 hover:text-white transition-colors" title="Copy">
                           <svg v-if="!copiedMessages.has(m.id + '-payload')" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                           <svg v-else class="w-2.5 h-2.5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                         </button>
                       </div>
                     </div>
                   </div>
                 </div>
               </div>

               <!-- Elicitation Request (agent message) -->
               <div v-else-if="m.metadata?.type === 'elicitation_request'" class="mt-4 border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden shadow-sm">
                 <div class="bg-gray-50 dark:bg-zinc-800/80 border-b border-gray-200 dark:border-zinc-700 px-3 py-2 flex items-center justify-between gap-3">
                   <div class="flex items-center gap-2">
                     <svg class="w-3.5 h-3.5 text-blue-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                     <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200">Input Requested</span>
                   </div>
                   <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-500 hidden sm:block">{{ m.metadata.requestId }}</span>
                 </div>
                 <div class="p-3 flex flex-col gap-3 min-w-0">
                   <template v-if="m.metadata.status === 'pending'">
                     <template v-if="m.metadata.mode === 'form'">
                       <div v-for="field in schemaFields(m)" :key="field.name" class="min-w-0">
                         <label class="text-[10px] font-semibold text-gray-700 dark:text-zinc-300">
                           {{ field.title || field.name }}<span v-if="(m.metadata.requestedSchema.required || []).includes(field.name)" class="text-red-500">*</span>
                         </label>
                         <p v-if="field.description" class="text-[9px] text-gray-400 dark:text-zinc-500 mb-1">{{ field.description }}</p>
                         <!-- Multi-select: array of primitives (checkboxes) -->
                         <div v-if="field.type === 'array' && fieldChoices(field)" class="flex flex-col gap-1.5 mt-1">
                           <label v-for="choice in fieldChoices(field)" :key="choice.value"
                                  class="flex items-center gap-2 px-2.5 py-1.5 rounded-sm border cursor-pointer transition-colors text-[11px]"
                                  :class="(getElicitFormValue(m, field.name) || []).includes(choice.value)
                                    ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-white'
                                    : 'border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600'">
                             <input type="checkbox" :value="choice.value"
                                    :checked="(getElicitFormValue(m, field.name) || []).includes(choice.value)"
                                    @change="toggleElicitArrayValue(m, field.name, choice.value)"
                                    class="sr-only" />
                             <span class="w-3.5 h-3.5 rounded-sm border flex items-center justify-center shrink-0"
                                   :class="(getElicitFormValue(m, field.name) || []).includes(choice.value) ? 'border-gray-900 dark:border-white bg-gray-900 dark:bg-white' : 'border-gray-300 dark:border-zinc-600'">
                               <svg v-if="(getElicitFormValue(m, field.name) || []).includes(choice.value)" class="w-2.5 h-2.5 text-white dark:text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                             </span>
                             <span class="flex flex-col">
                               <span class="font-medium">{{ choice.label }}</span>
                               <span v-if="choice.description" class="text-[9px] text-gray-400 dark:text-zinc-500">{{ choice.description }}</span>
                             </span>
                           </label>
                         </div>
                         <!-- Single-select: enum / oneOf / anyOf (radio) -->
                         <div v-else-if="fieldChoices(field)" class="flex flex-col gap-1.5 mt-1">
                           <label v-for="choice in fieldChoices(field)" :key="choice.value"
                                  class="flex items-center gap-2 px-2.5 py-1.5 rounded-sm border cursor-pointer transition-colors text-[11px]"
                                  :class="getElicitFormValue(m, field.name) === choice.value
                                    ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-white'
                                    : 'border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600'">
                             <input type="radio" :name="`${m.id}-${field.name}`" :value="choice.value"
                                    :checked="getElicitFormValue(m, field.name) === choice.value"
                                    @change="setElicitFormValue(m, field.name, choice.value)"
                                    class="sr-only" />
                             <span class="w-3.5 h-3.5 rounded-full border flex items-center justify-center shrink-0"
                                   :class="getElicitFormValue(m, field.name) === choice.value ? 'border-gray-900 dark:border-white' : 'border-gray-300 dark:border-zinc-600'">
                               <span v-if="getElicitFormValue(m, field.name) === choice.value" class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white"></span>
                             </span>
                             <span class="flex flex-col">
                               <span class="font-medium">{{ choice.label }}</span>
                               <span v-if="choice.description" class="text-[9px] text-gray-400 dark:text-zinc-500">{{ choice.description }}</span>
                             </span>
                           </label>
                         </div>
                         <label v-else-if="field.type === 'boolean'" class="flex items-center gap-2 mt-1">
                           <input type="checkbox" :checked="!!getElicitFormValue(m, field.name)" @change="setElicitFormValue(m, field.name, $event.target.checked)" />
                           <span class="text-[10px] text-gray-600 dark:text-zinc-400">Yes</span>
                         </label>
                         <input v-else :type="elicitInputType(field)"
                                :pattern="field.pattern" :minlength="field.minLength" :maxlength="field.maxLength"
                                :min="field.minimum" :max="field.maximum"
                                :value="getElicitFormValue(m, field.name)" @input="setElicitFormValue(m, field.name, $event.target.value)"
                                class="w-full mt-0.5 px-2 py-1.5 text-[11px] bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-800 dark:text-zinc-200" />
                       </div>
                       <div class="flex flex-wrap gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
                          <button @click="submitElicitation(m, 'accept')" :disabled="!!workspace.archivedAt"
                                  class="px-3 py-1.5 rounded-sm bg-gray-900 hover:bg-black dark:bg-white dark:hover:bg-gray-100 text-white dark:text-black text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                            Submit
                          </button>
                          <button @click="submitElicitation(m, 'decline')" :disabled="!!workspace.archivedAt"
                                  class="px-3 py-1.5 rounded-sm bg-white dark:bg-zinc-800 hover:bg-gray-50 dark:hover:bg-zinc-700 text-gray-700 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                            Decline
                          </button>
                       </div>
                     </template>
                     <template v-else>
                       <a :href="m.metadata.url" target="_blank" rel="noopener noreferrer"
                          class="text-[11px] text-blue-600 dark:text-blue-400 underline break-all">{{ m.metadata.url }}</a>
                       <div class="flex flex-wrap gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
                          <button @click="submitElicitation(m, 'accept')" :disabled="!!workspace.archivedAt"
                                  class="px-3 py-1.5 rounded-sm bg-gray-900 hover:bg-black dark:bg-white dark:hover:bg-gray-100 text-white dark:text-black text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                            I'm Done
                          </button>
                          <button @click="submitElicitation(m, 'cancel')" :disabled="!!workspace.archivedAt"
                                  class="px-3 py-1.5 rounded-sm bg-white dark:bg-zinc-800 hover:bg-gray-50 dark:hover:bg-zinc-700 text-gray-700 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                            Cancel
                          </button>
                       </div>
                     </template>
                   </template>
                   <div v-else
                        @click="m.metadata.content && (m._detailsExpanded = !m._detailsExpanded)"
                        class="border rounded-sm select-none overflow-hidden min-w-0 transition-all"
                        :class="[m.metadata.content ? 'cursor-pointer' : '', m.metadata.status === 'accept' ? 'border-gray-200 dark:border-zinc-700 bg-gray-50 dark:bg-zinc-800/50' : 'border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/5']">
                     <div class="flex items-center gap-2.5 px-3 py-2 min-w-0">
                       <svg v-if="m.metadata.status === 'accept'" class="w-3.5 h-3.5 text-gray-700 dark:text-zinc-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                       <svg v-else class="w-3.5 h-3.5 text-red-600 dark:text-red-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                       <span class="text-[10px] font-semibold flex-1 min-w-0 truncate" :class="m.metadata.status === 'accept' ? 'text-gray-700 dark:text-zinc-100' : 'text-red-700 dark:text-red-500'">
                         {{ m.metadata.status === 'accept' ? 'Answered' : m.metadata.status === 'decline' ? 'Declined' : 'Cancelled' }}
                       </span>
                       <svg v-if="m.metadata.content" class="w-3 h-3 text-gray-500 shrink-0 transition-transform duration-200" :class="m._detailsExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
                     </div>
                     <div v-if="m._detailsExpanded && m.metadata.content" class="px-3 pb-3 pt-1 border-t border-dashed border-gray-200 dark:border-zinc-700 min-w-0 flex flex-col gap-1.5">
                       <div v-for="(value, key) in m.metadata.content" :key="key" class="text-[10px] flex items-start gap-2 min-w-0">
                         <span class="font-semibold text-gray-500 dark:text-zinc-400 shrink-0">{{ elicitAnswerLabel(m, key) }}:</span>
                         <span class="text-gray-800 dark:text-zinc-200 break-all min-w-0">{{ formatElicitAnswerValue(value) }}</span>
                       </div>
                     </div>
                   </div>
                 </div>
               </div>

               <!-- Attachments on agent message -->
               <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-2 mt-3 pt-3 border-t border-gray-200 dark:border-zinc-700">
                 <div v-for="(att, i) in m.attachments" :key="i"
                      @click="previewAttachment(att)"
                      class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm border border-gray-200 dark:border-zinc-600 bg-white dark:bg-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-600 transition-colors cursor-pointer text-[9px] font-semibold text-gray-700 dark:text-zinc-200 shadow-sm">
                   <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                   <span class="truncate max-w-[140px]">{{ att.filename }}</span>
                 </div>
               </div>
             </div>
          </div>
        </div>

        <!-- Slack message — right aligned -->
        <div v-else-if="m.sender === 'slack'" class="group flex gap-3 flex-row-reverse animate-in fade-in slide-in-from-bottom-2 duration-300 self-end max-w-full md:max-w-[90%]">
          <div class="w-8 h-8 rounded-full bg-zinc-100 dark:bg-zinc-800 text-gray-700 dark:text-zinc-100 flex items-center justify-center shrink-0 mt-0.5 overflow-hidden p-1.5 border border-gray-200 dark:border-zinc-700 shadow-sm">
             <svg viewBox="0 0 127 127" class="w-4 h-4 text-[#4A154B] dark:text-zinc-300 animate-in spin-in-12 duration-500" fill="currentColor">
               <path d="M27.2 80c0 7.3-5.9 13.2-13.2 13.2C6.7 93.2.8 87.3.8 80c0-7.3 5.9-13.2 13.2-13.2h13.2V80zm6.6 0c0-7.3 5.9-13.2 13.2-13.2 7.3 0 13.2 5.9 13.2 13.2v33c0 7.3-5.9 13.2-13.2 13.2-7.3 0-13.2-5.9-13.2-13.2V80zM47 27.2c-7.3 0-13.2-5.9-13.2-13.2C33.8 6.7 39.7.8 47 .8c7.3 0 13.2 5.9 13.2 13.2V27.2H47zm0 6.6c7.3 0 13.2 5.9 13.2 13.2 0 7.3-5.9 13.2-13.2 13.2H14c-7.3 0-13.2-5.9-13.2-13.2 0-7.3 5.9-13.2 13.2-13.2h33zM99.8 47c0-7.3 5.9-13.2 13.2-13.2 7.3 0 13.2 5.9 13.2 13.2 0 7.3-5.9 13.2-13.2 13.2H99.8V47zm-6.6 0c0 7.3-5.9 13.2-13.2 13.2-7.3 0-13.2-5.9-13.2-13.2V14c0-7.3 5.9-13.2 13.2-13.2 7.3 0 13.2 5.9 13.2 13.2v33zM80 99.8c7.3 0 13.2 5.9 13.2 13.2 0 7.3-5.9 13.2-13.2 13.2-7.3 0-13.2-5.9-13.2-13.2V99.8H80zm0-6.6c-7.3 0-13.2-5.9-13.2-13.2 0-7.3 5.9-13.2 13.2-13.2h33c7.3 0 13.2 5.9 13.2 13.2 0 7.3-5.9-13.2-13.2-13.2H80z"/>
             </svg>
          </div>
          <div class="flex flex-col items-end min-w-0 max-w-full">
             <div class="bg-gray-100 text-gray-900 dark:bg-zinc-800 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 rounded-sm p-3.5 shadow-sm min-w-0 max-w-full">
               <div class="flex items-center justify-between mb-1.5">
                 <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 text-right">Slack ({{ getSlackUser(m) }}) · {{ formatDateTime(m.createdAt) }}</span>
                 <div class="flex items-center gap-1 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity duration-150">
                   <button type="button" @click.stop="toggleMessageRender(m.id)"
                           :class="!rawMessages.has(m.id) ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                           class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">MD</button>
                   <button type="button" @click.stop="copyMessageText(m.id, m.text)"
                           class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded" title="Copy raw text">
                     <svg v-if="!copiedMessages.has(m.id)" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                     <svg v-else class="w-2.5 h-2.5 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                   </button>
                 </div>
               </div>
               <div v-if="!rawMessages.has(m.id)" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(m.text)"></div>
               <div v-else class="text-[13px] font-medium leading-relaxed whitespace-pre-wrap text-right break-all text-gray-800 dark:text-zinc-200">{{ m.text }}</div>
               <!-- Attachments on slack message -->
               <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-2 mt-3 pt-3 border-t border-gray-200 dark:border-zinc-600 justify-end">
                 <div v-for="(att, i) in m.attachments" :key="i"
                      @click="previewAttachment(att)"
                      class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm border border-gray-200 dark:border-zinc-600 bg-white dark:bg-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-600 transition-colors cursor-pointer text-[9px] font-semibold text-gray-700 dark:text-zinc-200 shadow-sm">
                   <svg class="w-3 h-3 text-gray-500 dark:text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                   <span class="truncate max-w-[140px] text-gray-700 dark:text-gray-200">{{ att.filename }}</span>
                 </div>
               </div>
             </div>
          </div>
        </div>

        <!-- Human message — right aligned -->
        <div v-else class="group flex gap-3 flex-row-reverse animate-in fade-in slide-in-from-bottom-2 duration-300 self-end max-w-full md:max-w-[90%]" :class="m._pending ? 'opacity-80' : ''">
          <div class="w-8 h-8 rounded-full bg-gray-200 dark:bg-zinc-700 flex items-center justify-center shrink-0 mt-0.5 overflow-hidden">
             <svg class="w-4 h-4 text-gray-600 dark:text-zinc-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
          </div>
          <div class="flex flex-col items-end min-w-0 max-w-full">
             <div class="text-gray-900 dark:text-zinc-100 border rounded-sm p-3.5 shadow-sm min-w-0 max-w-full"
                  :class="m._pending ? 'bg-gray-100 dark:bg-zinc-800/60 border-dashed border-gray-300 dark:border-zinc-600' : 'bg-gray-200 dark:bg-zinc-800 border-gray-300 dark:border-zinc-700'">
               <div class="flex items-center justify-between mb-1.5">
                 <span v-if="m._pending" class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 text-right flex items-center gap-1.5">
                   <span class="w-1.5 h-1.5 rounded-full bg-gray-900 dark:bg-white animate-pulse shrink-0"></span>
                   Sending in {{ m._secondsLeft }}s&hellip;
                 </span>
                 <span v-else class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 text-right">You · {{ formatDateTime(m.createdAt) }}</span>
                 <div v-if="!m._pending" class="flex items-center gap-1 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity duration-150">
                   <button type="button" @click.stop="toggleMessageRender(m.id)"
                           :class="!rawMessages.has(m.id) ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                           class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">MD</button>
                   <button type="button" @click.stop="copyMessageText(m.id, m.text)"
                           class="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors p-0.5 rounded" title="Copy raw text">
                     <svg v-if="!copiedMessages.has(m.id)" class="w-2.5 h-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
                     <svg v-else class="w-2.5 h-2.5 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>
                   </button>
                 </div>
               </div>
               <div v-if="m._pending" dir="auto" class="text-[13px] font-medium leading-relaxed whitespace-pre-wrap break-all">{{ m.text }}</div>
               <div v-else-if="!rawMessages.has(m.id)" class="md-body text-[13px] text-gray-800 dark:text-zinc-200" v-html="renderMarkdown(m.text)"></div>
               <div v-else dir="auto" class="text-[13px] font-medium leading-relaxed whitespace-pre-wrap break-all">{{ m.text }}</div>
               <!-- Cancel / Send Now controls on the pending message -->
               <div v-if="m._pending" class="flex items-center gap-2 mt-3 pt-3 border-t border-dashed border-gray-300 dark:border-zinc-600 justify-end">
                 <button type="button" @click="cancelPendingSend" class="text-[10px] font-bold uppercase tracking-widest px-2.5 py-1 rounded-sm bg-white dark:bg-zinc-900 border border-gray-300 dark:border-zinc-700 text-gray-900 dark:text-zinc-100 hover:border-gray-400 dark:hover:border-zinc-500 transition-colors shrink-0">Cancel</button>
                 <button type="button" @click="sendPendingNow" class="text-[10px] font-bold uppercase tracking-widest px-2.5 py-1 rounded-sm bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 transition-opacity shrink-0">Send Now</button>
               </div>
               <!-- Attachments on human message -->
               <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-2 mt-3 pt-3 border-t border-gray-300 dark:border-zinc-600 justify-end">
                 <div v-for="(att, i) in m.attachments" :key="i"
                      @click="previewAttachment(att)"
                      class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm border border-gray-300 dark:border-zinc-600 bg-gray-100 dark:bg-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-600 transition-colors cursor-pointer text-[9px] font-semibold">
                   <svg class="w-3 h-3 text-gray-500 dark:text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                   <span class="truncate max-w-[140px] text-gray-700 dark:text-gray-200">{{ att.filename }}</span>
                 </div>
               </div>
             </div>
          </div>
        </div>

      </template>
    </div>

    <!-- Tool call trajectory (replaces the chat area in place) -->
    <TrajectoryPanel v-else :messages="sortedMessages" :tool-calls="sortedToolCalls" />

    <!-- Reply Box -->
    <footer v-if="activeView === 'chat' && !workspace.archivedAt" class="px-1 sm:px-4 py-2 sm:py-4 border-t border-gray-100 dark:border-zinc-800 shrink-0 z-20 bg-gray-50/50 dark:bg-zinc-900/50">

      <!-- Attachment previews -->
      <div v-if="replyAttachments.length > 0" class="flex flex-wrap gap-2 mb-3">
        <div v-for="(att, i) in replyAttachments" :key="i"
             class="flex items-center text-[10px] bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 rounded-lg px-2.5 py-1.5 font-bold shadow-sm">
          <span class="truncate max-w-[150px]">{{ att.filename }}</span>
          <button @click="replyAttachments.splice(i, 1)" class="ml-2 text-gray-500 hover:text-red-500 transition-colors">
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path d="M6 18L18 6M6 6l12 12"></path></svg>
          </button>
        </div>
      </div>

      <form @submit.prevent="submitReply">
        <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />

        <div class="flex flex-col bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm focus-within:border-gray-900 dark:focus-within:border-white focus-within:ring-0 transition-all group relative min-w-0 shadow-sm">
          <!-- Textarea on top -->
          <textarea
            ref="textareaRef"
            v-model="replyText"
            @input="adjustTextareaHeight"
            @keydown.meta.enter="submitReply"
            @keydown.ctrl.enter="submitReply"
            rows="1"
            :disabled="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending')"
            :placeholder="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending') ? 'Waiting for agent...' : 'Type instructions... (Cmd ⌘ + Enter to send)'"
            class="w-full px-3.5 pt-3 pb-1.5 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none border-none focus:outline-none focus:ring-0 disabled:opacity-50 resize-none min-h-[46px] max-h-[150px] custom-scrollbar"
          ></textarea>

          <!-- Bottom Toolbar -->
          <div class="flex items-center justify-between px-3 pb-2 pt-1">
            <!-- Left actions (Attachment paperclip & Mode info badge) -->
            <div class="flex items-center gap-2">
              <button type="button" @click="$refs.fileInput.click()"
                      :disabled="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending')"
                      class="h-6 w-6 rounded-sm text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-105 dark:hover:bg-zinc-700 transition-colors flex items-center justify-center disabled:opacity-30"
                      title="Attach files">
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path>
                </svg>
              </button>
              <!-- Speech-to-Text Mic -->
              <button v-if="sttSupported" type="button" @click="sttToggle"
                      :disabled="sttTranscribing || (!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending')"
                      @mouseenter="tooltipStore.show($event, sttRecording ? 'Stop recording' : sttTranscribing ? (sttModelLoading ? `Loading model... ${sttProgress}%` : 'Transcribing...') : 'Voice input', 'top')"
                      @mouseleave="tooltipStore.hide()"
                      :class="[
                        sttRecording ? 'bg-red-500 text-white border-red-500' : sttTranscribing ? 'bg-gray-200 dark:bg-zinc-600 text-gray-500 dark:text-zinc-300 border-transparent' : 'bg-gray-105 dark:bg-zinc-700/50 text-gray-400 dark:text-zinc-500 border-transparent hover:text-gray-700 dark:hover:text-zinc-300'
                      ]"
                      class="h-6 w-6 rounded-sm border transition-all flex items-center justify-center disabled:opacity-30 relative">
                <!-- Recording: pulsing dot -->
                <span v-if="sttRecording" class="w-2 h-2 rounded-full bg-white animate-pulse"></span>
                <!-- Transcribing: spinner -->
                <svg v-else-if="sttTranscribing" class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                <!-- Idle: mic icon -->
                <svg v-else class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4M12 15a3 3 0 003-3V5a3 3 0 00-6 0v7a3 3 0 003 3z" /></svg>
              </button>
              <!-- YOLO Toggle -->
              <button type="button" @click.stop="toggleYOLO"
                      @mouseenter="tooltipStore.show($event, task.allowAllCommands ? 'YOLO Active: Agent will execute all commands without approval' : 'YOLO Mode: Skip approval for sensitive commands', 'top')"
                      @mouseleave="tooltipStore.hide()"
                      :class="task.allowAllCommands ? 'bg-gray-600 text-white border-transparent' : 'bg-gray-105 dark:bg-zinc-700/50 text-gray-400 dark:text-zinc-500 border-transparent hover:text-gray-700 dark:hover:text-zinc-300'"
                      class="flex items-center gap-1 px-2 h-6 rounded-sm border transition-all text-[9px] font-bold uppercase tracking-wider">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M17.657 18.657A8 8 0 016.343 7.343S7 9 9 10c0-2 .5-5 2.986-7C14 5 16.09 5.777 17.656 7.343A7.99 7.99 0 0120 13a7.98 7.98 0 01-2.343 5.657z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 16.121A3 3 0 1012.015 11L11 14l2.015-2.879z" /></svg>
                YOLO
              </button>
              <!-- Context window. Not a message: the counters describe the
                   session as a whole and only the current value means
                   anything, so they sit here where they stay in view rather
                   than scrolling away up the thread. -->
              <div v-if="contextUsage" role="img" tabindex="0"
                   :aria-label="contextUsage.tooltip"
                   @mouseenter="tooltipStore.show($event, contextUsage.tooltip, 'top')"
                   @mouseleave="tooltipStore.hide()"
                   @focus="tooltipStore.show($event, contextUsage.tooltip, 'top')"
                   @blur="tooltipStore.hide()"
                   class="h-6 w-6 rounded-sm bg-gray-105 dark:bg-zinc-700/50 flex items-center justify-center cursor-default focus:outline-none focus-visible:ring-1 focus-visible:ring-gray-400 dark:focus-visible:ring-zinc-500">
                <svg class="w-3.5 h-3.5 -rotate-90" viewBox="0 0 20 20" aria-hidden="true">
                  <circle cx="10" cy="10" r="8" fill="none" stroke-width="3.5" stroke="currentColor" class="text-gray-300 dark:text-zinc-600" />
                  <circle cx="10" cy="10" r="8" fill="none" stroke-width="3.5" stroke-linecap="round" stroke="currentColor"
                          :stroke-dasharray="contextUsage.dashArray"
                          :stroke-dashoffset="contextUsage.dashOffset"
                          :class="contextUsage.tone === 'critical' ? 'text-red-500'
                                  : contextUsage.tone === 'high' ? 'text-amber-500'
                                  : 'text-gray-500 dark:text-zinc-300'"
                          class="transition-all duration-500" />
                </svg>
              </div>
            </div>

            <!-- Right actions: stop, then send -->
            <div class="flex items-center gap-2 shrink-0">
              <!-- Stop sits beside Send because that is where a reply is
                   composed, which is the moment someone decides the agent has
                   gone the wrong way.

                   Shown only while the agent is working, and only when what is
                   connected can actually be stopped. Not every agent can be:
                   stopping travels over a notification only the ACP gateway
                   acts on, so offering it to Claude Code speaking MCP directly
                   would be a button that reports success and changes nothing.

                   type="button" is load-bearing — this sits inside the reply
                   form, and a button without it submits the form on click. -->
              <button v-if="task.status === 'ongoing' && workspace?.agentSupportsStop"
                      type="button"
                      @click.stop="stopRunningTask"
                      @mouseenter="tooltipStore.show($event, 'Stop the agent working on this task', 'top')"
                      @mouseleave="tooltipStore.hide()"
                      class="h-6 w-6 rounded-sm border border-transparent dark:bg-zinc-700/50 text-gray-400 dark:text-zinc-500 hover:text-red-600 dark:hover:text-red-400 hover:border-red-300 dark:hover:border-red-800 transition-all flex items-center justify-center"
                      title="Stop the agent working on this task">
                <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="6" width="12" height="12" rx="1"></rect></svg>
              </button>

              <!-- Right circular send button -->
              <button type="submit"
                      :disabled="(!replyText.trim() && replyAttachments.length === 0) || (task.assignee !== 'human' && (!workspace.agentConnected || task.status === 'notstarted' || task.status === 'pending'))"
                      class="h-6 w-6 rounded-full bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 disabled:opacity-30 transition-all flex items-center justify-center shrink-0 shadow-sm"
                      title="Send Message">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M5 10l7-7m0 0l7 7m-7-7v18" />
                </svg>
              </button>
            </div>
          </div>
        </div>

        <!-- Status Warning Messages -->
        <div v-if="!workspace.agentConnected && task.assignee !== 'human'" class="flex items-center gap-3 mt-2 px-3 py-2 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/20 rounded-sm">
             <span class="w-2.5 h-2.5 rounded-full bg-red-500 animate-pulse shrink-0"></span>
             <p class="text-[10px] text-red-700 dark:text-red-400 font-bold">Agent Offline. Messages cannot be delivered.</p>
        </div>

        <div v-else-if="task.assignee !== 'human' && (task.status === 'notstarted' || task.status === 'pending')" class="flex items-center gap-3 mt-2 px-3 py-2 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/20 rounded-sm">
             <span class="w-2.5 h-2.5 rounded-full bg-amber-400 shrink-0"></span>
             <p class="text-[10px] text-amber-700 dark:text-amber-400 font-bold">Task must be started before messaging.</p>
        </div>
      </form>
    </footer>

    <!-- Attachment Preview Modal -->
    <div v-if="selectedAtt" class="fixed inset-0 z-[110] flex items-center justify-center" @keydown.esc="selectedAtt = null">
      <div class="absolute inset-0 bg-black/80 backdrop-blur-sm" @click="selectedAtt = null"></div>
      <button @click="selectedAtt = null" class="absolute top-6 right-6 text-white/50 hover:text-white z-20 p-2 rounded-full bg-white/10 hover:bg-white/20 transition-all">
        <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M6 18L18 6M6 6l12 12"></path></svg>
      </button>
      <div class="relative max-w-[90vw] max-h-[85vh] flex flex-col items-center gap-4 z-10">
        <div class="rounded-sm overflow-hidden flex items-center justify-center min-w-[300px] bg-black shadow-2xl border border-white/10">
          <img v-if="selectedAtt.mimeType?.startsWith('image/')" :src="getAttachmentUrl(workspaceId, taskId, selectedAtt.id)" class="max-w-full max-h-[70vh] object-scale-down" />
          <video v-else-if="selectedAtt.mimeType?.startsWith('video/')" controls autoplay :src="getAttachmentUrl(workspaceId, taskId, selectedAtt.id)" class="max-w-full max-h-[70vh]" />
          <div v-else-if="selectedAtt.mimeType?.startsWith('audio/')" class="p-16 flex flex-col items-center gap-6">
            <div class="w-20 h-20 rounded-full bg-gray-900 dark:bg-white flex items-center justify-center shadow-lg">
              <svg class="w-10 h-10 text-white" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
            </div>
            <audio controls autoplay :src="getAttachmentUrl(workspaceId, taskId, selectedAtt.id)" class="w-[400px]" />
          </div>
          <iframe v-else-if="selectedAtt.mimeType?.includes('pdf')" :src="getAttachmentUrl(workspaceId, taskId, selectedAtt.id)" class="w-[80vw] h-[75vh]" frameborder="0"></iframe>
          <div v-else class="p-20 flex flex-col items-center gap-4">
            <svg class="w-24 h-24 text-white/20" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1"><path d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" /></svg>
            <p class="text-white font-bold text-sm">{{ selectedAtt.filename }}</p>
          </div>
        </div>
        <div class="flex items-center gap-4 px-6 py-3 bg-zinc-900 border border-zinc-800 rounded-sm shadow-xl">
          <div class="flex flex-col">
            <p class="text-xs font-semibold text-white truncate max-w-[250px]">{{ selectedAtt.filename }}</p>
            <p class="text-[9px] font-semibold text-zinc-400">{{ selectedAtt.mimeType }}</p>
          </div>
          <div class="w-px h-8 bg-zinc-700"></div>
          <a :href="getAttachmentUrl(workspaceId, taskId, selectedAtt.id)" :download="selectedAtt.filename"
             class="flex items-center gap-2 px-4 py-2 rounded-sm bg-white text-black text-[10px] font-semibold hover:bg-gray-100 transition-all">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"></path></svg>
            Download
          </a>
        </div>
      </div>
    </div>

    <!-- Custom Tooltip -->
    <div v-if="tooltip.visible"
      class="fixed z-[100] px-3 py-1.5 text-[9px] font-semibold text-black dark:text-white bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm shadow-lg pointer-events-none transform -translate-x-1/2 whitespace-nowrap"
      :style="tooltip.style">
      {{ tooltip.text }}
    </div>
  </div>

  <!-- Loading State -->
  <div v-else class="h-full flex flex-col items-center justify-center bg-transparent">
    <div class="p-8 flex flex-col items-center gap-4 opacity-50">
      <div class="w-12 h-12 rounded-full border-4 border-gray-200 dark:border-zinc-700 border-t-gray-900 dark:border-t-white animate-spin"></div>
      <p class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500">Loading Context...</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, onUnmounted, watch, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, fetchTasks, archiveWorkspace, unarchiveWorkspace, updateWorkspace, getWorkspaceToken, getTask, updateTaskStatus, respondToTask, updateTaskAssignee, getAttachmentUrl, sendPermissionVerdict, respondToElicitation, stopTask, updateTaskAllowAllCommands, fetchUser } from '../api';
import { useTooltipStore } from '../stores/tooltipStore';
import { useToasts } from '../composables/useToasts';
import { useViewport } from '../composables/useViewport';
import { useSpeechToText } from '../composables/useSpeechToText';
import { usePendingSend } from '../composables/usePendingSend';
import { scrollToBottom as scrollContainerToBottom, shouldScrollOnViewChange } from '../composables/useChatScroll';
import { useEventBus } from '../useEventBus';
import { renderMarkdown } from '../utils/markdown';
import {
  contextGauge,
  isThreadTelemetry,
  latestUsageMessage,
  planContent,
  planIsWithdrawn,
  planProgress,
  telemetryText,
} from '../composables/useAgentTelemetry';
import { belongsInThread } from '../composables/useTrajectory';
import { mergeTaskUpdate } from '../composables/useTaskEvents';
import TrajectoryPanel from '../components/TrajectoryPanel.vue';
import {
  SHORTCUTS,
  formatShortcut,
  useShortcuts,
  usesCommandKey,
} from '../composables/useKeyboardShortcuts';
import { usePlatformStore } from '../stores/platformStore';
import { cacheTask, cacheTaskUpdate, sharedCache } from '../composables/useCachedTasks';

const { notifyError, notifySuccess } = useToasts();
const tooltipStore = useTooltipStore();
const platformStore = usePlatformStore();
const route = useRoute();
const router = useRouter();
const workspaceId = computed(() => route.params.id || route.params.workspaceId);
const taskId = computed(() => route.params.taskId);

const workspace = ref(null);
const task = ref(null);
const user = ref(null);
const descExpanded = ref(false);
const replyText = ref('');
const replyAttachments = ref([]);
// A held message is addressed to the task it was written in, so switching tasks
// cannot redirect it. See usePendingSend.
const {
  pending: pendingSend,
  start: holdMessage,
  flush: flushHeldMessage,
  cancel: takeBackHeldMessage,
} = usePendingSend({ deliver: (held) => deliverReply(held.text, held.atts, held.target) });

const {
  isRecording: sttRecording,
  isTranscribing: sttTranscribing,
  isModelLoading: sttModelLoading,
  modelProgress: sttProgress,
  error: sttError,
  isSupported: sttSupported,
  toggleRecording: sttToggle,
} = useSpeechToText(replyText, workspaceId);
const scrollContainer = ref(null);

const isDragging = ref(false);
let dragCounter = 0;

// Normalizes permission_request metadata: the API now sends camelCase keys,
// but messages persisted before that change still have snake_case keys.
function permMeta(m) {
  const md = m.metadata || {};
  const toolName = md.toolName ?? md.tool_name;
  const rawPreview = md.inputPreview ?? md.input_preview;
  // Some harnesses relay the same command as both toolName and inputPreview
  // (no distinct structured payload) — don't render it twice in that case.
  const inputPreview = rawPreview && rawPreview !== toolName ? rawPreview : null;
  return {
    requestId: md.requestId ?? md.request_id,
    toolName,
    inputPreview,
  };
}

const rawMessages = ref(new Set());
function toggleMessageRender(id) {
  const s = new Set(rawMessages.value);
  s.has(id) ? s.delete(id) : s.add(id);
  rawMessages.value = s;
}
const copiedMessages = ref(new Set());
async function copyMessageText(id, text) {
  await navigator.clipboard.writeText(text || '');
  const s = new Set(copiedMessages.value);
  s.add(id);
  copiedMessages.value = s;
  setTimeout(() => {
    const s2 = new Set(copiedMessages.value);
    s2.delete(id);
    copiedMessages.value = s2;
  }, 1500);
}

function processFiles(files) {
  if (!files || files.length === 0) return;
  for (const file of files) {
    const reader = new FileReader();
    reader.onload = (event) => {
      const base64Str = event.target.result.split(',')[1];
      replyAttachments.value.push({
        filename: file.name,
        mimeType: file.type,
        data: base64Str
      });
    };
    reader.readAsDataURL(file);
  }
}

function onDragEnter(e) {
  e.preventDefault();
  if (workspace.value?.archivedAt) return;
  if (!workspace.value?.agentConnected && task.value?.assignee !== 'human' && task.value?.status !== 'pending') {
    return;
  }
  dragCounter++;
  isDragging.value = true;
}

function onDragLeave(e) {
  e.preventDefault();
  dragCounter--;
  if (dragCounter <= 0) {
    isDragging.value = false;
    dragCounter = 0;
  }
}

function onDragOver(e) {
  e.preventDefault();
}

function onDrop(e) {
  e.preventDefault();
  isDragging.value = false;
  dragCounter = 0;
  if (workspace.value?.archivedAt) return;
  if (!workspace.value?.agentConnected && task.value?.assignee !== 'human' && task.value?.status !== 'pending') {
    return;
  }
  processFiles(e.dataTransfer.files);
}
const isStatusMenuOpen = ref(false);
const isDescriptionCollapsed = ref(true);
const isTaskBodyRaw = ref(false);
const taskBodyCopied = ref(false);

function expandDescription() {
  if (isDescriptionCollapsed.value) {
    isDescriptionCollapsed.value = false;
  }
}

function toggleTaskBodyRender() {
  isTaskBodyRaw.value = !isTaskBodyRaw.value;
}

async function copyTaskBodyText() {
  const text = stripNote(task.value?.body || '');
  await navigator.clipboard.writeText(text);
  taskBodyCopied.value = true;
  setTimeout(() => {
    taskBodyCopied.value = false;
  }, 1500);
}


const tooltip = ref({
  visible: false,
  text: '',
  style: { top: '0px', left: '0px' }
});

const showTooltip = (event, text) => {
  const rect = event.currentTarget.getBoundingClientRect();
  tooltip.value = {
    visible: true,
    text: text,
    style: {
      top: `${rect.bottom + 8}px`,
      left: `${rect.left + (rect.width / 2)}px`
    }
  };
};

const hideTooltip = () => {
  tooltip.value.visible = false;
};

const { isMobile } = useViewport();
const showHeader = ref(true);

const { connect, disconnect, events } = useEventBus(workspaceId);

const sortedMessages = computed(() => {
  if (!task.value || !task.value.messages) return [];
  return [...task.value.messages].sort((a,b) => new Date(a.createdAt) - new Date(b.createdAt));
});

// What the conversation shows: what was said, and anything still waiting on a
// human. The agent's reasoning and its resolved permission cards are how the
// answer came about rather than part of it, and are read in the trajectory —
// see belongsInThread.
const threadMessages = computed(() => sortedMessages.value.filter(belongsInThread));

// The session's context and cost as they stand, or null until an agent reports
// them. Only a connected ACP gateway sends these.
const contextUsage = computed(() => contextGauge(latestUsageMessage(sortedMessages.value)));

// Appends a synthetic, not-yet-sent message bubble while a delayed send is
// counting down, so the human can see and cancel exactly what's about to go
// out instead of it disappearing into the composer footer.
const displayMessages = computed(() => {
  if (!pendingSend.value) return threadMessages.value;
  return [...threadMessages.value, {
    id: '__pending-send__',
    sender: 'human',
    text: pendingSend.value.text,
    attachments: pendingSend.value.atts,
    _pending: true,
    _secondsLeft: pendingSend.value.secondsLeft,
  }];
});

const activeView = ref('chat');

// The two view shortcuts are registered by the view that owns the state rather
// than by the shell, which has no `activeView` to switch. Registering on mount
// also means they only exist while a task is open — pressing C on the board
// does nothing, which is correct.
useShortcuts(
  {
    'chat-view': () => { activeView.value = 'chat'; },
    'trajectory-view': () => { activeView.value = 'trajectory'; },
  },
  { mac: () => usesCommandKey(platformStore.$state) }
);

/** The key hint shown in each toggle's tooltip. */
const viewShortcut = (id) =>
  formatShortcut(SHORTCUTS.find((s) => s.id === id), { mac: usesCommandKey(platformStore.$state) });

const sortedToolCalls = computed(() => {
  if (!task.value || !task.value.toolCalls) return [];
  return [...task.value.toolCalls].sort((a,b) => new Date(a.createdAt) - new Date(b.createdAt));
});

watch(() => sortedMessages.value.length, (count) => {
  if (count === 0 && task.value?.body) {
    isDescriptionCollapsed.value = false;
  }
}, { immediate: true });

function scrollToBottom() {
  // The container is resolved after the wait, not before: see useChatScroll.
  scrollContainerToBottom(() => scrollContainer.value);
}

// Not deep: a new/updated message always produces a new array from the
// computed above, so a shallow watch already catches it. Deep watching would
// also fire on in-place UI-only mutations on existing messages (e.g. toggling
// a resolved permission card's expanded state), yanking the scroll position
// to the bottom just from expanding a card.
watch(sortedMessages, () => {
  scrollToBottom();
});

// The chat pane is behind a v-if, so switching to History destroys the scroll
// container and coming back builds a fresh one sitting at the top. Nothing
// else brings it down again — the message list has not changed, so the watcher
// above never fires — which left the reader at the oldest message.
watch(activeView, (view) => {
  if (shouldScrollOnViewChange(view)) scrollToBottom();
});

// Scroll when the pending bubble first appears, not on every countdown tick
// thereafter — ticking secondsLeft rebuilds displayMessages every second, and
// re-scrolling on that would yank the view out from under anyone reading up.
watch(() => !!pendingSend.value, (isPending) => {
  if (isPending) scrollToBottom();
});

async function load() {
  try {
    user.value = await fetchUser();
    const pRes = await getWorkspace(workspaceId.value);
    workspace.value = pRes.workspace;
    const tRes = await getTask(workspaceId.value, taskId.value);
    task.value = tRes.task;
    // The server is the source of truth for an open task, so the cache is
    // brought in line with what it just said rather than the other way round.
    cacheTask(sharedCache(), tRes.task);
    connect();
    nextTick(() => {
      scrollToBottom();
    });
  } catch(err) {
    console.error(err);
    notifyError("Failed to load task context: " + err.message);
  }
}

// Automatically reload task when route param changes (important for nested routes)
watch(() => route.params.taskId, (newTaskId, oldTaskId) => {
  if (newTaskId === oldTaskId) return;

  // Two things the composer must not carry across tasks. A message still
  // counting down goes to the task it was written in, before that task stops
  // being the one on screen. And a half-typed draft belongs to the task it was
  // typed into, so the next one opens empty rather than pre-filled with
  // something meant for someone else.
  flushHeldMessage();
  replyText.value = '';
  replyAttachments.value = [];
  nextTick(() => {
    adjustTextareaHeight();
  });

  if (newTaskId && newTaskId !== task.value?.id) {
    disconnect();
    load();
  }
});

async function handleFileUpload(e) {
  processFiles(e.target.files);
  e.target.value = '';
}

const handleVerdict = async (requestId, behavior) => {
  try {
    await sendPermissionVerdict(workspaceId.value, taskId.value, requestId, behavior);
    notifySuccess("Verdict sent successfully");
  } catch (err) {
    notifyError('Failed to send verdict: ' + err.message);
  }
};

function schemaFields(m) {
  const props = m.metadata?.requestedSchema?.properties;
  if (!props) return [];
  return Object.entries(props).map(([name, def]) => ({ name, ...def }));
}

// Normalizes a field's enum, whichever form it's expressed in (ACP allows
// both a plain `enum: [...]` array and titled `oneOf`/`anyOf` options), into
// a consistent {value, label, description} list. For an array-typed field
// (multi-select) the choices come from its `items` sub-schema instead.
function fieldChoices(field) {
  const source = field.type === 'array' ? field.items : field;
  if (!source) return null;
  if (Array.isArray(source.enum)) {
    return source.enum.map(v => ({ value: v, label: String(v) }));
  }
  const options = source.oneOf || source.anyOf;
  if (Array.isArray(options)) {
    return options.map(o => ({ value: o.const, label: o.title || String(o.const), description: o.description }));
  }
  return null;
}

// Maps a schema property to the best-matching native <input type="...">,
// so browser-native validation/keyboards apply (e.g. a numeric keypad on
// mobile for 'number', native date picker for 'date').
function elicitInputType(field) {
  if (field.type === 'number' || field.type === 'integer') return 'number';
  const byFormat = { email: 'email', date: 'date', 'date-time': 'datetime-local', uri: 'url', url: 'url' };
  return byFormat[field.format] || 'text';
}

const elicitFormValues = ref({});
function getElicitFormValue(m, field) {
  return (elicitFormValues.value[m.id] || {})[field];
}
function setElicitFormValue(m, field, value) {
  if (!elicitFormValues.value[m.id]) elicitFormValues.value[m.id] = {};
  elicitFormValues.value[m.id][field] = value;
}
function toggleElicitArrayValue(m, field, value) {
  if (!elicitFormValues.value[m.id]) elicitFormValues.value[m.id] = {};
  const current = elicitFormValues.value[m.id][field] || [];
  elicitFormValues.value[m.id][field] = current.includes(value)
    ? current.filter(v => v !== value)
    : [...current, value];
}

async function submitElicitation(m, action) {
  const requestId = m.metadata?.requestId;
  if (!requestId) return;
  const content = action === 'accept' && m.metadata.mode === 'form' ? (elicitFormValues.value[m.id] || {}) : undefined;
  try {
    await respondToElicitation(workspaceId.value, taskId.value, requestId, action, content);
    notifySuccess(action === 'accept' ? 'Response sent' : action === 'decline' ? 'Declined' : 'Cancelled');
  } catch (err) {
    notifyError('Failed to send response: ' + err.message);
  }
}

// Once resolved, m.metadata.content holds the answer keyed by schema property
// name — look up that property's own title for a human-readable label.
function elicitAnswerLabel(m, key) {
  return m.metadata?.requestedSchema?.properties?.[key]?.title || key;
}

function formatElicitAnswerValue(value) {
  if (Array.isArray(value)) return value.length ? value.join(', ') : '(none)';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (value === undefined || value === null || value === '') return '(empty)';
  return String(value);
}

async function updateStatus(newStatus) {
  try {
    const res = await updateTaskStatus(workspaceId.value, taskId.value, newStatus);
    task.value = res.task;
    notifySuccess(`Status updated to ${newStatus}`);
  } catch (err) {
    notifyError("Failed to update status: " + err.message);
  }
}

const updateAssignee = async (newAssignee) => {
  try {
    const res = await updateTaskAssignee(workspaceId.value, taskId.value, newAssignee);
    task.value = res.task;
    notifySuccess(`Task reassigned to ${newAssignee}`);
  } catch (err) {
    notifyError("Failed to reassign task: " + err.message);
  }
};

// Asks the agent to stop what it is doing. The task's own status is left
// alone: this is a message to the agent, and what it does next is its to say.
//
// An agent whose turn cannot be ended is refused the command it was standing
// at instead, which is a different thing to have happened and is said so.
const stopRunningTask = async () => {
  if (!task.value) return;
  try {
    const result = await stopTask(workspaceId.value, taskId.value);
    if (result?.stopped) {
      notifySuccess("Asked the agent to stop this task.");
    } else {
      notifySuccess("This agent cannot be stopped, so the command it was waiting on was refused.");
    }
  } catch (err) {
    notifyError("Failed to stop the task: " + err.message);
  }
};

const toggleYOLO = async () => {
  if (!task.value) return;
  const newVal = !task.value.allowAllCommands;
  try {
    const res = await updateTaskAllowAllCommands(workspaceId.value, taskId.value, newVal);
    task.value = res.task;
    if (newVal) notifySuccess("YOLO mode active: Agent will execute commands without approval.");
    else notifySuccess("YOLO mode disabled: Approval required for sensitive commands.");
  } catch (err) {
    notifyError("Failed to update YOLO mode: " + err.message);
  }
};

// `target` is where the message was written. It defaults to the task on screen
// for an immediate send, but a held one carries its own and must never fall back
// to whatever is showing when it finally goes.
async function deliverReply(text, atts, target = null) {
  const to = target ?? { workspaceId: workspaceId.value, taskId: taskId.value };
  const stillViewing = () => String(to.taskId) === String(taskId.value);
  try {
    const res = await respondToTask(to.workspaceId, to.taskId, 'text', text, atts);
    // Only adopt the reply if that task is still the one being shown; otherwise
    // this would paint another task's thread over the current one.
    if (stillViewing()) task.value = res.task;
  } catch(err) {
    notifyError("Failed to deliver message: " + err.message);
    // Putting the text back is only right while its own task is on screen. Into
    // a different task's composer it would be worse than losing it.
    if (stillViewing()) {
      replyText.value = text;
      replyAttachments.value = atts;
      nextTick(() => {
        adjustTextareaHeight();
      });
    }
  }
}

async function submitReply() {
  if (!replyText.value.trim() && replyAttachments.value.length === 0) return;
  const text = replyText.value;
  const atts = [...replyAttachments.value];
  replyText.value = '';
  replyAttachments.value = [];
  nextTick(() => {
    adjustTextareaHeight();
  });

  // Captured now, while we know which task this was typed into.
  const target = { workspaceId: workspaceId.value, taskId: taskId.value };

  const delaySeconds = workspace.value?.inputSendDelaySeconds || 0;
  if (delaySeconds <= 0) {
    await deliverReply(text, atts, target);
    return;
  }

  holdMessage({ text, atts, seconds: delaySeconds, target });
}

function sendPendingNow() {
  flushHeldMessage();
}

function cancelPendingSend() {
  const held = takeBackHeldMessage();
  if (!held) return;
  replyText.value = held.text;
  replyAttachments.value = held.atts;
  nextTick(() => {
    adjustTextareaHeight();
    textareaRef.value?.focus();
  });
}

const textareaRef = ref(null);

function adjustTextareaHeight() {
  const el = textareaRef.value;
  if (!el) return;
  el.style.height = '46px';
  const newHeight = Math.min(el.scrollHeight, 150);
  el.style.height = newHeight + 'px';
}

function getTaskDotStyle(t) {
  const status = typeof t === 'string' ? t : t.status;
  // If it's the task object, check if it's "Pending on Me"
  const isPendingOnMe = typeof t === 'object' && t.status !== 'completed' && t.status !== 'rejected' && (
    (t.status === 'notstarted' && t.assignee === 'human') ||
    (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
  );

  if (isPendingOnMe) {
    return 'bg-yellow-400 shadow-[0_0_8px_rgba(250,204,21,0.4)]';
  }

  switch (status) {
    case 'ongoing':
      return 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)] animate-pulse';
    case 'notstarted':
      return 'bg-gray-400 dark:bg-zinc-500';
    case 'completed':
      return 'bg-green-500';
    case 'rejected':
      return 'bg-red-500';
    case 'blocked':
      return 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]';
    case 'cron':
      return 'bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.4)]';
    default:
      return 'bg-gray-300 dark:bg-zinc-600';
  }
}

function formatDateTime(dateStr) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();

  if (diffMs >= 0 && diffMs < 24 * 60 * 60 * 1000) {
    const diffMin = Math.floor(diffMs / (60 * 1000));
    if (diffMin < 1) return 'JUST NOW';
    if (diffMin < 60) return `${diffMin}M AGO`;
    const diffHours = Math.floor(diffMin / 60);
    return `${diffHours}H AGO`;
  } else if (diffMs < 0 && diffMs > -60000) {
    return 'JUST NOW';
  }

  return date.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  });
}

// Global SSE listener to update local task state
watch(events, (evts) => {
  const last = evts[evts.length - 1];
  if (!last) return;
  
  if (['task.updated', 'task.created', 'reply.received', 'respond.ack', 'status.updated'].includes(last.type)) {
    if (last.payload && last.payload.id === taskId.value) {
      // Merged, not assigned: a payload built without the task's tool calls
      // would otherwise wipe the trajectory — see useTaskEvents. The cache gets
      // the merge for the same reason, never the raw payload.
      task.value = cacheTaskUpdate(sharedCache(), task.value, last.payload);
    }
  }
}, { deep: true });

const selectedAtt = ref(null);

function previewAttachment(att) {
  selectedAtt.value = att;
}

watch(() => task.value?.title, (title) => {
  if (title) document.title = `${title} | AgentRQ`;
}, { immediate: true });

onMounted(() => {
  load();
});
onUnmounted(disconnect);
onUnmounted(() => {
  // Leaving the view resolves a held message the same way switching tasks does:
  // it was asked for, and it is addressed to a task that is no longer on screen.
  flushHeldMessage();
});
function getSlackUser(m) {
  return m.metadata?.slack_user || 'Slack';
}

function stripNote(body) {
  if (!body) return '';
  const markerRegex = /\n\n(Self[\s-]Learning[\s-]Loop[\s-]Note|\[Self[\s-]Learning[\s-]Loop[\s-]Note\]|Self[\s-]Learning[\s-]Loop):/i;
  const match = body.match(markerRegex);
  if (match) {
    return body.substring(0, match.index).trim();
  }
  return body;
}
</script>

<style>
.md-body { line-height: 1.65; text-align: left; direction: ltr; word-break: break-word; }
.md-body > *:first-child { margin-top: 0; }
.md-body > *:last-child { margin-bottom: 0; }
.md-body h1, .md-body h2, .md-body h3, .md-body h4, .md-body h5, .md-body h6 { font-weight: 700; margin: 1em 0 0.4em; line-height: 1.3; }
.md-body h1 { font-size: 1.3em; }
.md-body h2 { font-size: 1.15em; }
.md-body h3 { font-size: 1.05em; }
.md-body h4, .md-body h5, .md-body h6 { font-size: 1em; }
.md-body p { margin: 0.55em 0; }
.md-body ul { list-style-type: "- "; padding-left: 1.5em; margin: 0.5em 0; }
.md-body ol { list-style-type: decimal; padding-left: 1.5em; margin: 0.5em 0; }
.md-body ul ul, .md-body ul ul ul { list-style-type: "- "; }
.md-body li { margin: 0.25em 0; display: list-item; }
.md-body li > p { margin: 0.2em 0; }
.md-body code { font-family: ui-monospace, monospace; font-size: 0.84em; background: rgba(0,0,0,0.06); border: 1px solid rgba(0,0,0,0.12); padding: 0.12em 0.38em; border-radius: 4px; white-space: pre-wrap; }
.dark .md-body code { background: rgba(255,255,255,0.08); border-color: rgba(255,255,255,0.18); }
.md-body pre { background: #f4f4f5; color: #27272a; border: 1px solid #e4e4e7; padding: 0.85em 1em; border-radius: 6px; overflow-x: auto; margin: 0.75em 0; }
.dark .md-body pre { background: #18181b; color: #d4d4d8; border-color: #3f3f46; }
.md-body pre code { background: none; border: none; padding: 0; font-size: 0.82em; white-space: pre-wrap; word-break: break-all; border-radius: 0; }
.dark .md-body pre code { background: none; border: none; }
.md-body blockquote { border-left: 3px solid #d1d5db; padding: 0.1em 0 0.1em 0.85em; color: #6b7280; margin: 0.6em 0; }
.md-body blockquote p { margin: 0.2em 0; }
.md-body a { text-decoration: underline; }
.md-body a:hover { opacity: 0.75; }
.md-body strong { font-weight: 700; }
.md-body em { font-style: italic; }
.md-body hr { border: none; border-top: 1px solid #e5e7eb; margin: 0.85em 0; }
.md-body table { border-collapse: collapse; font-size: 0.9em; margin: 0.6em 0; width: 100%; }
.md-body th, .md-body td { border: 1px solid #e5e7eb; padding: 0.35em 0.65em; text-align: left; }
.md-body th { background: rgba(0,0,0,0.04); font-weight: 600; }
.md-body img { max-width: 100%; border-radius: 4px; margin: 0.4em 0; }
</style>
