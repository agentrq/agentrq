<template>
  <div class="h-full flex flex-col w-full max-w-full overflow-x-hidden" v-if="task && workspace">

    <!-- Breadcrumb Header -->
    <header v-if="showHeader" class="pb-2 border-b-2 border-black shrink-0 flex items-center justify-between gap-4">
      <div class="flex items-center gap-2 text-xs font-black uppercase tracking-widest min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="hidden md:block text-gray-400 hover:text-black transition-colors shrink-0">
          {{ workspace.name }}
        </router-link>
        <svg class="hidden md:block w-3 h-3 text-gray-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" /></svg>
        <span class="text-black truncate flex-1 min-w-0 text-sm">{{ task.title }}</span>
        <div class="flex items-center gap-1.5 border-2 border-black px-1.5 py-0.5 text-[10px] font-black uppercase tracking-widest shrink-0"
              :class="task.status === 'ongoing' ? 'bg-[#00FF88] text-black' : task.status === 'completed' || task.status === 'done' ? 'bg-black text-white' : task.status === 'rejected' ? 'bg-red-500 text-white' : 'bg-white text-gray-500'">
          
          <!-- Ongoing Icon -->
          <svg v-if="task.status === 'ongoing'" class="w-3 h-3 animate-pulse" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 3l14 9-14 9V3z" />
          </svg>
          <!-- Completed Icon -->
          <svg v-else-if="task.status === 'completed' || task.status === 'done'" class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="4">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
          </svg>
          <!-- Not Started Icon -->
          <svg v-else-if="task.status === 'notstarted' || task.status === 'pending'" class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <!-- Rejected Icon -->
          <svg v-else-if="task.status === 'rejected'" class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>

          <span class="hidden sm:inline">{{ task.status }}</span>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <span v-if="workspace.archived_at" class="border-2 border-yellow-500 bg-yellow-300 text-black px-2 py-0.5 text-[10px] font-black uppercase tracking-widest">Archived</span>
        <span class="hidden md:inline text-[9px] font-black text-gray-300 uppercase tracking-widest">{{ task.id }}</span>
        <button @click="toggleFullscreen" class="p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all" title="Toggle Fullscreen">
          <svg v-if="!isFullscreen" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" /></svg>
          <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4l5 5m0 0V4m0 5H4m16-5l-5 5m0 0h4m-4 0V4M4 20l5-5m0 0v4m0-5H4m16 5l-5-5m0 0V20m0-5h4" /></svg>
        </button>
        <button @click="router.push('/workspaces/' + workspaceId)" class="p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <!-- Navigation Toggle removed in favor of global experiment -->

    <!-- Scrollable chat area -->
    <div ref="scrollContainer" class="flex-1 overflow-y-auto pt-2 pb-6 flex flex-col gap-0 scroll-smooth custom-scrollbar overflow-x-hidden" style="overscroll-behavior-y: contain;">

      <!-- Context Header (Not Sticky) -->
      <div class="pt-2 pb-3 bg-white border-b border-gray-100/50 mb-3">
        <!-- Task Definition box -->
        <div class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
          <div class="bg-black px-4 py-2 flex items-center justify-between">
            <div class="flex items-center gap-2">
              <div class="w-5 h-5 flex items-center justify-center text-[9px] font-black"
                   :class="task.created_by === 'human' ? 'bg-[#00FF88] text-black' : 'bg-gray-600 text-[#00FF88]'">
                {{ task.created_by === 'human' ? 'H' : 'A' }}
              </div>
              <span class="text-[10px] font-black text-white uppercase tracking-widest">Task Definition</span>
              <span class="text-[9px] font-black text-gray-400 uppercase tracking-widest">→ {{ task.assignee }}</span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-[9px] font-bold text-gray-500 uppercase tracking-widest">{{ formatDateTime(task.created_at) }}</span>
              <button @click="descExpanded = !descExpanded" class="text-[9px] font-black text-gray-400 hover:text-[#00FF88] uppercase tracking-widest flex items-center gap-1 transition-colors">
                {{ descExpanded ? 'Collapse' : 'Expand' }}
                <svg class="w-3 h-3 transition-transform duration-200" :class="descExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M19 9l-7 7-7-7" /></svg>
              </button>
            </div>
          </div>
          <div v-if="descExpanded" class="p-4 bg-white">
            <p class="text-sm font-medium text-gray-700 leading-relaxed whitespace-pre-wrap break-all">{{ task.body }}</p>
            <!-- Initial Attachments -->
            <div v-if="task.attachments && task.attachments.length > 0" class="mt-4 pt-4 border-t-2 border-dashed border-gray-200 flex flex-wrap gap-2">
              <div v-for="(att, i) in task.attachments" :key="i"
                   @click="previewAttachment(att)"
                   class="flex items-center gap-2 px-3 py-1.5 border-2 border-black bg-white hover:bg-[#00FF88] transition-colors cursor-pointer shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">
                <div class="w-5 h-5 flex items-center justify-center overflow-hidden">
                  <img v-if="att.mimeType && att.mimeType.startsWith('image/')" :src="getAttachmentUrl(workspaceId, att.id)" class="w-full h-full object-cover" />
                  <svg v-else class="w-4 h-4 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>
                </div>
                <span class="text-[10px] font-black uppercase tracking-tight truncate max-w-[120px]">{{ att.filename }}</span>
              </div>
            </div>
          </div>
          <div v-else class="px-4 py-2 bg-gray-50 cursor-pointer hover:bg-white transition-colors" @click="descExpanded = true">
            <p class="text-xs font-medium text-gray-500 line-clamp-1">{{ task.body }}</p>
          </div>
        </div>

        <!-- Autoscroll toggle -->
        <div v-if="sortedMessages.length > 0" class="flex justify-end mt-3">
          <button @click="autoscrollEnabled = !autoscrollEnabled"
                  class="flex items-center gap-1.5 px-3 py-1 border-2 border-black text-[9px] font-black uppercase tracking-widest transition-all"
                  :class="autoscrollEnabled ? 'bg-black text-[#00FF88]' : 'bg-white text-gray-400 hover:border-black hover:text-black'">
            <div class="w-1.5 h-1.5 rounded-full" :class="autoscrollEnabled ? 'bg-[#00FF88] animate-pulse' : 'bg-gray-300'"></div>
            Autoscroll
          </button>
        </div>
      </div>

      <!-- Messages -->
      <div class="flex flex-col gap-3">
        <template v-for="m in sortedMessages" :key="m.id">

          <!-- Agent message — left aligned, gray bubble -->
          <div v-if="m.sender === 'agent'" class="flex gap-3 animate-in fade-in slide-in-from-bottom-2 duration-300">
            <div class="w-7 h-7 bg-black border-2 border-black flex items-center justify-center shrink-0 mt-0.5">
              <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" data-lucide="bot" aria-hidden="true" class="lucide lucide-bot w-4 h-4 text-[#00FF88]"><path d="M12 8V4H8"></path><rect width="16" height="12" x="4" y="8" rx="2"></rect><path d="M2 14h2"></path><path d="M20 14h2"></path><path d="M15 13v2"></path><path d="M9 13v2"></path></svg>
            </div>
            <div class="bg-gray-50 border-2 border-black flex-1 p-3 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] min-w-0">
              <span class="text-[10px] font-black text-gray-700 block mb-1.5 uppercase tracking-widest">
                Claude Agent · {{ formatDateTime(m.created_at) }}
              </span>
              <div class="text-xs font-medium text-gray-800 leading-relaxed whitespace-pre-wrap break-all">{{ m.text }}</div>

              <!-- Permission Request (agent message) -->
              <div v-if="m.metadata?.type === 'permission_request'" class="mt-3 border-2 border-black bg-white">
                <div class="bg-black px-3 py-2 flex items-center justify-between">
                  <div class="flex items-center gap-2">
                    <svg class="w-3.5 h-3.5 text-yellow-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                    <span class="text-[10px] font-black text-white uppercase tracking-widest">Agent Authorization Required</span>
                  </div>
                  <span class="text-[9px] font-bold text-gray-400 uppercase tracking-tight">{{ m.metadata.request_id }}</span>
                </div>
                <div class="p-3 flex flex-col gap-3">
                  <div>
                    <span class="text-[9px] font-black text-gray-400 uppercase tracking-widest">Tool / Action</span>
                    <p class="text-xs font-black text-gray-900 mt-0.5 break-all">{{ m.metadata.tool_name }}</p>
                  </div>
                  <p v-if="m.metadata.description" class="text-xs text-gray-600 font-medium italic border-l-2 border-black pl-3">"{{ m.metadata.description }}"</p>
                  <pre v-if="m.metadata.input_preview" class="text-[10px] font-mono bg-gray-950 text-[#00FF88] p-3 overflow-x-auto whitespace-pre-wrap break-all border border-gray-700 custom-scrollbar">{{ m.metadata.input_preview }}</pre>

                  <!-- Pending verdict buttons -->
                  <div v-if="m.metadata.status === 'pending'" class="flex flex-wrap gap-2 pt-2">
                     <button @click="handleVerdict(m.metadata.request_id, 'allow')"
                             :disabled="!!workspace.archived_at"
                             class="px-3 py-1 bg-[#00FF88] border-2 border-black text-xs font-bold transition-all hover:translate-y-px active:translate-y-0.5 disabled:opacity-50">
                       Allow
                     </button>
                     <button @click="handleVerdict(m.metadata.request_id, 'allow_always')"
                             :disabled="!!workspace.archived_at"
                             class="px-3 py-1 bg-blue-100 border-2 border-black text-xs font-bold transition-all hover:translate-y-px active:translate-y-0.5 disabled:opacity-50">
                       Allow All
                     </button>
                     <button @click="handleVerdict(m.metadata.request_id, 'deny')"
                             :disabled="!!workspace.archived_at"
                             class="px-3 py-1 bg-red-100 border-2 border-black text-xs font-bold transition-all hover:translate-y-px active:translate-y-0.5 disabled:opacity-50">
                       Deny
                     </button>
                  </div>

                  <!-- Resolved verdict (collapsible) -->
                  <div v-else
                       @click="m._detailsExpanded = !m._detailsExpanded"
                       class="border-2 cursor-pointer transition-all select-none"
                       :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-[#00FF88] bg-[#00FF88]/10' : 'border-red-400 bg-red-50'">
                    <div class="flex items-center gap-2.5 px-3 py-2">
                      <svg v-if="m.metadata.status === 'allow' || m.metadata.status === 'allow_always'" class="w-3.5 h-3.5 text-green-700 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
                      <svg v-else class="w-3.5 h-3.5 text-red-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M6 18L18 6M6 6l12 12" /></svg>
                      <span class="text-[10px] font-black uppercase tracking-widest flex-1 truncate break-all"
                            :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-green-800' : 'text-red-700'">
                        {{ m.metadata.tool_name }}
                      </span>
                      <span v-if="m.metadata.status === 'allow_always'" class="text-[8px] font-black text-green-700 bg-[#00FF88] border border-green-700 px-1.5 py-0.5 uppercase tracking-widest shrink-0">Auto</span>
                      <span class="text-[9px] font-black uppercase tracking-widest shrink-0"
                            :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-green-700' : 'text-red-600'">
                        {{ m.metadata.status === 'deny' ? 'Denied' : m.metadata.status === 'allow_always' ? 'Always Allowed' : 'Allowed' }}
                      </span>
                      <svg class="w-3 h-3 shrink-0 transition-transform duration-200" :class="m._detailsExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M19 9l-7 7-7-7" /></svg>
                    </div>
                    <div v-if="m._detailsExpanded" class="px-3 pb-3 pt-1 border-t-2 border-dashed"
                         :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-[#00FF88]' : 'border-red-300'">
                      <p v-if="m.metadata.description" class="text-[11px] text-gray-600 italic mb-2">"{{ m.metadata.description }}"</p>
                      <pre v-if="m.metadata.input_preview" class="text-[9px] font-mono bg-gray-950 text-[#00FF88] p-2 overflow-x-auto whitespace-pre-wrap break-all border border-gray-700 mb-2">{{ m.metadata.input_preview }}</pre>
                      <span class="text-[8px] font-bold text-gray-400 uppercase tracking-tighter">ID: {{ m.metadata.request_id }}</span>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Attachments on agent message -->
              <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-1.5 mt-2.5 pt-2.5 border-t-2 border-dashed border-gray-200">
                <div v-for="(att, i) in m.attachments" :key="i"
                     @click="previewAttachment(att)"
                     class="flex items-center gap-1.5 px-2.5 py-1 border-2 border-black bg-white hover:bg-[#00FF88] transition-colors cursor-pointer text-[10px] font-black uppercase tracking-tight">
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                  <span class="truncate max-w-[140px]">{{ att.filename }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- Human message — right aligned, green bubble -->
          <div v-else class="flex gap-3 flex-row-reverse animate-in fade-in slide-in-from-bottom-2 duration-300">
            <div class="w-7 h-7 bg-[#00FF88] border-2 border-black flex items-center justify-center shrink-0 mt-0.5">
              <svg class="w-4 h-4 text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
            </div>
            <div class="bg-[#00FF88] border-2 border-black flex-1 p-3 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] min-w-0">
              <span class="text-[10px] font-black text-black block mb-1.5 uppercase tracking-widest text-right">
                You · {{ formatDateTime(m.created_at) }}
              </span>
              <div class="text-xs font-medium text-black leading-relaxed whitespace-pre-wrap text-right break-all">{{ m.text }}</div>
              <!-- Attachments on human message -->
              <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-1.5 mt-2.5 pt-2.5 border-t-2 border-dashed border-black/20 justify-end">
                <div v-for="(att, i) in m.attachments" :key="i"
                     @click="previewAttachment(att)"
                     class="flex items-center gap-1.5 px-2.5 py-1 border-2 border-black bg-white hover:bg-black hover:text-white transition-colors cursor-pointer text-[10px] font-black uppercase tracking-tight">
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                  <span class="truncate max-w-[140px]">{{ att.filename }}</span>
                </div>
              </div>
            </div>
          </div>

        </template>
      </div>
    </div>

    <!-- Reply Box -->
    <footer v-if="!workspace.archived_at" class="border-t-2 border-dashed border-gray-200 pt-4 shrink-0 z-20">

      <!-- Attachment previews -->
      <div v-if="replyAttachments.length > 0" class="flex flex-wrap gap-2 mb-3">
        <div v-for="(att, i) in replyAttachments" :key="i"
             class="flex items-center text-[10px] bg-black text-[#00FF88] border-2 border-black px-3 py-1 font-black tracking-tight uppercase">
          <span class="truncate max-w-[180px]">{{ att.filename }}</span>
          <button @click="replyAttachments.splice(i, 1)" class="ml-2 hover:text-white transition-colors">
            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="3"><path d="M6 18L18 6M6 6l12 12"></path></svg>
          </button>
        </div>
      </div>

      <form @submit.prevent="submitReply">
        <div class="flex items-end gap-2 w-full flex-nowrap">
          <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />

          <div class="flex-1 flex items-center border-2 border-black bg-white focus-within:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all group relative min-w-0">
            <textarea
              ref="textareaRef"
              v-model="replyText"
              @input="adjustTextareaHeight"
              rows="1"
              :disabled="!workspace.agent_connected || task.status === 'notstarted' || task.status === 'pending'"
              :placeholder="!workspace.agent_connected ? 'Waiting for agent...' : (task.status === 'notstarted' || task.status === 'pending') ? 'Task not started...' : 'Type instructions...'"
              class="flex-1 px-3 py-2 md:px-4 md:py-3 text-sm md:text-base font-medium text-gray-900 bg-transparent outline-none placeholder-gray-400 disabled:opacity-50 resize-none min-h-[38px] md:min-h-[46px] max-h-[150px] custom-scrollbar"
            ></textarea>
            <button type="button" @click="$refs.fileInput.click()"
                    :disabled="!workspace.agent_connected || task.status === 'notstarted' || task.status === 'pending'"
                    class="h-[38px] md:h-[46px] px-2.5 md:px-3 text-gray-400 hover:text-black transition-colors flex items-center justify-center border-l-2 border-transparent group-focus-within:border-black group-focus-within:border-dashed disabled:opacity-30 self-end">
              <svg class="w-4 h-4 md:w-5 md:h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
            </button>
          </div>
           <button type="submit"
                   :disabled="(!replyText.trim() && replyAttachments.length === 0) || !workspace.agent_connected || task.status === 'notstarted' || task.status === 'pending'"
                   class="h-[38px] w-[32px] md:h-[46px] md:w-[46px] bg-transparent md:bg-black text-black md:text-white border-0 md:border-2 md:border-black hover:text-[#00FF88] md:hover:bg-[#00FF88] md:hover:text-black shadow-none md:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] disabled:opacity-30 transition-all shrink-0 flex items-center justify-center"
                   title="Send Instruction">
             <svg class="w-5 h-5 md:w-5 md:h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
               <path stroke-linecap="round" stroke-linejoin="round" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
             </svg>
           </button>
        </div>

        <!-- Status Warning Messages -->
        <div v-if="!workspace.agent_connected" class="flex flex-col items-center gap-2 mt-4 px-4 py-3 bg-red-50 border-2 border-red-200 border-dashed">
           <div class="flex items-center gap-2">
             <span class="w-2.5 h-2.5 rounded-full bg-red-500 animate-pulse border border-black/10"></span>
             <span class="text-[10px] font-black text-red-700 uppercase tracking-[0.2em]">Agent Offline</span>
           </div>
           <p class="text-[9px] text-red-600 font-bold uppercase tracking-tight text-center italic">Delivery of instructions requires an active agent connection.</p>
        </div>
        
        <div v-else-if="task.status === 'notstarted' || task.status === 'pending'" class="flex flex-col items-center gap-2 mt-4 px-4 py-3 bg-yellow-50 border-2 border-yellow-200 border-dashed">
           <div class="flex items-center gap-2">
             <span class="w-2.5 h-2.5 rounded-full bg-yellow-400 border border-black/10"></span>
             <span class="text-[10px] font-black text-yellow-700 uppercase tracking-[0.2em]">Task Not Started</span>
           </div>
           <p class="text-[9px] text-yellow-600 font-bold uppercase tracking-tight text-center italic">Please wait for the task to start or be accepted before sending messages.</p>
        </div>

        <div v-if="workspace.agent_connected && task.status !== 'notstarted' && task.status !== 'pending'" class="hidden md:flex items-center justify-center gap-2 mt-3">
          <div class="h-px bg-gray-200 grow"></div>
          <span class="text-[9px] font-black text-gray-300 uppercase tracking-[0.2em] whitespace-nowrap">Shift+Enter for newline · Secure Agent Sync</span>
          <div class="h-px bg-gray-200 grow"></div>
        </div>
      </form>
    </footer>

    <!-- Attachment Preview Modal -->
    <div v-if="selectedAtt" class="fixed inset-0 z-[100] flex items-center justify-center" @keydown.esc="selectedAtt = null">
      <div class="absolute inset-0 bg-black/90" @click="selectedAtt = null"></div>
      <button @click="selectedAtt = null" class="absolute top-6 right-6 text-white/50 hover:text-white z-20 p-2 border-2 border-white/20 hover:border-white transition-all">
        <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M6 18L18 6M6 6l12 12"></path></svg>
      </button>
      <div class="relative max-w-[90vw] max-h-[85vh] flex flex-col items-center gap-4 z-10">
        <div class="border-2 border-white/20 overflow-hidden flex items-center justify-center min-w-[300px] bg-white/5">
          <img v-if="selectedAtt.mimeType?.startsWith('image/')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh] object-scale-down" />
          <video v-else-if="selectedAtt.mimeType?.startsWith('video/')" controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh]" />
          <div v-else-if="selectedAtt.mimeType?.startsWith('audio/')" class="p-16 flex flex-col items-center gap-6">
            <div class="w-20 h-20 bg-[#00FF88] border-2 border-white flex items-center justify-center">
              <svg class="w-10 h-10 text-black" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
            </div>
            <audio controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[400px]" />
          </div>
          <iframe v-else-if="selectedAtt.mimeType?.includes('pdf')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[80vw] h-[75vh]" frameborder="0"></iframe>
          <div v-else class="p-20 flex flex-col items-center gap-4">
            <svg class="w-24 h-24 text-white/20" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1"><path d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" /></svg>
            <p class="text-white font-black uppercase tracking-widest text-sm">{{ selectedAtt.filename }}</p>
          </div>
        </div>
        <div class="flex items-center gap-4 px-6 py-3 bg-black border-2 border-white/20">
          <div class="flex flex-col">
            <p class="text-xs font-black text-white truncate max-w-[250px]">{{ selectedAtt.filename }}</p>
            <p class="text-[9px] font-bold text-gray-400 uppercase tracking-tighter">{{ selectedAtt.mimeType }}</p>
          </div>
          <div class="w-px h-8 bg-white/10"></div>
          <a :href="getAttachmentUrl(workspaceId, selectedAtt.id)" :download="selectedAtt.filename"
             class="flex items-center gap-2 px-4 py-2 bg-[#00FF88] text-black border-2 border-[#00FF88] text-[10px] font-black uppercase tracking-widest hover:bg-white transition-all shadow-[2px_2px_0px_0px_rgba(255,255,255,0.3)]">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"></path></svg>
            Download
          </a>
        </div>
      </div>
    </div>

  </div>

  <!-- Loading State -->
  <div v-else class="h-full flex flex-col items-center justify-center bg-white">
    <div class="border-2 border-black p-8 shadow-[8px_8px_0px_0px_rgba(0,0,0,1)] flex flex-col items-center gap-4">
      <div class="w-16 h-16 bg-black flex items-center justify-center">
        <svg class="w-8 h-8 text-[#00FF88] animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
      </div>
      <p class="text-xs font-black text-gray-900 uppercase tracking-widest">Syncing Task Context</p>
      <p class="text-[10px] text-gray-400 font-bold uppercase tracking-widest">Retrieving workspace and message history...</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, onUnmounted, watch, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, getTask, respondToTask, getAttachmentUrl, sendPermissionVerdict } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';

const { notifyError, notifySuccess } = useToasts();
const route = useRoute();
const router = useRouter();
const workspaceId = route.params.workspaceId;
const taskId = route.params.taskId;

const workspace = ref(null);
const task = ref(null);
const descExpanded = ref(false);
const replyText = ref('');
const replyAttachments = ref([]);
const scrollContainer = ref(null);
const autoscrollEnabled = ref(true);
const isFullscreen = ref(false);

const isMobile = computed(() => window.innerWidth < 768);
const showHeader = ref(true);

function toggleFullscreen() {
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen().then(() => {
      isFullscreen.value = true;
    }).catch(err => {
      notifyError("Fullscreen not supported or blocked");
    });
  } else {
    document.exitFullscreen();
    isFullscreen.value = false;
  }
}

const { connect, disconnect, events } = useEventBus(workspaceId);

const sortedMessages = computed(() => {
  if (!task.value || !task.value.messages) return [];
  return [...task.value.messages].sort((a,b) => new Date(a.created_at) - new Date(b.created_at));
});

function scrollToBottom() {
  if (autoscrollEnabled.value && scrollContainer.value) {
    nextTick(() => {
      scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight;
    });
  }
}

watch(sortedMessages, () => {
  scrollToBottom();
}, { deep: true });

const statusClass = computed(() => {
  if (!task.value) return '';
  switch(task.value.status) {
    case 'ongoing': return 'bg-indigo-50 text-indigo-700 border-indigo-100';
    case 'done': return 'bg-green-50 text-green-700 border-green-100';
    case 'rejected': return 'bg-red-50 text-red-700 border-red-100';
    default: return 'bg-gray-50 text-gray-600 border-gray-100';
  }
});

async function load() {
  try {
    const pRes = await getWorkspace(workspaceId);
    workspace.value = pRes.workspace;
    const tRes = await getTask(workspaceId, taskId);
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
    await sendPermissionVerdict(workspaceId, taskId, requestId, behavior);
    notifySuccess("Verdict sent successfully");
  } catch (err) {
    notifyError('Failed to send verdict: ' + err.message);
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
    const res = await respondToTask(workspaceId, taskId, 'text', text, atts);
    task.value = res.task;
    notifySuccess("Message sent");
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
  el.style.height = isMobile.value ? '38px' : '46px';
  const newHeight = Math.min(el.scrollHeight, 150);
  el.style.height = newHeight + 'px';
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
    if (last.payload && last.payload.id === taskId) {
      task.value = last.payload;
    }
  }
}, { deep: true });

const selectedAtt = ref(null);

function previewAttachment(att) {
  selectedAtt.value = att;
}

function openAttachment(attId) {
  window.open(getAttachmentUrl(workspaceId, attId), '_blank');
}

watch(() => task.value?.title, (title) => {
  if (title) document.title = `${title} | AgentRQ`;
}, { immediate: true });

onMounted(() => {
  load();
  scrollToBottom();
});
onUnmounted(disconnect);
</script>

<style>
.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
}
.custom-scrollbar::-webkit-scrollbar-track {
  background: transparent;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background-color: #f3f4f6;
  border-radius: 20px;
}
.custom-scrollbar:hover::-webkit-scrollbar-thumb {
  background-color: #e5e7eb;
}
.shadow-soft {
  box-shadow: 0 10px 30px -10px rgba(0,0,0,0.05);
}
</style>
