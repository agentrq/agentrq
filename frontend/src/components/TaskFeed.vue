<template>
  <div class="flex-1 flex flex-col min-h-0 w-full bg-gray-50/30">

    <div v-if="isArchived" class="p-3 bg-amber-50 border-b border-amber-100 flex items-center justify-center gap-2">
      <svg class="w-3.5 h-3.5 text-amber-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
      <span class="text-[10px] font-black text-amber-900 uppercase tracking-widest">Archived Workspace • Read Only</span>
    </div>
    
    <!-- Delete Confirmation Modal -->
    <DeleteModal 
      :show="showDeleteModal" 
      :taskTitle="taskToDeleteTitle"
      title="Delete Task" 
      @close="closeDeleteModal" 
      @confirm="onDeleteConfirm" 
    />
    <!-- Action Bar -->
    <div v-if="!isArchived" class="hidden md:flex pt-2 pb-4 flex-row items-center justify-between gap-2 shrink-0 flex-wrap">
        <button @click="$emit('toggleScheduled')"
                class="flex items-center gap-1.5 px-2.5 py-2 border-2 border-black text-[10px] font-black uppercase tracking-widest transition-all w-max shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]"
                :class="showScheduledOnly ? 'bg-[#00FF88] text-black translate-y-[1px]' : 'bg-white text-gray-500 hover:bg-[#00FF88]/20 hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]'"
                title="Toggle Scheduled">
           <svg class="w-4 h-4 text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
             <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" />
           </svg>
           <span class="hidden md:inline">Scheduled</span>
        </button>
       
       <div class="flex items-center gap-2 ml-auto">
         <button @click="$emit('archive')"
                 class="flex items-center gap-1.5 px-3 py-2 bg-white border-2 border-black text-[10px] font-black text-gray-500 hover:text-red-500 hover:bg-gray-50 transition-all uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]"
                 title="Archive Mission">
            <svg class="w-3.5 h-3.5 font-bold" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
            <span class="hidden md:inline">Archive</span>
         </button>
          <button @click="startCreate" 
                  class="group flex items-center gap-2 bg-[#00FF88] text-black border-2 border-black hover:bg-black hover:text-[#00FF88] px-3 py-2 text-[10px] font-black uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]"
                  title="New Task">
            <svg class="w-4 h-4 transform group-hover:rotate-90 transition-transform duration-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
            <span class="hidden md:inline">New Task</span>
         </button>
       </div>
    </div>

    <!-- Create/Edit Task Inline Form -->
    <Transition name="fade-down">
      <div v-if="isFormOpen" class="fixed inset-0 z-40 flex items-center justify-center p-4 md:relative md:inset-auto md:p-0 md:bg-transparent md:z-10 md:block">
        <!-- Backdrop for mobile -->
        <div class="fixed inset-0 bg-black/60 md:hidden" @click="isFormOpen = false"></div>
        
        <div class="bg-white border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] w-full max-w-2xl md:mb-6 shrink-0 z-10 relative flex flex-col max-h-[90vh] md:max-h-none">
        <div class="px-6 py-4 border-b-2 border-black bg-black flex justify-between items-center shrink-0">
            <h2 class="text-sm font-black text-white uppercase tracking-widest">{{ isEditMode ? 'Edit Chronic Task' : 'Define New Task' }}</h2>
            <button @click="isFormOpen = false" class="text-white/60 hover:text-[#00FF88] transition-colors p-1 border border-white/20 hover:border-[#00FF88]">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
        </div>
        <form @submit.prevent="isEditMode ? submitEditTask() : submitHumanTask()" class="p-6 flex flex-col gap-4 overflow-y-auto custom-scrollbar">
            <div class="flex flex-col gap-1.5">
               <label class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Title</label>
               <input v-model="newTask.title" placeholder="Requirement summary..." class="w-full bg-white border-2 border-black px-4 py-2 text-sm outline-none font-bold text-gray-900 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" required />
            </div>
            <div class="flex flex-col gap-1.5">
               <label class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Description / Instructions</label>
               <textarea v-model="newTask.body" placeholder="Provide detailed context for the agent..." class="w-full bg-white border-2 border-black px-4 py-2.5 text-sm outline-none font-medium text-gray-800 transition-all resize-none focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] min-h-[100px]" required></textarea>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div class="flex flex-col gap-1.5">
                 <label class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Assignee</label>
                 <div class="flex p-1 bg-gray-100 border-2 border-black w-fit">
                    <button type="button" 
                            @click="newTask.assignee = 'agent'"
                            :class="newTask.assignee === 'agent' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                            class="px-5 py-1.5 text-[10px] font-black uppercase tracking-widest transition-all">
                      Agent
                    </button>
                    <button type="button" 
                            @click="newTask.assignee = 'human'"
                            :class="newTask.assignee === 'human' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                            class="px-5 py-1.5 text-[10px] font-black uppercase tracking-widest transition-all">
                      Human
                    </button>
                 </div>
              </div>              <div class="flex flex-col gap-2">
                  <label class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Schedule Type</label>
                  <div class="flex p-1 bg-gray-100 border-2 border-black w-fit">
                    <button type="button" 
                            @click="scheduleType = 'none'"
                            :class="scheduleType === 'none' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                            class="px-4 py-1.5 text-[9px] font-black uppercase tracking-widest transition-all">
                      None
                    </button>
                    <button type="button" 
                            @click="scheduleType = 'onetime'"
                            :class="scheduleType === 'onetime' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                            class="px-4 py-1.5 text-[9px] font-black uppercase tracking-widest transition-all">
                      One-time
                    </button>
                    <button type="button" 
                            @click="scheduleType = 'repeated'"
                            :class="scheduleType === 'repeated' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                            class="px-4 py-1.5 text-[9px] font-black uppercase tracking-widest transition-all">
                      Repeated
                    </button>
                  </div>

                  <!-- One-time specific UI -->
                  <div v-if="scheduleType === 'onetime'" class="mt-2 flex flex-col gap-1.5">
                    <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Execution Date & Time</label>
                    <input type="datetime-local" 
                           v-model="oneTimeDate"
                           class="bg-white border-2 border-black px-3 py-1.5 text-xs font-black uppercase tracking-widest text-black outline-none h-9 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" />
                  </div>

                  <!-- Repeated specific UI -->
                  <div v-if="scheduleType === 'repeated'" class="mt-2 flex flex-col gap-3">
                    <div class="flex flex-col gap-1.5">
                      <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Frequency</label>
                      <select v-model="repeatPreset" 
                              class="bg-white border-2 border-black px-2 py-1.5 text-[10px] font-black uppercase tracking-widest text-black outline-none h-9">
                        <option value="hourly">Hourly</option>
                        <option value="daily">Daily</option>
                        <option value="weekly">Weekly</option>
                        <option value="monthly">Monthly</option>
                        <option value="custom">Custom Repeat...</option>
                      </select>
                    </div>

                    <!-- Custom repeat days/time -->
                    <div v-if="repeatPreset === 'custom'" class="flex flex-col gap-2 bg-gray-50 p-3 border-2 border-black border-dashed">
                       <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Select Days</label>
                       <div class="flex flex-wrap gap-1">
                         <button v-for="d in daysOptions" :key="d.value"
                                 type="button"
                                 @click="toggleDay(d.value)"
                                 :class="selectedDays.includes(d.value) ? 'bg-[#00FF88] border-black' : 'bg-white border-gray-200 text-gray-400'"
                                 class="w-7 h-7 border-2 text-[10px] font-black flex items-center justify-center transition-all hover:border-black">
                           {{ d.label }}
                         </button>
                       </div>
                    </div>

                    <div v-if="repeatPreset !== 'hourly'" class="flex flex-col gap-1.5">
                      <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Time of Day</label>
                      <input type="time" v-model="repeatTime"
                             class="bg-white border-2 border-black px-3 py-1.5 text-xs font-black uppercase tracking-widest text-black outline-none h-9 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" />
                    </div>
                  </div>

                  <!-- Cron Preview (Debug/Expert Mode) -->
                  <div v-if="scheduleType !== 'none'" class="mt-3 p-2 bg-gray-900 border border-gray-700">
                    <div class="flex items-center justify-between mb-1">
                      <span class="text-[8px] font-black text-gray-400 uppercase tracking-widest">Generated Cron</span>
                      <span v-if="nextRunPreview" class="text-[8px] font-black text-[#00FF88] uppercase tracking-widest">Next Run: {{ nextRunPreview }}</span>
                    </div>
                    <code class="text-[10px] font-mono text-white block">{{ newTask.cronSchedule }}</code>
                  </div>
               </div>
>
            </div>

            <div v-if="!isEditMode" class="flex flex-col gap-2 pt-2">
               <div class="flex items-center justify-between">
                  <h3 class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Attachments ({{ newTaskAttachments.length }})</h3>
                  <button type="button" @click="$refs.fileInput.click()" class="text-[10px] font-black text-black border-b-2 border-black hover:text-[#00FF88] hover:bg-black uppercase tracking-widest transition-colors">Add Files</button>
               </div>
               <div v-if="newTaskAttachments.length > 0" class="flex flex-wrap gap-2 p-3 bg-gray-50 border-2 border-black min-h-[50px]">
                  <div v-for="(att, i) in newTaskAttachments" :key="i" class="flex items-center text-[10px] bg-white border-2 border-black px-3 py-1 font-black uppercase tracking-widest">
                    <span class="truncate max-w-[140px]">{{ att.filename }}</span>
                    <button @click.prevent="newTaskAttachments.splice(i, 1)" class="ml-2 text-gray-400 hover:text-red-500">
                      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M6 18L18 6M6 6l12 12"></path></svg>
                    </button>
                  </div>
               </div>
               <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />
            </div>

            <div class="mt-2 flex gap-3 flex-row-reverse">
               <button type="submit" :disabled="sending || !newTask.title || !newTask.body" class="bg-black text-white px-6 py-2.5 border-2 border-black text-xs font-black uppercase tracking-widest hover:bg-[#00FF88] hover:text-black transition-all shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px] flex items-center justify-center gap-2 disabled:opacity-50">
                  <svg v-if="sending" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                  {{ sending ? (isEditMode ? 'Saving...' : 'Dispatching...') : (isEditMode ? 'Save Changes' : 'Create Task') }}
               </button>
               <button type="button" @click="isFormOpen = false" class="px-5 py-2.5 border-2 border-black bg-white text-xs font-black uppercase tracking-widest hover:bg-gray-100 transition-all ml-auto">Cancel</button>
            </div>
        </form>
        </div>
      </div>
    </Transition>

    <!-- Single List Area -->
    <div class="flex-1 overflow-y-auto pt-4 pb-6 custom-scrollbar pr-2 md:pr-4">
      <div v-if="localTasks.length === 0" class="flex flex-col items-center justify-center text-gray-300 opacity-80 py-16 border-2 border-dashed border-gray-200 bg-gray-50 mt-2">
        <svg class="w-8 h-8 mb-3 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 14l-7 7m0 0l-7-7m7 7V3" /></svg>
        <span class="text-xs font-black uppercase tracking-widest text-gray-500">No tasks found in workspace</span>
      </div>

      <div v-else-if="displayGroups.length === 0" class="flex flex-col items-center justify-center text-gray-300 opacity-80 py-16 border-2 border-dashed border-gray-200 bg-gray-50 mt-2">
        <svg class="w-8 h-8 mb-3 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
        <span class="text-xs font-black uppercase tracking-widest text-gray-500">No tasks match filter</span>
      </div>

      <div v-else class="space-y-8">
        <div v-for="grp in displayGroups" :key="grp.title">
          <div class="mb-3 flex items-center gap-2">
            <h3 class="text-xs font-black uppercase tracking-widest text-black">{{ grp.title }}</h3>
            <span class="text-[10px] font-bold text-gray-400 border border-gray-200 bg-white px-1 shadow-[1px_1px_0px_0px_rgba(0,0,0,0.1)]">{{ grp.title === 'Completed' ? grp.totalCompleted : grp.tasks.length }}</span>
            <div class="h-px bg-gray-200 flex-1 ml-2"></div>
          </div>
          
          <div class="space-y-3">
            <div v-for="(t, idx) in grp.tasks" :key="t.id"
                 @click="openTask(t)"
                 class="flex items-center justify-between p-3.5 border-2 cursor-pointer bg-white shadow-[3px_3px_0px_0px_rgba(0,0,0,1)] hover:-translate-y-0.5 hover:shadow-[5px_5px_0px_0px_rgba(0,0,0,1)] transition-all group rounded-sm relative"
                 :class="[getTaskBgStyle(t.status), activeStatusMenuId === t.id ? 'z-50' : 'z-0']">
                 
              <div class="flex items-center gap-3.5 flex-1 min-w-0 pr-4">
                 <span class="w-2.5 h-2.5 rounded-full border border-black shrink-0" :class="getTaskDotStyle(t.status)"></span>
                 <div class="flex flex-col gap-1 w-full relative">
                   <span class="font-black text-[14px] text-gray-900 leading-snug truncate group-hover:text-indigo-700 transition-colors w-full font-bold text-sm">{{ t.title }}</span>
                   
                   <div class="flex flex-wrap items-center gap-2 md:gap-3 text-[9px] font-black uppercase tracking-widest mt-0.5">
                     <span class="text-gray-500">{{ formatTime(t.created_at) }}</span>
                     
                     <span class="flex items-center gap-1">
                       <span class="text-gray-400">BY</span>
                       <span :class="t.created_by === 'human' ? 'text-black' : 'text-indigo-600'">{{ t.created_by === 'human' ? 'YOU' : 'AGENT' }}</span>
                     </span>
                     
                     <span class="flex items-center gap-1" v-if="t.assignee">
                       <span class="text-gray-400">FOR</span>
                       <span :class="t.assignee === 'human' ? 'text-black' : 'text-indigo-600'">{{ t.assignee === 'human' ? 'YOU' : 'AGENT' }}</span>
                     </span>

                     <span v-if="t.status === 'cron'" class="text-indigo-800 bg-indigo-100 border border-indigo-300 px-1 py-0.5">
                       ⏰ {{ formatCron(t.cron_schedule) }}
                     </span>
                     <span v-if="t.status === 'cron' && getNextRunLabel(t.cron_schedule)" class="text-indigo-600 font-bold">
                       • NEXT: {{ getNextRunLabel(t.cron_schedule) }}
                     </span>

                     <span class="flex items-center gap-1 bg-white border border-gray-200 px-1 text-gray-600" v-if="t.Messages && t.Messages.length > 0">
                       <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" /></svg>
                       {{ t.Messages.length }}
                     </span>
                     
                     <div v-if="t.attachments && t.attachments.length > 0" class="flex items-center gap-1 bg-white border border-gray-200 px-1 text-gray-600">
                        <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                        {{ t.attachments.length }}
                     </div>
                   </div>
                 </div>
              </div>
               
               <div v-if="!isArchived && grp.title === 'Not Started'" class="flex flex-col gap-0.5 mr-1 shrink-0">
                    <button @click.stop="reorderTask(t, -1)" 
                            :disabled="idx === 0"
                            class="p-1 text-gray-400 hover:text-black hover:bg-gray-100 disabled:opacity-20 transition-all rounded-sm" title="Move Up">
                      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 15l7-7 7 7" /></svg>
                    </button>
                    <button @click.stop="reorderTask(t, 1)" 
                            :disabled="idx === grp.tasks.length - 1"
                            class="p-1 text-gray-400 hover:text-black hover:bg-gray-100 disabled:opacity-20 transition-all rounded-sm" title="Move Down">
                      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M19 9l-7 7-7-7" /></svg>
                    </button>
                  </div>
              <div class="flex items-center justify-end gap-3 shrink-0">
                 <div class="hidden md:block relative group/status shrink-0">
                    <div v-if="t.status === 'cron'"
                         class="text-[10px] font-black uppercase tracking-widest px-2 py-1 flex items-center justify-center min-w-[90px] gap-1 border-2" 
                         :class="getTaskBadgeStyle(t.status)">
                      {{ getTaskLabel(t.status) }}
                    </div>
                    <button v-else
                            @click.stop="activeStatusMenuId = activeStatusMenuId === t.id ? null : t.id"
                            class="text-[10px] font-black uppercase tracking-widest px-2 py-1 flex items-center justify-center min-w-[90px] gap-1 transition-all hover:translate-y-px active:translate-y-0.5" 
                            :class="getTaskBadgeStyle(t.status)">
                      {{ getTaskLabel(t.status) }}
                      <svg class="w-2.5 h-2.5 ml-0.5 transition-transform duration-200" :class="activeStatusMenuId === t.id ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M19 9l-7 7-7-7" /></svg>
                    </button>
 
                   <!-- Status Dropdown (Feed) -->
                   <div v-if="activeStatusMenuId === t.id" 
                        v-click-outside="() => activeStatusMenuId = null"
                        class="absolute right-0 top-full mt-1 w-36 bg-white border-2 border-black shadow-[3px_3px_0px_0px_rgba(0,0,0,1)] z-50 animate-in slide-in-from-top-1 duration-150">
                     <div class="p-1 flex flex-col gap-0.5">
                        <button v-if="t.status !== 'notstarted'" @click.stop="respond(t.id, 'notstarted'); activeStatusMenuId = null"
                                class="flex items-center gap-2 px-2.5 py-1.5 text-[9px] font-black uppercase tracking-widest hover:bg-gray-100 text-gray-500 transition-colors text-left">
                          <div class="w-1.5 h-1.5 rounded-full bg-gray-300"></div>
                          Queue
                        </button>
                        <button v-if="t.status !== 'ongoing'" @click.stop="respond(t.id, 'ongoing'); activeStatusMenuId = null"
                                class="flex items-center gap-2 px-2.5 py-1.5 text-[9px] font-black uppercase tracking-widest hover:bg-[#00FF88]/10 text-black transition-colors text-left">
                          <div class="w-1.5 h-1.5 rounded-full bg-[#00FF88]"></div>
                          Start
                        </button>
                        <button v-if="t.status !== 'completed'" @click.stop="respond(t.id, 'completed'); activeStatusMenuId = null"
                                class="flex items-center gap-2 px-2.5 py-1.5 text-[9px] font-black uppercase tracking-widest hover:bg-black hover:text-white transition-colors text-left border-t border-gray-100 mt-0.5 pt-1.5">
                          <div class="w-1.5 h-1.5 rounded-full bg-green-500"></div>
                          Complete
                        </button>
                        <button v-if="t.status !== 'rejected'" @click.stop="respond(t.id, 'rejected'); activeStatusMenuId = null"
                                class="flex items-center gap-2 px-2.5 py-1.5 text-[9px] font-black uppercase tracking-widest hover:bg-red-50 text-red-600 transition-colors text-left">
                          <div class="w-1.5 h-1.5 rounded-full bg-red-500"></div>
                          Reject
                        </button>
                     </div>
                   </div>
                 </div>
                  <button v-if="!isArchived && t.status === 'cron'" @click.stop="triggerEdit(t)" class="opacity-100 sm:opacity-0 sm:group-hover:opacity-100 text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 p-1.5 rounded-sm transition-all shrink-0 ml-1">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                  </button>
                  <button v-if="!isArchived" @click.stop="triggerDelete(t)" class="opacity-100 sm:opacity-0 sm:group-hover:opacity-100 text-gray-400 hover:text-red-500 hover:bg-red-50 p-1.5 rounded-sm transition-all shrink-0 ml-1">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                  </button>
              </div>
            </div>
          </div>
          
          <button v-if="grp.hasMore" @click.stop="completedLimit += 5" class="w-full mt-3 py-3 border-2 border-dashed border-gray-300 text-gray-500 text-[10px] font-black uppercase tracking-widest hover:border-black hover:text-black hover:bg-white transition-all shadow-none hover:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">
            Load More ({{ grp.totalCompleted - completedLimit }} remaining)
          </button>
        </div>
      </div>
    </div>
    
    <!-- Refined Stats Footer -->
    <div v-if="localTasks.length > 0 && !isArchived" class="mt-4 pt-4 pb-2 border-t-2 border-dashed border-gray-200 flex items-center justify-between text-[11px] md:text-xs font-bold text-gray-400 uppercase tracking-widest shrink-0">
        <div class="flex items-center gap-4">
           <div class="flex items-center gap-1.5" title="Active Tasks">
             <span class="hidden md:inline">Active Tasks</span>
             <span class="md:hidden">⚡</span>
             <span class="text-black font-black">{{ activeTaskCount }}</span>
           </div>
           <div class="flex items-center gap-1.5 border-l-2 border-gray-100 pl-4" title="Scheduled Runs">
             <span class="hidden md:inline">Scheduled</span>
             <span class="md:hidden">⏰</span>
             <span class="text-black font-black">{{ scheduledCount }}</span>
           </div>
           <div class="flex items-center gap-1.5 border-l-2 border-gray-100 pl-4 transition-all" 
                :class="pendingInputCount > 0 ? 'bg-yellow-400 text-black px-2 -my-1 py-1 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]' : ''"
                title="Your Attention Needed">
             <span class="hidden md:inline">{{ pendingInputCount > 0 ? 'Action Required' : 'Pending on Me' }}</span>
             <span class="md:hidden">!</span>
             <span class="font-black" :class="pendingInputCount > 0 ? 'text-black' : 'text-gray-300'">{{ pendingInputCount }}</span>
           </div>
        </div>
        <div class="flex items-center gap-2 group/status">
            <div class="flex items-center gap-1.5 px-2 py-1 border-2 border-black bg-white shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">
              <span class="w-2 h-2 rounded-full border border-black shadow-[1px_1px_0px_0px_rgba(0,0,0,0.1)] transition-colors duration-500" 
                    :class="isAgentConnected ? 'bg-[#00FF88] shadow-[#00FF88]/40' : 'bg-red-500 animate-pulse'"></span>
              <span class="text-[9px] font-black text-black">{{ isAgentConnected ? 'Live' : 'Offline' }}</span>
            </div>
        </div>
    </div>
  </div>
</template>


<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import cronParser from 'cron-parser';
import { createTask, respondToTask, deleteTask, getAttachmentUrl, updateScheduledTask, updateTaskOrder, updateTaskStatus } from '../api';
import DeleteModal from './DeleteModal.vue';
import { useToasts } from '../composables/useToasts';

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
  initialTasks: { type: Array, default: () => [] },
  liveEvents: { type: Array, default: () => [] },
  isArchived: { type: Boolean, default: false },
  isAgentConnected: { type: Boolean, default: false },
  filterScheduled: { type: Boolean, default: false }
});

const emit = defineEmits(['archive', 'toggleScheduled']);

const router = useRouter();
const { notifyError, notifySuccess, notifyInfo } = useToasts();
const fileInput = ref(null);
const responses = ref({});
const newTask = ref({ title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '0 * * * *' });
const newTaskAttachments = ref([]);
const activeStatusMenuId = ref(null);
const sending = ref(false);
const toastMessage = ref('');

// Scheduling state
const scheduleType = ref('none'); // 'none' | 'onetime' | 'repeated'
const oneTimeDate = ref('');
const repeatPreset = ref('daily');
const repeatTime = ref('09:00');
const selectedDays = ref([1, 2, 3, 4, 5]); // Mon-Fri
const daysOptions = [
  { label: 'M', value: 1 },
  { label: 'T', value: 2 },
  { label: 'W', value: 3 },
  { label: 'T', value: 4 },
  { label: 'F', value: 5 },
  { label: 'S', value: 6 },
  { label: 'S', value: 0 },
];

const nextRunPreview = computed(() => {
  if (scheduleType.value === 'none' || !newTask.value.cronSchedule) return '';
  try {
    const interval = cronParser.parseExpression(newTask.value.cronSchedule);
    const next = interval.next().toDate();
    return formatRelativeTime(next);
  } catch (e) {
    return '';
  }
});

function toggleDay(day) {
  const idx = selectedDays.value.indexOf(day);
  if (idx === -1) selectedDays.value.push(day);
  else if (selectedDays.value.length > 1) selectedDays.value.splice(idx, 1);
}

// Watchers to build cron string
watch([scheduleType, oneTimeDate, repeatPreset, repeatTime, selectedDays], () => {
  if (scheduleType.value === 'none') {
    newTask.value.cronSchedule = '';
    return;
  }

  if (scheduleType.value === 'onetime') {
    if (!oneTimeDate.value) {
      newTask.value.cronSchedule = '';
      return;
    }
    const d = new Date(oneTimeDate.value);
    // min hour dom month *
    newTask.value.cronSchedule = `${d.getMinutes()} ${d.getHours()} ${d.getDate()} ${d.getMonth() + 1} *`;
    return;
  }

  if (scheduleType.value === 'repeated') {
    const [hours, minutes] = repeatTime.value.split(':').map(Number);
    
    if (repeatPreset.value === 'hourly') {
      newTask.value.cronSchedule = `0 * * * *`;
    } else if (repeatPreset.value === 'daily') {
      newTask.value.cronSchedule = `${minutes} ${hours} * * *`;
    } else if (repeatPreset.value === 'weekly') {
      newTask.value.cronSchedule = `${minutes} ${hours} * * 0`;
    } else if (repeatPreset.value === 'monthly') {
      newTask.value.cronSchedule = `${minutes} ${hours} 1 * *`;
    } else if (repeatPreset.value === 'custom') {
      const days = [...selectedDays.value].sort().join(',');
      newTask.value.cronSchedule = `${minutes} ${hours} * * ${days}`;
    }
  }
}, { deep: true });

const isFormOpen = ref(false);
const isEditMode = ref(false);
const editingTaskId = ref(null);

const showDeleteModal = ref(false);
const taskToDeleteId = ref(null);
const taskToDeleteTitle = ref('');

const localTasks = ref([...props.initialTasks]);
const showScheduledOnly = computed(() => props.filterScheduled);
const completedLimit = ref(5);

// Sync with initial loads or reloads
watch(() => props.initialTasks, (newTasks) => {
  localTasks.value = [...newTasks];
}, { deep: true });

// Listen to stream events and update the local list
watch(() => props.liveEvents.length, (newLen, oldLen) => {
  if (newLen > oldLen) {
    const fresh = props.liveEvents.slice(oldLen);
    fresh.forEach(ev => {
      if (ev.type === 'task.deleted') {
        const id = ev.payload.id;
        localTasks.value = localTasks.value.filter(x => x.id !== id);
        return;
      }

      if (ev.type === 'task.updated' || ev.type === 'task.created' || ev.type === 'status.updated' || ev.type === 'respond.ack') {
        const t = ev.payload;
        const idx = localTasks.value.findIndex(x => x.id === t.id);
        if (idx !== -1) {
          localTasks.value[idx] = t;
        } else {
          localTasks.value.push(t);
          // Trigger notification if it's an agent task
          if (t.created_by === 'agent') {
            notifyInfo(`Agent defined a new task: ${t.title}`, 'New Task');
          }
        }
      }
    });
  }
});

function formatTime(dateStr) {
  if (!dateStr) return 'Just now';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return 'Just now';
  return d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
}

function formatCron(cron) {
  if (!cron) return '';
  
  // Detect one-time (has specific date parts and NO wildcards in DOM/Month)
  const parts = cron.split(' ');
  if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') {
    return `ONE-TIME`;
  }

  const presets = {
    '0 * * * *': 'Hourly',
    '*/15 * * * *': 'Every 15m',
    '*/30 * * * *': 'Every 30m',
  };
  if (presets[cron]) return presets[cron];

  try {
    const [min, hour, dom, month, dow] = parts;
    if (dow !== '*' && dom === '*' && month === '*') {
      const days = dow.split(',').map(d => daysOptions.find(o => o.value == d)?.label || d).join(',');
      return `Weekly (${days}) at ${hour}:${min.padStart(2, '0')}`;
    }
    if (dom !== '*' && month === '*' && dow === '*') {
      return `Monthly (Day ${dom}) at ${hour}:${min.padStart(2, '0')}`;
    }
    if (dom === '*' && month === '*' && dow === '*') {
      return `Daily at ${hour}:${min.padStart(2, '0')}`;
    }
  } catch (e) {}

  return cron;
}

function getNextRunLabel(cron) {
  if (!cron) return '';
  try {
    const interval = cronParser.parseExpression(cron);
    const next = interval.next().toDate();
    return formatRelativeTime(next);
  } catch (e) {
    return '';
  }
}

function formatRelativeTime(date) {
  const now = new Date();
  const diffMs = date.getTime() - now.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffDay > 0) return `In ${diffDay}d ${diffHour % 24}h`;
  if (diffHour > 0) return `In ${diffHour}h ${diffMin % 60}m`;
  if (diffMin > 0) return `In ${diffMin}m`;
  return 'Just now';
}

function getTaskOrder(t) {
  if (t.sort_order) return t.sort_order;
  if (!t.created_at) return Date.now();
  return new Date(t.created_at).getTime() / 1000.0;
}

const allTasks = computed(() => {
  return [...localTasks.value].sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
});

const displayGroups = computed(() => {
  if (showScheduledOnly.value) {
    const cronTasks = localTasks.value.filter(t => t.status === 'cron').sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
    if (cronTasks.length === 0) {
      return [{ title: 'Scheduled Tasks', tasks: [], totalCompleted: 0 }];
    }

    const categories = [
      { label: 'Every 15 mins', values: ['*/15 * * * *'] },
      { label: 'Every 30 mins', values: ['*/30 * * * *'] },
      { label: 'Hourly', values: ['0 * * * *'] },
      { label: 'Daily', values: ['0 0 * * *'] },
      { label: 'Weekly', values: ['0 0 * * 0'] },
      { label: 'Monthly', values: ['0 0 1 * *'] },
    ];

    const grps = [];
    const handledIds = new Set();

    categories.forEach(cat => {
      const matched = cronTasks.filter(t => cat.values.includes(t.cron_schedule));
      if (matched.length > 0) {
        grps.push({ title: cat.label, tasks: matched });
        matched.forEach(t => handledIds.add(t.id));
      }
    });

    const other = cronTasks.filter(t => !handledIds.has(t.id));
    if (other.length > 0) {
      grps.push({ title: 'Other', tasks: other });
    }

    return grps;
  }

  const ongoing = localTasks.value.filter(t => ['ongoing', 'blocked'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const notStarted = localTasks.value.filter(t => ['notstarted'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const completed = localTasks.value.filter(t => ['completed', 'rejected'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));

  const groups = [];
  if (ongoing.length > 0) groups.push({ title: 'Ongoing', tasks: ongoing });
  if (notStarted.length > 0) groups.push({ title: 'Not Started', tasks: notStarted });

  if (completed.length > 0) {
    groups.push({
      title: 'Completed',
      tasks: completed.slice(0, completedLimit.value),
      hasMore: completed.length > completedLimit.value,
      totalCompleted: completed.length
    });
  }

  return groups;
});

const pendingInputCount = computed(() => {
  return localTasks.value.filter(t => t.created_by === 'agent' && (t.status === 'notstarted')).length;
});

const scheduledCount = computed(() => {
  return localTasks.value.filter(t => t.status === 'cron').length;
});

const activeTaskCount = computed(() => {
  return localTasks.value.length - scheduledCount.value;
});

// Timer for updating relative times
const now = ref(new Date());
let timer = null;
onMounted(() => {
  timer = setInterval(() => {
    now.value = new Date();
  }, 30000); // 30s
});
onUnmounted(() => {
  if (timer) clearInterval(timer);
});

function getTaskBgStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-50 border-black';
  if (status === 'blocked') return 'bg-red-50 border-black';
  if (status === 'completed') return 'bg-green-50 border-black';
  if (status === 'cron') return 'bg-indigo-50 border-indigo-200';
  return 'bg-gray-50 border-gray-300 border-dashed shadow-none text-gray-500 hover:border-gray-400 hover:bg-gray-100';
}
function getTaskDotStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-400';
  if (status === 'blocked') return 'bg-red-500';
  if (status === 'completed') return 'bg-green-500';
  if (status === 'cron') return 'bg-indigo-500 border-indigo-600';
  return 'bg-gray-300 border-gray-400';
}
function getTaskBadgeStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-200 border-black text-black border-2';
  if (status === 'blocked') return 'bg-red-200 border-black text-black border-2';
  if (status === 'completed') return 'bg-green-200 border-black text-black border-2';
  if (status === 'cron') return 'bg-indigo-200 border-indigo-400 text-indigo-800 border-2';
  return 'bg-gray-100 border-gray-300 text-gray-500 font-bold border-2';
}
function getTaskLabel(status) {
  if (status === 'notstarted') return 'NOT STARTED';
  if (status === 'cron') return 'CHRONIC';
  return status;
}

function handleFileUpload(event) {
  const files = event.target.files;
  if (!files || files.length === 0) return;

  for (let i = 0; i < files.length; i++) {
    const fn = files[i];
    const reader = new FileReader();
    reader.onload = (e) => {
      const base64Str = e.target.result.split(',')[1];
      newTaskAttachments.value.push({
        filename: fn.name,
        mimeType: fn.type || 'application/octet-stream',
        data: base64Str
      });
    };
    reader.readAsDataURL(fn);
  }
  if (fileInput.value) fileInput.value.value = '';
}

async function submitHumanTask() {
  if (!newTask.value.title.trim() || !newTask.value.body.trim()) return;
  sending.value = true;
  try {
    const status = scheduleType.value !== 'none' ? 'cron' : 'notstarted';
    const cronSched = newTask.value.cronSchedule;
    
    const res = await createTask(
      props.workspaceId, 
      newTask.value.title, 
      newTask.value.body, 
      newTask.value.assignee, 
      newTaskAttachments.value,
      status,
      cronSched
    );
    const idx = localTasks.value.findIndex(x => x.id === res.task.id);
    if (idx === -1) localTasks.value.push(res.task);
    newTask.value = { title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '' };
    scheduleType.value = 'none';
    newTaskAttachments.value = [];
    isFormOpen.value = false;
    notifySuccess(status === 'cron' ? 'Task scheduled successfully' : 'Task dispatched to pipeline');
  } catch(err) {
    notifyError("Dispatch Error: " + err.message);
  } finally {
    sending.value = false;
  }
}

async function submitEditTask() {
  if (!newTask.value.title.trim() || !newTask.value.body.trim()) return;
  sending.value = true;
  try {
    const res = await updateScheduledTask(
      props.workspaceId,
      editingTaskId.value,
      newTask.value.title,
      newTask.value.body,
      newTask.value.assignee,
      newTask.value.cronSchedule
    );
    const idx = localTasks.value.findIndex(x => x.id === res.task.id);
    if (idx !== -1) localTasks.value[idx] = res.task;
    isFormOpen.value = false;
    isEditMode.value = false;
    editingTaskId.value = null;
    notifySuccess('Chronic task updated');
  } catch(err) {
    notifyError("Update Error: " + err.message);
  } finally {
    sending.value = false;
  }
}

function startCreate() {
  isEditMode.value = false;
  editingTaskId.value = null;
  newTask.value = { title: '', body: '', assignee: 'agent', cronSchedule: '' };
  scheduleType.value = 'none';
  oneTimeDate.value = '';
  repeatPreset.value = 'daily';
  repeatTime.value = '09:00';
  selectedDays.value = [1, 2, 3, 4, 5];
  newTaskAttachments.value = [];
  isFormOpen.value = true;
}

function openTask(task) {
  if (task.status === 'cron') return;
  router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}`);
}

function triggerEdit(task) {
  isEditMode.value = true;
  editingTaskId.value = task.id;
  newTask.value = { 
    title: task.title, 
    body: task.body, 
    assignee: task.assignee, 
    cronSchedule: task.cron_schedule 
  };

  // Attempt to parse existing cron into UI state
  if (task.cron_schedule) {
    const parts = task.cron_schedule.split(' ');
    if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') {
      scheduleType.value = 'onetime';
      // Approximate date-time (for display mostly, since Year is missing in cron)
      // We'll leave it blank or set to current year
      const now = new Date();
      oneTimeDate.value = `${now.getFullYear()}-${String(parts[3]).padStart(2, '0')}-${String(parts[2]).padStart(2, '0')}T${String(parts[1]).padStart(2, '0')}:${String(parts[0]).padStart(2, '0')}`;
    } else {
      scheduleType.value = 'repeated';
      if (task.cron_schedule === '0 * * * *') {
        repeatPreset.value = 'hourly';
      } else {
        const [min, hour, dom, month, dow] = parts;
        repeatTime.value = `${String(hour).padStart(2, '0')}:${String(min).padStart(2, '0')}`;
        if (dow !== '*' && dom === '*' && month === '*') {
          repeatPreset.value = 'custom';
          selectedDays.value = dow.split(',').map(Number);
        } else if (dom === '*' && month === '*' && dow === '*') {
          repeatPreset.value = 'daily';
        } else if (dom === '*' && month === '*' && dow === '0') {
          repeatPreset.value = 'weekly';
        } else {
          repeatPreset.value = 'custom';
        }
      }
    }
  } else {
    scheduleType.value = 'none';
  }

  isFormOpen.value = true;
}
async function respond(taskId, action) {
  const text = responses.value[taskId] || '';
  try {
    let res;
    // Check if it's a direct status update (allowed by clicking badge) or a response to agent
    if (['notstarted', 'ongoing', 'completed', 'rejected'].includes(action)) {
        res = await updateTaskStatus(props.workspaceId, taskId, action);
    } else {
        res = await respondToTask(props.workspaceId, taskId, action, text);
    }
    const idx = localTasks.value.findIndex(x => x.id === taskId);
    if (idx !== -1) {
      localTasks.value[idx] = res.task;
    }
    delete responses.value[taskId];
    notifySuccess('Status updated successfully');
  } catch(err) {
    notifyError("Failed to update status: " + err.message);
  }
}

async function triggerDelete(task) {
  taskToDeleteId.value = task.id;
  taskToDeleteTitle.value = task.title;
  showDeleteModal.value = true;
}

function closeDeleteModal() {
  showDeleteModal.value = false;
  taskToDeleteId.value = null;
  taskToDeleteTitle.value = '';
}

async function onDeleteConfirm() {
  const taskId = taskToDeleteId.value;
  if (!taskId) return;
  
  try {
    await deleteTask(props.workspaceId, taskId);
    localTasks.value = localTasks.value.filter(x => x.id !== taskId);
    notifySuccess('Task purged');
  } catch(err) {
    notifyError('Delete Error: ' + err.message);
  } finally {
    closeDeleteModal();
  }
}

async function reorderTask(task, direction) {
  const group = displayGroups.value.find(g => g.title === 'Not Started');
  if (!group) return;
  
  const idx = group.tasks.findIndex(x => x.id === task.id);
  if (idx === -1) return;
  
  const targetIdx = idx + direction;
  if (targetIdx < 0 || targetIdx >= group.tasks.length) return;
  
  const neighbor = group.tasks[targetIdx];
  // Sort is DESC so: UP = idx decreases relative to list but order value should INCREASE
  // actually index 0 is at the top.
  // Move UP (direction -1) to targetIdx (idx - 1)
  // Move DOWN (direction 1) to targetIdx (idx + 1)
  
  const neighborOrder = getTaskOrder(neighbor);
  let newOrder;
  if (direction === -1) {
    // Moving UP above neighbor: order must be > neighborOrder
    newOrder = neighborOrder + 0.001; 
  } else {
    // Moving DOWN below neighbor: order must be < neighborOrder
    newOrder = neighborOrder - 0.001;
  }
  
  try {
    const res = await updateTaskOrder(props.workspaceId, task.id, newOrder);
    const localIdx = localTasks.value.findIndex(x => x.id === task.id);
    if (localIdx !== -1) {
      localTasks.value[localIdx] = res.task;
    }
  } catch (err) {
    notifyError('Reorder Error: ' + err.message);
  }
}
defineExpose({ startCreate });
</script>

<style scoped>
.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: all 0.3s ease-out;
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(-10px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
