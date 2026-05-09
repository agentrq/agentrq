<template>
  <div class="h-full bg-white dark:bg-zinc-900 flex flex-col w-full max-w-full overflow-x-hidden">
    <!-- Breadcrumb Header -->
    <header class="py-4 border-b border-gray-100 dark:border-zinc-800 shrink-0 flex items-center justify-between gap-4 bg-white dark:bg-zinc-900 sticky top-0 z-30 px-6">
      <div class="flex items-center gap-2 text-xs font-semibold min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 transition-colors shrink-0">
          {{ workspace?.name || 'Workspace' }}
        </router-link>
        <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-900 dark:text-zinc-50 truncate flex-1 min-w-0 text-sm">{{ isEditMode ? 'Edit Task' : 'New Task Definition' }}</span>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button @click="() => goBack()" class="p-2 text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm transition-all">
          <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto pt-6 md:pt-10 pb-12 px-4 md:px-8 scroll-smooth custom-scrollbar">
      <div class="w-full space-y-8">
        <form id="taskForm" @submit.prevent="isEditMode ? submitEditProtocol() : submitHumanTask()" class="space-y-8">
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-8 items-start">
            
            <!-- Requirement Definition (Box 1) -->
            <div class="border border-gray-200 dark:border-zinc-800 rounded-sm bg-white dark:bg-zinc-900 shadow-sm overflow-hidden flex flex-col h-full">
              <div class="bg-gray-50 dark:bg-zinc-800/80 px-6 py-4 flex items-center justify-between border-b border-gray-200 dark:border-zinc-800">
                <div class="flex items-center gap-3">
                  <div class="w-6 h-6 rounded-sm bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300 flex items-center justify-center text-[10px] font-semibold border border-gray-200 dark:border-zinc-700">1</div>
                  <span class="text-[11px] font-bold text-gray-900 dark:text-zinc-50">Requirement Definition</span>
                </div>
              </div>
              <div class="p-6 space-y-6 flex-1">
                <div class="flex flex-col gap-2">
                  <label class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400">Title</label>
                  <input v-model="newTask.title" 
                         placeholder="Task summary..." 
                         class="w-full bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-4 py-3 text-sm outline-none font-bold text-gray-900 dark:text-zinc-50 focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all placeholder:text-gray-500 dark:placeholder:text-zinc-600 shadow-sm" 
                         required />
                </div>
                
                <div class="flex flex-col gap-2">
                  <label class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400">Instructions</label>
                  <textarea v-model="newTask.body" 
                            placeholder="Provide detailed context..." 
                            class="w-full bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-4 py-3 text-sm outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none focus:border-gray-900 dark:focus:border-white focus:ring-0 min-h-[160px] placeholder:text-gray-500 dark:placeholder:text-zinc-600 shadow-sm custom-scrollbar" 
                            required></textarea>
                </div>

                <!-- Inline Assets -->
                <div v-if="!isEditMode" class="pt-2 border-t border-gray-100 dark:border-zinc-800/50">
                  <div class="flex items-center justify-between mb-3">
                    <span class="text-[10px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Attachments</span>
                  </div>
                  
                  <div 
                    @dragover.prevent="isDragging = true"
                    @dragleave.prevent="isDragging = false"
                    @drop.prevent="handleDrop"
                    :class="[
                      'relative border-2 border-dashed rounded-sm transition-all duration-200 flex flex-col items-center justify-center p-4',
                      isDragging ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800' : 'border-gray-200 dark:border-zinc-800 bg-transparent hover:bg-gray-50/50 dark:hover:bg-zinc-900/50'
                    ]"
                  >
                    <input type="file" ref="fileInput" multiple class="absolute inset-0 opacity-0 cursor-pointer" @change="handleFileUpload" />
                    
                    <div v-if="newTaskAttachments.length === 0" class="flex flex-col items-center gap-1 text-center">
                      <svg class="w-5 h-5 text-gray-500 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" /></svg>
                      <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-400 uppercase tracking-tight">Drop files here or click to browse</span>
                    </div>

                    <div v-else class="w-full flex flex-wrap gap-2">
                       <div v-for="(att, i) in newTaskAttachments" :key="i" class="flex items-center gap-2 text-[9px] bg-white dark:bg-zinc-800 border border-gray-100 dark:border-zinc-700 rounded-sm pl-3 pr-1 py-1 font-bold shadow-sm group pointer-events-auto relative z-10" @click.stop>
                         <span class="truncate max-w-[140px] text-gray-600 dark:text-zinc-400">{{ att.filename }}</span>
                         <button @click.prevent="newTaskAttachments.splice(i, 1)" class="p-1 text-gray-500 hover:text-red-500 transition-colors">
                           <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M6 18L18 6M6 6l12 12"></path></svg>
                         </button>
                       </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- Execution Strategy (Box 2) -->
            <div class="border border-gray-200 dark:border-zinc-800 rounded-sm bg-white dark:bg-zinc-900 shadow-sm overflow-hidden flex flex-col h-full">
              <div class="bg-gray-50 dark:bg-zinc-800/80 px-6 py-4 flex items-center justify-between border-b border-gray-200 dark:border-zinc-800">
                <div class="flex items-center gap-3">
                  <div class="w-6 h-6 rounded-sm bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300 flex items-center justify-center text-[10px] font-semibold border border-gray-200 dark:border-zinc-700">2</div>
                  <span class="text-[11px] font-bold text-gray-900 dark:text-zinc-50">Execution Strategy</span>
                </div>
              </div>
              
              <div class="p-6 space-y-8 flex-1">
                <!-- Assignee & YOLO Permissions -->
                <div class="flex flex-wrap items-start gap-8">
                  <!-- Responsibility Column -->
                  <div class="flex flex-col gap-3">
                    <label class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-wider">Responsibility</label>
                    <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-sm w-fit">
                      <button type="button" 
                              @click="newTask.assignee = 'agent'"
                              :class="newTask.assignee === 'agent' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-6 py-2 rounded-sm text-[10px] font-semibold transition-all">
                        Agent
                      </button>
                      <button type="button" 
                              @click="newTask.assignee = 'human'"
                              :class="newTask.assignee === 'human' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-6 py-2 rounded-sm text-[10px] font-semibold transition-all">
                        Human
                      </button>
                    </div>
                  </div>

                  <!-- YOLO Column -->
                  <div v-if="newTask.assignee === 'agent'" class="flex flex-col gap-3 animate-in fade-in slide-in-from-left-2 duration-200">
                    <label class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-wider">YOLO (Auto-Allow)</label>
                    <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-sm w-fit">
                      <button type="button" 
                              @click="newTask.allowAllCommands = true"
                              :class="newTask.allowAllCommands ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-6 py-2 rounded-sm text-[10px] font-semibold transition-all uppercase">
                        ON
                      </button>
                      <button type="button" 
                              @click="newTask.allowAllCommands = false"
                              :class="!newTask.allowAllCommands ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-6 py-2 rounded-sm text-[10px] font-semibold transition-all uppercase">
                        OFF
                      </button>
                    </div>
                  </div>
                </div>
                <!-- Schedule Section -->
                <div class="flex flex-col gap-6 pt-6 border-t border-gray-100 dark:border-zinc-800/50">
                  <div class="flex flex-col gap-4">
                    <label class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-wider">Execution Type</label>
                    <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-sm w-fit">
                      <button type="button" @click="scheduleType = 'none'"
                              :class="scheduleType === 'none' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-4 py-2 rounded-sm text-[10px] font-semibold transition-all">None</button>
                      <button type="button" @click="scheduleType = 'onetime'"
                              :class="scheduleType === 'onetime' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-4 py-2 rounded-sm text-[10px] font-semibold transition-all">One-time</button>
                      <button type="button" @click="scheduleType = 'repeated'"
                              :class="scheduleType === 'repeated' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                              class="px-4 py-2 rounded-sm text-[10px] font-semibold transition-all">Repeated</button>
                    </div>
                  </div>

                  <!-- Schedule Options -->
                  <div class="min-h-[100px]">
                    <div v-if="scheduleType === 'onetime'" class="animate-in fade-in slide-in-from-top-2 duration-200">
                      <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-2 block">Launch Date/Time</label>
                      <input type="datetime-local" v-model="oneTimeDate"
                             class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2.5 text-xs font-semibold text-gray-900 dark:text-zinc-50 outline-none focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all shadow-sm w-full max-w-xs" />
                    </div>

                    <div v-if="scheduleType === 'repeated'" class="space-y-6 animate-in fade-in slide-in-from-top-2 duration-200">
                      <div class="flex flex-wrap items-end gap-4">
                        <div class="flex flex-col gap-2 min-w-[160px] flex-1 max-w-[200px]">
                          <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Frequency</label>
                          <select v-model="repeatPreset" 
                                  class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-[10px] font-semibold text-gray-900 dark:text-zinc-50 outline-none focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all shadow-sm w-full">
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

                        <div v-if="!['15min', '30min', 'hourly', '2hour'].includes(repeatPreset)" class="flex flex-col gap-2">
                          <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Time</label>
                          <input type="time" v-model="repeatTime"
                                 class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-[10px] font-semibold text-gray-900 dark:text-zinc-50 outline-none focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all shadow-sm w-full max-w-[100px]" />
                        </div>
                      </div>

                      <div v-if="repeatPreset === 'custom'" class="flex flex-col gap-2 pt-2">
                         <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Active Days</label>
                         <div class="flex flex-wrap gap-1.5">
                           <button v-for="d in daysOptions" :key="d.value" type="button" @click="toggleDay(d.value)"
                                   :class="selectedDays.includes(d.value) ? 'bg-gray-900 dark:bg-white text-white dark:text-zinc-900 border-black dark:border-white' : 'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-500'"
                                   class="w-7 h-7 rounded-sm border text-[9px] font-bold flex items-center justify-center transition-all">
                             {{ d.label }}
                           </button>
                         </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Footer Stats (Inside Box 2) -->
              <div v-if="scheduleType !== 'none'" class="bg-gray-50 dark:bg-zinc-800/50 p-5 border-t border-gray-200 dark:border-zinc-800 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                  <div class="flex flex-col gap-1.5">
                     <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Resolved Cron</span>
                     <code class="text-[10px] font-mono text-gray-900 dark:text-zinc-100 select-all bg-white dark:bg-zinc-950 px-2 py-1 rounded-sm border border-gray-200 dark:border-zinc-700">{{ newTask.cronSchedule || '----' }}</code>
                  </div>
                  <div v-if="nextRunPreview" class="flex flex-col sm:items-end gap-1.5">
                     <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Next Run</span>
                     <span class="text-[10px] font-semibold text-sky-600 dark:text-sky-400 truncate max-w-[120px]">{{ nextRunPreview }}</span>
                  </div>
              </div>
            </div>
          </div>

          <!-- Final Action -->
          <div class="pt-6 flex flex-col sm:flex-row-reverse gap-4">
             <button type="submit"
                     :disabled="sending || !newTask.title || !newTask.body"
                     class="flex-1 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 rounded-sm px-8 py-4 text-xs font-semibold hover:bg-zinc-800 dark:hover:bg-zinc-100 transition-all shadow-sm active:scale-[0.98] flex items-center justify-center gap-3 disabled:opacity-50 border border-transparent">
                <svg v-if="sending" class="w-5 h-5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                <svg v-else class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7" /></svg>
                {{ sending ? (isEditMode ? 'Updating...' : 'Creating...') : (isEditMode ? 'Create Task' : 'Create Task') }}
             </button>
             <button type="button" @click="() => goBack()" class="px-8 py-4 rounded-sm border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 text-xs font-semibold hover:bg-gray-50 dark:hover:bg-zinc-800 transition-all shadow-sm">Cancel</button>
          </div>
        </form>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import cronParser from 'cron-parser';
import { getWorkspace, createTask, updateScheduledTask, getTask } from '../api';
import { useToasts } from '../composables/useToasts';
import { useCron } from '../composables/useCron';

const { getNextRunLabel, daysOptions } = useCron();

const route = useRoute();
const router = useRouter();
const { notifyError, notifySuccess } = useToasts();

const workspaceId = route.params.id;
const taskId = route.params.taskId;
const isEditMode = computed(() => !!taskId);

const workspace = ref(null);
const sending = ref(false);
const fileInput = ref(null);

const newTask = ref({ title: '', body: '', assignee: 'agent', cronSchedule: '', allowAllCommands: false });
const newTaskAttachments = ref([]);

// Scheduling state
const scheduleType = ref('none');
const oneTimeDate = ref('');
const repeatPreset = ref('daily');
const repeatTime = ref('09:00');
const selectedDays = ref([1, 2, 3, 4, 5]); // Mon-Fri
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
    const res = await getWorkspace(workspaceId);
    workspace.value = res.workspace;

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
      
      if (t.cronSchedule) {
        parseCronToUI(t.cronSchedule);
      }
    } else {
      // Default to workspace setting for new tasks
      newTask.value.allowAllCommands = res.workspace.allowAllCommands || false;
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
           // Find the target day by taking a known Sunday in the current month/year context
           // so that we handle the current DST transition correctly.
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
      // Adjust date to the selected local day
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
      status, newTask.value.cronSchedule, newTask.value.allowAllCommands
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
