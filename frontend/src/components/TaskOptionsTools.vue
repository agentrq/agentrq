<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The controls of a new task, as a row of buttons: attach, voice, who it is
 * for, clear context, YOLO, schedule, what fires on completion, and the
 * agent's model. The New Task form and the Pano's chat box both draw it, over
 * the state of one `useTaskOptions`, so a control behaves the same in both.
 *
 * It asks no breakpoint of the window: it is laid out by the width of the
 * nearest `@container` above it, as the form is.
 */
import { ref, toRef } from 'vue'
import { useTooltipStore } from '../stores/tooltipStore'
import { useSpeechToText } from '../composables/useSpeechToText'
import { clearContextTooltip } from '../composables/useClearContext'
import AgentModelPicker from './AgentModelPicker.vue'

const props = defineProps({
  /** The `useTaskOptions` this row edits. */
  opts: { type: Object, required: true },
  /** The workspace the task is for, as the store has it. */
  workspace: { type: Object, default: null },
  workspaceId: { type: [String, Number], required: true },
  /** Whether /clear could reach a running session of that workspace. */
  clearContextOffered: { type: Boolean, default: false },
  /** Editing a scheduled task: it cannot be given a completion trigger. */
  isEditMode: { type: Boolean, default: false },
  /** The text field voice input dictates into, for the cursor. */
  inputEl: { type: Object, default: null },
})
/** The text voice input dictates into. */
const text = defineModel('text', { type: String, default: '' })

const tooltipStore = useTooltipStore()
const fileInput = ref(null)
const showScheduleMenu = ref(false)
const showEventMenu = ref(false)

const {
  assignee, allowAllCommands, clearContext, cronSchedule, events, workflows,
  selectedEventId, selectedWorkflowId, completionKind, completionTriggerLabel,
  scheduleType, oneTimeDate, repeatPreset, repeatTime, selectedDays, daysOptions, nextRunPreview,
  toggleDay, processFiles,
} = props.opts

const {
  isRecording: sttRecording,
  isTranscribing: sttTranscribing,
  isModelLoading: sttModelLoading,
  modelProgress: sttProgress,
  isSupported: sttSupported,
  toggleRecording: sttToggle,
} = useSpeechToText(text, props.workspaceId, toRef(props, 'inputEl'))

function handleFileUpload(event) {
  processFiles(event.target.files)
  fileInput.value.value = ''
}
</script>

<template>
                <!-- Attachment Button -->
                <button type="button" data-test="task-attach" @click="$refs.fileInput.click()"
                        class="h-7 w-7 rounded-md text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 transition-colors flex items-center justify-center"
                        title="Attach files">
                  <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                </button>
                <input type="file" data-test="task-file-input" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />

                <!-- STT Button -->
                <button v-if="sttSupported" type="button" @click="sttToggle(); tooltipStore.hide()"
                        :disabled="sttTranscribing"
                        @mouseenter="tooltipStore.show($event, sttRecording ? 'Stop recording' : sttTranscribing ? (sttModelLoading ? `Loading STT model... ${sttProgress}%` : 'Transcribing...') : 'Voice input', 'top')"
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
                  <button type="button" data-test="task-assignee-agent" @click="assignee = 'agent'; tooltipStore.hide()"
                          @mouseenter="tooltipStore.show($event, 'Assign to Agent', 'top')" @mouseleave="tooltipStore.hide()"
                          :class="assignee === 'agent' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300'"
                          class="px-2 rounded flex items-center justify-center text-[10px] font-bold uppercase tracking-wider transition-all">
                    <span class="hidden @min-[40rem]:inline">Agent</span>
                    <svg class="@min-[40rem]:hidden w-3.5 h-3.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
                  </button>
                  <button type="button" data-test="task-assignee-human" @click="assignee = 'human'; tooltipStore.hide()"
                          @mouseenter="tooltipStore.show($event, 'Assign to Human', 'top')" @mouseleave="tooltipStore.hide()"
                          :class="assignee === 'human' ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300'"
                          class="px-2 rounded flex items-center justify-center text-[10px] font-bold uppercase tracking-wider transition-all">
                    <span class="hidden @min-[40rem]:inline">Human</span>
                    <svg class="@min-[40rem]:hidden w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
                  </button>
                </div>

                <!-- Clear-context toggle. Icon only, and shown only when the
                     workspace has a running Claude Code session to type /clear
                     into — see useClearContext for why it is hidden rather
                     than disabled when it cannot work. -->
                <button v-if="assignee === 'agent' && clearContextOffered" type="button" data-test="task-clear-context"
                        @click.stop="clearContext = !clearContext; tooltipStore.hide()"
                        @mouseenter="tooltipStore.show($event, clearContextTooltip(clearContext), 'top')"
                        @mouseleave="tooltipStore.hide()"
                        :class="clearContext ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-black border-transparent shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                        class="flex items-center justify-center w-7 h-7 rounded-md transition-all">
                  <!-- An eraser. Drawn as a broom first, which at 14px read as a pen —
                       i.e. "edit", which is a plausible and wrong meaning next to a
                       task form. Compared side by side before choosing. -->
                  <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 20H20" /><path stroke-linecap="round" stroke-linejoin="round" d="M14.5 4.5l5 5a1.5 1.5 0 010 2.12l-7.4 7.4a1.5 1.5 0 01-2.12 0l-5-5a1.5 1.5 0 010-2.12l7.4-7.4a1.5 1.5 0 012.12 0z" /><path stroke-linecap="round" stroke-linejoin="round" d="M8 9l7 7" /></svg>
                </button>

                <!-- YOLO Toggle -->
                <button v-if="assignee === 'agent'" type="button" data-test="task-yolo" @click.stop="allowAllCommands = !allowAllCommands; tooltipStore.hide()"
                        @mouseenter="tooltipStore.show($event, allowAllCommands ? 'YOLO Active: Agent will execute all commands without approval' : 'YOLO Mode: Skip approval for sensitive commands', 'top')"
                        @mouseleave="tooltipStore.hide()"
                        :class="allowAllCommands ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-black border-transparent shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                        class="flex items-center justify-center gap-1 w-7 h-7 @min-[40rem]:w-auto @min-[40rem]:px-2.5 rounded-md transition-all text-[10px] font-bold uppercase tracking-wider">
                  <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M17.657 18.657A8 8 0 016.343 7.343S7 9 9 10c0-2 .5-5 2.986-7C14 5 16.09 5.777 17.656 7.343A7.99 7.99 0 0120 13a7.98 7.98 0 01-2.343 5.657z" /><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 16.121A3 3 0 1012.015 11L11 14l2.015-2.879z" /></svg>
                  <span class="hidden @min-[40rem]:inline">YOLO</span>
                </button>

                <!-- Schedule / Cron Dropdown (Popover) -->
                <div class="relative" v-click-outside="() => showScheduleMenu = false">
                   <button type="button" data-test="task-schedule" @click="showScheduleMenu = !showScheduleMenu; showEventMenu = false; tooltipStore.hide()"
                           @mouseenter="tooltipStore.show($event, scheduleType === 'none' ? 'Set Schedule' : (cronSchedule || 'Schedule Active'), 'top')"
                           @mouseleave="tooltipStore.hide()"
                           :class="scheduleType !== 'none' ? 'bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:border-blue-800 shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                           class="flex items-center justify-center w-7 h-7 rounded-md transition-all">
                     <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                   </button>

                   <!-- Schedule Menu Content -->
                   <div v-if="showScheduleMenu" class="fixed @min-[40rem]:absolute left-4 right-4 @min-[40rem]:left-0 @min-[40rem]:right-auto bottom-4 @min-[40rem]:bottom-full mb-3 w-auto @min-[40rem]:w-[320px] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-4 animate-in fade-in slide-in-from-bottom-2">
                       <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mb-3 border-b border-gray-100 dark:border-zinc-800 pb-2">Execution Strategy</h3>
                       
                       <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-sm mb-4">
                         <button type="button" @click="scheduleType = 'none'"
                                 :class="scheduleType === 'none' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">None</button>
                         <button type="button" data-test="task-schedule-onetime" @click="scheduleType = 'onetime'"
                                 :class="scheduleType === 'onetime' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">One-time</button>
                         <button type="button" data-test="task-schedule-repeated" @click="scheduleType = 'repeated'"
                                 :class="scheduleType === 'repeated' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300 border border-transparent'"
                                 class="px-2 py-1.5 flex-1 rounded-sm text-[10px] font-semibold transition-all">Repeated</button>
                       </div>

                       <div class="min-h-[100px]">
                         <div v-if="scheduleType === 'onetime'" class="animate-in fade-in slide-in-from-top-2 duration-200">
                           <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest mb-2 block">Launch Date/Time</label>
                           <input type="datetime-local" data-test="task-onetime-date" v-model="oneTimeDate"
                                  class="bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-xs font-semibold text-gray-900 dark:text-zinc-50 outline-none w-full" />
                         </div>

                         <div v-if="scheduleType === 'repeated'" class="space-y-4 animate-in fade-in slide-in-from-top-2 duration-200">
                           <div class="flex gap-3">
                             <div class="flex flex-col gap-1.5 flex-1">
                               <label class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">Frequency</label>
                               <select v-model="repeatPreset" data-test="task-repeat-preset" 
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
                          <code class="text-[9px] font-mono text-gray-600 dark:text-zinc-400">{{ cronSchedule || '----' }}</code>
                          <span class="text-[9px] font-bold text-sky-600 dark:text-sky-400 truncate max-w-[120px]">{{ nextRunPreview }}</span>
                       </div>
                   </div>
                </div>

                <!-- On-completion trigger: a single event, or a whole workflow -->
                <div class="relative" v-if="!isEditMode && (events.length > 0 || workflows.length > 0)" v-click-outside="() => showEventMenu = false">
                   <button type="button" data-test="task-completion" @click="showEventMenu = !showEventMenu; showScheduleMenu = false; tooltipStore.hide()"
                           @mouseenter="tooltipStore.show($event, completionTriggerLabel, 'top')"
                           @mouseleave="tooltipStore.hide()"
                           :class="(selectedEventId || selectedWorkflowId) ? 'bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-900/30 dark:text-purple-400 dark:border-purple-800 shadow-sm' : 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
                           class="flex items-center justify-center w-7 h-7 rounded-md transition-all">
                     <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                   </button>
                   <div v-if="showEventMenu" class="fixed @min-[40rem]:absolute left-4 right-4 @min-[40rem]:left-auto @min-[40rem]:right-0 bottom-4 @min-[40rem]:bottom-full mb-3 w-auto @min-[40rem]:w-[260px] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-4 animate-in fade-in slide-in-from-bottom-2">
                       <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 mb-3 border-b border-gray-100 dark:border-zinc-800 pb-2">On Completion</h3>

                       <!-- One control, two kinds. Selecting either clears the
                            other, so an impossible "both" state cannot be built. -->
                       <div class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-lg mb-3">
                         <button type="button" data-test="task-completion-event" @click="completionKind = 'event'; selectedWorkflowId = ''"
                                 class="grow px-3 py-1 rounded-md text-[10px] font-semibold transition-all"
                                 :class="completionKind === 'event' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'">
                           Event
                         </button>
                         <button type="button" data-test="task-completion-workflow" @click="completionKind = 'workflow'; selectedEventId = ''"
                                 class="grow px-3 py-1 rounded-md text-[10px] font-semibold transition-all"
                                 :class="completionKind === 'workflow' ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700' : 'text-gray-500 dark:text-zinc-500 border border-transparent'">
                           Workflow
                         </button>
                       </div>

                       <select v-if="completionKind === 'event'" v-model="selectedEventId" data-test="task-event" class="w-full bg-gray-50 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-md px-2 py-2 text-[11px] font-semibold text-gray-900 dark:text-zinc-100 outline-none">
                         <option value="">None</option>
                         <option v-for="ev in events" :key="ev.id" :value="ev.id">{{ ev.name }}</option>
                       </select>

                       <template v-else>
                         <select v-model="selectedWorkflowId" data-test="task-workflow" class="w-full bg-gray-50 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-md px-2 py-2 text-[11px] font-semibold text-gray-900 dark:text-zinc-100 outline-none">
                           <option value="">None</option>
                           <!-- A workflow with no start event cannot run, so it
                                is offered as disabled rather than hidden: the
                                absence would otherwise look like a bug. -->
                           <option v-for="wf in workflows" :key="wf.id" :value="wf.id" :disabled="!wf.startEventId">
                             {{ wf.name }}{{ wf.startEventId ? '' : ' (no start event)' }}
                           </option>
                         </select>
                         <p class="text-[10px] text-gray-400 dark:text-zinc-500 mt-2 leading-snug">
                           Runs the whole pipeline, not just one event
                         </p>
                       </template>
                   </div>
                </div>

                <!-- Which model the agent will use, chosen before the task is
                     written rather than after it has started. Absent unless the
                     connected agent offers a choice and will act on it — and
                     absent for a task assigned to a person, where no agent will
                     run it and the choice would decide nothing. Guarded exactly
                     as the YOLO toggle beside it is, so the two agree about
                     what an agent task means. -->
                <AgentModelPicker v-if="assignee === 'agent'" :workspace="workspace" />
</template>
