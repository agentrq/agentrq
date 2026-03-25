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
    <div v-if="!isArchived" class="pt-2 pb-4 flex flex-col md:flex-row items-stretch md:items-center justify-between gap-3 shrink-0">
        <button @click="showScheduledOnly = !showScheduledOnly"
                class="flex items-center gap-2 px-3 py-2 border-2 border-black text-[11px] font-black uppercase tracking-widest transition-all w-max shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]"
                :class="showScheduledOnly ? 'bg-[#00FF88] text-black translate-y-[1px]' : 'bg-white text-gray-500 hover:bg-[#00FF88]/20 hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]'"
                :title="showScheduledOnly ? 'Show All' : 'Show Scheduled Only'">
           <svg class="w-4 h-4 text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
             <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" />
           </svg>
           <span class="hidden md:inline">Scheduled Only</span>
        </button>
       
       <div class="flex items-center gap-2.5 ml-auto">
         <button @click="$emit('archive')" 
                 class="flex items-center gap-2 px-5 py-2.5 bg-white border-2 border-black text-[11px] font-black text-gray-500 hover:text-red-500 hover:bg-gray-50 transition-all uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]">
            <svg class="w-3.5 h-3.5 font-bold" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
            Archive
         </button>
          <button @click="startCreate" 
                  class="group flex items-center gap-2.5 bg-[#00FF88] text-black border-2 border-black hover:bg-black hover:text-[#00FF88] px-5 py-2.5 text-[11px] font-black uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all hover:translate-y-[1px] hover:shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[2px]">
            <svg class="w-4 h-4 transform group-hover:rotate-90 transition-transform duration-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
            New Task
         </button>
       </div>
    </div>

    <!-- Create/Edit Task Inline Form -->
    <Transition name="fade-down">
      <div v-if="isFormOpen" class="bg-white border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] mb-6 shrink-0 z-10 relative">
        <div class="px-6 py-4 border-b-2 border-black bg-black flex justify-between items-center shrink-0">
            <h2 class="text-sm font-black text-white uppercase tracking-widest">{{ isEditMode ? 'Edit Chronic Task' : 'Define New Task' }}</h2>
            <button @click="isFormOpen = false" class="text-white/60 hover:text-[#00FF88] transition-colors p-1 border border-white/20 hover:border-[#00FF88]">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
        </div>
        <form @submit.prevent="isEditMode ? submitEditTask() : submitHumanTask()" class="p-6 flex flex-col gap-4 overflow-y-auto max-h-[70vh] custom-scrollbar">
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
              </div>

              <div class="flex flex-col gap-1.5">
                  <div class="flex items-center gap-2 mb-1">
                     <label class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Chronic Task (Recurring)</label>
                     <button type="button" @click="newTask.isRecurring = !newTask.isRecurring" 
                             class="w-10 h-5 flex items-center transition-all duration-300 border-2 border-black p-0.5"
                             :class="newTask.isRecurring ? 'bg-[#00FF88]' : 'bg-gray-200'">
                        <div class="w-3 h-3 bg-black transform transition-all duration-300"
                             :class="newTask.isRecurring ? 'translate-x-[18px]' : 'translate-x-0'"></div>
                     </button>
                  </div>
                  
                  <div v-if="newTask.isRecurring" class="flex gap-2">
                     <select v-model="newTask.cronSchedule" class="flex-1 bg-white border-2 border-black px-2 py-1 text-[11px] font-black uppercase tracking-widest text-black outline-none h-8">
                      <option value="*/15 * * * *">Every 15 Min</option>
                      <option value="*/30 * * * *">Every 30 Min</option>
                      <option value="0 * * * *">Hourly</option>
                      <option value="0 0 * * *">Daily</option>
                      <option value="0 0 * * 0">Weekly</option>
                      <option value="0 0 1 * *">Monthly</option>
                    </select>
                    <input v-model="newTask.cronSchedule" placeholder="Cron: * * * * *" class="flex-1 bg-white border-2 border-black px-2 py-1 text-[11px] font-mono font-bold text-black outline-none h-8" />
                  </div>
               </div>
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
            <div v-for="t in grp.tasks" :key="t.id"
                 @click="openTask(t)"
                 class="flex items-center justify-between p-3.5 border-2 cursor-pointer bg-white shadow-[3px_3px_0px_0px_rgba(0,0,0,1)] hover:-translate-y-0.5 hover:shadow-[5px_5px_0px_0px_rgba(0,0,0,1)] transition-all group rounded-sm"
                 :class="getTaskBgStyle(t.status)">
                 
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
                       CRON: {{ formatCron(t.cron_schedule) }}
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

              <div class="flex items-center justify-end gap-3 shrink-0">
                 <div v-if="!isArchived && t.created_by === 'agent' && (t.status === 'notstarted' || t.status === 'pending')" class="hidden md:flex items-center gap-1.5 mr-2">
                                           <button @click.stop.prevent="respond(t.id, 'allow')" 
                              :disabled="!isAgentConnected"
                              class="px-3 py-1 bg-black text-white hover:bg-zinc-800 text-[10px] font-black uppercase tracking-widest border-2 border-black shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-0.5 transition-all disabled:opacity-50 disabled:cursor-not-allowed disabled:shadow-none disabled:translate-y-0">Approve</button>

                                           <button @click.stop.prevent="respond(t.id, 'reject')" 
                              :disabled="!isAgentConnected"
                              class="px-3 py-1 bg-white text-red-600 hover:bg-red-50 text-[10px] font-black uppercase tracking-widest border-2 border-red-200 shadow-[2px_2px_0px_0px_rgba(254,204,203,1)] active:shadow-none active:translate-y-0.5 transition-all disabled:opacity-50 disabled:cursor-not-allowed disabled:shadow-none disabled:translate-y-0">Reject</button>

                 </div>
                 <span class="text-[10px] font-black uppercase tracking-widest px-2 py-1 flex items-center justify-center min-w-[90px]" :class="getTaskBadgeStyle(t.status)">
                   {{ getTaskLabel(t.status) }}
                 </span>
                  <button v-if="!isArchived && t.status === 'cron'" @click.stop="triggerEdit(t)" class="opacity-0 group-hover:opacity-100 text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 p-1.5 rounded-sm transition-all shrink-0 ml-1">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                  </button>
                  <button v-if="!isArchived" @click.stop="triggerDelete(t)" class="opacity-0 group-hover:opacity-100 text-gray-400 hover:text-red-500 hover:bg-red-50 p-1.5 rounded-sm transition-all shrink-0 ml-1">
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
import { ref, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { createTask, respondToTask, deleteTask, getAttachmentUrl, updateScheduledTask } from '../api';
import DeleteModal from './DeleteModal.vue';
import { useToasts } from '../composables/useToasts';

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
  initialTasks: { type: Array, default: () => [] },
  liveEvents: { type: Array, default: () => [] },
  isArchived: { type: Boolean, default: false },
  isAgentConnected: { type: Boolean, default: false }
});

const emit = defineEmits(['archive']);

const router = useRouter();
const { notifyError, notifySuccess, notifyInfo } = useToasts();
const fileInput = ref(null);
const responses = ref({});
const newTask = ref({ title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '0 * * * *' });
const newTaskAttachments = ref([]);
const sending = ref(false);
const toastMessage = ref('');

const isFormOpen = ref(false);
const isEditMode = ref(false);
const editingTaskId = ref(null);

const showDeleteModal = ref(false);
const taskToDeleteId = ref(null);
const taskToDeleteTitle = ref('');

const localTasks = ref([...props.initialTasks]);
const showScheduledOnly = ref(false);
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
  const presets = {
    '*/15 * * * *': 'Every 15m',
    '*/30 * * * *': 'Every 30m',
    '0 * * * *': 'Hourly',
    '0 0 * * *': 'Daily',
    '0 0 * * 0': 'Weekly',
    '0 0 1 * *': 'Monthly'
  };
  return presets[cron] || cron;
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
    const cron = localTasks.value.filter(t => t.status === 'cron').sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
    if (cron.length === 0) return [];
    return [{ title: 'Scheduled Tasks', tasks: cron }];
  }

  const ongoing = localTasks.value.filter(t => ['ongoing', 'blocked', 'requires_action'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const notStarted = localTasks.value.filter(t => ['notstarted', 'pending'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const completed = localTasks.value.filter(t => ['completed', 'done', 'rejected'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));

  const groups = [];
  if (ongoing.length > 0) groups.push({ title: 'Ongoing', tasks: ongoing });
  
  if (completed.length > 0) {
    groups.push({ 
      title: 'Completed', 
      tasks: completed.slice(0, completedLimit.value),
      hasMore: completed.length > completedLimit.value,
      totalCompleted: completed.length
    });
  }
  
  if (notStarted.length > 0) groups.push({ title: 'Not Started', tasks: notStarted });

  return groups;
});

const pendingInputCount = computed(() => {
  return localTasks.value.filter(t => t.created_by === 'agent' && (t.status === 'notstarted' || t.status === 'pending')).length;
});

const scheduledCount = computed(() => {
  return localTasks.value.filter(t => t.status === 'cron').length;
});

const activeTaskCount = computed(() => {
  return localTasks.value.length - scheduledCount.value;
});

function getTaskBgStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-50 border-black';
  if (status === 'blocked' || status === 'requires_action') return 'bg-red-50 border-black';
  if (status === 'completed' || status === 'done') return 'bg-green-50 border-black';
  if (status === 'cron') return 'bg-indigo-50 border-indigo-200';
  return 'bg-gray-50 border-gray-300 border-dashed shadow-none text-gray-500 hover:border-gray-400 hover:bg-gray-100';
}
function getTaskDotStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-400';
  if (status === 'blocked' || status === 'requires_action') return 'bg-red-500';
  if (status === 'completed' || status === 'done') return 'bg-green-500';
  if (status === 'cron') return 'bg-indigo-500 border-indigo-600';
  return 'bg-gray-300 border-gray-400';
}
function getTaskBadgeStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-200 border-black text-black border-2';
  if (status === 'blocked' || status === 'requires_action') return 'bg-red-200 border-black text-black border-2';
  if (status === 'completed' || status === 'done') return 'bg-green-200 border-black text-black border-2';
  if (status === 'cron') return 'bg-indigo-200 border-indigo-400 text-indigo-800 border-2';
  return 'bg-gray-100 border-gray-300 text-gray-500 font-bold border-2';
}
function getTaskLabel(status) {
  if (status === 'notstarted' || status === 'pending') return 'NOT STARTED';
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
    const status = newTask.value.isRecurring ? 'cron' : 'notstarted';
    const cronSched = newTask.value.isRecurring ? newTask.value.cronSchedule : '';
    
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
    newTask.value = { title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '0 * * * *' };
    newTaskAttachments.value = [];
    isFormOpen.value = false;
    notifySuccess(status === 'cron' ? 'Chronic task scheduled successfully' : 'Task dispatched to pipeline');
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
  newTask.value = { title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '0 * * * *' };
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
    isRecurring: true, 
    cronSchedule: task.cron_schedule 
  };
  isFormOpen.value = true;
}
async function respond(taskId, action) {
  const text = responses.value[taskId] || '';
  try {
    const res = await respondToTask(props.workspaceId, taskId, action, text);
    const idx = localTasks.value.findIndex(x => x.id === taskId);
    if (idx !== -1) {
      localTasks.value[idx] = res.task;
    }
    delete responses.value[taskId];
    notifySuccess('Action confirmed');
  } catch(err) {
    notifyError("Failed to confirm action: " + err.message);
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
