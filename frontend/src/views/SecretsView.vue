<template>
  <div class="min-h-screen bg-white flex flex-col w-full max-w-full overflow-x-hidden">
    <header class="py-3 border-b-2 border-black shrink-0 flex items-center justify-between gap-4 bg-white sticky top-0 z-30">
      <div class="flex items-center gap-2 text-xs font-black uppercase tracking-widest min-w-0 flex-1">
        <span class="text-black truncate flex-1 min-w-0 text-sm">Security & Secrets</span>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto pt-4 md:pt-6 pb-12 px-0 md:px-4 scroll-smooth custom-scrollbar">
      <div class="w-full max-w-4xl mx-auto space-y-4 md:space-y-8">

        <div class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
          <div class="bg-black px-4 py-2 flex items-center justify-between">
            <div class="flex items-center gap-2">
              <div class="w-5 h-5 bg-[#00FF88] text-black flex items-center justify-center text-[9px] font-black">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                </svg>
              </div>
              <span class="text-[10px] font-black text-white uppercase tracking-widest">Active Secrets</span>
            </div>
          </div>
          <div class="p-6">
            <div v-if="loading" class="text-xs font-bold text-gray-500 uppercase tracking-widest">Loading...</div>
            <div v-else-if="secrets.length === 0" class="text-xs font-bold text-gray-500 uppercase tracking-widest text-center py-8">
              No secrets configured
            </div>
            <div v-else class="space-y-3">
              <div v-for="secret in secrets" :key="secret.id" class="flex items-center justify-between p-4 border-2 border-black bg-gray-50">
                <div class="flex flex-col">
                  <span class="text-xs font-black uppercase tracking-widest">{{ secret.key }}</span>
                  <span class="text-[10px] text-gray-500 font-bold uppercase tracking-wider mt-1">Configured: {{ new Date(secret.updated_at).toLocaleDateString() }}</span>
                </div>
                <div class="px-3 py-1 bg-[#00FF88] text-black text-[9px] font-black uppercase tracking-widest border border-black">
                  Secure
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] bg-white">
          <div class="bg-black px-4 py-2 flex items-center justify-between">
            <div class="flex items-center gap-2">
              <div class="w-5 h-5 bg-[#00FF88] text-black flex items-center justify-center text-[9px] font-black">+</div>
              <span class="text-[10px] font-black text-white uppercase tracking-widest">Add or Update Secret</span>
            </div>
          </div>
          <div class="p-6">
            <form @submit.prevent="submitSecret" class="space-y-6">
              <div class="flex flex-col gap-2">
                 <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Secret Key</label>
                 <select v-model="form.key"
                         class="w-full bg-white border-2 border-black px-4 py-3 text-sm outline-none font-bold text-gray-900 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all"
                         required>
                    <option value="" disabled>Select a key type</option>
                    <option value="GOOGLE_JULES_API_KEY">GOOGLE_JULES_API_KEY</option>
                 </select>
              </div>
              <div class="flex flex-col gap-2">
                 <label class="text-[10px] font-black text-gray-400 uppercase tracking-[0.2em]">Secret Value</label>
                 <input type="password" v-model="form.value"
                        placeholder="Enter the secure value..."
                        class="w-full bg-white border-2 border-black px-4 py-3 text-sm outline-none font-bold text-gray-900 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all placeholder:text-gray-300"
                        required />
              </div>
              <div class="pt-2">
                <button type="submit"
                        :disabled="sending || !form.key || !form.value"
                        class="bg-black text-[#00FF88] px-8 py-4 border-2 border-black text-xs font-black uppercase tracking-[0.2em] hover:bg-[#00FF88] hover:text-black transition-all shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] hover:translate-y-[2px] hover:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[4px] disabled:opacity-50">
                  {{ sending ? 'Saving...' : 'Save Secret' }}
                </button>
              </div>
            </form>
          </div>
        </div>

      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { fetchSecrets, createSecret } from '../api';
import { useToasts } from '../composables/useToasts';

const { notifyError, notifySuccess } = useToasts();
const secrets = ref([]);
const loading = ref(true);
const sending = ref(false);

const form = ref({
  key: '',
  value: ''
});

async function loadSecrets() {
  try {
    loading.value = true;
    const res = await fetchSecrets();
    secrets.value = res.secrets || [];
  } catch (err) {
    notifyError("Failed to load secrets: " + err.message);
  } finally {
    loading.value = false;
  }
}

async function submitSecret() {
  sending.value = true;
  try {
    await createSecret(form.value.key, form.value.value);
    notifySuccess('Secret securely stored');
    form.value.key = '';
    form.value.value = '';
    await loadSecrets();
  } catch (err) {
    notifyError("Dispatch Error: " + err.message);
  } finally {
    sending.value = false;
  }
}

onMounted(() => {
  loadSecrets();
});
</script>