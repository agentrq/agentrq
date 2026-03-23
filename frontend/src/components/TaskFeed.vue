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

    <!-- Create Task Modal -->
    <div v-if="isCreateModalOpen" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
      <div class="absolute inset-0 bg-black/40 backdrop-blur-sm" @click="isCreateModalOpen = false"></div>
      <div class="relative bg-white w-full max-w-xl rounded-2xl shadow-2xl border border-gray-100 overflow-hidden animate-in zoom-in-95 duration-200">
         <div class="p-6 border-b border-gray-100 flex justify-between items-center bg-gray-50/50">
            <h2 class="text-lg font-bold text-gray-900">Define New Task</h2>
            <button @click="isCreateModalOpen = false" class="text-gray-400 hover:text-black transition-colors">
              <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
         </div>
         <form @submit.prevent="submitHumanTask" class="p-6 flex flex-col gap-4">
            <div class="flex flex-col gap-1.5">
               <label class="text-[10px] font-bold text-gray-400 uppercase tracking-widest ml-1">Title</label>
               <input v-model="newTask.title" placeholder="Requirement summary..." class="w-full bg-gray-50 border border-gray-200 rounded-xl px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none font-semibold text-gray-900" required />
            </div>
            <div class="flex flex-col gap-1.5">
               <label class="text-[10px] font-bold text-gray-400 uppercase tracking-widest ml-1">Description / Instructions</label>
               <textarea v-model="newTask.body" placeholder="Provide detailed context for the agent..." class="w-full bg-gray-50 border border-gray-200 rounded-xl px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none resize-none min-h-[120px] max-h-64 text-gray-800 leading-relaxed" required></textarea>
            </div>

            <div class="flex flex-col gap-1.5">
               <label class="text-[10px] font-bold text-gray-400 uppercase tracking-widest ml-1">Assignee</label>
               <div class="flex p-1 bg-gray-100/50 rounded-xl border border-gray-100 w-fit">
                  <button type="button" 
                          @click="newTask.assignee = 'agent'"
                          :class="newTask.assignee === 'agent' ? 'bg-white text-black shadow-sm' : 'text-gray-400 hover:text-gray-600'"
                          class="px-4 py-2 rounded-lg text-[10px] font-black uppercase tracking-widest transition-all">
                    Agent
                  </button>
                  <button type="button" 
                          @click="newTask.assignee = 'human'"
                          :class="newTask.assignee === 'human' ? 'bg-white text-black shadow-sm' : 'text-gray-400 hover:text-gray-600'"
                          class="px-4 py-2 rounded-lg text-[10px] font-black uppercase tracking-widest transition-all">
                    Human
                  </button>
               </div>
            </div>

            <div class="flex flex-col gap-1.5 mt-2">
                <div class="flex items-center gap-2 mb-1">
                   <label class="text-[10px] font-bold text-gray-400 uppercase tracking-widest ml-1">Chronic Task (Recurring)</label>
                   <button type="button" @click="newTask.isRecurring = !newTask.isRecurring" 
                           class="w-10 h-5 flex items-center transition-all duration-300 rounded-full p-1 border border-gray-100"
                           :class="newTask.isRecurring ? 'bg-indigo-600 border-indigo-500' : 'bg-gray-100'">
                      <div class="w-3 h-3 bg-white rounded-full shadow-sm transform transition-all duration-300"
                           :class="newTask.isRecurring ? 'translate-x-5' : 'translate-x-0'"></div>
                   </button>
                </div>
                
                <Transition name="fade-slide">
                  <div v-if="newTask.isRecurring" class="p-4 bg-indigo-50/50 border border-indigo-100 rounded-2xl flex flex-col gap-3">
                    <p class="text-[10px] text-indigo-600 font-bold leading-tight uppercase tracking-tight">Set a recurring schedule. New instances appear in To Do automatically.</p>
                    <div class="flex gap-2">
                       <select v-model="newTask.cronSchedule" class="flex-1 bg-white border border-indigo-200 rounded-lg px-3 py-2 text-xs font-bold text-indigo-900 focus:ring-1 focus:ring-indigo-500 outline-none shadow-sm h-9">
                        <option value="*/15 * * * *">Every 15 Minutes</option>
                        <option value="*/30 * * * *">Every 30 Minutes</option>
                        <option value="0 * * * *">Hourly</option>
                        <option value="0 0 * * *">Daily at Midnight</option>
                        <option value="0 0 * * 0">Weekly (Sunday)</option>
                        <option value="0 0 1 * *">Monthly (1st)</option>
                      </select>
                      <input v-model="newTask.cronSchedule" placeholder="Custom Cron: * * * * *" class="flex-1 bg-white border border-indigo-200 rounded-lg px-3 py-2 text-[11px] font-mono font-bold text-indigo-600 focus:ring-1 focus:ring-indigo-500 outline-none shadow-sm h-9" />
                    </div>
                  </div>
                </Transition>
             </div>

            <!-- Attachments in Modal -->
            <div class="flex flex-col gap-2">
               <div class="flex items-center justify-between mb-2">
                  <h3 class="text-[11px] font-bold text-gray-400 uppercase tracking-[0.2em]">Attachments ({{ newTaskAttachments.length }})</h3>
                  <div class="flex items-center gap-1.5 h-6">
                     <button type="button" @click="$refs.fileInput.click()" class="text-[10px] font-bold text-indigo-600 hover:text-indigo-800 uppercase tracking-widest">Add Files</button>
                  </div>
               </div>
               <div v-if="newTaskAttachments.length > 0" class="flex flex-wrap gap-2 p-2 bg-gray-50 rounded-xl border border-gray-200 min-h-[50px]">
                  <div v-for="(att, i) in newTaskAttachments" :key="i" class="flex items-center text-[10px] bg-white border border-gray-200 px-3 py-1.5 rounded-full shadow-sm font-bold animate-in fade-in slide-in-from-bottom-2">
                    <span class="truncate max-w-[140px]">{{ att.filename }}</span>
                    <button @click.prevent="newTaskAttachments.splice(i, 1)" class="ml-2 text-gray-400 hover:text-red-500">
                      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>
                    </button>
                  </div>
               </div>
               <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />
            </div>

            <div class="mt-4 flex gap-3">
               <button type="button" @click="isCreateModalOpen = false" class="flex-1 py-3 px-4 border border-gray-200 text-gray-600 hover:bg-gray-50 rounded-xl text-sm font-bold transition-all">Cancel</button>
               <button type="submit" :disabled="sending || !newTask.title || !newTask.body" class="flex-[2] py-3 px-4 bg-black text-white hover:bg-gray-800 disabled:opacity-50 rounded-xl text-sm font-bold shadow-lg shadow-black/10 transition-all flex items-center justify-center gap-2">
                  <svg v-if="sending" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                  {{ sending ? 'Dispatching...' : 'Create Task' }}
               </button>
            </div>
         </form>
      </div>
    </div>

    <div v-if="!isArchived" class="pt-2 pb-0 flex items-center justify-end gap-2.5 shrink-0">
       <button @click="$emit('archive')" 
               class="flex items-center gap-2 px-5 py-2.5 bg-white border border-gray-200 rounded-lg text-[11px] font-black text-gray-400 hover:text-red-500 hover:border-red-100 hover:bg-red-50 transition-all uppercase tracking-widest">
          <svg class="w-3.5 h-3.5 font-bold" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
          Archive
       </button>
       <button @click="isCreateModalOpen = true" class="group flex items-center gap-2.5 bg-black hover:bg-zinc-800 text-white px-5 py-2.5 rounded-lg text-[11px] font-black uppercase tracking-widest transition-all">
          <svg class="w-4 h-4 transform group-hover:rotate-90 transition-transform duration-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
          New Task
       </button>
    </div>

    <!-- Kanban Board Area -->
    <div class="flex-1 flex flex-col md:grid md:grid-cols-4 pt-4 gap-6 items-stretch overflow-y-auto md:overflow-hidden pb-6 custom-scrollbar">
      
      <!-- Columns -->
      <div v-for="col in columns" :key="col.id" class="flex flex-col shrink-0 md:h-full min-h-0">
        
        <!-- Column Header -->
        <div class="flex items-center justify-between mb-4 px-1.5 shrink-0">
          <div class="flex items-center gap-2.5">
             <div class="w-1.5 h-1.5 rounded-full" :class="col.id === 'notstarted' ? 'bg-gray-400' : (col.id === 'ongoing' ? 'bg-indigo-500' : 'bg-green-500')"></div>
             <h3 class="text-[11px] font-extrabold text-gray-900 uppercase tracking-widest">{{ col.name }}</h3>
          </div>
          <span class="text-[10px] font-black text-gray-400 bg-white border border-gray-100 w-6 h-6 flex items-center justify-center rounded-lg shadow-sm">{{ col.tasks.length }}</span>
        </div>

        <!-- Drop Zone -->
        <div class="grow overflow-y-visible md:overflow-y-auto bg-gray-50/50 border border-gray-100 rounded-2xl p-3 min-h-[150px] transition-all hover:bg-gray-100/30 w-full"
             @dragover.prevent
             @dragenter.prevent
             @drop="onDrop($event, col.id)">
             
          <div v-if="col.tasks.length === 0" class="h-full flex flex-col items-center justify-center text-gray-300 opacity-60 py-8">
            <svg class="w-6 h-6 mb-2" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M19 14l-7 7m0 0l-7-7m7 7V3" /></svg>
            <span class="text-[9px] font-bold uppercase tracking-tighter">Empty</span>
          </div>
             
          <!-- Task Cards -->
          <div v-for="t in col.tasks" :key="t.id"
               :draggable="!isArchived && t.status !== 'cron'" 
               @dragstart="onDragStart($event, t.id)"
               @dragover.prevent
               @dragenter.prevent
               @drop.stop="onDropOnTask($event, t.id, col.id)"
               @click="openTask(t)"
               class="bg-white p-4 mb-3 shrink-0 rounded-xl border border-gray-100 cursor-pointer active:cursor-grabbing hover:border-indigo-200 transition-all select-none group relative overflow-hidden">
            
            <div class="flex justify-between items-baseline mb-2.5">
              <div class="flex flex-col gap-1">
                <div class="flex items-center gap-1.5">
                  <div class="w-5 h-5 rounded-full flex items-center justify-center text-[8px] font-black text-white shadow-sm"
                        :class="t.created_by === 'human' ? 'bg-black' : 'bg-indigo-600'">
                      {{ t.created_by === 'human' ? 'U' : 'A' }}
                  </div>
                  <span class="text-[8px] font-black uppercase tracking-widest" :class="t.created_by === 'human' ? 'text-black' : 'text-indigo-600'">
                    By {{ t.created_by === 'human' ? 'YOU' : 'AGENT' }}
                  </span>
                </div>
                <div class="flex items-center gap-1.5 pl-6">
                  <span class="text-[7px] font-bold text-gray-400 uppercase tracking-widest">Assignee:</span>
                  <span class="text-[8px] font-black uppercase tracking-widest" :class="t.assignee === 'human' ? 'text-black' : 'text-indigo-600'">
                    {{ t.assignee === 'human' ? 'YOU' : 'AGENT' }}
                  </span>
                </div>
              </div>
               <div class="flex items-center gap-1.5">
                <span v-if="t.status === 'cron'" class="px-1.5 py-0.5 bg-indigo-50 text-indigo-600 text-[8px] font-black rounded border border-indigo-100 uppercase tracking-tighter">
                  Recurring: {{ formatCron(t.cron_schedule) }}
                </span>
                <span class="text-[8px] text-gray-400 font-bold uppercase tracking-tight">
                  {{ formatTime(t.created_at) }}
                </span>
                <button v-if="!isArchived" @click.stop="triggerDelete(t)" class="opacity-0 group-hover:opacity-100 text-gray-300 hover:text-red-500 transition-all p-0.5 hover:bg-red-50 rounded-md">
                   <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                </button>
              </div>
            </div>
            
            <h4 class="text-base font-bold text-gray-900 leading-snug group-hover:text-indigo-600 transition-colors mb-1">{{ t.title }}</h4>
            <p v-if="t.body" class="text-[12px] text-gray-400/80 font-medium line-clamp-2 leading-relaxed">{{ t.body }}</p>
            
            <!-- Render Attachments Mini -->
            <div v-if="t.attachments && t.attachments.length > 0" class="flex flex-wrap gap-1.5 mt-2 pt-3 border-t border-gray-50">
              <div v-for="(att, i) in t.attachments" :key="i" class="w-7 h-7 rounded-lg shrink-0 overflow-hidden ring-1 ring-gray-100 shadow-sm">
                 <img v-if="att.mimeType && att.mimeType.startsWith('image/')" :src="getAttachmentUrl(props.workspaceId, att.id)" class="w-full h-full object-cover" />
                 <div v-else class="w-full h-full bg-gray-50 flex items-center justify-center text-gray-300">
                   <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                 </div>
              </div>
            </div>
            
            <!-- Agent Actions -->
            <div v-if="!isArchived && t.created_by === 'agent' && t.status === 'notstarted'" class="mt-4 pt-4 border-t border-gray-100 flex flex-col gap-2 relative z-10" @mousedown.stop>
               <input v-model="responses[t.id]" placeholder="Add context or notes..." class="w-full text-xs rounded-xl bg-gray-50 border-gray-100 p-2.5 text-gray-800 focus:ring-1 focus:ring-black focus:bg-white transition-all shadow-sm italic" />
               <div class="flex space-x-2 mt-1">
                 <button @click.prevent="respond(t.id, 'allow')" class="flex-1 py-2 bg-black text-white hover:bg-zinc-800 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-lg active:scale-95">
                   Approve
                 </button>
                 <button @click.prevent="respond(t.id, 'reject')" class="flex-1 py-2 bg-white border border-gray-100 text-red-600 hover:bg-red-50 hover:border-red-200 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm active:scale-95">
                   Reject
                 </button>
               </div>
            </div>

            <div v-if="t.Messages && t.Messages.length > 0" class="absolute bottom-0 right-0 p-2 bg-white/80 backdrop-blur-sm rounded-tl-xl border-l border-t border-gray-100 flex items-center gap-1 opacity-40 group-hover:opacity-100 transition-opacity">
               <svg class="w-3 h-3 text-indigo-600" fill="currentColor" viewBox="0 0 24 24"><path d="M12 2C6.477 2 2 6.477 2 12c0 1.891.526 3.658 1.439 5.166L2.1 21.897l4.735-1.332C8.342 21.474 10.109 22 12 22c5.523 0 10-4.477 10-10S17.523 2 12 2z"/></svg>
               <span class="text-[9px] font-black text-gray-900">{{ t.Messages.length }}</span>
            </div>
          </div>
        </div>
      </div>
    </div> <!-- / Board -->
  </div>
</template>


<script setup>
import { ref, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { createTask, respondToTask, updateTaskStatus, updateTaskOrder, deleteTask, getAttachmentUrl } from '../api';
import DeleteModal from './DeleteModal.vue';

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
  initialTasks: { type: Array, default: () => [] },
  liveEvents: { type: Array, default: () => [] },
  isArchived: { type: Boolean, default: false }
});

const emit = defineEmits(['archive']);

const router = useRouter();
const fileInput = ref(null);
const responses = ref({});
const newTask = ref({ title: '', body: '', assignee: 'agent', isRecurring: false, cronSchedule: '0 * * * *' });
const newTaskAttachments = ref([]);
const sending = ref(false);
const toastMessage = ref('');
const toastTimeout = ref(null);
const isCreateModalOpen = ref(false);

const showDeleteModal = ref(false);
const taskToDeleteId = ref(null);
const taskToDeleteTitle = ref('');

const localTasks = ref([...props.initialTasks]);

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

function showNotification(msg) {
  if (toastTimeout.value) clearTimeout(toastTimeout.value);
  toastMessage.value = msg;
  toastTimeout.value = setTimeout(() => {
    toastMessage.value = '';
  }, 5000);
}

function getTaskOrder(t) {
  if (t.sort_order) return t.sort_order;
  if (!t.created_at) return Date.now();
  return new Date(t.created_at).getTime() / 1000.0;
}

const allTasks = computed(() => {
  return [...localTasks.value].sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
});

const columns = computed(() => {
  const all = allTasks.value;
  return [
    { 
      id: 'cron', 
      name: 'Chronic Tasks', 
      tasks: all.filter(x => x.status === 'cron') 
    },
    { 
      id: 'notstarted', 
      name: 'To Do', 
      tasks: all.filter(x => !x.status || x.status === 'notstarted' || x.status === 'pending') 
    },
    { 
      id: 'ongoing', 
      name: 'In Progress', 
      tasks: all.filter(x => x.status === 'ongoing') 
    },
    { 
      id: 'completed', 
      name: 'Done', 
      tasks: all.filter(x => x.status === 'completed' || x.status === 'done' || x.status === 'rejected') 
    }
  ];
});

function onDragStart(e, taskId) {
  e.dataTransfer.setData('taskId', taskId);
  e.dataTransfer.effectAllowed = 'move';
}

async function onDrop(e, statusId) {
  const taskId = e.dataTransfer.getData('taskId');
  if(!taskId) return;
  const task = localTasks.value.find(t => t.id === taskId);
  if(!task || task.status === 'cron') return;

  const oldStatus = task.status;
  const statusChanged = task.status !== statusId;
  task.status = statusId; 

  const colTasks = columns.value.find(c => c.id === statusId).tasks.filter(t => t.id !== taskId);
  const oldOrder = task.sort_order;
  let newOrder = getTaskOrder(task);
  if(colTasks.length > 0) {
    newOrder = getTaskOrder(colTasks[colTasks.length - 1]) + 1024;
  }
  task.sort_order = newOrder;

  try {
     if (statusChanged) await updateTaskStatus(props.workspaceId, taskId, statusId);
     if (oldOrder !== newOrder) await updateTaskOrder(props.workspaceId, taskId, newOrder);
  } catch (err) {
    task.status = oldStatus;
    task.sort_order = oldOrder;
    alert('Failed to transition stage: ' + err.message);
  }
}

async function onDropOnTask(e, targetTaskId, statusId) {
  const taskId = e.dataTransfer.getData('taskId');
  if(!taskId || taskId === targetTaskId) return;
  
  const task = localTasks.value.find(t => t.id === taskId);
  if(!task || task.status === 'cron') return;

  let statusChanged = false;
  const oldStatus = task.status;
  if(task.status !== statusId) {
    task.status = statusId;
    statusChanged = true;
  }

  const colTasks = columns.value.find(c => c.id === statusId).tasks;
  let newOrder = getTaskOrder(task);
  
  const targetTask = colTasks.find(t => t.id === targetTaskId);
  if (targetTask) {
    const rect = e.currentTarget.getBoundingClientRect();
    const offsetY = e.clientY - rect.top;
    const insertAfter = offsetY > rect.height / 2;
    
    const sorted = colTasks.filter(t => t.id !== taskId);
    const targetIdx = sorted.findIndex(t => t.id === targetTaskId);
    
    if (insertAfter) {
      if (targetIdx === sorted.length - 1) {
        newOrder = getTaskOrder(sorted[targetIdx]) + 1024;
      } else {
        newOrder = (getTaskOrder(sorted[targetIdx]) + getTaskOrder(sorted[targetIdx+1])) / 2;
      }
    } else {
      if (targetIdx === 0) {
        newOrder = getTaskOrder(sorted[targetIdx]) - 1024;
      } else {
        newOrder = (getTaskOrder(sorted[targetIdx-1]) + getTaskOrder(sorted[targetIdx])) / 2;
      }
    }
  }

  const oldOrder = task.sort_order;
  task.sort_order = newOrder;

  try {
     if (statusChanged && oldStatus !== statusId) await updateTaskStatus(props.workspaceId, taskId, statusId);
     if (oldOrder !== newOrder) await updateTaskOrder(props.workspaceId, taskId, newOrder);
  } catch (err) {
    task.status = oldStatus;
    task.sort_order = oldOrder;
    alert('Failed to transition stage/order: ' + err.message);
  }
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
    isCreateModalOpen.value = false;
    notifySuccess(status === 'cron' ? 'Chronic task scheduled successfully' : 'Task dispatched to pipeline');
  } catch(err) {
    notifyError("Dispatch Error: " + err.message);
  } finally {
    sending.value = false;
  }
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
  } catch(err) {
    notifyError("Failed to confirm action: " + err.message);
  }
}

function openTask(task) {
  router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}`);
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
