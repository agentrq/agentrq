<template>
  <div class="h-full bg-white dark:bg-zinc-900 flex flex-col w-full max-w-full overflow-x-hidden relative">
    <!-- Breadcrumb Header -->
    <header class="py-4 border-b border-gray-100 dark:border-zinc-800 shrink-0 flex items-center justify-between gap-4 bg-white dark:bg-zinc-900 sticky top-0 z-30 px-6">
      <div class="flex items-center gap-2 text-xs font-semibold min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 transition-colors shrink-0">
          {{ workspace?.name || 'Workspace' }}
        </router-link>
        <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-900 dark:text-zinc-50 truncate flex-1 min-w-0 text-sm">{{ isEditMode ? 'Edit Task' : 'New Task' }}</span>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button @click="() => goBack()" class="p-2 text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm transition-all">
          <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto pt-8 md:pt-16 pb-24 px-1 sm:px-2 md:px-4 scroll-smooth custom-scrollbar flex items-start justify-center"
          @dragover.prevent="isDragging = true"
          @dragleave.prevent="isDragging = false"
          @drop.prevent="handleDrop">
      
      <div class="w-full max-w-3xl space-y-4">
        
        <h1 class="text-xl md:text-3xl font-black text-gray-800 dark:text-zinc-200 tracking-tight text-center mb-8">
          {{ isEditMode ? 'Edit Scheduled Task' : 'What do you want to achieve?' }}
        </h1>

        <!-- Drag & Drop Overlay inside the main container -->
        <div v-if="isDragging" class="absolute inset-4 z-50 border-4 border-dashed border-gray-300 dark:border-zinc-700 bg-white/95 dark:bg-zinc-900/95 rounded-2xl flex flex-col items-center justify-center shadow-2xl animate-in zoom-in-95 pointer-events-none">
           <svg class="w-16 h-16 text-gray-400 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" /></svg>
           <p class="text-xl font-black text-gray-700 dark:text-zinc-300">Drop files to attach</p>
        </div>

        <form id="taskForm" @submit.prevent="isEditMode ? submitEditProtocol() : submitHumanTask()" 
              class="flex flex-col bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-xl focus-within:border-gray-900 dark:focus-within:border-white focus-within:ring-0 transition-all shadow-xl relative overflow-visible z-10">
          
          <!-- Auto-generated Title Input (Top edge) -->
          <div class="px-4 pt-3 pb-1 border-b border-gray-100 dark:border-zinc-800/50 flex items-center gap-2 group relative">
            <svg class="w-3.5 h-3.5 text-gray-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
            
            <input v-model="titleRef" 
                   @input="markOverridden"
                   :placeholder="isModelLoading ? `Loading AI Model... ${modelProgress}%` : isGenerating ? 'Generating title...' : isAutoTitleSupported ? 'Task Title (Click sparkle to auto-generate)' : 'Task Title'"
                   class="w-full bg-transparent outline-none border-none text-[13px] font-bold text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 pl-1" />

            <!-- AI Sparkles Button -->
            <button v-if="isAutoTitleSupported && bodyRef && bodyRef.trim().length >= 5" type="button" @click="generateTitle(); tooltipStore.hide()"
                    :disabled="isGenerating"
                    @mouseenter="tooltipStore.show($event, isModelLoading ? `Loading Model... ${modelProgress}%` : 'Generate title from description using local AI', 'top')"
                    @mouseleave="tooltipStore.hide()"
                    class="p-1 hover:bg-gray-100 dark:hover:bg-zinc-850 rounded text-gray-450 dark:text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400 transition-all shrink-0">
               <svg v-if="isGenerating" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path></svg>
               <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9.813 15.904L9 21l-.813-5.096L3 15l5.188-.813L9 9l.813 5.187L15 15l-5.187.813zM19.071 4.929l-.571 3.571-3.571.571 3.571.571.571 3.571.571-3.571 3.571-.571-3.571-.571-.571-3.571z" /></svg>
            </button>
          </div>

          <!-- Description Textarea -->
          <textarea v-model="bodyRef" 
                    placeholder="Provide detailed context or instructions..." 
                    class="w-full px-4 pt-3 pb-2 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none border-none focus:outline-none focus:ring-0 resize-none min-h-[160px] custom-scrollbar"
                    required></textarea>

          <!-- Attachments Preview -->
          <div v-if="newTaskAttachments.length > 0" class="flex flex-wrap gap-2 px-4 pb-2 border-t border-gray-50 dark:border-zinc-800/50 pt-2">
            <div v-for="(att, i) in newTaskAttachments" :key="i"
                 class="flex items-center text-[10px] bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 rounded-md px-2.5 py-1 font-bold shadow-sm">
              <span class="truncate max-w-[150px]">{{ att.filename }}</span>
              <button @click.prevent="newTaskAttachments.splice(i, 1)" class="ml-2 text-gray-500 hover:text-red-500 transition-colors">
                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5"><path d="M6 18L18 6M6 6l12 12"></path></svg>
              </button>
            </div>
          </div>

          <!-- Bottom Toolbar -->
          <div class="flex items-center justify-between px-3 pb-2 pt-2 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-900/50 rounded-b-xl flex-wrap gap-2 relative">
             <div class="flex items-center gap-1 sm:gap-2 flex-wrap">
                <!-- Attachment Button -->
                <button type="button" @click="$refs.fileInput.click()"
                        class="h-7 w-7 rounded-md text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 transition-colors flex items-center justify-center"
                        title="Attach files">
                  <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                </button>
                <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />

                <!-- STT Button -->
                <button v-if="sttSupported" type="button" @click="sttToggle(); tooltipStore.hide()"
                        :disabled="sttTranscribing"
                        @mouseenter="tooltipStore.show($event, sttRecording ? 'Stop recording' : sttTranscribing ? (sttModelLoadingSTT ? `Loading STT model... ${sttProgressSTT}%` : 'Transcribing...') : 'Voice input', 'top')"
                        @mouseleave="tooltipStore.hide()"
                        :class="[
                          sttRecording ? 'bg-red-500 text-white border-red-500' : sttTranscribing ? 'bg-gray-200 dark:bg-zinc-600 text-gray-500 dark:text-zinc-300 border-transparent' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700'
                        ]"
                        class="h-7 w-7 rounded-md border border-transparent transition-all flex items-center justify-center disabled:opacity-30 relative">
                  <span v-if="sttRecording" class="w-2 h-2 rounded-full bg-white animate-pulse"></span>
                  <svg v-else-if="sttTranscribing" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                  <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4M12 15a3 3 0 003-3V5a3 3 0 00-6 0v7a3 3 0 003 3z" /></svg>
                </button>

                <!-- Divider -->
                <div class="w-px h-4 bg-gray-200 dark:bg-zinc-700 mx-1"></div>

                <!-- Agent/Human Toggle -->
                <div class="flex p-0.5 bg-gray-200 dark:bg-zinc-800 border border-gray-300 dark:border-zinc-700/50 rounded-md h-7">
                  <button type="button" @click="newTask.assignee = 'agent'; tooltipStore.hide()"
                          @mouseenter="tooltipStore.show($event, 'Assign to Agent', 'top')" @mouseleave="tooltipStore.hide()"
                          :class="newTask.assignee === 'agent' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300'"
                          class="px-2 rounded flex items-center justify-center text-[10px] font-bold uppercase tracking-wider transition-all">
                    <span class="hidden sm:inline">Agent</span>
                    <svg class="sm:hidden w-3.5 h-3.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
                  </button>
                  <button type="button" @click="newTask.assignee = 'human'; tooltipStore.hide()"
                          @mouseenter="tooltipStore.show($event, 'Assign to Human', 'top')" @mouseleave="tooltipStore.hide()"
                          :class="newTask.assignee === 'human' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300'"
                          class="px-2 rounded flex items-center justify-center text-[10px] font-bold uppercase tracking-wider transition-all">
                    <span class="hidden sm:inline">Human</span>
                    <svg class="sm:hidden w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
                  </button>
                </div>

                <!-- YOLO Toggle -->
                <button v-if="newTask.assignee === 'agent'" type="button" @click.stop="newTask.allowAllCommands = !newTask.allowAllCommands; tooltipStore.hide()"
                        @mouseenter="tooltipStore.show($event, newTask.allowAllCommands ? 'YOLO Active: Agent will execute all commands without approval' : 'YOLO Mode: Skip approval for sensitive commands', 'top')"
                        @mouseleave="tooltipStore.hide()"
                        :class="newTask.allowAllCommands ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-black border-transparent shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                        class="flex items-center justify-center gap-1 w-7 h-7 sm:w-auto sm:px-2.5 rounded-md transition-all text-[10px] font-bold uppercase tracking-wider">
                  <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M17.657 18.657A8 8 0 016.343 7.343S7 9 9 10c0-2 .5-5 2.986-7C14 5 16.09 5.777 17.656 7.343A7.99 7.99 0 0120 13a7.98 7.98 0 01-2.343 5.657z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 16.121A3 3 0 1012.015 11L11 14l2.015-2.879z" /></svg>
                  <span class="hidden sm:inline">YOLO</span>
                </button>

                <!-- Schedule / Cron Dropdown (Popover) -->
                <div class="relative" v-click-outside="() => showScheduleMenu = false">
                   <button type="button" @click="showScheduleMenu = !showScheduleMenu; showEventMenu = false; tooltipStore.hide()"
                           @mouseenter="tooltipStore.show($event, scheduleType === 'none' ? 'Set Schedule' : (newTask.cronSchedule || 'Schedule Active'), 'top')"
                           @mouseleave="tooltipStore.hide()"
                           :class="scheduleType !== 'none' ? 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:border-blue-800 shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                           class="flex items-center justify-center w-7 h-7 rounded-md transition-all">
                     <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                   </button>

                   <!-- Schedule Menu Content -->
                   <div v-if="showScheduleMenu" class="fixed sm:absolute left-4 right-4 sm:left-0 sm:right-auto bottom-4 sm:bottom-full mb-3 w-auto sm:w-[320px] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-4 animate-in fade-in slide-in-from-bottom-2">
                       <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mb-3 border-b border-gray-100 dark:border-zinc-800 pb-2">Execution Strategy</h3>
                       
                       <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-sm mb-4">
                         <button type="button" @click="scheduleType = 'none'"
                                 :class="scheduleType === 'none' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">None</button>
                         <button type="button" @click="scheduleType = 'onetime'"
                                 :class="scheduleType === 'onetime' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">One-time</button>
                         <button type="button" @click="scheduleType = 'repeated'"
                                 :class="scheduleType === 'repeated' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">Repeated</button>
                       </div>

                       <div class="min-h-[100px]">
                         <div v-if="scheduleType === 'onetime'" class="animate-in fade-in slide-in-from-top-2 duration-200">
                           <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-2 block">Launch Date/Time</label>
                           <input type="datetime-local" v-model="oneTimeDate"
                                  class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-xs font-semibold text-gray-900 dark:text-zinc-50 outline-none w-full" />
                         </div>

                         <div v-if="scheduleType === 'repeated'" class="space-y-4 animate-in fade-in slide-in-from-top-2 duration-200">
                           <div class="flex gap-3">
                             <div class="flex flex-col gap-1.5 flex-1">
                               <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Frequency</label>
                               <select v-model="repeatPreset" 
                                       class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-2 py-2 text-[10px] font-semibold text-gray-900 dark:text-zinc-50 outline-none w-full">
                                 <option value="15min">Every 15 mins</option>
                                 <option value="30min">Every 30 mins</option>
                                 <option value="hourly">Hourly</option>
                                 <option value="2hour">Bi-hourly</option>
                                 <option value="12hour">Twice a day</option>
                                 <option value="daily">Daily</option>
                                 <option value="weekly">Weekly</option>
                                 <option value="monthly">Monthly</option>
                                 <option value="custom">Custom...</option>
                               </select>
                             </div>

                             <div v-if="!['15min', '30min', 'hourly', '2hour'].includes(repeatPreset)" class="flex flex-col gap-1.5 w-[90px]">
                               <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Time</label>
                               <input type="time" v-model="repeatTime"
                                      class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-2 py-1.5 text-[10px] font-semibold text-gray-900 dark:text-zinc-50 outline-none w-full" />
                             </div>
                           </div>

                           <div v-if="repeatPreset === 'custom'" class="flex flex-col gap-1.5">
                              <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Active Days</label>
                              <div class="flex flex-wrap gap-1">
                                <button v-for="d in daysOptions" :key="d.value" type="button" @click="toggleDay(d.value)"
                                        :class="selectedDays.includes(d.value) ? 'bg-gray-900 dark:bg-white text-white dark:text-zinc-900 border-black dark:border-white' : 'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-500'"
                                        class="w-6 h-6 rounded-sm border text-[9px] font-bold flex items-center justify-center transition-all">
                                  {{ d.label }}
                                </button>
                              </div>
                           </div>
                         </div>
                       </div>
                       
                       <div v-if="scheduleType !== 'none'" class="mt-4 pt-3 border-t border-gray-100 dark:border-zinc-800 flex items-center justify-between">
                          <code class="text-[9px] font-mono text-gray-600 dark:text-zinc-400">{{ newTask.cronSchedule || '----' }}</code>
                          <span class="text-[9px] font-bold text-sky-600 dark:text-sky-400 truncate max-w-[120px]">{{ nextRunPreview }}</span>
                       </div>
                   </div>
                </div>

                <!-- Emit Event Dropdown -->
                <div class="relative" v-if="!isEditMode && events.length > 0" v-click-outside="() => showEventMenu = false">
                   <button type="button" @click="showEventMenu = !showEventMenu; showScheduleMenu = false; tooltipStore.hide()"
                           @mouseenter="tooltipStore.show($event, selectedEventId ? 'Event Emitted on Completion' : 'Emit Event on Completion', 'top')"
                           @mouseleave="tooltipStore.hide()"
                           :class="selectedEventId ? 'bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-900/30 dark:text-purple-400 dark:border-purple-800 shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                           class="flex items-center justify-center w-7 h-7 rounded-md transition-all">
                     <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                   </button>
                   <div v-if="showEventMenu" class="fixed sm:absolute left-4 right-4 sm:left-auto sm:right-0 bottom-4 sm:bottom-full mb-3 w-auto sm:w-[240px] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-4 animate-in fade-in slide-in-from-bottom-2">
                       <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mb-3 border-b border-gray-100 dark:border-zinc-800 pb-2">Emit Event on Completion</h3>
                       <select v-model="selectedEventId" class="w-full bg-gray-50 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-md px-2 py-2 text-[11px] font-semibold text-gray-900 dark:text-zinc-100 outline-none">
                         <option value="">None</option>
                         <option v-for="ev in events" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
                       </select>
                   </div>
                </div>

             </div>

             <!-- Submit Button -->
             <button type="submit"
                     :disabled="sending || !newTask.title || !newTask.body"
                     class="h-8 w-8 sm:h-9 sm:w-9 rounded-full bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 disabled:opacity-30 transition-all flex items-center justify-center shrink-0 shadow-md border border-transparent">
                <svg v-if="sending" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                <svg v-else class="w-4 h-4 translate-x-px" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M14 5l7 7m0 0l-7 7m7-7H3" /></svg>
             </button>
          </div>
        </form>
        
        <div class="flex justify-center mt-6">
          <button type="button" @click="() => goBack()" class="px-6 py-2 rounded-full bg-transparent text-gray-500 hover:text-gray-800 dark:text-zinc-500 dark:hover:text-zinc-300 text-[10px] font-bold uppercase tracking-widest transition-colors">Cancel</button>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, createTask, updateScheduledTask, getTask, fetchEvents } from '../api';
import { useToasts } from '../composables/useToasts';
import { useCron } from '../composables/useCron';
import { useSpeechToText } from '../composables/useSpeechToText';
import { useAutoTitle } from '../composables/useAutoTitle';
import { useTooltipStore } from '../stores/tooltipStore';

const { getNextRunLabel, daysOptions } = useCron();
const route = useRoute();
const router = useRouter();
const { notifyError, notifySuccess } = useToasts();
const tooltipStore = useTooltipStore();

const workspaceId = route.params.id;
const taskId = route.params.taskId;
const isEditMode = computed(() => !!taskId);

const workspace = ref(null);
const sending = ref(false);
const fileInput = ref(null);

const newTask = ref({ title: '', body: '', assignee: 'agent', cronSchedule: '', allowAllCommands: false });
const newTaskAttachments = ref([]);

// We use computed refs to bridge to the composables
const titleRef = computed({
  get: () => newTask.value.title,
  set: (v) => { newTask.value.title = v; },
});
const bodyRef = computed({
  get: () => newTask.value.body,
  set: (v) => { newTask.value.body = v; },
});

// STT
const {
  isRecording: sttRecording,
  isTranscribing: sttTranscribing,
  isModelLoading: sttModelLoadingSTT,
  modelProgress: sttProgressSTT,
  error: sttError,
  isSupported: sttSupported,
  toggleRecording: sttToggle,
} = useSpeechToText(bodyRef, workspaceId);

// Auto-Title
const {
  isSupported: isAutoTitleSupported,
  isGenerating,
  isModelLoading,
  modelProgress,
  isOverridden,
  markOverridden,
  generateTitle
} = useAutoTitle(bodyRef, titleRef);

// Event-on-completion
const events = ref([]);
const selectedEventId = ref('');
const showEventMenu = ref(false);

// Scheduling state
const scheduleType = ref('none');
const oneTimeDate = ref('');
const repeatPreset = ref('daily');
const repeatTime = ref('09:00');
const selectedDays = ref([1, 2, 3, 4, 5]); // Mon-Fri
const showScheduleMenu = ref(false);

const isDragging = ref(false);

function handleDrop(e) {
  isDragging.value = false;
  const files = e.dataTransfer.files;
  if (files && files.length > 0) {
    processFiles(files);
  }
}

onMounted(async () => {
  try {
    const [wsRes, eventsRes] = await Promise.all([
      getWorkspace(workspaceId),
      fetchEvents().catch(() => ({ events: [] })),
    ]);
    workspace.value = wsRes.workspace;
    events.value = eventsRes.events ?? [];

    if (isEditMode.value) {
      const taskRes = await getTask(workspaceId, taskId);
      const t = taskRes.task;
      newTask.value = {
        title: t.title,
        body: t.body,
        assignee: t.assignee,
        cronSchedule: t.cronSchedule,
        allowAllCommands: t.allowAllCommands || false
      };
      markOverridden(); // Assume edited tasks shouldn't auto-title

      if (t.cronSchedule) {
        parseCronToUI(t.cronSchedule);
      }
    } else {
      newTask.value.allowAllCommands = wsRes.workspace.allowAllCommands || false;
    }
  } catch (err) {
    notifyError("Access Error: " + err.message);
    router.push(`/workspaces/${workspaceId}`);
  }
});

function parseCronToUI(cron) {
  const parts = cron.split(' ');
  if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') {
    scheduleType.value = 'onetime';
    const [min, hour, dom, month] = parts;
    const currentYear = new Date().getFullYear();
    const utcDate = new Date(Date.UTC(currentYear, month - 1, dom, hour, min));
    const year = utcDate.getFullYear();
    const mon = String(utcDate.getMonth() + 1).padStart(2, '0');
    const day = String(utcDate.getDate()).padStart(2, '0');
    const hh = String(utcDate.getHours()).padStart(2, '0');
    const mm = String(utcDate.getMinutes()).padStart(2, '0');
    oneTimeDate.value = `${year}-${mon}-${day}T${hh}:${mm}`;
  } else {
    scheduleType.value = 'repeated';
    if (cron === '*/15 * * * *') {
      repeatPreset.value = '15min';
    } else if (cron === '*/30 * * * *') {
      repeatPreset.value = '30min';
    } else if (cron === '0 * * * *') {
      repeatPreset.value = 'hourly';
    } else if (cron === '0 */2 * * *') {
      repeatPreset.value = '2hour';
    } else {
      const [min, hour, dom, month, dow] = parts;
      const firstHour = Number(hour.split(',')[0]);
      const now = new Date();
      const d = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate(), firstHour, min));
      repeatTime.value = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
      
      if (dom === '*' && month === '*' && dow === '*') {
        const hoursArr = hour.split(',').map(Number);
        if (hoursArr.length === 2 && Math.abs(hoursArr[1] - hoursArr[0]) === 12) {
          repeatPreset.value = '12hour';
        } else {
          repeatPreset.value = 'daily';
        }
      } else if (dow !== '*' && dom === '*' && month === '*') {
        const now = new Date();
        const utcDays = dow.split(',').map(Number);
        const localDays = utcDays.map(ud => {
           const base = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()));
           const currentUTCDay = base.getUTCDay();
           const offset = ud - currentUTCDay;
           const temp = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate() + offset, firstHour, min));
           return temp.getDay();
        });
        selectedDays.value = localDays;
        repeatPreset.value = (localDays.length === 1 && localDays[0] === 0) ? 'weekly' : 'custom';
      } else {
        repeatPreset.value = 'custom';
      }
    }
  }
}

const nextRunPreview = computed(() => {
  if (scheduleType.value === 'none' || !newTask.value.cronSchedule) return '';
  return getNextRunLabel(newTask.value.cronSchedule);
});


function toggleDay(day) {
  const idx = selectedDays.value.indexOf(day);
  if (idx === -1) selectedDays.value.push(day);
  else if (selectedDays.value.length > 1) selectedDays.value.splice(idx, 1);
}

watch([scheduleType, oneTimeDate, repeatPreset, repeatTime, selectedDays], () => {
  if (scheduleType.value === 'none') { newTask.value.cronSchedule = ''; return; }

  if (scheduleType.value === 'onetime') {
    if (!oneTimeDate.value) { newTask.value.cronSchedule = ''; return; }
    const d = new Date(oneTimeDate.value);
    newTask.value.cronSchedule = `${d.getUTCMinutes()} ${d.getUTCHours()} ${d.getUTCDate()} ${d.getUTCMonth() + 1} *`;
    return;
  }

  const [localHours, localMinutes] = repeatTime.value.split(':').map(Number);
  const d = new Date();
  d.setHours(localHours, localMinutes, 0, 0);
  const minutes = d.getUTCMinutes();
  const hours = d.getUTCHours();

  if (repeatPreset.value === '15min') {
    newTask.value.cronSchedule = '*/15 * * * *';
  } else if (repeatPreset.value === '30min') {
    newTask.value.cronSchedule = '*/30 * * * *';
  } else if (repeatPreset.value === 'hourly') {
    newTask.value.cronSchedule = `0 * * * *`;
  } else if (repeatPreset.value === '2hour') {
    newTask.value.cronSchedule = `0 */2 * * *`;
  } else if (repeatPreset.value === '12hour') {
    const h1 = d.getUTCHours();
    const tempD = new Date(d);
    tempD.setHours(tempD.getHours() + 12);
    const h2 = tempD.getUTCHours();
    const hoursStr = [h1, h2].sort((a,b)=>a-b).join(',');
    newTask.value.cronSchedule = `${minutes} ${hoursStr} * * *`;
  } else if (repeatPreset.value === 'daily') {
    newTask.value.cronSchedule = `${minutes} ${hours} * * *`;
  } else if (repeatPreset.value === 'weekly') {
    const utcDay = d.getUTCDay();
    newTask.value.cronSchedule = `${minutes} ${hours} * * ${utcDay}`;
  } else if (repeatPreset.value === 'monthly') {
    const dd = new Date();
    dd.setHours(localHours, localMinutes, 0, 0);
    dd.setDate(1);
    newTask.value.cronSchedule = `${minutes} ${hours} ${dd.getUTCDate()} * *`;
  } else if (repeatPreset.value === 'custom') {
    const utcDays = new Set();
    selectedDays.value.forEach(day => {
      const dd = new Date();
      dd.setHours(localHours, localMinutes, 0, 0);
      const currentDay = dd.getDay();
      dd.setDate(dd.getDate() + (day - currentDay));
      utcDays.add(dd.getUTCDay());
    });
    const days = [...utcDays].sort().join(',');
    newTask.value.cronSchedule = `${minutes} ${hours} * * ${days}`;
  }
}, { deep: true });

function handleFileUpload(event) {
  const files = event.target.files;
  processFiles(files);
  if (fileInput.value) fileInput.value.value = '';
}

function processFiles(files) {
  for (let i = 0; i < files.length; i++) {
    const fn = files[i];
    const reader = new FileReader();
    reader.onload = (e) => {
      newTaskAttachments.value.push({
        filename: fn.name,
        mimeType: fn.type || 'application/octet-stream',
        data: e.target.result.split(',')[1]
      });
    };
    reader.readAsDataURL(fn);
  }
}

async function submitHumanTask() {
  sending.value = true;
  try {
    const status = scheduleType.value !== 'none' ? 'cron' : 'notstarted';
    await createTask(
      workspaceId, newTask.value.title, newTask.value.body,
      newTask.value.assignee, newTaskAttachments.value,
      status, newTask.value.cronSchedule, newTask.value.allowAllCommands,
      selectedEventId.value
    );
    notifySuccess('Task Created successfully');
    goBack(status === 'cron');
  } catch(err) {
    notifyError("Dispatch Error: " + err.message);
  } finally { sending.value = false; }
}

async function submitEditProtocol() {
  sending.value = true;
  try {
    await updateScheduledTask(
      workspaceId, taskId, newTask.value.title, newTask.value.body,
      newTask.value.assignee, newTask.value.cronSchedule,
      newTask.value.allowAllCommands
    );
    notifySuccess('Scheduled Task Updated');
    goBack(newTask.value.cronSchedule !== '');
  } catch(err) {
    notifyError("Update Error: " + err.message);
  } finally { sending.value = false; }
}

function goBack(isScheduled = false) {
  if (isScheduled) {
    router.push({ path: `/workspaces/${workspaceId}`, query: { scheduled: 'true' } });
  } else {
    router.push(`/workspaces/${workspaceId}`);
  }
}
</script>
