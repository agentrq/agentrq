// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The same page, showing another task: both task routes end in the task's ID.
 *
 * @param {string} path the current route's path
 * @param {string} taskId
 */
export function forkedTaskPath(path, taskId) {
  return path.replace(/[^/]+\/?$/, taskId);
}

/**
 * What to tell the person once a fork exists. The server makes it ongoing only
 * when the agent took it on at once, so a notstarted fork is waiting its turn.
 *
 * @param {{ status: string }} task the fork
 */
export function forkNotice(task) {
  return task?.status === 'ongoing'
    ? { title: 'Forked', message: 'The agent has the new task and is starting on it.' }
    : { title: 'Forked', message: 'Queued: the agent picks the new task up when it is free.' };
}
