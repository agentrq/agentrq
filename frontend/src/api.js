// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

const cleanBase = (window.__AGENTRQ_BASE_PATH__ || '').replace(/\/$/, '');
export const API_BASE_URL = `${cleanBase}/api/v1`;

import { createAuthedFetch } from './composables/useAuthedFetch';

/**
 * Every call in this module goes through here rather than through fetch.
 *
 * An expired access token comes back as a 401, which this renews once and
 * replays — so a session that is still valid does not drop the user at the
 * login screen just because the short-lived half of it aged out.
 */
const apiFetch = createAuthedFetch({
  fetchImpl: (input, init) => fetch(input, init),
  refreshUrl: `${API_BASE_URL}/auth/refresh`,
});

export async function fetchWorkspaces(includeArchived = false) {
  const url = includeArchived ? `${API_BASE_URL}/workspaces?archived=true` : `${API_BASE_URL}/workspaces`;
  const res = await apiFetch(url);
  if (!res.ok) {
    throw new Error('Failed to fetch workspaces');
  }
  return res.json();
}

let _userCache = null;
let _userFetchPromise = null;

export async function fetchUser() {
  if (_userCache) return _userCache;
  if (_userFetchPromise) return _userFetchPromise;

  _userFetchPromise = (async () => {
    try {
      const res = await apiFetch(`${API_BASE_URL}/auth/user`);
      if (!res.ok) {
        if (res.status === 401) return null;
        throw new Error('Failed to fetch user');
      }
      _userCache = await res.json();
      return _userCache;
    } finally {
      _userFetchPromise = null;
    }
  })();

  return _userFetchPromise;
}


export async function createWorkspace(name, description, icon = '', selfLearningLoopNote = '', workingDirectory = '') {
  const res = await apiFetch(`${API_BASE_URL}/workspaces`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace: { name, description, icon, selfLearningLoopNote, workingDirectory } })
  });
  if (!res.ok) throw new Error('Failed to create workspace');
  return res.json();
}

export async function getWorkspace(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}`);
  if (!res.ok) throw new Error('Failed to fetch workspace');
  return res.json();
}

export async function deleteWorkspace(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete workspace');
  return true;
}

export async function fetchTasks(workspaceId, { status, filter, limit = 10, offset = 0 } = {}) {
  const params = new URLSearchParams();
  if (status) params.append('status', status);
  if (filter) params.append('filter', filter);
  if (limit) params.append('limit', limit);
  if (offset) params.append('offset', offset);

  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks?${params.toString()}`);
  if (!res.ok) throw new Error('Failed to fetch tasks');
  return res.json();
}

export async function fetchGlobalTasks({ status, filter, limit = 10, offset = 0 } = {}) {
  const params = new URLSearchParams();
  if (status) params.append('status', status);
  if (filter) params.append('filter', filter);
  if (limit) params.append('limit', limit);
  if (offset) params.append('offset', offset);
  
  const res = await apiFetch(`${API_BASE_URL}/tasks?${params.toString()}`);
  if (!res.ok) throw new Error('Failed to fetch global tasks');
  return res.json();
}

export async function getTask(workspaceId, taskId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}`);
  if (!res.ok) throw new Error('Failed to fetch task');
  return res.json();
}

// The trailing eventId/workflowId are mutually exclusive: they are the two
// forms of "what fires when this completes", and the form only lets one be set.
export async function createTask(workspaceId, title, body, assignee = 'agent', attachments = [], status = 'notstarted', cronSchedule = '', allowAllCommands = false, eventId = '', workflowId = '') {
  const task = { title, body, createdBy: 'human', assignee, attachments, status, cronSchedule, allowAllCommands };
  if (eventId) task.eventId = eventId;
  if (workflowId) task.workflowId = workflowId;
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task })
  });
  if (!res.ok) throw new Error('Failed to create task');
  return res.json();
}

export async function respondToTask(workspaceId, taskId, action, text = '', attachments = []) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/respond`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ response: { action, text, attachments } })
  });
  if (!res.ok) throw new Error('Failed to respond to task');
  return res.json();
}

export async function updateTaskStatus(workspaceId, taskId, value) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/status`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ status: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task status');
  return res.json();
}

export async function updateTaskOrder(workspaceId, taskId, value) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/order`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ order: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task order');
  return res.json();
}

export async function updateTaskAssignee(workspaceId, taskId, value) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/assignee`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ assignee: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task assignee');
  return res.json();
}

export async function moveTask(workspaceId, taskId, destinationWorkspaceId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/workspace`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace: { value: destinationWorkspaceId } })
  });
  if (!res.ok) throw new Error('Failed to move task');
  return res.json();
}

export async function updateTaskAllowAllCommands(workspaceId, taskId, value) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/allow_all`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ allowAll: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task allow all commands flag');
  return res.json();
}

export async function deleteTask(workspaceId, taskId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}`, {
    method: 'DELETE'
  });
  if (!res.ok) throw new Error('Failed to delete task');
  return true;
}

export async function archiveWorkspace(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/archive`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to archive workspace');
  return true;
}

// ── Machines ─────────────────────────────────────────────────────────────────
//
// A machine is a computer running agentrqd, enrolled against this account.
// Machines belong to the account rather than to a workspace, which is why none
// of these take a workspace id.

export async function fetchMachines() {
  const res = await apiFetch(`${API_BASE_URL}/machines`);
  if (!res.ok) throw new Error('Failed to fetch machines');
  return res.json();
}

export async function getMachine(id) {
  const res = await apiFetch(`${API_BASE_URL}/machines/${id}`);
  if (!res.ok) throw new Error('Failed to fetch machine');
  return res.json();
}

/**
 * Rename a machine, or turn it off.
 *
 * Only the fields passed are changed, which is why the caller sends one key
 * rather than a whole machine: sending everything would mean a rename could
 * silently re-enable a machine somebody had deliberately turned off.
 */
export async function updateMachine(id, changes) {
  const res = await apiFetch(`${API_BASE_URL}/machines/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(changes)
  });
  if (!res.ok) throw new Error('Failed to update machine');
  return res.json();
}

export async function deleteMachine(id) {
  const res = await apiFetch(`${API_BASE_URL}/machines/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete machine');
  return true;
}

/**
 * Mint a short code to type on the machine being enrolled.
 *
 * Returned once and stored only as a hash, so it cannot be shown again — the
 * caller has to keep it or ask for another.
 */
export async function createEnrolmentCode() {
  const res = await apiFetch(`${API_BASE_URL}/machines/codes`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({})
  });
  if (!res.ok) throw new Error('Failed to create an enrolment code');
  return res.json();
}

/**
 * Approve a machine's agentrqd update.
 *
 * The one request in the product that deliberately destroys work in progress:
 * the daemon stops every session on that machine, replaces itself, and starts
 * them again as new processes with empty terminals. The version is required so
 * that "yes" means yes to a particular release rather than to whatever the
 * release feed offers by the time the daemon looks.
 *
 * Answers 202: the daemon has been asked, and its own next connection is what
 * says whether it came back.
 */
export async function approveMachineUpdate(machineId, version) {
  const res = await apiFetch(`${API_BASE_URL}/machines/${machineId}/update`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ version })
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error?.message || body?.error || 'Failed to approve the update');
  }
  return true;
}

export async function fetchMachineSessions(machineId) {
  const res = await apiFetch(`${API_BASE_URL}/machines/${machineId}/sessions`);
  if (!res.ok) throw new Error('Failed to fetch sessions');
  return res.json();
}

/**
 * Start an agent for a workspace on a chosen machine.
 *
 * Answers 202: the daemon has been asked, and the session's own state report —
 * which arrives over the event stream — says whether it started. Callers must
 * not treat this resolving as the agent running.
 */
export async function launchAgent(workspaceId, { machineId, kind, model = '', agent = '', cols = 0, rows = 0 }) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/agent`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ machineId, kind, model, agent, cols, rows })
  });
  if (!res.ok) {
    // The server says why it refused — the workspace already has an agent, the
    // folder is not set, the machine is not connected — and that reason is
    // worth more than "failed".
    const body = await res.json().catch(() => null);
    throw new Error(body?.error?.message || body?.error || 'Failed to launch the agent');
  }
  return res.json();
}

/**
 * Ask the daemon to end a session.
 *
 * Answers 202 for the same reason as launching: the daemon reports what
 * actually happened, and a UI that marked the session dead on its own would be
 * a kill switch that lies about having worked.
 */
export async function killSession(sessionId) {
  const res = await apiFetch(`${API_BASE_URL}/sessions/${sessionId}`, { method: 'DELETE' });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error?.message || body?.error || 'Failed to stop the session');
  }
  return true;
}

/**
 * Where the server actually is.
 *
 * Almost nothing needs this — the API is addressed with same-origin relative
 * URLs, which is what makes the desktop build work at all. The two callers
 * that do need it need it for the same reason: they are producing something
 * that has to reach the server from *outside* the renderer. The desktop app's
 * page is served from `app://`, so its own origin is not an address anything
 * else can use, and the shell is asked instead.
 *
 * Async because asking the shell is IPC.
 */
export async function serverOrigin() {
  if (window.agentrq?.connection?.get) {
    const { serverUrl } = (await window.agentrq.connection.get()) || {};
    if (serverUrl) return new URL(serverUrl).origin;
  }
  return window.location.origin;
}

/**
 * The WebSocket a terminal session is watched over.
 *
 * This is one of the two **absolute** URLs in the frontend, and a deliberate
 * exception to the rule in AGENTS.md rather than an oversight: Electron's
 * custom-protocol handler — which forwards every other API call to the
 * configured server — does not intercept WebSockets, so a relative URL would
 * resolve to `app://` and simply fail to open.
 */
export async function terminalSocketUrl(sessionId) {
  const url = new URL(`${API_BASE_URL}/sessions/${sessionId}/terminal`, await serverOrigin());
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export function getAttachmentUrl(workspaceId, taskId, attachmentId) {
  return `${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/attachments/${attachmentId}`;
}

export async function getWorkspaceToken(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/token`);
  if (!res.ok) throw new Error('Failed to fetch workspace token');
  return res.json();
}

export async function unarchiveWorkspace(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/unarchive`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to unarchive workspace');
  return true;
}

export async function updateWorkspace(id, workspace) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace })
  });
  if (!res.ok) throw new Error('Failed to update workspace');
  return res.json();
}

export async function replyToTask(workspaceId, taskId, text, attachments = []) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/reply`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ reply: { text, attachments } })
  });
  if (!res.ok) throw new Error('Failed to send reply');
  return res.json();
}

export async function sendPermissionVerdict(workspaceId, taskId, requestId, behavior) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/permission`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requestId, behavior })
  });
  if (!res.ok) throw new Error('Failed to send verdict');
  return res;
}

/**
 * Ask the workspace's connected agent to switch to a different model.
 *
 * Answers 202 rather than 200: the agent has been asked, and only its own next
 * models notification — which arrives over the event stream — says whether it
 * switched. Callers must not treat this resolving as the model having changed.
 */
export async function setAgentModel(workspaceId, modelId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/agent/model`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ modelId })
  });
  if (!res.ok) {
    // The server says why it refused — most usefully that the connected agent
    // cannot be told to switch, or that it never offered this model — and that
    // reason is worth more than "failed".
    const body = await res.json().catch(() => null);
    throw new Error(body?.error || 'Failed to change the model');
  }
  return res.json();
}

/**
 * Ask the workspace's connected gateway to run a different number of tasks at
 * once.
 *
 * Answers 202 rather than 200, and for a stronger reason than the model
 * switch: the gateway answers every set with a fresh concurrency report —
 * accepted, clamped to its own range, or ignored because the value was not a
 * number — and that report, which arrives over the event stream, is the only
 * thing that knows which of the three happened. Callers must not treat this
 * resolving as the limit having changed.
 */
export async function setAgentConcurrency(workspaceId, maxConcurrency) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/agent/concurrency`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ maxConcurrency })
  });
  if (!res.ok) {
    // The server says why it refused — most usefully that nothing connected
    // can be told to change its concurrency — and that reason is worth more
    // than "failed".
    const body = await res.json().catch(() => null);
    throw new Error(body?.error || 'Failed to change the concurrency');
  }
  return res.json();
}

export async function stopTask(workspaceId, taskId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/stop`, {
    method: 'POST'
  });
  if (!res.ok) {
    // The server says why when it refuses — most usefully that whatever is
    // connected has no stop — and that reason is worth more than "failed".
    const body = await res.json().catch(() => null);
    throw new Error(body?.error || 'Failed to stop the task');
  }
  return res.json();
}

export async function respondToElicitation(workspaceId, taskId, requestId, action, content) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/elicitation`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requestId, action, content })
  });
  if (!res.ok) throw new Error('Failed to send response');
  return res;
}
export async function updateScheduledTask(workspaceId, taskId, title, body, assignee, cronSchedule, allowAllCommands) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/scheduled`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      task: {
        title,
        body,
        assignee,
        cronSchedule,
        allowAllCommands
      }
    })
  });
  if (!res.ok) throw new Error('Failed to update scheduled task');
  return res.json();
}

export async function fetchTaskCounts(workspaceId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/counts`);
  if (!res.ok) throw new Error('Failed to fetch task counts');
  return res.json();
}

export async function fetchWorkspaceStats(id, range = '7d', from = 0, to = 0) {
  const params = new URLSearchParams();
  params.append('range', range);
  if (from) params.append('from', from);
  if (to) params.append('to', to);
  
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/stats?${params.toString()}`);
  if (!res.ok) throw new Error('Failed to fetch workspace stats');
  return res.json();
}

// The same statistics as fetchWorkspaceStats, summed across every workspace the
// signed-in user owns, plus a per-workspace breakdown. There is no id to pass:
// the scope is the session.
export async function fetchUserStats(range = '7d', from = 0, to = 0) {
  const params = new URLSearchParams();
  params.append('range', range);
  if (from) params.append('from', from);
  if (to) params.append('to', to);

  const res = await apiFetch(`${API_BASE_URL}/stats?${params.toString()}`);
  if (!res.ok) throw new Error('Failed to fetch account stats');
  return res.json();
}

export async function setWorkspaceSlackChannel(id, channelId, channelName) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/slack`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ channelId, channelName })
  });
  if (!res.ok) throw new Error('Failed to set workspace Slack channel');
  return res.json();
}

export async function removeWorkspaceSlackChannel(id) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${id}/slack`, {
    method: 'DELETE'
  });
  if (!res.ok) throw new Error('Failed to remove workspace Slack channel');
  return true;
}

// A workspace's memories, as the settings screen lists them. The content is
// deliberately absent here — each memory is up to 16 KiB, and the list only
// needs names and sizes.
export async function fetchWorkspaceMemories(workspaceId) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/memories`);
  if (!res.ok) throw new Error('Failed to fetch workspace memories');
  return res.json();
}

export async function getWorkspaceMemory(workspaceId, name) {
  const res = await apiFetch(`${API_BASE_URL}/workspaces/${workspaceId}/memories/${encodeURIComponent(name)}`);
  if (!res.ok) throw new Error('Failed to fetch memory');
  return res.json();
}

export async function fetchGlobalTaskStats() {
  const res = await apiFetch(`${API_BASE_URL}/tasks/stats`);
  if (!res.ok) throw new Error('Failed to fetch global task stats');
  return res.json();
}

export async function fetchEvents() {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 10000);
  try {
    const res = await apiFetch(`${API_BASE_URL}/events`, { signal: controller.signal });
    if (!res.ok) throw new Error('Failed to fetch events');
    return res.json();
  } finally {
    clearTimeout(timer);
  }
}

export async function createEvent(name, payloadGuidelines) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 10000);
  try {
    const res = await apiFetch(`${API_BASE_URL}/events`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, payloadGuidelines }),
      signal: controller.signal
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error?.message || body.error || `Failed to create event (${res.status})`);
    }
    return res.json();
  } finally {
    clearTimeout(timer);
  }
}

export async function getEvent(id) {
  const res = await apiFetch(`${API_BASE_URL}/events/${id}`);
  if (!res.ok) throw new Error('Failed to fetch event');
  return res.json();
}

export async function updateEvent(id, payloadGuidelines) {
  const res = await apiFetch(`${API_BASE_URL}/events/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ payloadGuidelines }),
  });
  if (!res.ok) throw new Error('Failed to update event');
  return res.json();
}

export async function deleteEvent(id) {
  const res = await apiFetch(`${API_BASE_URL}/events/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete event');
  return true;
}

export async function fetchEventTriggers(eventId) {
  const res = await apiFetch(`${API_BASE_URL}/events/${eventId}/triggers`);
  if (!res.ok) throw new Error('Failed to fetch event triggers');
  return res.json();
}

export async function createEventTrigger(eventId, { workspaceId, title, body, assignee = 'agent', cronSchedule = '', allowAllCommands = false, emitEventId = '' }) {
  const payload = { workspaceId, title, body, assignee, cronSchedule, allowAllCommands };
  if (emitEventId) payload.emitEventId = emitEventId;
  const res = await apiFetch(`${API_BASE_URL}/events/${eventId}/triggers`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error('Failed to create event trigger');
  return res.json();
}

export async function updateEventTrigger(eventId, triggerId, { workspaceId, title, body, assignee = 'agent', cronSchedule = '', allowAllCommands = false, emitEventId = '' }) {
  const payload = { workspaceId, title, body, assignee, cronSchedule, allowAllCommands };
  if (emitEventId) payload.emitEventId = emitEventId;
  const res = await apiFetch(`${API_BASE_URL}/events/${eventId}/triggers/${triggerId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error('Failed to update event trigger');
  return res.json();
}

export async function deleteEventTrigger(eventId, triggerId) {
  const res = await apiFetch(`${API_BASE_URL}/events/${eventId}/triggers/${triggerId}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete event trigger');
  return true;
}

export async function fetchEventTasks(eventId) {
  const res = await apiFetch(`${API_BASE_URL}/events/${eventId}/tasks`);
  if (!res.ok) throw new Error('Failed to fetch event tasks');
  return res.json();
}

// ── Workflows (experimental) ─────────────────────────────────────────────────

export async function fetchWorkflows() {
  const res = await apiFetch(`${API_BASE_URL}/workflows`);
  if (!res.ok) throw new Error('Failed to fetch workflows');
  return res.json();
}

export async function getWorkflow(id) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${id}`);
  if (!res.ok) throw new Error('Failed to fetch workflow');
  return res.json();
}

export async function createWorkflow({ name, description = '', startEventId = '' }) {
  const payload = { name, description };
  if (startEventId) payload.startEventId = startEventId;
  const res = await apiFetch(`${API_BASE_URL}/workflows`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error?.message || body.error || `Failed to create workflow (${res.status})`);
  }
  return res.json();
}

// Fields are omitted rather than sent empty so a layout save cannot blank the
// name; the backend treats an absent field as unchanged.
export async function updateWorkflow(id, { name, description, startEventId, layout } = {}) {
  const payload = {};
  if (name !== undefined) payload.name = name;
  if (description !== undefined) payload.description = description;
  if (startEventId !== undefined) payload.startEventId = startEventId;
  if (layout !== undefined) payload.layout = layout;
  const res = await apiFetch(`${API_BASE_URL}/workflows/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (!res.ok) throw new Error('Failed to update workflow');
  return res.json();
}

export async function deleteWorkflow(id) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete workflow');
  return true;
}

export async function fetchWorkflowSteps(workflowId) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/steps`);
  if (!res.ok) throw new Error('Failed to fetch workflow steps');
  return res.json();
}

export async function createWorkflowStep(workflowId, { eventId, workspaceId, emitEventId = '', title, body = '', assignee = 'agent', allowAllCommands = false }) {
  const payload = { eventId, workspaceId, title, body, assignee, allowAllCommands };
  if (emitEventId) payload.emitEventId = emitEventId;
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/steps`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (!res.ok) {
    // 409 is a rejected cycle: surface the backend's reason so the editor can
    // explain which connection was refused, not just that something failed.
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error?.message || body.error || `Failed to create workflow step (${res.status})`);
  }
  return res.json();
}

export async function deleteWorkflowStep(workflowId, stepId) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/steps/${stepId}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete workflow step');
  return true;
}

export async function fetchWorkflowTasks(workflowId) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/tasks`);
  if (!res.ok) throw new Error('Failed to fetch workflow tasks');
  return res.json();
}

export async function fetchWorkflowText(workflowId) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/text`);
  if (!res.ok) throw new Error('Failed to fetch workflow text');
  return res.json();
}

// Rejects with a `.line` property when the backend reports a parse error, so
// the editor can mark the offending row.
export async function replaceWorkflowFromText(workflowId, text) {
  const res = await apiFetch(`${API_BASE_URL}/workflows/${workflowId}/text`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text })
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const err = new Error(body.error?.message || body.error || `Failed to save workflow (${res.status})`);
    if (body.error?.line) err.line = body.error.line;
    throw err;
  }
  return res.json();
}

// Telemetry action names the backend accepts. Anything else is rejected there,
// so these strings must match its allowlist exactly.
export const TELEMETRY_LOCAL_AI_TITLE_GENERATE = 'local_ai_title_generate';
export const TELEMETRY_LOCAL_AI_RECORDING_END = 'local_ai_recording_end';

// Interface usage. Reported for the same reason as the local-AI actions: a
// keypress, a search and a copy all begin and end in the tab, so nothing
// server-side would otherwise know they happened. See `useUiTelemetry`, which
// is how these are sent.
export const TELEMETRY_UI_SHORTCUT_USE = 'ui_shortcut_use';
export const TELEMETRY_UI_SEARCH = 'ui_search';
export const TELEMETRY_UI_SEARCH_OPEN = 'ui_search_open';
export const TELEMETRY_UI_COPY_LINK = 'ui_copy_link';
export const TELEMETRY_UI_COPY_MARKDOWN = 'ui_copy_markdown';
export const TELEMETRY_UI_TRAJECTORY_VIEW = 'ui_trajectory_view';

// Records one local-AI feature use. Never throws and never blocks the caller:
// a metric is not worth failing a user's click over, so a rejected or
// unreachable report is dropped rather than surfaced.
export function recordTelemetry(action, workspaceId) {
  if (!action || !workspaceId) return;
  fetch(`${API_BASE_URL}/telemetry`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action, workspaceId }),
    keepalive: true
  }).catch(() => {});
}
