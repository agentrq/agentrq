export const API_BASE_URL = '/api/v1';

export async function fetchWorkspaces(includeArchived = false) {
  const url = includeArchived ? `${API_BASE_URL}/workspaces?archived=true` : `${API_BASE_URL}/workspaces`;
  const res = await fetch(url);
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
      const res = await fetch(`${API_BASE_URL}/auth/user`);
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


export async function createWorkspace(name, description, icon = '') {
  const res = await fetch(`${API_BASE_URL}/workspaces`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace: { name, description, icon } })
  });
  if (!res.ok) throw new Error('Failed to create workspace');
  return res.json();
}

export async function getWorkspace(id) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}`);
  if (!res.ok) throw new Error('Failed to fetch workspace');
  return res.json();
}

export async function deleteWorkspace(id) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete workspace');
  return true;
}

export async function fetchTasks(workspaceId) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks`);
  if (!res.ok) throw new Error('Failed to fetch tasks');
  return res.json();
}

export async function getTask(workspaceId, taskId) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}`);
  if (!res.ok) throw new Error('Failed to fetch task');
  return res.json();
}

export async function createTask(workspaceId, title, body, assignee = 'agent', attachments = [], status = 'notstarted', cronSchedule = '') {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ 
      task: { 
        title, 
        body, 
        created_by: 'human', 
        assignee, 
        attachments, 
        status, 
        cron_schedule: cronSchedule 
      } 
    })
  });
  if (!res.ok) throw new Error('Failed to create task');
  return res.json();
}

export async function respondToTask(workspaceId, taskId, action, text = '', attachments = []) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/respond`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ response: { action, text, attachments } })
  });
  if (!res.ok) throw new Error('Failed to respond to task');
  return res.json();
}

export async function updateTaskStatus(workspaceId, taskId, value) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/status`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ status: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task status');
  return res.json();
}

export async function updateTaskOrder(workspaceId, taskId, value) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/order`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ order: { value } })
  });
  if (!res.ok) throw new Error('Failed to update task order');
  return res.json();
}

export async function deleteTask(workspaceId, taskId) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}`, {
    method: 'DELETE'
  });
  if (!res.ok) throw new Error('Failed to delete task');
  return true;
}

export async function archiveWorkspace(id) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}/archive`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to archive workspace');
  return true;
}

export function getAttachmentUrl(workspaceId, attachmentId) {
  return `${API_BASE_URL}/workspaces/${workspaceId}/attachments/${attachmentId}`;
}

export async function getWorkspaceToken(id) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}/token`);
  if (!res.ok) throw new Error('Failed to fetch workspace token');
  return res.json();
}

export async function unarchiveWorkspace(id) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}/unarchive`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to unarchive workspace');
  return true;
}

export async function updateWorkspace(id, workspace) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workspace })
  });
  if (!res.ok) throw new Error('Failed to update workspace');
  return res.json();
}

export async function replyToTask(workspaceId, taskId, text, attachments = []) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/reply`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ reply: { text, attachments } })
  });
  if (!res.ok) throw new Error('Failed to send reply');
  return res.json();
}

export async function sendPermissionVerdict(workspaceId, taskId, requestId, behavior) {
  const res = await fetch(`${API_BASE_URL}/workspaces/${workspaceId}/tasks/${taskId}/permission`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ request_id: requestId, behavior })
  });
  if (!res.ok) throw new Error('Failed to send verdict');
  return res;
}
