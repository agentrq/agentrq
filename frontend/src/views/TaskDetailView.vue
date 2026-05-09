<template>
  <div class="h-full flex flex-col w-full max-w-full overflow-x-hidden relative bg-white dark:bg-zinc-900" v-if="task && workspace">

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

            <!-- YOLO Toggle -->
            <button @click.stop="toggleYOLO"
                    @mouseenter="tooltipStore.show($event, task.allowAllCommands ? 'YOLO Active: Agent will execute all commands without approval' : 'YOLO Mode: Skip approval for sensitive commands', 'bottom')"
                    @mouseleave="tooltipStore.hide()"
                    :class="task.allowAllCommands ? 'bg-orange-500 text-white shadow-orange-500/20 shadow-lg border-transparent' : 'bg-gray-100 dark:bg-zinc-800 text-gray-400 dark:text-zinc-500 border-transparent'"
                    class="flex items-center gap-1 px-2 rounded-lg border transition-all h-7">
               <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M17.657 18.657A8 8 0 016.343 7.343S7 9 9 10c0-2 .5-5 2.986-7C14 5 16.09 5.777 17.656 7.343A7.99 7.99 0 0120 13a7.98 7.98 0 01-2.343 5.657z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 16.121A3 3 0 1012.015 11L11 14l2.015-2.879z" /></svg>
               <span class="hidden sm:inline text-[8px] font-black uppercase tracking-tighter">YOLO</span>
            </button>

            <!-- Edit Button (For Scheduled Tasks) -->
            <button v-if="task.cron" @click="router.push(`/workspaces/${workspaceId}/tasks/${taskId}/edit`)"
                    @mouseenter="tooltipStore.show($event, 'Edit Scheduled Task', 'bottom')"
                    @mouseleave="tooltipStore.hide()"
                    class="h-7 px-2 text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center gap-1.5" title="Edit Task">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
              <span class="hidden sm:inline text-[8px] font-black uppercase tracking-tighter">Edit</span>
            </button>

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
          </div>
        </div>

        <!-- Body Content (Collapsed) -->
        <div class="mt-0.5">
          <div v-if="task.body" class="mb-1">
            <p @click="isDescriptionCollapsed = !isDescriptionCollapsed"
               :class="[
                 isDescriptionCollapsed 
                   ? 'truncate text-[11px] text-gray-500 dark:text-zinc-500 py-1' 
                   : 'whitespace-pre-wrap p-3 bg-gray-50/50 dark:bg-zinc-800/30 rounded-xl border border-gray-100 dark:border-zinc-800 text-[13px] text-gray-600 dark:text-zinc-300 animate-in fade-in slide-in-from-top-1 duration-200'
               ]"
               class="cursor-pointer font-medium leading-relaxed transition-all hover:text-gray-800 dark:hover:text-zinc-100">
              {{ isDescriptionCollapsed ? stripNote(task.body) : task.body }}
            </p>
          </div>
          
          <!-- Attachments -->
          <div v-if="task.attachments && task.attachments.length > 0" class="mt-8 flex flex-wrap gap-3">
            <div v-for="(att, i) in task.attachments" :key="i"
                 @click="previewAttachment(att)"
                 class="flex items-center gap-3 px-4 py-2 rounded-xl border border-gray-100 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-800/50 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-all cursor-pointer group shadow-sm">
              <div class="w-6 h-6 flex items-center justify-center overflow-hidden rounded-lg">
                <img v-if="att.mimeType && att.mimeType.startsWith('image/')" :src="getAttachmentUrl(workspaceId, att.id)" class="w-full h-full object-cover" />
                <svg v-else class="w-4 h-4 text-gray-500 group-hover:text-black dark:group-hover:text-white transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>
              </div>
              <span class="text-[10px] font-bold text-gray-500 dark:text-zinc-400 group-hover:text-black dark:group-hover:text-white transition-colors uppercase tracking-widest">{{ att.filename }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Scrollable chat area -->
    <div ref="scrollContainer" class="flex-1 overflow-y-auto pl-4 pr-4 pt-0 pb-6 flex flex-col gap-4 scroll-smooth custom-scrollbar overflow-x-hidden" style="overscroll-behavior-y: contain;">

      <!-- Messages -->
      <template v-for="m in sortedMessages" :key="m.id">

        <!-- Agent message — left aligned -->
        <div v-if="m.sender === 'agent'" class="flex gap-3 animate-in fade-in slide-in-from-bottom-2 duration-300 max-w-[90%]">
          <div class="w-8 h-8 rounded-full bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 flex items-center justify-center shrink-0 mt-0.5">
            <svg class="w-4 h-4 text-gray-700 dark:text-zinc-100" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
          </div>
          <div class="flex flex-col items-start min-w-0">
             <div class="bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm p-3.5 shadow-sm min-w-0">
               <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 block mb-1.5">
                 Agent · {{ formatDateTime(m.createdAt) }}
               </span>
               <div class="text-[13px] font-medium text-gray-800 dark:text-zinc-200 leading-relaxed whitespace-pre-wrap break-all">{{ m.text }}</div>

               <!-- Permission Request (agent message) -->
               <div v-if="m.metadata?.type === 'permission_request'" class="mt-4 border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden shadow-sm">
                 <div class="bg-gray-50 dark:bg-zinc-800/80 border-b border-gray-200 dark:border-zinc-700 px-3 py-2 flex items-center justify-between gap-3">
                   <div class="flex items-center gap-2">
                     <svg class="w-3.5 h-3.5 text-yellow-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                     <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200">Authorization Required</span>
                   </div>
                   <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-500 hidden sm:block">{{ m.metadata.requestId }}</span>
                 </div>
                 <div class="p-3 flex flex-col gap-3">
                   <div>
                     <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-500">Action</span>
                     <p class="text-xs font-semibold text-gray-800 dark:text-zinc-200 mt-0.5 break-all">{{ m.metadata.toolName }}</p>
                   </div>
                   <p v-if="m.metadata.description" class="text-xs text-gray-600 dark:text-zinc-400 font-medium italic border-l-2 border-gray-300 dark:border-zinc-600 pl-2">"{{ m.metadata.description }}"</p>
                   <pre v-if="m.metadata.inputPreview" class="text-[10px] font-mono bg-zinc-950 text-zinc-300 p-3 rounded-sm overflow-x-auto whitespace-pre-wrap break-all custom-scrollbar">{{ m.metadata.inputPreview }}</pre>

                   <!-- Pending verdict buttons -->
                   <div v-if="m.metadata.status === 'pending'" class="flex flex-wrap gap-2 pt-2 border-t border-gray-100 dark:border-zinc-800">
                      <button @click="handleVerdict(m.metadata.request_id, 'allow')"
                              :disabled="!!workspace.archived_at"
                              class="px-3 py-1.5 rounded-sm bg-gray-900 hover:bg-black dark:bg-white dark:hover:bg-gray-100 text-white dark:text-black text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                        Allow Once
                      </button>
                      <button @click="handleVerdict(m.metadata.request_id, 'allow_always')"
                              :disabled="!!workspace.archived_at"
                              class="px-3 py-1.5 rounded-sm bg-white dark:bg-zinc-800 hover:bg-gray-50 dark:hover:bg-zinc-700 text-gray-700 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 text-[10px] font-semibold transition-all disabled:opacity-50 shadow-sm">
                        Always Allow
                      </button>
                      <button @click="handleVerdict(m.metadata.request_id, 'deny')"
                              :disabled="!!workspace.archived_at"
                              class="px-3 py-1.5 rounded-sm bg-red-50 hover:bg-red-100 dark:bg-red-500/10 dark:hover:bg-red-500/20 text-red-700 dark:text-red-500 text-[10px] font-semibold transition-all disabled:opacity-50 border border-red-100 dark:border-red-500/20">
                        Deny
                      </button>
                   </div>

                   <!-- Resolved verdict (collapsible) -->
                   <div v-else
                        @click="m._detailsExpanded = !m._detailsExpanded"
                        class="border rounded-sm cursor-pointer transition-all select-none overflow-hidden"
                        :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-gray-200 dark:border-zinc-700 bg-gray-50 dark:bg-zinc-800/50' : 'border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/5'">
                     <div class="flex items-center gap-2.5 px-3 py-2">
                       <svg v-if="m.metadata.status === 'allow' || m.metadata.status === 'allow_always'" class="w-3.5 h-3.5 text-gray-700 dark:text-zinc-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
                       <svg v-else class="w-3.5 h-3.5 text-red-600 dark:text-red-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                       <span class="text-[10px] font-semibold flex-1 truncate break-all"
                             :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-gray-700 dark:text-zinc-100' : 'text-red-700 dark:text-red-500'">
                         {{ m.metadata.toolName }}
                       </span>
                       <span class="text-[9px] font-semibold shrink-0"
                             :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-gray-500 dark:text-zinc-400' : 'text-red-600 dark:text-red-500'">
                         {{ m.metadata.status === 'deny' ? 'Denied' : m.metadata.status === 'allow_always' ? 'Always' : 'Allowed' }}
                       </span>
                       <svg class="w-3 h-3 text-gray-500 shrink-0 transition-transform duration-200" :class="m._detailsExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
                     </div>
                     <div v-if="m._detailsExpanded" class="px-3 pb-3 pt-1 border-t border-dashed"
                          :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-gray-200 dark:border-zinc-700' : 'border-red-200 dark:border-red-500/20'">
                       <p v-if="m.metadata.description" class="text-[11px] text-gray-600 dark:text-zinc-400 italic mb-2">"{{ m.metadata.description }}"</p>
                       <pre v-if="m.metadata.inputPreview" class="text-[9px] font-mono bg-zinc-950 text-gray-300 p-2 rounded overflow-x-auto whitespace-pre-wrap break-all mb-2">{{ m.metadata.inputPreview }}</pre>
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

        <!-- Human message — right aligned -->
        <div v-else class="flex gap-3 flex-row-reverse animate-in fade-in slide-in-from-bottom-2 duration-300 self-end max-w-[90%]">
          <div class="w-8 h-8 rounded-full bg-gray-200 dark:bg-zinc-700 flex items-center justify-center shrink-0 mt-0.5 overflow-hidden">
             <svg class="w-4 h-4 text-gray-600 dark:text-zinc-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
          </div>
          <div class="flex flex-col items-end min-w-0">
             <div class="bg-gray-900 text-white dark:bg-zinc-800 dark:text-zinc-100 border border-transparent dark:border-zinc-700 rounded-sm p-3.5 shadow-sm min-w-0">
               <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 block mb-1.5 text-right">
                 You · {{ formatDateTime(m.createdAt) }}
               </span>
               <div class="text-[13px] font-medium leading-relaxed whitespace-pre-wrap text-right break-all">{{ m.text }}</div>
               <!-- Attachments on human message -->
               <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-2 mt-3 pt-3 border-t border-gray-700 dark:border-zinc-600 justify-end">
                 <div v-for="(att, i) in m.attachments" :key="i"
                      @click="previewAttachment(att)"
                      class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm border border-gray-700 dark:border-zinc-600 bg-gray-800 dark:bg-zinc-700 hover:bg-gray-700 dark:hover:bg-zinc-600 transition-colors cursor-pointer text-[9px] font-semibold">
                   <svg class="w-3 h-3 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                   <span class="truncate max-w-[140px] text-gray-200">{{ att.filename }}</span>
                 </div>
               </div>
             </div>
          </div>
        </div>

      </template>
    </div>

    <!-- Reply Box -->
    <footer v-if="!workspace.archivedAt" class="px-2 sm:px-4 py-2 sm:py-4 border-t border-gray-100 dark:border-zinc-800 shrink-0 z-20 bg-gray-50/50 dark:bg-zinc-900/50">

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
        <div class="flex items-end gap-1.5 sm:gap-2 w-full flex-nowrap">
          <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />

          <div class="flex-1 flex items-center bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm focus-within:border-gray-900 dark:focus-within:border-white focus-within:ring-0 transition-all group relative min-w-0 shadow-sm">
            <textarea
              ref="textareaRef"
              v-model="replyText"
              @input="adjustTextareaHeight"
              @keydown.meta.enter="submitReply"
              @keydown.ctrl.enter="submitReply"
              rows="1"
              :disabled="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending')"
              :placeholder="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending') ? 'Waiting for agent...' : 'Type instructions...'"
              class="flex-1 px-3 sm:px-4 py-2.5 sm:py-3 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none placeholder-gray-400 dark:placeholder-zinc-500 disabled:opacity-50 resize-none min-h-[46px] max-h-[150px] custom-scrollbar"
            ></textarea>
            <button type="button" @click="$refs.fileInput.click()"
                    :disabled="(!workspace.agentConnected && task.assignee !== 'human' && task.status !== 'pending')"
                    class="h-[46px] px-2 sm:px-3 text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 transition-colors flex items-center justify-center disabled:opacity-30 self-end">
              <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
            </button>
          </div>
           <button type="submit"
                   :disabled="(!replyText.trim() && replyAttachments.length === 0) || (task.assignee !== 'human' && (!workspace.agentConnected || task.status === 'notstarted' || task.status === 'pending'))"
                   class="h-[46px] w-[46px] rounded-sm bg-gray-900 dark:bg-white text-white dark:text-zinc-900 hover:bg-zinc-700 dark:hover:bg-zinc-100 shadow-sm disabled:opacity-30 transition-all shrink-0 flex items-center justify-center"
                   title="Send Message">
             <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
               <path stroke-linecap="round" stroke-linejoin="round" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
             </svg>
           </button>
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
          <img v-if="selectedAtt.mimeType?.startsWith('image/')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh] object-scale-down" />
          <video v-else-if="selectedAtt.mimeType?.startsWith('video/')" controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh]" />
          <div v-else-if="selectedAtt.mimeType?.startsWith('audio/')" class="p-16 flex flex-col items-center gap-6">
            <div class="w-20 h-20 rounded-full bg-gray-900 dark:bg-white flex items-center justify-center shadow-lg">
              <svg class="w-10 h-10 text-white" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
            </div>
            <audio controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[400px]" />
          </div>
          <iframe v-else-if="selectedAtt.mimeType?.includes('pdf')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[80vw] h-[75vh]" frameborder="0"></iframe>
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
          <a :href="getAttachmentUrl(workspaceId, selectedAtt.id)" :download="selectedAtt.filename"
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
import { getWorkspace, fetchTasks, archiveWorkspace, unarchiveWorkspace, updateWorkspace, getWorkspaceToken, getTask, updateTaskStatus, respondToTask, updateTaskAssignee, getAttachmentUrl, sendPermissionVerdict, updateTaskAllowAllCommands, fetchUser } from '../api';
import { useTooltipStore } from '../stores/tooltipStore';
import { useToasts } from '../composables/useToasts';
import { useViewport } from '../composables/useViewport';
import { useEventBus } from '../useEventBus';

const { notifyError, notifySuccess } = useToasts();
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
const scrollContainer = ref(null);
const isStatusMenuOpen = ref(false);
const isDescriptionCollapsed = ref(true);


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

watch(() => sortedMessages.value.length, (count) => {
  if (count === 0 && task.value?.body) {
    isDescriptionCollapsed.value = false;
  }
}, { immediate: true });

function scrollToBottom() {
  if (scrollContainer.value) {
    nextTick(() => {
      scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight;
    });
  }
}

watch(sortedMessages, () => {
  scrollToBottom();
}, { deep: true });

async function load() {
  try {
    user.value = await fetchUser();
    const pRes = await getWorkspace(workspaceId.value);
    workspace.value = pRes.workspace;
    const tRes = await getTask(workspaceId.value, taskId.value);
    task.value = tRes.task;
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
watch(() => route.params.taskId, (newTaskId) => {
  if (newTaskId && newTaskId !== task.value?.id) {
    disconnect();
    load();
  }
});

async function handleFileUpload(e) {
  const files = e.target.files;
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

async function submitReply() {
  if (!replyText.value.trim() && replyAttachments.value.length === 0) return;
  const text = replyText.value;
  const atts = [...replyAttachments.value];
  replyText.value = '';
  replyAttachments.value = [];
  nextTick(() => {
    adjustTextareaHeight();
  });
  try {
    const res = await respondToTask(workspaceId.value, taskId.value, 'text', text, atts);
    task.value = res.task;
  } catch(err) {
    notifyError("Failed to deliver message: " + err.message);
    replyText.value = text;
    replyAttachments.value = atts;
    nextTick(() => {
      adjustTextareaHeight();
    });
  }
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
  
  if (['task.updated', 'task.created'].includes(last.type)) {
    if (last.payload && last.payload.id === taskId.value) {
      task.value = last.payload;
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
function stripNote(body) {
  if (!body) return '';
  const markerRegex = /\n\n(Self[\s-]Learning[\s-]Loop[\s-]Note|\[Self[\s-]Learning[\s-]Loop[\s-]Note\]|Self[\s-]Learning[\s-]Loop:)/i;
  const match = body.match(markerRegex);
  if (match) {
    return body.substring(0, match.index).trim();
  }
  return body;
}
</script>
