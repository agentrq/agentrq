<template>
  <div class="min-h-screen bg-white flex flex-col w-full max-w-full overflow-x-hidden">
    <!-- Breadcrumb Header -->
    <header class="py-3 border-b-2 border-black shrink-0 flex items-center justify-between gap-4 bg-white sticky top-0 z-30">
      <div class="flex items-center gap-2 text-xs font-black uppercase tracking-widest min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="text-gray-400 hover:text-black transition-colors shrink-0">
          {{ workspace?.name || 'Workspace' }}
        </router-link>
        <svg class="w-3 h-3 text-gray-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" /></svg>
        <span class="text-black truncate flex-1 min-w-0 text-sm">{{ isEditMode ? 'Edit Protocol' : 'New Task Definition' }}</span>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button @click="() => goBack()" class="p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto pt-4 md:pt-6 pb-12 px-0 md:px-4 scroll-smooth custom-scrollbar">
      <div class="w-full max-w-4xl mx-auto space-y-4 md:space-y-8">
        
        <form id="taskForm" @submit.prevent="isEditMode ? submitEditProtocol() : submitHumanTask()" class="space-y-8">
            <!-- Basic Details Section -->
            <div class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
              <div class="bg-black px-4 py-2 flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <div class="w-5 h-5 bg-[#00FF88] text-black flex items-center justify-center text-[9px] font-black">1</div>
                  <span class="text-[10px] font-black text-white uppercase tracking-widest">Requirement Definition</span>
                </div>
              </div>
              <div class="p-6 space-y-6">
                <div class="flex flex-col gap-2">
                   <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Title</label>
                   <input v-model="newTask.title" 
                          placeholder="Requirement summary..." 
                          class="w-full bg-white border-2 border-black px-4 py-3 text-sm outline-none font-bold text-gray-900 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all placeholder:text-gray-300" 
                          required />
                </div>
                
                <div class="flex flex-col gap-2">
                   <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Instructions</label>
                   <textarea v-model="newTask.body" 
                             placeholder="Provide detailed context for the agent..." 
                             class="w-full bg-white border-2 border-black px-4 py-3 text-sm outline-none font-medium text-gray-800 transition-all resize-none focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] min-h-[160px] placeholder:text-gray-300" 
                             required></textarea>
                </div>
              </div>
            </div>

            <!-- Execution Strategy Section -->
            <div class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
              <div class="bg-black px-4 py-2 flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <div class="w-5 h-5 bg-[#00FF88] text-black flex items-center justify-center text-[9px] font-black">2</div>
                  <span class="text-[10px] font-black text-white uppercase tracking-widest">Execution Strategy</span>
                </div>
              </div>
              <div class="p-6 grid grid-cols-1 md:grid-cols-2 gap-10">
                 <!-- Assignee -->
                 <div class="flex flex-col gap-3">
                    <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Responsibility</label>
                    <div class="flex p-1 bg-gray-100 border-2 border-black w-fit">
                       <button type="button" 
                               @click="newTask.assignee = 'agent'"
                               :class="newTask.assignee === 'agent' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                               class="px-6 py-2 text-[10px] font-black uppercase tracking-widest transition-all">
                         Agent
                       </button>
                       <button type="button" 
                               @click="newTask.assignee = 'human'"
                               :class="newTask.assignee === 'human' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                               class="px-6 py-2 text-[10px] font-black uppercase tracking-widest transition-all">
                         Human
                       </button>
                    </div>

                    <!-- Allow all commands toggle -->
                    <div v-if="newTask.assignee === 'agent'" class="mt-4 flex items-center gap-3">
                        <label class="relative inline-flex items-center cursor-pointer">
                          <input type="checkbox" v-model="newTask.allowAllCommands" class="sr-only peer">
                          <div class="w-9 h-5 bg-gray-200 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-[#00FF88] border-2 border-black border-transparent"></div>
                        </label>
                        <span class="text-[10px] font-black text-gray-700 uppercase tracking-widest">Allow All Commands</span>
                    </div>
                    <p v-if="newTask.assignee === 'agent' && newTask.allowAllCommands" class="text-[9px] font-bold text-gray-500 mt-1 max-w-xs">Warning: the agent will execute commands without asking for permission.</p>
                 </div>

                 <!-- Schedule Type -->
                 <div class="flex flex-col gap-3">
                    <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Schedule</label>
                    <div class="flex p-1 bg-gray-100 border-2 border-black w-fit">
                      <button type="button" @click="scheduleType = 'none'"
                              :class="scheduleType === 'none' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                              class="px-3 py-2 text-[9px] font-black uppercase tracking-widest transition-all">None</button>
                      <button type="button" @click="scheduleType = 'onetime'"
                              :class="scheduleType === 'onetime' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                              class="px-3 py-2 text-[9px] font-black uppercase tracking-widest transition-all">One-time</button>
                      <button type="button" @click="scheduleType = 'repeated'"
                              :class="scheduleType === 'repeated' ? 'bg-black text-[#00FF88]' : 'text-gray-500 hover:text-black'"
                              class="px-3 py-2 text-[9px] font-black uppercase tracking-widest transition-all">Repeated</button>
                    </div>

                    <!-- One-time specific -->
                    <div v-if="scheduleType === 'onetime'" class="mt-2 flex flex-col gap-2">
                      <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Launch Date/Time</label>
                      <input type="datetime-local" v-model="oneTimeDate"
                             class="bg-white border-2 border-black px-3 py-2 text-xs font-black uppercase tracking-widest text-black outline-none focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" />
                    </div>

                    <!-- Repeated specific -->
                    <div v-if="scheduleType === 'repeated'" class="mt-2 flex flex-col gap-4">
                      <div class="flex flex-col gap-2">
                        <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Frequency</label>
                        <select v-model="repeatPreset" 
                                class="bg-white border-2 border-black px-2 py-2 text-[10px] font-black uppercase tracking-widest text-black outline-none">
                          <option value="15min">Every 15 mins</option>
                          <option value="30min">Every 30 mins</option>
                          <option value="hourly">Hourly</option>
                          <option value="2hour">Bi-hourly</option>
                          <option value="12hour">Twice a day</option>
                          <option value="daily">Daily</option>
                          <option value="weekly">Weekly</option>
                          <option value="monthly">Monthly</option>
                          <option value="custom">Custom Configuration...</option>
                        </select>
                      </div>

                      <!-- Custom repeat days -->
                      <div v-if="repeatPreset === 'custom'" class="space-y-2">
                         <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Active Days</label>
                         <div class="flex flex-wrap gap-1.5">
                           <button v-for="d in daysOptions" :key="d.value" type="button" @click="toggleDay(d.value)"
                                   :class="selectedDays.includes(d.value) ? 'bg-[#00FF88] border-black' : 'bg-white border-gray-200 text-gray-400'"
                                   class="w-7 h-7 border-2 text-[10px] font-black flex items-center justify-center transition-all hover:border-black">
                             {{ d.label }}
                           </button>
                         </div>
                      </div>

                      <div v-if="!['15min', '30min', 'hourly', '2hour'].includes(repeatPreset)" class="flex flex-col gap-2">
                        <label class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Launch Time</label>
                        <input type="time" v-model="repeatTime"
                               class="bg-white border-2 border-black px-3 py-2 text-xs font-black uppercase tracking-widest text-black outline-none focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" />
                      </div>
                    </div>
                 </div>
              </div>
              <div v-if="scheduleType !== 'none'" class="bg-gray-50 p-4 border-t-2 border-black flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                  <div class="flex flex-col gap-1">
                     <span class="text-[8px] font-black text-gray-400 uppercase tracking-widest">Schedule</span>
                     <code class="text-xs font-mono text-black select-all tracking-wider">{{ newTask.cronSchedule || '----' }}</code>
                  </div>
                  <div v-if="nextRunPreview" class="flex flex-col sm:items-end gap-1">
                     <span class="text-[8px] font-black text-gray-400 uppercase tracking-widest">Next Execution</span>
                     <span class="text-xs font-black text-sky-600 uppercase tracking-widest">{{ nextRunPreview }}</span>
                  </div>
              </div>
            </div>

            <!-- Assets Section -->
            <div v-if="!isEditMode" class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
               <div class="bg-black px-4 py-2 flex items-center justify-between">
                 <div class="flex items-center gap-2">
                   <div class="w-5 h-5 bg-[#00FF88] text-black flex items-center justify-center text-[9px] font-black">3</div>
                   <span class="text-[10px] font-black text-white uppercase tracking-widest">Documentation / Assets</span>
                 </div>
                 <button type="button" @click="$refs.fileInput.click()" class="text-[9px] font-black text-[#00FF88] hover:text-white transition-colors uppercase tracking-widest">Upload Files</button>
               </div>
               
               <div class="p-6">
                 <div v-if="newTaskAttachments.length > 0" class="flex flex-wrap gap-2">
                    <div v-for="(att, i) in newTaskAttachments" :key="i" class="flex items-center text-[10px] bg-white border-2 border-black px-3 py-1.5 font-black uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">
                      <span class="truncate max-w-[180px]">{{ att.filename }}</span>
                      <button @click.prevent="newTaskAttachments.splice(i, 1)" class="ml-3 text-red-500 hover:scale-110 transition-transform">
                        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M6 18L18 6M6 6l12 12"></path></svg>
                      </button>
                    </div>
                 </div>
                 <div v-else class="text-center py-6 border-2 border-dashed border-gray-100 text-[10px] font-black text-gray-300 uppercase tracking-widest">
                   No collateral attached to this definition
                 </div>
                 <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />
               </div>
            </div>

            <!-- Final Action -->
            <div class="pt-4 flex flex-col sm:flex-row-reverse gap-4">
               <button type="submit" 
                       :disabled="sending || !newTask.title || !newTask.body" 
                       class="flex-1 bg-black text-[#00FF88] px-8 py-4 border-2 border-black text-xs font-black uppercase tracking-[0.2em] hover:bg-[#00FF88] hover:text-black transition-all shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] hover:translate-y-[2px] hover:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[4px] flex items-center justify-center gap-3 disabled:opacity-50">
                  <svg v-if="sending" class="w-5 h-5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                  <svg v-else class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7" /></svg>
                  {{ sending ? (isEditMode ? 'Updating...' : 'Creating...') : (isEditMode ? 'Update Task' : 'Create Task') }}
               </button>
               <button type="button" @click="() => goBack()" class="px-8 py-4 border-2 border-black bg-white text-xs font-black uppercase tracking-[0.2em] hover:bg-gray-100 transition-all font-bold">Cancel</button>
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
  if (fileInput.value) fileInput.value.value = '';
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
    notifySuccess('Mission Protocol Initialized');
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
    notifySuccess('Scheduled Protocol Updated');
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

<style scoped>
.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
}
.custom-scrollbar::-webkit-scrollbar-track {
  background: transparent;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background: #e5e7eb;
}
.custom-scrollbar::-webkit-scrollbar-thumb:hover {
  background: #d1d5db;
}
</style>
