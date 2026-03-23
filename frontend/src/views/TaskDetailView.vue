<template>
  <div class="h-full flex flex-col" v-if="task && workspace">
    <!-- Breadcrumbs -->
    <header class="py-4 border-b border-gray-100 shrink-0 bg-gray-50/0 flex items-center justify-between">
      <div class="flex items-center gap-2 text-sm">
        <router-link :to="'/workspaces/' + workspaceId" class="text-gray-500 hover:text-black transition-colors font-medium">
          {{ workspace.name }}
        </router-link>
        <svg class="w-4 h-4 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-900 font-bold truncate max-w-[300px]">{{ task.title }}</span>
        <span class="ml-2 px-2.5 py-0.5 rounded-full text-[10px] uppercase font-extrabold tracking-widest border shadow-sm" :class="statusClass">{{ task.status }}</span>
      </div>
      <div v-if="workspace.archived_at" class="flex items-center gap-2 px-3 py-1 bg-amber-50 rounded-full border border-amber-100">
        <svg class="w-2.5 h-2.5 text-amber-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
        <span class="text-[9px] font-black text-amber-700 uppercase tracking-widest">Archived</span>
      </div>
      <div class="flex items-center gap-3">
        <span class="hidden md:inline text-[10px] font-bold text-gray-400 uppercase tracking-widest">ID: {{ task.id }}</span>
        <button @click="router.push('/workspaces/' + workspaceId)" class="p-2 text-gray-400 hover:text-black hover:bg-white rounded-lg transition-all border border-transparent hover:border-gray-100">
           <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
    </header>

    <!-- Main Content Area -->
    <div ref="scrollContainer" class="flex-1 overflow-y-auto py-8 flex flex-col w-full scroll-smooth custom-scrollbar"
         :class="descExpanded ? 'gap-10' : 'gap-4'">
      
      <!-- Compact Task Description Trigger -->
      <section class="group relative bg-zinc-50/50 rounded-2xl border border-gray-100 shadow-sm hover:shadow-md transition-all duration-300"
               :class="descExpanded ? 'p-6' : 'p-3 self-start'">
        <div class="flex items-center justify-between" :class="descExpanded ? 'mb-4' : 'gap-3'">
          <div class="flex items-center" :class="descExpanded ? 'gap-3' : 'gap-2'">
             <div class="rounded-full flex items-center justify-center text-white font-black ring-4 ring-white shadow-lg shrink-0 transition-all font-inter"
                  :class="[
                    descExpanded ? 'w-10 h-10 text-xs' : 'w-7 h-7 text-[9px]',
                    task.created_by === 'human' ? 'bg-black' : 'bg-indigo-600'
                  ]">
               {{ task.created_by === 'human' ? 'H' : 'A' }}
             </div>
             <div class="flex flex-col" v-if="descExpanded">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-bold text-gray-900 leading-none">Task Definition</span>
                  <div class="flex items-center gap-1.5 px-2 py-0.5 bg-indigo-50 rounded-full border border-indigo-100">
                    <span class="text-[8px] font-black text-indigo-400 uppercase tracking-widest">Assignee:</span>
                    <span class="text-[9px] font-black text-indigo-600 uppercase tracking-widest">{{ task.assignee === 'human' ? 'HUMAN' : 'AGENT' }}</span>
                  </div>
                </div>
                <span class="text-[10px] text-gray-400 mt-1.5 uppercase tracking-wider font-bold">{{ formatDateTime(task.created_at) }}</span>
             </div>
             <div v-else class="flex flex-col">
               <span class="text-[10px] font-bold text-gray-700 uppercase tracking-widest px-1">Definition</span>
               <span class="text-[7px] font-black text-indigo-500 uppercase tracking-[0.15em] px-1 opacity-70">to {{ task.assignee }}</span>
             </div>
          </div>
          <button @click="descExpanded = !descExpanded" class="text-[10px] font-extrabold text-indigo-600 hover:text-indigo-800 uppercase tracking-widest flex items-center gap-1.5 transition-colors group/btn">
            <span v-if="descExpanded">Collapse</span>
            <svg class="w-3.5 h-3.5 transition-transform duration-300 group-hover/btn:translate-y-0.5" :class="descExpanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
          </button>
        </div>
        
        <div v-if="descExpanded" class="text-gray-700 leading-relaxed text-sm whitespace-pre-wrap transition-all duration-500 overflow-hidden mt-2 pb-2">
           {{ task.body }}
        </div>

        <!-- Initial Attachments -->
        <div v-if="task.attachments && task.attachments.length > 0 && descExpanded" class="mt-8 pt-8 border-t border-gray-200/50 grid grid-cols-2 md:grid-cols-4 lg:grid-cols-5 gap-4">
           <div v-for="(att, i) in task.attachments" :key="i" class="group relative flex flex-col gap-2 p-2 rounded-xl border border-transparent hover:border-gray-100 hover:bg-white transition-all shadow-none hover:shadow-lg">
               <div class="aspect-square rounded-lg overflow-hidden border border-gray-100 bg-gray-50 flex items-center justify-center cursor-pointer"
                    @click="previewAttachment(att)">
                  <img v-if="att.mimeType && att.mimeType.startsWith('image/')" :src="getAttachmentUrl(workspaceId, att.id)" class="w-full h-full object-cover group-hover:scale-110 transition-transform duration-500" />
                  <video v-else-if="att.mimeType && att.mimeType.startsWith('video/')" :src="getAttachmentUrl(workspaceId, att.id)" class="w-full h-full object-cover" />
                  <div v-else-if="att.mimeType && att.mimeType.startsWith('audio/')" class="p-2 flex flex-col items-center gap-1">
                    <svg class="w-8 h-8 text-indigo-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" /></svg>
                    <span class="text-[6px] font-bold opacity-40">AUDIO</span>
                  </div>
                  <div v-else class="flex flex-col items-center gap-2 text-gray-300 group-hover:text-indigo-400 transition-colors">
                    <svg class="w-10 h-10" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path></svg>
                    <span class="text-[8px] font-bold uppercase tracking-tighter">{{ att.mimeType?.split('/')[1] || 'FILE' }}</span>
                  </div>
               </div>
               <span class="text-[10px] text-gray-500 font-bold truncate px-1 text-center cursor-pointer hover:text-indigo-600 transition-colors" 
                     :title="att.filename" @click="previewAttachment(att)">{{ att.filename }}</span>
           </div>
        </div>
      </section>

      <!-- Message History -->
      <!-- Message History -->
      <section class="flex flex-col pb-12 relative">
        <!-- Vertical Thread Line -->
        <div v-if="sortedMessages.length > 0" class="absolute left-4 top-0 bottom-12 w-px bg-gray-100/80"></div>

        <!-- Autoscroll Toggle -->
        <div v-if="sortedMessages.length > 0" class="sticky top-0 z-20 flex justify-end px-6 mb-4 -mt-4">
           <button @click="autoscrollEnabled = !autoscrollEnabled" 
                   :class="autoscrollEnabled ? 'bg-black text-white' : 'bg-white text-gray-400 border border-gray-100'"
                   class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-full transition-all shadow-sm group">
              <div :class="autoscrollEnabled ? 'bg-green-400' : 'bg-gray-300'" class="w-1.5 h-1.5 rounded-full animate-pulse"></div>
              <span class="text-[9px] font-black uppercase tracking-widest">Autoscroll</span>
           </button>
        </div>

        <template v-for="m in sortedMessages" :key="m.id">
          <div class="flex gap-4 group relative py-3 animate-in fade-in slide-in-from-bottom-2 duration-300">
            <!-- Icon / Dot -->
            <div class="w-8 h-8 shrink-0 rounded-full flex items-center justify-center text-[9px] font-black z-10 transition-all border-[3px] border-white shadow-sm"
                 :class="m.sender === 'agent' ? 'bg-indigo-600 text-white' : 'bg-black text-white'">
              {{ m.sender === 'agent' ? 'A' : 'H' }}
            </div>

            <div class="flex flex-col min-w-0 flex-1 pt-1">
              <div class="flex items-center gap-2 mb-1">
                <span class="text-[10px] font-extrabold text-gray-900 uppercase tracking-tight">
                  {{ m.sender === 'agent' ? 'Agent' : 'You' }}
                </span>
                <span class="text-[9px] text-gray-400 font-bold uppercase tracking-widest opacity-40">
                  {{ formatDateTime(m.created_at) }}
                </span>
              </div>
              
              <div class="text-[13px] leading-relaxed text-gray-700 whitespace-pre-wrap">
                {{ m.text }}
              </div>

              <!-- Permission Request Relay -->
              <div v-if="m.metadata?.type === 'permission_request'" class="mt-4 p-5 bg-indigo-50/50 rounded-2xl border border-indigo-100 flex flex-col gap-4 shadow-sm backdrop-blur-sm animate-in zoom-in-95 duration-500">
                <div class="flex items-center justify-between">
                   <div class="flex items-center gap-2.5 text-indigo-900 font-extrabold text-[10px] uppercase tracking-widest">
                      <div class="w-6 h-6 rounded-lg bg-indigo-600 flex items-center justify-center text-white shadow-lg shadow-indigo-200">
                        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                      </div>
                      Agent Authorization Required
                   </div>
                   <span class="text-[9px] font-bold text-indigo-300 uppercase tracking-tighter">ID: {{ m.metadata.request_id }}</span>
                </div>
                
                <div class="flex flex-col gap-1.5">
                   <span class="text-[9px] font-black text-indigo-400 uppercase tracking-widest opacity-60">Tool / Action</span>
                   <p class="text-xs font-bold text-gray-800 leading-tight">{{ m.metadata.tool_name }}</p>
                </div>

                <div class="p-3 bg-white/60 rounded-xl border border-indigo-50/50 text-[11px] font-medium text-gray-600 leading-relaxed italic">
                   "{{ m.metadata.description }}"
                </div>

                <div class="flex flex-col gap-1.5">
                   <span class="text-[9px] font-black text-indigo-400 uppercase tracking-widest opacity-60">Input Preview</span>
                   <pre class="text-[10px] font-mono bg-zinc-900 text-zinc-300 p-4 rounded-xl overflow-x-auto shadow-inner border border-zinc-800">{{ m.metadata.input_preview }}</pre>
                </div>

                <div v-if="m.metadata.status === 'pending'" class="flex flex-wrap gap-2.5 pt-1">
                  <button @click="handleVerdict(m.metadata.request_id, 'allow')" 
                          :disabled="!!workspace.archived_at"
                          :class="workspace.archived_at ? 'opacity-30 cursor-not-allowed' : 'hover:bg-indigo-700 active:scale-95 shadow-lg shadow-indigo-100'"
                          class="flex-1 py-3 bg-indigo-600 text-white rounded-xl text-[10px] font-black uppercase tracking-widest transition-all flex items-center justify-center gap-2 group min-w-[120px]">
                    <svg class="w-3.5 h-3.5 group-hover:scale-110 transition-transform" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
                    Allow
                  </button>
                  <button @click="handleVerdict(m.metadata.request_id, 'allow_always')" 
                          :disabled="!!workspace.archived_at"
                          :class="workspace.archived_at ? 'opacity-30 cursor-not-allowed' : 'hover:bg-black hover:text-white active:scale-95 shadow-lg shadow-black/10'"
                          class="flex-1 py-3 bg-zinc-100 text-zinc-600 border border-zinc-200 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all flex items-center justify-center gap-2 group min-w-[120px]">
                    <svg class="w-3.5 h-3.5 group-hover:scale-110 transition-transform" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                    Allow Always
                  </button>
                  <button @click="handleVerdict(m.metadata.request_id, 'deny')" 
                          :disabled="!!workspace.archived_at"
                          :class="workspace.archived_at ? 'opacity-30 cursor-not-allowed' : 'hover:bg-red-50 hover:text-red-600 hover:border-red-100 active:scale-95'"
                          class="px-5 py-3 bg-white text-gray-500 border border-gray-100/80 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all flex items-center justify-center gap-2 group">
                    <svg class="w-3.5 h-3.5 group-hover:scale-110 transition-transform" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M6 18L18 6M6 6l12 12" /></svg>
                    Deny
                  </button>
                </div>
                <div v-else 
                     @click="m._detailsExpanded = !m._detailsExpanded"
                     class="rounded-xl border cursor-pointer transition-all duration-300 select-none"
                     :class="[
                       m.metadata.status === 'allow' || m.metadata.status === 'allow_always' 
                         ? 'bg-emerald-50/80 border-emerald-100/50 hover:border-emerald-200' 
                         : 'bg-red-50/80 border-red-100/50 hover:border-red-200'
                     ]">
                  <!-- Compact 1-line summary -->
                  <div class="flex items-center gap-2.5 px-4 py-3">
                    <svg v-if="m.metadata.status === 'allow' || m.metadata.status === 'allow_always'" class="w-3.5 h-3.5 text-emerald-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
                    <svg v-else class="w-3.5 h-3.5 text-red-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M6 18L18 6M6 6l12 12" /></svg>
                    
                    <div class="flex-1 min-w-0 flex items-center gap-2">
                      <span class="text-[11px] font-bold truncate"
                            :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-emerald-700' : 'text-red-700'">
                        {{ m.metadata.tool_name }}
                      </span>
                      <span v-if="m.metadata.description" class="text-[10px] text-gray-400 truncate hidden sm:inline">
                        — {{ m.metadata.description }}
                      </span>
                    </div>

                    <span v-if="m.metadata.status === 'allow_always'" 
                          class="text-[7px] font-black text-emerald-500 uppercase tracking-widest bg-emerald-100 px-1.5 py-0.5 rounded shrink-0">
                      Auto
                    </span>

                    <svg class="w-3 h-3 shrink-0 transition-transform duration-200"
                         :class="[
                           m._detailsExpanded ? 'rotate-180' : '',
                           m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-emerald-300' : 'text-red-300'
                         ]"
                         fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                    </svg>
                  </div>

                  <!-- Expandable details -->
                  <div v-if="m._detailsExpanded" class="px-4 pb-4 pt-1 border-t animate-in fade-in slide-in-from-top-1 duration-200"
                       :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'border-emerald-100/50' : 'border-red-100/50'">
                    <div class="flex flex-col gap-2.5">
                      <div v-if="m.metadata.description" class="flex flex-col gap-1">
                        <span class="text-[8px] font-black uppercase tracking-widest opacity-40">Description</span>
                        <p class="text-[11px] text-gray-600 italic leading-relaxed">"{{ m.metadata.description }}"</p>
                      </div>
                      <div v-if="m.metadata.input_preview" class="flex flex-col gap-1">
                        <span class="text-[8px] font-black uppercase tracking-widest opacity-40">Input Preview</span>
                        <pre class="text-[9px] font-mono bg-zinc-900 text-zinc-300 p-3 rounded-lg overflow-x-auto border border-zinc-800">{{ m.metadata.input_preview }}</pre>
                      </div>
                      <div class="flex items-center justify-between">
                        <span class="text-[8px] font-bold text-gray-300 uppercase tracking-tighter">ID: {{ m.metadata.request_id }}</span>
                        <span class="text-[9px] font-black uppercase tracking-widest"
                              :class="m.metadata.status === 'allow' || m.metadata.status === 'allow_always' ? 'text-emerald-500' : 'text-red-500'">
                          {{ m.metadata.status === 'deny' ? 'Denied' : m.metadata.status === 'allow_always' ? 'Always Allowed' : 'Allowed' }}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Extra Compact Message Attachments -->
              <div v-if="m.attachments && m.attachments.length > 0" class="flex flex-wrap gap-1.5 mt-2.5">
                <div v-for="(att, i) in m.attachments" :key="i" 
                     @click="previewAttachment(att)"
                     class="flex items-center gap-2 px-2.5 py-1 rounded-xl bg-gray-50 border border-gray-100 hover:border-indigo-300 hover:bg-white hover:shadow-md transition-all cursor-pointer group/att shadow-sm">
                  
                  <div class="w-6 h-6 rounded-lg bg-white overflow-hidden flex items-center justify-center border border-gray-100 group-hover/att:text-indigo-600 transition-colors">
                    <img v-if="att.mimeType?.startsWith('image/')" :src="getAttachmentUrl(workspaceId, att.id)" class="w-full h-full object-cover" />
                    <svg v-else-if="att.mimeType?.startsWith('audio/') || att.mimeType?.startsWith('video/')" class="w-3.5 h-3.5 text-indigo-500" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                    <svg v-else-if="att.mimeType?.includes('pdf')" class="w-3.5 h-3.5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path></svg>
                    <svg v-else class="w-3 h-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                  </div>

                  <div class="flex flex-col min-w-0 pr-1">
                    <span class="text-[9px] font-bold text-gray-700 truncate max-w-[150px]">{{ att.filename }}</span>
                    <span class="text-[7.5px] font-medium text-gray-400 uppercase tracking-tighter">{{ att.mimeType?.split('/')[1] || 'FILE' }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </template>
        

      </section>
    </div>

    <!-- Chat Box / Attachment Area -->
    <footer v-if="!workspace.archived_at" class="bg-white border-t border-gray-100 py-6 md:py-8 z-20">
      <div class="flex flex-col gap-4">
        <!-- New Attachments Preview -->
        <div v-if="replyAttachments.length > 0" class="flex flex-wrap gap-2 animate-in fade-in duration-300">
          <div v-for="(att, i) in replyAttachments" :key="i" class="flex items-center text-[10px] bg-indigo-600 text-white border border-indigo-500 px-4 py-1.5 rounded-full shadow-lg font-black tracking-tight transform-all hover:scale-105 transition-transform">
            <span class="truncate max-w-[180px]">{{ att.filename }}</span>
            <button @click="replyAttachments.splice(i, 1)" class="ml-2.5 hover:text-white/70 transition-colors">
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="3"><path d="M6 18L18 6M6 6l12 12"></path></svg>
            </button>
          </div>
        </div>

        <form @submit.prevent="submitReply" class="relative">
          <div class="flex flex-col bg-zinc-50 border border-gray-200 focus-within:border-indigo-600 focus-within:bg-white focus-within:ring-4 focus-within:ring-indigo-100 transition-all duration-300 rounded-[2rem] overflow-hidden shadow-sm">
             <div class="flex items-end px-4 py-3 gap-3">
                <button type="button" @click="$refs.fileInput.click()" class="p-3 rounded-2xl text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-all shrink-0 bg-white border border-gray-100 shadow-sm">
                  <svg class="w-5.5 h-5.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path></svg>
                </button>
                <input type="file" ref="fileInput" multiple class="hidden" @change="handleFileUpload" />
                
                <textarea 
                  v-model="replyText" 
                  @keydown.enter.exact.prevent="submitReply"
                  placeholder="Ask Agent a question or provide feedback..." 
                  class="flex-1 bg-transparent border-0 outline-none focus:ring-0 resize-none py-3 px-1 min-h-[48px] max-h-64 text-[15px] font-medium text-gray-800 placeholder-gray-400 no-scrollbar select-text leading-snug"
                ></textarea>
                
                <button type="submit" 
                        :disabled="!replyText.trim() && replyAttachments.length === 0"
                        class="p-3.5 rounded-2xl bg-black text-white hover:bg-indigo-600 disabled:opacity-20 transition-all shadow-xl flex-shrink-0 mb-0.5 hover:scale-110 active:scale-95">
                  <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"></path></svg>
                </button>
             </div>
          </div>
        </form>
        <div class="flex items-center justify-center gap-2 mt-1 px-4">
           <div class="h-px bg-gray-100 grow"></div>
           <span class="text-[9px] font-black text-gray-300 uppercase tracking-[0.2em] whitespace-nowrap">Secure Agent Sync Enabled</span>
           <div class="h-px bg-gray-100 grow"></div>
        </div>
      </div>
    </footer>

    <!-- Full Attachment Preview Modal -->
    <div v-if="selectedAtt" class="fixed inset-0 z-[100] flex items-center justify-center" @keydown.esc="selectedAtt = null">
      <div class="absolute inset-0 bg-black/90 backdrop-blur-xl animate-in fade-in duration-300" @click="selectedAtt = null"></div>
      
      <!-- Close Button -->
      <button @click="selectedAtt = null" class="absolute top-8 right-8 text-white/40 hover:text-white transition-colors z-20 hover:scale-110 active:scale-95">
        <svg class="w-12 h-12" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path d="M6 18L18 6M6 6l12 12"></path></svg>
      </button>

      <div class="relative max-w-[90vw] max-h-[85vh] flex flex-col items-center animate-in zoom-in-95 duration-500">
          <!-- Content View -->
          <div class="bg-white/5 p-4 rounded-3xl border border-white/10 shadow-2xl overflow-hidden mb-6 flex items-center justify-center min-w-[300px]">
             <!-- Image -->
             <img v-if="selectedAtt.mimeType?.startsWith('image/')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh] rounded-2xl object-scale-down" />
             
             <!-- Video -->
             <video v-else-if="selectedAtt.mimeType?.startsWith('video/')" controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="max-w-full max-h-[70vh] rounded-2xl" />
             
             <!-- Audio -->
             <div v-else-if="selectedAtt.mimeType?.startsWith('audio/')" class="p-12 flex flex-col items-center gap-6">
               <div class="w-24 h-24 bg-indigo-500 rounded-full flex items-center justify-center shadow-lg shadow-indigo-500/20">
                  <svg class="w-12 h-12 text-white" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
               </div>
               <audio controls autoplay :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[400px]" />
             </div>
             
             <!-- PDF (using iframe or direct link warning) -->
             <iframe v-else-if="selectedAtt.mimeType?.includes('pdf')" :src="getAttachmentUrl(workspaceId, selectedAtt.id)" class="w-[80vw] h-[75vh] rounded-2xl" frameborder="0"></iframe>
             
             <!-- Other -->
             <div v-else class="p-20 flex flex-col items-center gap-6">
                <svg class="w-32 h-32 text-white/20" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1"><path d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" /></svg>
                <div class="text-center">
                  <p class="text-white text-lg font-black uppercase tracking-widest">{{ selectedAtt.filename }}</p>
                  <p class="text-white/40 text-[10px] font-bold mt-1 tracking-tighter uppercase">{{ selectedAtt.mimeType }}</p>
                </div>
             </div>
          </div>

          <!-- Metadata & Actions -->
          <div class="flex items-center gap-6 px-10 py-5 bg-white rounded-full shadow-2xl border border-gray-100">
             <div class="flex flex-col">
                <p class="text-xs font-black text-gray-900 truncate max-w-[300px]">{{ selectedAtt.filename }}</p>
                <p class="text-[9px] font-bold text-gray-400 mt-0.5 tracking-tighter uppercase">{{ selectedAtt.mimeType }}</p>
             </div>
             <div class="h-8 w-px bg-gray-100"></div>
             <a :href="getAttachmentUrl(workspaceId, selectedAtt.id)" :download="selectedAtt.filename" class="flex items-center gap-2 px-6 py-2.5 bg-black text-white rounded-full text-[10px] font-black uppercase tracking-widest hover:bg-zinc-800 transition-all active:scale-95 shadow-lg">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"></path></svg>
                Download
             </a>
          </div>
      </div>
    </div>
  </div>
  
  <!-- Loading State -->
  <div v-else class="h-full flex flex-col items-center justify-center bg-zinc-50 font-inter">
     <div class="relative mb-8">
        <div class="w-20 h-20 border-4 border-indigo-100 border-t-indigo-600 rounded-full animate-spin"></div>
        <div class="absolute inset-0 flex items-center justify-center font-black text-indigo-600 text-xs tracking-tighter uppercase">RQ</div>
     </div>
     <h2 class="text-xl font-black text-gray-900 tracking-tight">Syncing Task Context</h2>
     <p class="text-sm text-gray-400 font-medium mt-2">Retrieving workspace and message history...</p>
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
  try {
    const res = await respondToTask(workspaceId, taskId, 'text', text, atts);
    task.value = res.task;
  } catch(err) {
    notifyError("Failed to deliver message: " + err.message);
    replyText.value = text;
    replyAttachments.value = atts;
  }
}

function formatDateTime(dateStr) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
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

onMounted(load);
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
