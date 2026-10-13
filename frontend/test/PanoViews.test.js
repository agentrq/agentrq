// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The Pano's two pages, mounted: the board of post-its, and one doc with its
 * editor (the "note studio"). The rules they follow are tested in
 * `usePano.test.js`; this is the wiring — what each click, key and live update
 * does on the page.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(),
  listPanoDocs: vi.fn(),
  getPanoDoc: vi.fn(),
  getTask: vi.fn(),
  createPanoDoc: vi.fn(),
  updatePanoDoc: vi.fn(),
  deletePanoDoc: vi.fn(),
  addPanoComment: vi.fn(),
  attachPanoFiles: vi.fn(),
  createTask: vi.fn(),
  ensureSupervisorWorkspace: vi.fn(),
  fetchEvents: vi.fn(),
  fetchWorkflows: vi.fn(),
  fetchWorkspaceSession: vi.fn(),
}));
vi.mock('../src/api', () => api);

// Voice input, faked: it dictates a word into the text it was given.
vi.mock('../src/composables/useSpeechToText', async () => {
  const { ref } = await import('vue');
  return {
    useSpeechToText: (target) => ({
      isRecording: ref(false),
      isTranscribing: ref(false),
      isModelLoading: ref(false),
      modelProgress: ref(0),
      isSupported: ref(true),
      toggleRecording: () => { target.value = `${target.value} dictated`; },
    }),
  };
});

// The event stream, faked: the page's handlers are kept so a test can send
// them an event.
const bus = vi.hoisted(() => ({ handlers: [], connect: vi.fn(), disconnect: vi.fn() }));
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({
    connect: bus.connect,
    disconnect: bus.disconnect,
    onEvent: (fn) => {
      bus.handlers.push(fn);
      return () => {};
    },
  }),
}));

const { default: PanoView } = await import('../src/views/PanoView.vue');
const { default: PanoDocView } = await import('../src/views/PanoDocView.vue');
const { useWorkspaceStore } = await import('../src/stores/workspaceStore');
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges');
const { PANO_COLORS, PANO_MAX_FILE_BYTES, panoTilt, isPanoChatRoute } = await import('../src/composables/usePano');
const { default: PanoChatColumn } = await import('../src/components/PanoChatColumn.vue');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

// The app's `v-click-outside`, as `src/app.js` registers it.
const clickOutside = {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!(el === event.target || el.contains(event.target))) binding.value(event);
    };
    document.body.addEventListener('click', el._clickOutside);
  },
  unmounted(el) {
    document.body.removeEventListener('click', el._clickOutside);
  },
};

const settle = () => new Promise((r) => setTimeout(r, 0));
const apps = [];
const WORKSPACES = [{ id: 'ws1', name: 'Home' }, { id: 'ws2', name: 'ops' }];
const HOUR = 60 * 60 * 1000;

async function mount(at, workspaces = WORKSPACES) {
  const el = document.createElement('div');
  document.body.append(el);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/pano', component: PanoView },
      { path: '/pano/chat/:workspaceId/:taskId', components: { default: PanoView, chat: { render() { return h('p', { 'data-test': 'pano-chat-task' }, `${this.$route.params.workspaceId}/${this.$route.params.taskId}`); } } } },
      { path: '/workspaces/:id', component: { render: () => null } },
      { path: '/pano/new', component: PanoDocView },
      { path: '/pano/:id', component: PanoDocView },
      { path: '/pano/:docId/chat/:workspaceId/:taskId', components: { default: PanoDocView, chat: { render: () => null } } },
      { path: '/workspaces/:id/tasks/:taskId', component: { render: () => null } },
    ],
  });
  await router.push(at);
  // As App.vue does: a new doc path is a new page, the board and its chats
  // are one, and a chat gets its own column.
  const app = createApp({
    render: () => h('div', [
      isPanoChatRoute(router.currentRoute.value) ? h(PanoChatColumn) : null,
      h(RouterView, null, { default: ({ Component, route }) => Component && h(Component, { key: route.path.startsWith('/pano/chat') ? '/pano' : route.path }) }),
    ]),
  });
  app.directive('click-outside', clickOutside);
  const pinia = createPinia();
  app.use(pinia);
  app.use(router);
  useWorkspaceStore(pinia).workspaces = workspaces;
  app.mount(el);
  apps.push(app);
  await settle();
  return { el, router };
}

const $ = (el, test) => el.querySelector(`[data-test="${test}"]`);
const $$ = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)];
const text = (node) => node.textContent.replace(/\s+/g, ' ').trim();
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  await settle();
};
const type = async (node, value) => {
  node.value = value;
  node.dispatchEvent(new Event('input'));
  await settle();
};
const key = async (target, init) => {
  const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
  target.dispatchEvent(event);
  await settle();
  return event;
};
const emit = async (event) => {
  for (const handler of bus.handlers) handler(event);
  await settle();
};
const pick = async (input, files) => {
  Object.defineProperty(input, 'files', { value: files, configurable: true });
  input.dispatchEvent(new Event('change'));
  await settle();
};
const deferred = () => {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};
const lastToast = () => toasts.value.at(-1);
// The dialog leaves on an animation frame.
const dialogGone = async () => {
  await new Promise((r) => setTimeout(r, 50));
  return document.querySelector('[role="dialog"]') === null;
};
const modalButton = (label) => [...document.querySelectorAll('[role="dialog"] button')].find((b) => text(b) === label);

/** A browser File as far as the Pano reads one. */
function fakeFile(name, { type: mime = 'text/plain', size } = {}) {
  const data = new Uint8Array([104, 105]);
  return { name, type: mime, size: size ?? data.length, arrayBuffer: () => Promise.resolve(data.buffer) };
}

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  toasts.value = [];
  bus.handlers.length = 0;
  vi.clearAllMocks();
  api.getTask.mockRejectedValue(new Error('not read'));
  api.ensureSupervisorWorkspace.mockResolvedValue({ workspace: { id: 'sup', name: 'supervisor' } });
  api.fetchWorkspaces.mockResolvedValue({ workspaces: WORKSPACES });
});

describe('the board', () => {
  const now = Date.now();
  const DOCS = [
    { id: 'd1', title: 'Release plan', excerpt: '- [ ] ship\n- [x] test\n\nmore', color: 'pink', updatedAt: new Date(now - 2 * HOUR).toISOString(), expiresAt: new Date(now + 3 * 24 * HOUR + 60000).toISOString(), checklistDone: 1, checklistTotal: 2 },
    { id: 'd2', title: 'Agent notes', excerpt: 'from an agent', color: 'blue', createdByWorkspaceId: 'ws2', updatedByWorkspaceId: 'ws2', updatedAt: new Date(now).toISOString(), expiresAt: new Date(now).toISOString() },
    { id: 'd3', title: 'Odd paper', excerpt: '', color: 'magenta', updatedAt: new Date(now).toISOString(), expiresAt: new Date(now).toISOString() },
  ];

  beforeEach(() => {
    api.listPanoDocs.mockImplementation(() => Promise.resolve({ docs: DOCS.map((d) => ({ ...d })) }));
    api.getPanoDoc.mockImplementation((id) => Promise.resolve({ doc: { id, title: 'Release plan', markdown: '- [ ] ship\n- [x] test' } }));
    api.updatePanoDoc.mockImplementation((id, body) => Promise.resolve({ doc: { id, ...body } }));
    api.deletePanoDoc.mockImplementation(() => Promise.resolve(true));
  });

  it('shows that it is loading, then the cards in their colors', async () => {
    const list = deferred();
    api.listPanoDocs.mockReturnValueOnce(list.promise);
    const { el } = await mount('/pano');
    expect(text(el)).toContain('Loading the Pano…');
    expect($$(el, 'pano-card')).toHaveLength(0);
    list.resolve({ docs: DOCS });
    await settle();
    expect(text(el)).not.toContain('Loading the Pano…');
    const cards = $$(el, 'pano-card');
    expect(cards.map((c) => text(c.querySelector('h2')))).toEqual(['Release plan', 'Agent notes', 'Odd paper']);
    expect(cards[0].classList.contains(PANO_COLORS[1].card.split(' ')[0])).toBe(true);
    expect(cards[2].classList.contains(PANO_COLORS[0].card)).toBe(true);
    expect(cards[0].style.getPropertyValue('--pano-ink')).toBe(PANO_COLORS[1].inkHex);
    // Every card leans by the angle its id picks, and straightens on hover.
    expect(cards[0].style.getPropertyValue('--pano-tilt')).toBe(`${panoTilt('d1')}deg`);
    expect(cards[0].className).toContain('rotate-[var(--pano-tilt)] hover:rotate-0');
    expect(text($(el, 'pano-new-card'))).toContain('Pick its paper, or press N');
    expect($(el, 'pano-open').getAttribute('href')).toBe('/pano/d1');
    expect($(el, 'pano-card-edit').getAttribute('href')).toBe('/pano/d1?edit=1');
    expect($(el, 'pano-new-pink').getAttribute('href')).toBe('/pano/new?color=pink');
  });

  it('names who wrote each card and when, and shows its checklist', async () => {
    const { el } = await mount('/pano');
    const [mine, theirs] = $$(el, 'pano-card');
    expect(text(mine.querySelector('footer'))).toContain('You · 2h ago');
    expect(mine.querySelector('footer span').textContent).toBe('Y');
    expect(mine.querySelector('footer [title^="Expires"]').getAttribute('title')).toBe('Expires in 3 days');
    expect(text(theirs.querySelector('footer'))).toContain('ops · just now');
    expect(theirs.querySelector('footer span').textContent).toBe('O');
    expect(mine.querySelector('[data-test="pano-progress"]')).not.toBeNull();
    expect(theirs.querySelector('[data-test="pano-progress"]')).toBeNull();
    // The card's boxes can be ticked.
    expect([...mine.querySelectorAll('[data-test="pano-excerpt"] input')].map((b) => b.disabled)).toEqual([false, false]);
  });

  it('offers the to-task button only on docs the person wrote', async () => {
    const { el } = await mount('/pano');
    expect($$(el, 'pano-card').map((c) => !!c.querySelector('[data-test="pano-to-task"]'))).toEqual([true, false, true]);
  });

  it('says what failed', async () => {
    api.listPanoDocs.mockRejectedValueOnce(new Error('board down'));
    const { el } = await mount('/pano');
    expect(text($(el, 'pano-error'))).toBe('board down');
    expect($(el, 'pano-new-card')).toBeNull();
  });

  it('starts an empty board with the new note, and no filter summary', async () => {
    api.listPanoDocs.mockResolvedValueOnce({ docs: [] });
    const { el } = await mount('/pano');
    expect($$(el, 'pano-card')).toHaveLength(0);
    expect(text($(el, 'pano-new-card'))).toContain('Nothing on the Pano yet.');
    await type($(el, 'pano-search'), 'x');
    expect($(el, 'pano-filter-summary')).toBeNull();
    expect($(el, 'pano-no-match')).toBeNull();
  });

  it('filters by title, says how many it shows, and resets', async () => {
    const { el } = await mount('/pano');
    await type($(el, 'pano-search'), '   ');
    expect($(el, 'pano-filter-summary')).toBeNull();
    await type($(el, 'pano-search'), 'plan');
    expect(text($(el, 'pano-filter-summary'))).toContain('Showing 1 of 3');
    expect($$(el, 'pano-card')).toHaveLength(1);
    await type($(el, 'pano-search'), 'zzz');
    expect(text($(el, 'pano-no-match'))).toBe('No doc\'s title matches “zzz”.');
    await click($(el, 'pano-filter-reset'));
    expect($(el, 'pano-search').value).toBe('');
    expect($(el, 'pano-filter-summary')).toBeNull();
    expect($$(el, 'pano-card')).toHaveLength(3);
  });

  it('filters by color with the pills, with the title too, and resets both', async () => {
    const { el } = await mount('/pano');
    expect($(el, 'pano-filter-all').getAttribute('aria-checked')).toBe('true');
    await click($(el, 'pano-filter-blue'));
    expect($(el, 'pano-filter-blue').getAttribute('aria-checked')).toBe('true');
    expect($(el, 'pano-filter-blue').className).toContain('bg-[#d97706]');
    expect($(el, 'pano-filter-all').getAttribute('aria-checked')).toBe('false');
    expect($$(el, 'pano-card').map((c) => text(c.querySelector('h2')))).toEqual(['Agent notes']);
    expect(text($(el, 'pano-filter-summary'))).toContain('Showing 1 of 3');
    await type($(el, 'pano-search'), 'plan');
    expect(text($(el, 'pano-no-match'))).toBe('No doc\'s title matches “plan” in this color.');
    await click($(el, 'pano-filter-reset'));
    expect($$(el, 'pano-card')).toHaveLength(3);
    await click($(el, 'pano-filter-amber'));
    expect(text($(el, 'pano-no-match'))).toBe('No doc in this color.');
    // A picked pill unpicks on a second click; All picks every color.
    await click($(el, 'pano-filter-amber'));
    expect($$(el, 'pano-card')).toHaveLength(3);
    await click($(el, 'pano-filter-pink'));
    await click($(el, 'pano-filter-all'));
    expect($$(el, 'pano-card')).toHaveLength(3);
  });

  it('starts a note on N, either case', async () => {
    const { el, router } = await mount('/pano');
    const event = await key(document.body, { key: 'n' });
    expect(event.defaultPrevented).toBe(true);
    await settle();
    expect(router.currentRoute.value.path).toBe('/pano/new');
    await router.push('/pano');
    await settle();
    await key(window, { key: 'N' });
    expect(router.currentRoute.value.path).toBe('/pano/new');
    expect(el).toBeDefined();
  });

  it('ignores N while typing, with a modifier, and any other key', async () => {
    const { el, router } = await mount('/pano');
    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    const inner = document.createElement('span');
    editable.append(inner);
    document.body.append(editable);
    for (const target of [$(el, 'pano-search'), inner]) {
      expect((await key(target, { key: 'n' })).defaultPrevented).toBe(false);
    }
    for (const mod of ['metaKey', 'ctrlKey', 'altKey']) {
      expect((await key(document.body, { key: 'n', [mod]: true })).defaultPrevented).toBe(false);
    }
    expect((await key(document.body, { key: 'm' })).defaultPrevented).toBe(false);
    expect(router.currentRoute.value.path).toBe('/pano');
  });

  it('stops listening for N once it is gone', async () => {
    const { router } = await mount('/pano');
    await router.push('/pano/d1');
    await settle();
    expect(bus.disconnect).toHaveBeenCalled();
    await router.push('/pano/new');
    await settle();
    // Back on a doc page, N is just a letter.
    await router.push('/pano/d1');
    await settle();
    expect((await key(document.body, { key: 'n' })).defaultPrevented).toBe(false);
  });

  it('opens and closes a card\'s color menu, and closes it on a click elsewhere', async () => {
    const { el } = await mount('/pano');
    const [first, second] = $$(el, 'pano-card');
    const button = first.querySelector('[data-test="pano-card-color"]');
    await click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
    const menu = first.querySelector('[data-test="pano-card-colors"]');
    expect([...menu.querySelectorAll('button')].map((b) => b.getAttribute('aria-checked'))).toEqual(['false', 'true', 'false', 'false', 'false', 'false', 'false']);
    await click(button);
    expect(first.querySelector('[data-test="pano-card-colors"]')).toBeNull();
    await click(button);
    // Opening another card's menu closes this one.
    await click(second.querySelector('[data-test="pano-card-color"]'));
    expect(first.querySelector('[data-test="pano-card-colors"]')).toBeNull();
    expect(second.querySelector('[data-test="pano-card-colors"]')).not.toBeNull();
    await click(document.body);
    expect($(el, 'pano-card-colors')).toBeNull();
  });

  it('recolors a card and reloads the board', async () => {
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-color'));
    await click($(el, 'pano-card-color-green'));
    expect(api.getPanoDoc).toHaveBeenCalledWith('d1');
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ color: 'green' }));
    expect(api.listPanoDocs).toHaveBeenCalledTimes(2);
    expect($(el, 'pano-card-colors')).toBeNull();
  });

  it('does nothing for the color the card already has', async () => {
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-color'));
    await click($(el, 'pano-card-color-pink'));
    expect(api.getPanoDoc).not.toHaveBeenCalled();
    expect(api.updatePanoDoc).not.toHaveBeenCalled();
    expect($(el, 'pano-card-colors')).toBeNull();
  });

  it('says why a recolor failed', async () => {
    api.updatePanoDoc.mockRejectedValueOnce(new Error('cannot recolor'));
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-color'));
    await click($(el, 'pano-card-color-blue'));
    expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot recolor' });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(1);
  });

  it('deletes a card after asking, and reloads', async () => {
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-delete'));
    expect(text(document.querySelector('[role="dialog"]'))).toContain('Delete \'Release plan\' with its files and comments?');
    await click(modalButton('Delete'));
    expect(api.deletePanoDoc).toHaveBeenCalledWith('d1');
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Doc deleted' });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(2);
    expect(await dialogGone()).toBe(true);
  });

  it('keeps a card when the dialog is dismissed', async () => {
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-delete'));
    await click(modalButton('Cancel'));
    expect(await dialogGone()).toBe(true);
    expect(api.deletePanoDoc).not.toHaveBeenCalled();
  });

  it('says why a delete failed', async () => {
    api.deletePanoDoc.mockRejectedValueOnce(new Error('cannot delete'));
    const { el } = await mount('/pano');
    await click($(el, 'pano-card-delete'));
    await click(modalButton('Delete'));
    expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot delete' });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(1);
  });

  it('ticks a card\'s checkbox without opening the doc, and reloads', async () => {
    const { el, router } = await mount('/pano');
    const boxes = el.querySelectorAll('[data-test="pano-excerpt"] input');
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    boxes[0].dispatchEvent(event);
    await settle();
    expect(event.defaultPrevented).toBe(true);
    expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ markdown: '- [x] ship\n- [x] test' }));
    expect(api.listPanoDocs).toHaveBeenCalledTimes(2);
    expect(router.currentRoute.value.path).toBe('/pano');

    // A ticked box unticks: the call carries the box's new state.
    await click(el.querySelectorAll('[data-test="pano-excerpt"] input')[1]);
    expect(api.updatePanoDoc).toHaveBeenLastCalledWith('d1', expect.objectContaining({ markdown: '- [ ] ship\n- [ ] test' }));
  });

  it('opens the doc, ticking nothing, for a click elsewhere in the excerpt', async () => {
    const { el, router } = await mount('/pano');
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    $(el, 'pano-excerpt').querySelector('p').dispatchEvent(event);
    await settle();
    expect(api.updatePanoDoc).not.toHaveBeenCalled();
    expect(router.currentRoute.value.path).toBe('/pano/d1');
  });

  it('says why a tick failed', async () => {
    api.getPanoDoc.mockRejectedValueOnce(new Error('cannot tick'));
    const { el } = await mount('/pano');
    await click(el.querySelector('[data-test="pano-excerpt"] input'));
    expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot tick' });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(1);
  });

  it('reloads quietly when the Pano changes, and on a browser agent\'s change', async () => {
    const { el } = await mount('/pano');
    expect(bus.connect).toHaveBeenCalled();
    await emit({ type: 'task.updated' });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(1);
    const list = deferred();
    api.listPanoDocs.mockReturnValueOnce(list.promise);
    await emit({ type: 'pano.changed', payload: { id: 'd9' } });
    expect(api.listPanoDocs).toHaveBeenCalledTimes(2);
    expect(text(el)).not.toContain('Loading the Pano…');
    list.resolve({ docs: [DOCS[1]] });
    await settle();
    expect($$(el, 'pano-card')).toHaveLength(1);
    notifyWebMCPChange();
    await settle();
    expect(api.listPanoDocs).toHaveBeenCalledTimes(3);
  });
});

describe('the chat box\'s task options', () => {
  const SUP = { id: 'sup', name: 'supervisor', agentConnected: true };
  const sent = () => api.createTask.mock.calls[0];
  const change = async (node, value) => {
    node.value = value;
    node.dispatchEvent(new Event('change'));
    await settle();
  };
  const send = async (el, message = 'hello') => {
    await type($(el, 'pano-chat-input'), message);
    await key($(el, 'pano-chat-input'), { key: 'Enter', metaKey: true });
  };

  beforeEach(() => {
    api.listPanoDocs.mockResolvedValue({ docs: [] });
    api.createTask.mockImplementation((workspaceId, title, body, a, f, status) => Promise.resolve({ task: { id: 't1', workspaceId, status } }));
    api.fetchEvents.mockResolvedValue({ events: [{ id: 'e1', name: 'shipped' }] });
    api.fetchWorkflows.mockResolvedValue({ workflows: [{ id: 'w1', name: 'release', startEventId: 'e1' }] });
    api.fetchWorkspaceSession.mockResolvedValue(null);
  });

  it('are offered only while the agent is there, and send a plain task as the form does', async () => {
    const offline = await mount('/pano', [...WORKSPACES, { ...SUP, agentConnected: false }]);
    expect($(offline.el, 'task-attach')).toBeNull();

    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    for (const control of ['task-attach', 'task-assignee-agent', 'task-assignee-human', 'task-yolo', 'task-schedule', 'task-completion']) {
      expect($(el, control)).not.toBeNull();
    }
    expect($(el, 'task-clear-context')).toBeNull();
    await send(el);
    expect(sent().slice(0, 1)).toEqual(['sup']);
    expect(sent().slice(3)).toEqual(['agent', [], 'notstarted', '', false, '', '', false]);
  });

  it('start from the supervisor\'s own settings', async () => {
    api.fetchWorkspaceSession.mockResolvedValue({ id: 'sess', kind: 'claude-code', status: 'running' });
    const { el } = await mount('/pano', [...WORKSPACES, { ...SUP, allowAllCommands: true, clearContextDefault: true }]);
    await send(el);
    expect(sent().slice(3)).toEqual(['agent', [], 'notstarted', '', true, '', '', true]);
  });

  it('take what voice input dictates into the message', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await type($(el, 'pano-chat-input'), 'hello');
    await click([...$(el, 'pano-chat').querySelectorAll('button')].find((b) => b.querySelector('svg path[d^="M19 11"]')));
    expect($(el, 'pano-chat-input').value).toBe('hello dictated');
    await key($(el, 'pano-chat-input'), { key: 'Enter', metaKey: true });
    expect(sent()[1]).toBe('hello dictated');
  });

  it('send the assignee, and drop the agent-only controls for a person', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-assignee-human'));
    expect($(el, 'task-yolo')).toBeNull();
    await send(el);
    expect(sent()[3]).toBe('human');
  });

  it('send allow-all-commands and clear-context when they are switched on', async () => {
    api.fetchWorkspaceSession.mockResolvedValue({ id: 'sess', kind: 'claude-code', status: 'running' });
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-yolo'));
    await click($(el, 'task-clear-context'));
    await send(el);
    expect(sent()[7]).toBe(true);
    expect(sent()[10]).toBe(true);
  });

  it('send the files chosen, which can be taken back, and clear them with the message', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await pick($(el, 'task-file-input'), [new File(['one'], 'one.txt', { type: 'text/plain' }), new File(['two'], 'two.txt', { type: 'text/plain' })]);
    await new Promise((r) => setTimeout(r, 30));
    expect($$(el, 'attachment-chip').map(text)).toEqual(['one.txt', 'two.txt']);
    await click($$(el, 'attachment-remove')[0]);
    expect($$(el, 'attachment-chip').map(text)).toEqual(['two.txt']);
    await send(el);
    expect(sent()[4]).toEqual([{ filename: 'two.txt', mimeType: 'text/plain', data: btoa('two') }]);
  });

  it('send a repeated schedule as a scheduled task, and keep the board rather than open a chat', async () => {
    const { el, router } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-schedule'));
    await click($(el, 'task-schedule-repeated'));
    await change($(el, 'task-repeat-preset'), 'hourly');
    await send(el);
    expect(sent().slice(5, 7)).toEqual(['cron', '0 * * * *']);
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Task scheduled' });
    expect(router.currentRoute.value.fullPath).toBe('/pano');
  });

  it('send a one-time schedule', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-schedule'));
    await click($(el, 'task-schedule-onetime'));
    await type($(el, 'task-onetime-date'), '2030-03-04T05:06');
    await send(el);
    const d = new Date('2030-03-04T05:06');
    expect(sent()[5]).toBe('cron');
    expect(sent()[6]).toBe(`${d.getUTCMinutes()} ${d.getUTCHours()} ${d.getUTCDate()} ${d.getUTCMonth() + 1} *`);
  });

  it('send the event to run on completion', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-completion'));
    await change($(el, 'task-event'), 'e1');
    await send(el);
    expect(sent().slice(8, 10)).toEqual(['e1', '']);
  });

  it('send a workflow instead of the event picked before it, and the reverse', async () => {
    const { el } = await mount('/pano', [...WORKSPACES, SUP]);
    await click($(el, 'task-completion'));
    await change($(el, 'task-event'), 'e1');
    await click($(el, 'task-completion-workflow'));
    await change($(el, 'task-workflow'), 'w1');
    await send(el);
    expect(sent().slice(8, 10)).toEqual(['', 'w1']);
  });
});

describe('the chat with the supervisor', () => {
  const OFFLINE = [...WORKSPACES, { id: 'sup', name: 'supervisor', agentConnected: false }];
  const ONLINE = [...WORKSPACES, { id: 'sup', name: 'supervisor', agentConnected: true }];

  beforeEach(() => {
    api.listPanoDocs.mockResolvedValue({ docs: [] });
    api.createTask.mockImplementation((workspaceId, title) => Promise.resolve({ task: { id: 't1', workspaceId, title } }));
  });

  it('is turned off while the supervisor\'s agent is away, and points at connecting it', async () => {
    const { el } = await mount('/pano', OFFLINE);
    const input = $(el, 'pano-chat-input');
    expect(input.disabled).toBe(true);
    expect(input.placeholder).toBe('Connect your supervisor agent to start chatting');
    expect($(el, 'pano-chat').querySelector('button[type=submit]').disabled).toBe(true);
    expect(text($(el, 'pano-chat-offline'))).toContain('Connect your supervisor agent to start chatting');
    expect($(el, 'pano-chat-connect').getAttribute('href')).toBe('/workspaces/sup');
    expect(api.ensureSupervisorWorkspace).not.toHaveBeenCalled();
  });

  it('turns on as soon as the agent connects', async () => {
    const { el } = await mount('/pano', OFFLINE);
    useWorkspaceStore().updateAgentStatus('sup', true);
    await settle();
    expect($(el, 'pano-chat-input').disabled).toBe(false);
    expect($(el, 'pano-chat-input').placeholder).toBe('Ask supervisor about the Pano... (Cmd ⌘ + Enter to send)');
    expect($(el, 'pano-chat-offline')).toBeNull();
  });

  it('makes the supervisor again when it was deleted, and reads the workspaces back', async () => {
    await mount('/pano');
    expect(api.ensureSupervisorWorkspace).toHaveBeenCalledTimes(1);
    expect(api.fetchWorkspaces).toHaveBeenCalledTimes(1);
  });

  it('says why the supervisor could not be made', async () => {
    api.ensureSupervisorWorkspace.mockRejectedValueOnce(new Error('Failed to set up the supervisor workspace'));
    await mount('/pano');
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to set up the supervisor workspace' });
  });

  it('sends on Cmd+Enter as a supervisor task that says where it came from, and opens it beside the board', async () => {
    const { el, router } = await mount('/pano', ONLINE);
    await type($(el, 'pano-chat-input'), 'Post a release checklist\nfor Friday');
    const event = await key($(el, 'pano-chat-input'), { key: 'Enter', metaKey: true });
    expect(event.defaultPrevented).toBe(true);
    expect(api.createTask).toHaveBeenCalledTimes(1);
    const [workspaceId, title, body] = api.createTask.mock.calls[0];
    expect(workspaceId).toBe('sup');
    expect(title).toBe('Post a release checklist');
    expect(body).toContain('Post a release checklist\nfor Friday');
    expect(body).toContain('Sent from the Pano chat');
    await settle();
    expect(router.currentRoute.value.fullPath).toBe('/pano/chat/sup/t1');
    expect(text($(el, 'pano-chat-task'))).toBe('sup/t1');
    expect($(el, 'pano-chat')).toBeNull();
    expect($$(el, 'pano-new-card')).toHaveLength(1);
  });

  it('sends with Ctrl+Enter and the button too, and keeps Enter for a new line', async () => {
    const { el, router } = await mount('/pano', ONLINE);
    expect($(el, 'pano-chat').querySelector('button[type=submit]').disabled).toBe(true);
    await type($(el, 'pano-chat-input'), 'hello');
    expect($(el, 'pano-chat').querySelector('button[type=submit]').disabled).toBe(false);
    const event = await key($(el, 'pano-chat-input'), { key: 'Enter' });
    expect(event.defaultPrevented).toBe(false);
    expect(api.createTask).not.toHaveBeenCalled();
    await key($(el, 'pano-chat-input'), { key: 'Enter', ctrlKey: true });
    expect(api.createTask).toHaveBeenCalledTimes(1);
    await router.push('/pano');
    await settle();
    await type($(el, 'pano-chat-input'), 'again');
    $(el, 'pano-chat').dispatchEvent(new Event('submit', { cancelable: true }));
    await settle();
    expect(api.createTask).toHaveBeenCalledTimes(2);
  });

  it('sends nothing for an empty box', async () => {
    const { el, router } = await mount('/pano', ONLINE);
    await key($(el, 'pano-chat-input'), { key: 'Enter', metaKey: true });
    expect(api.createTask).not.toHaveBeenCalled();
    expect(router.currentRoute.value.fullPath).toBe('/pano');
  });

  it('says why a message was not sent, and keeps it', async () => {
    api.createTask.mockRejectedValueOnce(new Error('Failed to create task'));
    const { el, router } = await mount('/pano', ONLINE);
    await type($(el, 'pano-chat-input'), 'hello');
    await key($(el, 'pano-chat-input'), { key: 'Enter', metaKey: true });
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to create task' });
    expect($(el, 'pano-chat-input').value).toBe('hello');
    expect(router.currentRoute.value.fullPath).toBe('/pano');
  });

  it('opens a chat from its address in a column of its own, and closes back to the same board', async () => {
    const { el, router } = await mount('/pano/chat/sup/t9', ONLINE);
    expect(text(el.querySelector('[data-pano-chat] [data-test="pano-chat-task"]'))).toBe('sup/t9');
    expect($(el, 'pano-new-card')).not.toBeNull();
    expect($(el, 'pano-chat')).toBeNull();
    await router.push('/pano');
    await settle();
    expect(el.querySelector('[data-pano-chat]')).toBeNull();
    expect($(el, 'pano-chat')).not.toBeNull();
    expect(api.listPanoDocs).toHaveBeenCalledTimes(1);
  });
});

describe('a doc', () => {
  const DOC = {
    id: 'd1',
    title: 'Release plan',
    markdown: '# Plan\n\n- [ ] ship\n- [x] test\n\n![chart](chart.png)',
    ttlDays: 7,
    color: 'green',
    updatedAt: '2026-10-13T12:00:00Z',
    expiresAt: new Date(Date.now() + 2 * 24 * HOUR + 60000).toISOString(),
    updatedByWorkspaceId: 'ws1',
    checklistDone: 1,
    checklistTotal: 2,
    files: [{ id: 'f1', filename: 'chart.png', url: 'https://x/storage/chart', size: 2048 }],
    attachments: [{ id: 'a1', filename: 'log.txt', url: 'https://x/storage/log', size: 10, workspaceId: 'ws2' }],
    comments: [{ id: 'c1', body: '**nice**', workspaceId: 'ws2', createdAt: '2026-10-13T12:00:00Z' }],
  };

  beforeEach(() => {
    api.getPanoDoc.mockImplementation((id) => (id === 'd1'
      ? Promise.resolve({ doc: { ...DOC } })
      : Promise.reject(Object.assign(new Error('not found'), { status: 404 }))));
    api.createPanoDoc.mockImplementation(() => Promise.resolve({ doc: { id: 'new1' } }));
    api.updatePanoDoc.mockImplementation(() => Promise.resolve({ doc: { id: 'd1' } }));
    api.deletePanoDoc.mockImplementation(() => Promise.resolve(true));
    api.addPanoComment.mockImplementation(() => Promise.resolve({}));
    api.attachPanoFiles.mockImplementation(() => Promise.resolve({}));
  });

  describe('reading it', () => {
    it('shows that it is loading, then the note, its files, attachments and comments', async () => {
      const got = deferred();
      api.getPanoDoc.mockReturnValueOnce(got.promise);
      const { el } = await mount('/pano/d1');
      expect(text(el)).toContain('Loading…');
      got.resolve({ doc: DOC });
      await settle();
      expect(text($(el, 'pano-title'))).toBe('Release plan');
      expect(text($(el, 'pano-meta'))).toMatch(/^By You · updated \w{3} \d+, \d\d:\d\d by Home$/);
      expect($(el, 'pano-body').querySelector('img').getAttribute('src')).toBe('https://x/storage/chart');
      expect([...$(el, 'pano-body').querySelectorAll('input')].map((b) => b.disabled)).toEqual([false, false]);
      expect($(el, 'pano-progress')).not.toBeNull();
      expect(text(el)).toContain('Expires in 2 days');
      expect(text(el)).toContain('PaperMint green');
      expect(text(el)).toContain('chart.png2.0 KB');
      expect(text($(el, 'pano-attachments'))).toBe('log.txt10 B · ops');
      expect($(el, 'pano-comments').innerHTML).toContain('<strong>nice</strong>');
      expect(text($(el, 'pano-comments'))).toContain('ops ·');
      expect($(el, 'pano-to-task')).not.toBeNull();
      expect($(el, 'pano-editor')).toBeNull();
      expect(bus.connect).toHaveBeenCalled();
    });

    it('says when there is nothing linked, attached or said yet, and no to-task for an agent\'s doc', async () => {
      api.getPanoDoc.mockResolvedValueOnce({ doc: { ...DOC, createdByWorkspaceId: 'ws2', files: [], attachments: undefined, comments: [] } });
      const { el } = await mount('/pano/d1');
      expect(text(el)).not.toContain('Linked files');
      expect(text(el)).toContain('No attachments.');
      expect(text(el)).toContain('No comments yet.');
      expect(text($(el, 'pano-meta'))).toMatch(/^By ops/);
      expect($(el, 'pano-to-task')).toBeNull();
    });

    it('says a missing doc is missing or expired, and offers nothing to do with it', async () => {
      const { el } = await mount('/pano/gone?edit=1');
      expect(text($(el, 'pano-doc-error'))).toBe('This Pano doc does not exist, or it has expired.');
      expect($(el, 'pano-editor')).toBeNull();
      expect($(el, 'pano-edit')).toBeNull();
    });

    it('opens straight in the editor from the board\'s edit button', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      expect($(el, 'pano-editor')).not.toBeNull();
      expect(text($(el, 'pano-title'))).toBe('Edit note');
      expect(text($(el, 'pano-editor'))).toContain('Pano · Release plan');
      expect($(el, 'pano-title-input').value).toBe('Release plan');
      expect($(el, 'pano-ttl-input').value).toBe('7');
      expect(text($(el, 'pano-save'))).toBe('Save changes');
      expect($(el, 'pano-editor-delete')).not.toBeNull();
    });

    it('ticks a checkbox in the note, and reads it back', async () => {
      const { el } = await mount('/pano/d1');
      const event = new MouseEvent('click', { bubbles: true, cancelable: true });
      $(el, 'pano-body').querySelectorAll('input')[1].dispatchEvent(event);
      await settle();
      expect(event.defaultPrevented).toBe(true);
      expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ markdown: expect.stringContaining('- [ ] ship\n- [ ] test'), color: '', ttlDays: 0 }));
      expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    });

    it('ignores a click elsewhere in the note, and says why a tick failed', async () => {
      const { el } = await mount('/pano/d1');
      await click($(el, 'pano-body').querySelector('h1'));
      expect(api.updatePanoDoc).not.toHaveBeenCalled();
      api.updatePanoDoc.mockRejectedValueOnce(new Error('cannot tick'));
      await click($(el, 'pano-body').querySelector('input'));
      expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot tick' });
    });

    it('comments, and clears the box', async () => {
      const { el } = await mount('/pano/d1');
      expect($(el, 'pano-comment').disabled).toBe(true);
      await type($(el, 'pano-comment-input'), '  looks good  ');
      expect($(el, 'pano-comment').disabled).toBe(false);
      await click($(el, 'pano-comment'));
      expect(api.addPanoComment).toHaveBeenCalledWith('d1', 'looks good');
      expect($(el, 'pano-comment-input').value).toBe('');
      expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    });

    it('sends no blank comment, and keeps a comment that failed', async () => {
      const { el } = await mount('/pano/d1');
      await type($(el, 'pano-comment-input'), '   ');
      $(el, 'pano-comment').closest('form').dispatchEvent(new Event('submit', { cancelable: true }));
      await settle();
      expect(api.addPanoComment).not.toHaveBeenCalled();
      api.addPanoComment.mockRejectedValueOnce(new Error('cannot comment'));
      await type($(el, 'pano-comment-input'), 'hello');
      await click($(el, 'pano-comment'));
      expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot comment' });
      expect($(el, 'pano-comment-input').value).toBe('hello');
    });

    it('pings a workspace @mentioned in a comment, picked from the suggestions', async () => {
      api.createTask.mockResolvedValue({ task: { id: 't1' } });
      const { el } = await mount('/pano/d1');
      const input = $(el, 'pano-comment-input');
      await type(input, 'ask @o');
      expect($$(el, 'pano-mention-option').map(text)).toEqual(['@ops', '@Home']);
      expect($$(el, 'pano-mention-option')[0].getAttribute('aria-selected')).toBe('true');
      await key(input, { key: 'ArrowDown' });
      expect($$(el, 'pano-mention-option')[1].getAttribute('aria-selected')).toBe('true');
      await key(input, { key: 'ArrowUp' });
      await key(input, { key: 'ArrowUp' });
      expect($$(el, 'pano-mention-option')[1].getAttribute('aria-selected')).toBe('true');
      $$(el, 'pano-mention-option')[0].dispatchEvent(new MouseEvent('mouseenter'));
      await settle();
      expect((await key(input, { key: 'Enter' })).defaultPrevented).toBe(true);
      expect(input.value).toBe('ask @ops ');
      expect($(el, 'pano-mention-menu')).toBeNull();
      await type(input, 'ask @ops to look');
      await key(input, { key: 'Enter', metaKey: true });
      expect(api.addPanoComment).toHaveBeenCalledWith('d1', 'ask @ops to look');
      expect(api.createTask).toHaveBeenCalledWith('ws2', 'Pano: Release plan', expect.stringContaining('> ask @ops to look'));
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Pinged 1 workspace' });
      expect(input.value).toBe('');
    });

    it('picks a mention with Tab or a click, closes on Esc or blur, and pings several', async () => {
      api.createTask.mockResolvedValue({ task: { id: 't1' } });
      const { el } = await mount('/pano/d1');
      const input = $(el, 'pano-comment-input');
      await type(input, '@H');
      expect((await key(input, { key: 'o' })).defaultPrevented).toBe(false);
      await key(input, { key: 'Tab' });
      expect(input.value).toBe('@Home ');
      await type(input, '@Home @');
      $$(el, 'pano-mention-option')[1].dispatchEvent(new MouseEvent('mousedown', { cancelable: true }));
      await settle();
      expect(input.value).toBe('@Home @ops ');
      await type(input, '@Home @ops @');
      await key(input, { key: 'Escape' });
      expect($(el, 'pano-mention-menu')).toBeNull();
      await type(input, '@Home @ops @');
      input.dispatchEvent(new Event('blur'));
      await settle();
      expect($(el, 'pano-mention-menu')).toBeNull();
      await type(input, '@Home @ops @nobody');
      expect($(el, 'pano-mention-menu')).toBeNull();
      expect((await key(input, { key: 'Enter' })).defaultPrevented).toBe(false);
      await key(input, { key: 'Enter', ctrlKey: true });
      // The server makes one task a second, so the second ping waits.
      await new Promise((r) => setTimeout(r, 1200));
      expect(api.createTask).toHaveBeenCalledTimes(2);
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Pinged 2 workspaces' });
    });

    it('says which workspace it could not ping, and still clears the posted comment', async () => {
      api.createTask.mockRejectedValueOnce(new Error('rate limit exceeded'));
      const { el } = await mount('/pano/d1');
      const input = $(el, 'pano-comment-input');
      await type(input, 'hey @ops');
      await key(input, { key: 'Escape' });
      await click($(el, 'pano-comment'));
      expect(lastToast()).toMatchObject({ type: 'error', message: 'Could not ping ops: rate limit exceeded' });
      expect(input.value).toBe('');
    });

    it('highlights the workspaces a comment mentions', async () => {
      api.getPanoDoc.mockResolvedValueOnce({ doc: { ...DOC, comments: [{ id: 'c1', body: 'thanks @ops', workspaceId: 'ws1', createdAt: '2026-10-13T12:00:00Z' }] } });
      const { el } = await mount('/pano/d1');
      expect($(el, 'pano-comments').querySelector('.pano-mention').textContent).toBe('@ops');
    });

    it('assigns a checklist item to a workspace from its menu', async () => {
      api.createTask.mockResolvedValue({ task: { id: 't7' } });
      const { el, router } = await mount('/pano/d1');
      const buttons = $(el, 'pano-body').querySelectorAll('[data-pano-assign]');
      expect(buttons).toHaveLength(2);
      await click(buttons[0]);
      expect(buttons[0].getAttribute('aria-expanded')).toBe('true');
      expect(text($(el, 'pano-item-menu'))).toBe('Assign toHomeops');
      // Another item's button moves the menu there.
      await click(buttons[1]);
      expect(buttons[0].getAttribute('aria-expanded')).toBe('false');
      expect($$(el, 'pano-item-menu')).toHaveLength(1);
      await click($$(el, 'pano-item-assign')[1]);
      expect(api.createTask).toHaveBeenCalledWith('ws2', 'test', expect.stringContaining('A checklist item of the Pano doc `d1`'));
      expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ markdown: expect.stringContaining('- [x] test → [@ops](/workspaces/ws2/tasks/t7 "notstarted")') }));
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Assigned to ops' });
      expect($(el, 'pano-item-menu')).toBeNull();
      expect(router.currentRoute.value.path).toBe('/pano/d1');
    });

    it('shows an assigned item\'s task and status, opens it, and says why an assignment failed', async () => {
      api.getPanoDoc.mockResolvedValue({ doc: { ...DOC, markdown: '- [ ] ship → [@ops](/workspaces/ws2/tasks/t7 "ongoing")' } });
      const { el, router } = await mount('/pano/d1');
      const chip = $(el, 'pano-body').querySelector('a.pano-assignee');
      expect(chip.dataset.status).toBe('ongoing');
      expect(chip.title).toBe('Ongoing · task t7');
      await click($(el, 'pano-body').querySelector('[data-pano-assign]'));
      expect(text($(el, 'pano-item-task'))).toBe('@opsOngoingt7');
      expect(text($(el, 'pano-item-menu'))).toContain('Reassign to');
      await key($(el, 'pano-item-menu'), { key: 'Escape' });
      expect($(el, 'pano-item-menu')).toBeNull();
      await click($(el, 'pano-body').querySelector('[data-pano-assign]'));
      await click($(el, 'pano-title'));
      expect($(el, 'pano-item-menu')).toBeNull();
      await click($(el, 'pano-body').querySelector('[data-pano-assign]'));
      api.createTask.mockRejectedValueOnce(new Error('cannot assign'));
      await click($$(el, 'pano-item-assign')[0]);
      expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot assign' });
      await click(chip);
      await settle();
      expect(router.currentRoute.value.path).toBe('/workspaces/ws2/tasks/t7');
    });

    it('opens an assigned task beside the doc on a large screen, from the chip and from the menu', async () => {
      vi.stubGlobal('matchMedia', () => ({ matches: true }));
      try {
        api.getPanoDoc.mockResolvedValue({ doc: { ...DOC, markdown: '- [ ] ship → [@ops](/workspaces/ws2/tasks/t7 "ongoing")' } });
        const { el, router } = await mount('/pano/d1');
        await click($(el, 'pano-body').querySelector('a.pano-assignee'));
        await settle();
        expect(router.currentRoute.value.path).toBe('/pano/d1/chat/ws2/t7');
        expect($(el, 'pano-body')).not.toBeNull();
        await click($(el, 'pano-body').querySelector('[data-pano-assign]'));
        await click($(el, 'pano-item-task'));
        await settle();
        expect(router.currentRoute.value.path).toBe('/pano/d1/chat/ws2/t7');
      } finally {
        vi.unstubAllGlobals();
      }
    });

    it('lists the tasks a doc points at above its comments, once each, and opens one beside the doc', async () => {
      vi.stubGlobal('matchMedia', () => ({ matches: true }));
      try {
        api.getPanoDoc.mockResolvedValue({ doc: { ...DOC, markdown: '- [ ] ship → [@ops](/workspaces/ws2/tasks/t7 "ongoing")\n\nSee [the fix](/workspaces/ws1/tasks/t9) and [again](/workspaces/ws1/tasks/t9).' } });
        api.getTask.mockImplementation(async (ws, id) => {
          if (id === 't7') throw new Error('gone');
          return { task: { id, title: 'Fix the long thing' } };
        });
        const { el, router } = await mount('/pano/d1');
        await settle();
        const links = $$(el, 'pano-related-task');
        // The title of a task that can be read; the link's own text for one that cannot.
        expect(links.map((a) => a.textContent)).toEqual(['@ops', 'Fix the long thing']);
        expect(api.getTask).toHaveBeenCalledTimes(2);
        // A status shows as its dot, named for screen readers, not as a word.
        expect(text($(el, 'pano-related'))).not.toContain('Ongoing');
        expect($$(el, 'pano-related-status').map((d) => [d.closest('li').dataset.status, d.getAttribute('aria-label')]))
          .toEqual([['ongoing', 'Ongoing'], ['notstarted', 'Not started']]);
        await click(links[1]);
        await settle();
        expect(router.currentRoute.value.path).toBe('/pano/d1/chat/ws1/t9');
      } finally {
        vi.unstubAllGlobals();
      }
    });

    it('lists no related tasks when the doc points at none', async () => {
      const { el } = await mount('/pano/d1');
      expect($(el, 'pano-related')).toBeNull();
    });

    it('offers no workspace to assign to when there is none', async () => {
      const { el } = await mount('/pano/d1');
      useWorkspaceStore().workspaces = [];
      await click($(el, 'pano-body').querySelector('[data-pano-assign]'));
      expect(text($(el, 'pano-item-menu'))).toBe('Assign toNo workspaces yet.');
    });

    it('attaches one file or several, and says so', async () => {
      const { el } = await mount('/pano/d1');
      const input = $(el, 'pano-attach-input');
      const opened = vi.spyOn(input, 'click');
      await click($(el, 'pano-attach'));
      expect(opened).toHaveBeenCalled();
      await pick(input, [fakeFile('a.txt')]);
      expect(api.attachPanoFiles).toHaveBeenCalledWith('d1', [{ filename: 'a.txt', mimeType: 'text/plain', data: 'aGk=' }]);
      expect(lastToast()).toMatchObject({ type: 'success', message: 'File attached' });
      await pick(input, [fakeFile('a.txt'), fakeFile('b.txt')]);
      expect(api.attachPanoFiles).toHaveBeenCalledTimes(2);
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Files attached' });
      expect(input.value).toBe('');
    });

    it('attaches nothing when no file was picked, and says why an attach failed', async () => {
      const { el } = await mount('/pano/d1');
      const input = $(el, 'pano-attach-input');
      await pick(input, []);
      expect(api.attachPanoFiles).not.toHaveBeenCalled();
      expect(toasts.value).toEqual([]);
      await pick(input, [fakeFile('big.bin', { size: PANO_MAX_FILE_BYTES + 1 })]);
      expect(lastToast()).toMatchObject({ type: 'error', message: 'big.bin is over the 2.0 MB limit' });
      expect(api.attachPanoFiles).not.toHaveBeenCalled();
    });

    it('deletes the doc after asking, and goes back to the board', async () => {
      const { el, router } = await mount('/pano/d1');
      await click($(el, 'pano-delete'));
      expect(text(document.querySelector('[role="dialog"]'))).toContain('Delete \'Release plan\'');
      await click(modalButton('Delete'));
      expect(api.deletePanoDoc).toHaveBeenCalledWith('d1');
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Doc deleted' });
      expect(router.currentRoute.value.path).toBe('/pano');
    });

    it('does not read back its own delete', async () => {
      const removed = deferred();
      api.deletePanoDoc.mockReturnValueOnce(removed.promise);
      const { el } = await mount('/pano/d1');
      await click($(el, 'pano-delete'));
      await click(modalButton('Delete'));
      await emit({ type: 'pano.changed', payload: { id: 'd1' } });
      expect(api.getPanoDoc).toHaveBeenCalledTimes(1);
      removed.resolve(true);
      await settle();
    });

    it('stays on a doc whose delete failed, and reads it back again on a change', async () => {
      api.deletePanoDoc.mockRejectedValueOnce(new Error('cannot delete'));
      const { el, router } = await mount('/pano/d1');
      await click($(el, 'pano-delete'));
      await click(modalButton('Delete'));
      expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot delete' });
      expect(router.currentRoute.value.path).toBe('/pano/d1');
      await emit({ type: 'pano.changed', payload: { id: 'd1' } });
      expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
    });

    it('keeps the doc when the dialog is dismissed', async () => {
      const { el } = await mount('/pano/d1');
      await click($(el, 'pano-delete'));
      await click(modalButton('Cancel'));
      expect(await dialogGone()).toBe(true);
      expect(api.deletePanoDoc).not.toHaveBeenCalled();
    });

    it('reads the doc back when it changes, but not for another doc, another event, or over an edit', async () => {
      const { el } = await mount('/pano/d1');
      await emit({ type: 'pano.changed', payload: { id: 'd2' } });
      await emit({ type: 'pano.changed' });
      await emit({ type: 'task.updated', payload: { id: 'd1' } });
      expect(api.getPanoDoc).toHaveBeenCalledTimes(1);
      await emit({ type: 'pano.changed', payload: { id: 'd1' } });
      expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
      expect(text(el)).not.toContain('Loading…');
      notifyWebMCPChange();
      await settle();
      expect(api.getPanoDoc).toHaveBeenCalledTimes(3);
      await click($(el, 'pano-edit'));
      await emit({ type: 'pano.changed', payload: { id: 'd1' } });
      notifyWebMCPChange();
      await settle();
      expect(api.getPanoDoc).toHaveBeenCalledTimes(3);
    });
  });

  describe('the editor', () => {
    it('opens on Edit with the doc as it is, in its color, and cancels', async () => {
      const { el } = await mount('/pano/d1');
      await click($(el, 'pano-edit'));
      expect($(el, 'pano-editor')).not.toBeNull();
      expect($(el, 'pano-markdown-input').value).toBe(DOC.markdown);
      expect(text($(el, 'pano-color-label'))).toBe('Mint green');
      expect($(el, 'pano-color-green').getAttribute('aria-checked')).toBe('true');
      await click($(el, 'pano-cancel'));
      expect($(el, 'pano-editor')).toBeNull();
      expect(text($(el, 'pano-title'))).toBe('Release plan');
    });

    it('changes the paper', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      await click($(el, 'pano-color-purple'));
      expect(text($(el, 'pano-color-label'))).toBe(PANO_COLORS[5].label);
      expect($(el, 'pano-color-purple').getAttribute('aria-checked')).toBe('true');
      expect($(el, 'pano-color-green').getAttribute('aria-checked')).toBe('false');
    });

    it('switches between Write and Preview', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      expect($(el, 'pano-write-tab').getAttribute('aria-selected')).toBe('true');
      expect($(el, 'pano-preview')).toBeNull();
      await click($(el, 'pano-preview-tab'));
      expect($(el, 'pano-preview-tab').getAttribute('aria-selected')).toBe('true');
      expect($(el, 'pano-preview').querySelector('h1').textContent).toBe('Plan');
      expect($(el, 'pano-markdown-input').style.display).toBe('none');
      await click($(el, 'pano-write-tab'));
      expect($(el, 'pano-preview')).toBeNull();
      expect($(el, 'pano-markdown-input').style.display).toBe('');
    });

    it('saves on ⌘S or Ctrl+S, and reads the doc back', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      await type($(el, 'pano-title-input'), 'Release plan v2');
      await type($(el, 'pano-ttl-input'), '14');
      const event = await key($(el, 'pano-markdown-input'), { key: 's', metaKey: true });
      expect(event.defaultPrevented).toBe(true);
      expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ title: 'Release plan v2', ttlDays: 14, color: 'green', files: [] }));
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Doc saved' });
      expect($(el, 'pano-editor')).toBeNull();
      expect(api.getPanoDoc).toHaveBeenCalledTimes(2);
      await click($(el, 'pano-edit'));
      await key($(el, 'pano-title-input'), { key: 'S', ctrlKey: true });
      expect(api.updatePanoDoc).toHaveBeenCalledTimes(2);
    });

    it('sends the version it read, and on a conflict keeps the draft, says so, and offers the latest', async () => {
      api.getPanoDoc.mockImplementation((id) => Promise.resolve({ doc: { id, title: 'Release plan', markdown: '- [ ] ship', color: 'green', ttlDays: 7, version: 3 } }));
      api.updatePanoDoc.mockRejectedValueOnce(Object.assign(new Error('the Pano doc changed since you read it'), { status: 409 }));
      const { el } = await mount('/pano/d1?edit=1');
      expect($(el, 'pano-conflict')).toBeNull();
      await type($(el, 'pano-title-input'), 'My title');
      await click($(el, 'pano-save'));
      expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ title: 'My title', version: 3 }));
      expect(lastToast()).toMatchObject({ type: 'error', message: expect.stringContaining('This note changed while you were editing it') });
      expect(text($(el, 'pano-conflict'))).toContain('changed while you were editing it');
      expect($(el, 'pano-title-input').value).toBe('My title');

      api.getPanoDoc.mockImplementation((id) => Promise.resolve({ doc: { id, title: 'Theirs', markdown: '- [x] ship', color: 'green', ttlDays: 7, version: 4 } }));
      await click($(el, 'pano-conflict-reload'));
      expect($(el, 'pano-conflict')).toBeNull();
      expect($(el, 'pano-title-input').value).toBe('Theirs');
      await click($(el, 'pano-save'));
      expect(api.updatePanoDoc).toHaveBeenLastCalledWith('d1', expect.objectContaining({ title: 'Theirs', version: 4 }));
    });

    it('saves with the button, and stays in the editor when it fails', async () => {
      api.updatePanoDoc.mockRejectedValueOnce(new Error('file x.png is not linked'));
      const { el } = await mount('/pano/d1?edit=1');
      await click($(el, 'pano-save'));
      expect(lastToast()).toMatchObject({ type: 'error', message: 'file x.png is not linked' });
      expect($(el, 'pano-editor')).not.toBeNull();
      expect($(el, 'pano-save').disabled).toBe(false);
    });

    it('leaves the editor on Esc, and ignores other keys', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      expect((await key($(el, 'pano-markdown-input'), { key: 's' })).defaultPrevented).toBe(false);
      expect((await key($(el, 'pano-markdown-input'), { key: 'x', metaKey: true })).defaultPrevented).toBe(false);
      expect($(el, 'pano-editor')).not.toBeNull();
      const event = await key($(el, 'pano-markdown-input'), { key: 'Escape' });
      expect(event.defaultPrevented).toBe(true);
      expect($(el, 'pano-editor')).toBeNull();
      expect(api.updatePanoDoc).not.toHaveBeenCalled();
    });

    it('will not save without a title or without markdown', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      await type($(el, 'pano-title-input'), '   ');
      expect($(el, 'pano-save').disabled).toBe(true);
      await key($(el, 'pano-title-input'), { key: 's', metaKey: true });
      expect(api.updatePanoDoc).not.toHaveBeenCalled();
      await type($(el, 'pano-title-input'), 'T');
      expect($(el, 'pano-save').disabled).toBe(false);
      await type($(el, 'pano-markdown-input'), '  \n ');
      expect($(el, 'pano-save').disabled).toBe(true);
      await key($(el, 'pano-title-input'), { key: 's', ctrlKey: true });
      expect(api.updatePanoDoc).not.toHaveBeenCalled();
    });

    it('wraps the selection in bold, italic or code, and keeps it selected', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      const area = $(el, 'pano-markdown-input');
      await type(area, 'say hi now');
      area.setSelectionRange(4, 6);
      await click($(el, 'pano-format-bold'));
      expect(area.value).toBe('say **hi** now');
      expect([area.selectionStart, area.selectionEnd]).toEqual([6, 8]);
      expect(document.activeElement).toBe(area);
      await click($(el, 'pano-format-italic'));
      expect(area.value).toBe('say **_hi_** now');
      expect([area.selectionStart, area.selectionEnd]).toEqual([7, 9]);
      await click($(el, 'pano-format-code'));
      expect(area.value).toBe('say **_`hi`_** now');
    });

    it('starts lines as list or checklist items, from Preview too', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      const area = $(el, 'pano-markdown-input');
      await type(area, 'one\ntwo');
      area.setSelectionRange(0, 7);
      await click($(el, 'pano-format-list'));
      expect(area.value).toBe('- one\n- two');
      await click($(el, 'pano-preview-tab'));
      area.setSelectionRange(0, 0);
      await click($(el, 'pano-format-checkbox'));
      expect($(el, 'pano-preview')).toBeNull();
      expect(area.value).toBe('- [ ] - one\n- two');
      expect([area.selectionStart, area.selectionEnd]).toEqual([6, 6]);
    });

    it('adds files to the draft, linked from the note, and removes them', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      const input = $(el, 'pano-draft-file');
      const opened = vi.spyOn(input, 'click');
      await click($(el, 'pano-add-file'));
      expect(opened).toHaveBeenCalled();
      await pick(input, [fakeFile('my chart.png', { type: 'image/png' }), fakeFile('notes.txt')]);
      expect(input.value).toBe('');
      const area = $(el, 'pano-markdown-input');
      expect(area.value).toBe(`${DOC.markdown}\n\n![my-chart.png](my-chart.png)\n\n[notes.txt](notes.txt)`);
      const items = () => [...$(el, 'pano-editor').querySelectorAll('li')];
      expect(items().map((li) => li.querySelector('span').textContent)).toEqual(['my-chart.png', 'notes.txt']);
      await click(items()[0].querySelector('button'));
      expect(items().map((li) => li.querySelector('span').textContent)).toEqual(['notes.txt']);
      await key(area, { key: 's', metaKey: true });
      expect(api.updatePanoDoc).toHaveBeenCalledWith('d1', expect.objectContaining({ files: [{ filename: 'notes.txt', mimeType: 'text/plain', data: 'aGk=' }] }));
    });

    it('refuses a draft file over the limit, and still adds the rest', async () => {
      const { el } = await mount('/pano/d1?edit=1');
      await pick($(el, 'pano-draft-file'), [fakeFile('huge.bin', { size: PANO_MAX_FILE_BYTES + 1 }), fakeFile('ok.txt')]);
      expect(lastToast()).toMatchObject({ type: 'error', message: 'huge.bin is over the 2.0 MB limit' });
      expect([...$(el, 'pano-editor').querySelectorAll('li span')].map((s) => s.textContent)).toEqual(['ok.txt']);
    });

    it('deletes from the editor', async () => {
      const { el, router } = await mount('/pano/d1?edit=1');
      await click($(el, 'pano-editor-delete'));
      await click(modalButton('Delete'));
      expect(api.deletePanoDoc).toHaveBeenCalledWith('d1');
      expect(router.currentRoute.value.path).toBe('/pano');
    });
  });

  describe('a new note', () => {
    it('opens in the editor, in yellow, and reads nothing', async () => {
      const { el } = await mount('/pano/new');
      expect(api.getPanoDoc).not.toHaveBeenCalled();
      expect(bus.connect).not.toHaveBeenCalled();
      expect(text($(el, 'pano-title'))).toBe('New note');
      expect(text($(el, 'pano-editor'))).toContain('Pano · every workspace can read it');
      expect(text($(el, 'pano-color-label'))).toBe('Canary yellow');
      expect(text($(el, 'pano-save'))).toBe('Post note');
      expect($(el, 'pano-save').disabled).toBe(true);
      expect($(el, 'pano-editor-delete')).toBeNull();
      expect($(el, 'pano-ttl-input').value).toBe('30');
      await click($(el, 'pano-preview-tab'));
      expect(text($(el, 'pano-preview'))).toBe('Nothing written yet.');
      // A browser agent's change has nothing to read back.
      notifyWebMCPChange();
      await settle();
      expect(api.getPanoDoc).not.toHaveBeenCalled();
    });

    it('starts in the paper picked on the board', async () => {
      const { el } = await mount('/pano/new?color=pink');
      expect(text($(el, 'pano-color-label'))).toBe(PANO_COLORS[1].label);
      expect($(el, 'pano-color-pink').getAttribute('aria-checked')).toBe('true');
    });

    it('posts the note and opens it', async () => {
      const { el, router } = await mount('/pano/new?color=blue');
      await type($(el, 'pano-title-input'), 'Fresh');
      await type($(el, 'pano-markdown-input'), 'Body');
      await click($(el, 'pano-save'));
      expect(api.createPanoDoc).toHaveBeenCalledWith({ title: 'Fresh', markdown: 'Body', ttlDays: 30, color: 'blue', files: [] });
      expect(lastToast()).toMatchObject({ type: 'success', message: 'Doc posted' });
      expect(router.currentRoute.value.path).toBe('/pano/new1');
    });

    it('stays on a note that failed to post', async () => {
      api.createPanoDoc.mockRejectedValueOnce(new Error('cannot post'));
      const { el, router } = await mount('/pano/new');
      await type($(el, 'pano-title-input'), 'Fresh');
      await type($(el, 'pano-markdown-input'), 'Body');
      await key($(el, 'pano-markdown-input'), { key: 's', metaKey: true });
      expect(lastToast()).toMatchObject({ type: 'error', message: 'cannot post' });
      expect(router.currentRoute.value.path).toBe('/pano/new');
      expect($(el, 'pano-editor')).not.toBeNull();
    });

    it('goes back to the board on cancel or Esc', async () => {
      const { el, router } = await mount('/pano/new');
      await click($(el, 'pano-cancel'));
      expect(router.currentRoute.value.path).toBe('/pano');
      const again = await mount('/pano/new');
      await key($(again.el, 'pano-title-input'), { key: 'Escape' });
      expect(again.router.currentRoute.value.path).toBe('/pano');
    });
  });
});
